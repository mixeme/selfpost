package notify

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mixeme/selfpost/internal/dnscheck"
	"github.com/mixeme/selfpost/internal/health"
	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
)

// outbox collects what would have been handed to Postfix.
type outbox struct {
	sent []string // "to|subject"
	body []string
	fail bool
}

func (o *outbox) Send(to, subject, body string) error {
	if o.fail {
		return errors.New("sendmail: queue file write error")
	}
	o.sent = append(o.sent, to+"|"+subject)
	o.body = append(o.body, body)
	return nil
}

func (o *outbox) take() []string {
	s := o.sent
	o.sent, o.body = nil, nil
	return s
}

// bench is a notifier over a real database with a clock and readings the test
// sets: one global user, one user of example.com, one user of another domain.
type bench struct {
	n    *Notifier
	st   *store.Store
	out  *outbox
	now  time.Time
	dns  map[string]dnscheck.Domain
	cert health.Certificate
	q    []postfix.QueuedMessage
	qErr error
}

func newBench(t *testing.T) *bench {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	b := &bench{st: st, out: &outbox{}, now: time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC), dns: map[string]dnscheck.Domain{}}
	mine, err := st.AddDomain("example.com", "sp")
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.AddDomain("other.example", "sp")
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range []struct {
		name  string
		role  store.Role
		reach store.Reach
	}{
		{"root", store.RoleGlobal, store.Reach{}},
		{"mine", store.RoleDomain, store.Reach{DomainIDs: []int64{mine.ID}}},
		{"theirs", store.RoleDomain, store.Reach{DomainIDs: []int64{other.ID}}},
	} {
		id, err := st.CreateUser(u.name, "hash", u.role, u.reach)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.UpdateUser(id, u.name, "hash", u.name+"@example.org"); err != nil {
			t.Fatal(err)
		}
	}

	b.n = New(st, Checks{
		Domain: func(d store.Domain) (dnscheck.Domain, error) {
			res, ok := b.dns[d.Name]
			if !ok {
				return good(), nil
			}
			return res, nil
		},
		Certificate: func() health.Certificate { return b.cert },
		Queue:       func() ([]postfix.QueuedMessage, error) { return b.q, b.qErr },
	}, b.out, "mail.example.org")
	b.n.now = func() time.Time { return b.now }
	return b
}

// hour runs the hourly check and moves the clock on by an hour.
func (b *bench) hour() []string {
	b.n.Check()
	b.now = b.now.Add(time.Hour)
	return b.out.take()
}

func good() dnscheck.Domain {
	ok := dnscheck.Result{Status: health.StatusOK}
	return dnscheck.Domain{DKIM: ok, SPF: ok, DMARC: ok}
}

func badDKIM() dnscheck.Domain {
	d := good()
	d.DKIM = dnscheck.Result{Status: health.StatusError, Detail: "A TXT record exists but its public key is not the one this server signs with."}
	return d
}

func want(t *testing.T, what string, got []string, wanted ...string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(wanted, "\n") {
		t.Errorf("%s: sent\n  %s\nwant\n  %s", what, strings.Join(got, "\n  "), strings.Join(wanted, "\n  "))
	}
}

// A DNS mismatch is said once, after two checks in a row found it, to the
// global users and the domain's own; it is said to be over once, after two
// checks in a row found the records right.
func TestDNSMismatchIsSaidOnceAndClearedOnce(t *testing.T) {
	b := newBench(t)
	b.dns["example.com"] = badDKIM()

	want(t, "first bad check", b.hour())
	problem := "[SelfPost] example.com: DKIM no longer matches"
	want(t, "second bad check", b.hour(), "mine@example.org|"+problem, "root@example.org|"+problem)
	want(t, "third bad check", b.hour())

	b.dns["example.com"] = good()
	want(t, "first good check", b.hour())
	// One good check does not end it: wrong again is the same trouble, not a new one.
	b.dns["example.com"] = badDKIM()
	want(t, "wrong again", b.hour())
	b.dns["example.com"] = good()
	want(t, "good once more", b.hour())
	cleared := "[SelfPost] example.com: DNS records match again"
	want(t, "second good check in a row", b.hour(), "mine@example.org|"+cleared, "root@example.org|"+cleared)
	want(t, "after it cleared", b.hour())

	if _, have, _ := b.st.GetNotifyState("dns:example.com"); have {
		t.Error("state left behind after the trouble was over")
	}
}

// One bad check followed by a good one was the resolver, and nothing is said.
// A lookup that did not answer says nothing either way; a warning is advice
// the page gives, not a thing to write about.
func TestDNSNoiseIsNotSaid(t *testing.T) {
	b := newBench(t)

	b.dns["example.com"] = badDKIM()
	want(t, "one bad check", b.hour())
	b.dns["example.com"] = good()
	want(t, "then good", b.hour())
	b.dns["example.com"] = badDKIM()
	want(t, "bad again, counted from one", b.hour())

	unknown := good()
	unknown.SPF = dnscheck.Result{Status: health.StatusUnknown, Detail: "Could not look up the SPF record."}
	b.dns["example.com"] = unknown
	want(t, "resolver did not answer", b.hour())
	if st, have, _ := b.st.GetNotifyState("dns:example.com"); !have || st.Count != 1 {
		t.Errorf("an unanswered lookup moved the state: %+v, %v", st, have)
	}

	warn := good()
	warn.DMARC = dnscheck.Result{Status: health.StatusWarn, Detail: "No DMARC record."}
	b.dns["other.example"] = warn
	want(t, "a warning", b.hour())
	want(t, "a warning, twice", b.hour())
}

// The instance switch stops everything, state included; a domain that is
// deleted takes its state with it; a message that could not be handed over is
// tried again at the next check.
func TestDNSSwitchDeletionAndRetry(t *testing.T) {
	b := newBench(t)
	b.dns["example.com"] = badDKIM()

	if err := b.st.SetNotificationsEnabled(false); err != nil {
		t.Fatal(err)
	}
	want(t, "switched off", b.hour())
	want(t, "switched off", b.hour())
	if _, have, _ := b.st.GetNotifyState("dns:example.com"); have {
		t.Fatal("state moved while notifications were off")
	}
	if err := b.st.SetNotificationsEnabled(true); err != nil {
		t.Fatal(err)
	}

	want(t, "on again, first check", b.hour())
	b.out.fail = true
	want(t, "second check, mail system down", b.hour())
	b.out.fail = false
	if got := b.hour(); len(got) != 2 {
		t.Errorf("after the mail system came back: %v, want the two messages", got)
	}

	domains, _ := b.st.ListDomains()
	for _, d := range domains {
		if d.Name == "example.com" {
			if err := b.st.DeleteDomain(d.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	want(t, "domain deleted", b.hour())
	if _, have, _ := b.st.GetNotifyState("dns:example.com"); have {
		t.Error("a deleted domain kept its state")
	}
}

// The certificate is said at 14 days and again at 3 if it is still the same
// one, to global users only; a new certificate starts over.
func TestCertificateIsSaidTwice(t *testing.T) {
	b := newBench(t)
	expiry := b.now.Add(20 * 24 * time.Hour)
	b.cert = health.Certificate{NotAfter: expiry}
	want(t, "20 days left", b.hour())

	b.now = expiry.Add(-14 * 24 * time.Hour)
	want(t, "14 days left", b.hour(), "root@example.org|[SelfPost] TLS certificate of mail.example.org expires in 14 days")
	want(t, "an hour later", b.hour())
	b.now = expiry.Add(-8 * 24 * time.Hour)
	want(t, "8 days left", b.hour())

	b.now = expiry.Add(-3 * 24 * time.Hour)
	want(t, "3 days left", b.hour(), "root@example.org|[SelfPost] TLS certificate of mail.example.org expires in 3 days")
	b.now = expiry.Add(time.Hour)
	want(t, "expired, already said", b.hour())

	// Renewed: nothing to say, and the next expiry is a new subject.
	renewed := b.now.Add(90 * 24 * time.Hour)
	b.cert = health.Certificate{NotAfter: renewed}
	want(t, "renewed", b.hour())
	b.now = renewed.Add(-2 * 24 * time.Hour)
	want(t, "the new one, first seen with 2 days left", b.hour(), "root@example.org|[SelfPost] TLS certificate of mail.example.org expires in 2 days")

	// No certificate configured: Health says so, mail does not.
	b.cert = health.Certificate{Status: health.StatusUnknown}
	want(t, "no certificate", b.hour())
}

// A message older than three days is said once, and its going once; a queue
// that cannot be listed says nothing.
func TestQueueIsSaidOnceAndClearedOnce(t *testing.T) {
	b := newBench(t)
	b.q = []postfix.QueuedMessage{
		{ID: "FRESH001", Queue: "deferred", Sender: "app@example.com", Arrival: b.now.Add(-2 * 24 * time.Hour)},
	}
	want(t, "two days old", b.hour())

	b.q = append(b.q, postfix.QueuedMessage{ID: "OLD00001", Queue: "deferred", Sender: "app@example.com", Arrival: b.now.Add(-4 * 24 * time.Hour)})
	b.n.Check()
	if len(b.out.sent) != 1 || b.out.sent[0] != "root@example.org|[SelfPost] A message has been in the queue of mail.example.org for more than 3 days" {
		t.Fatalf("four days old: %v", b.out.sent)
	}
	if body := b.out.body[0]; !strings.Contains(body, "OLD00001") || strings.Contains(body, "FRESH001") {
		t.Errorf("the message names the wrong mail:\n%s", body)
	}
	b.out.take()
	want(t, "still there", b.hour())

	b.qErr = errors.New("postqueue: Permission denied")
	b.q = nil
	want(t, "queue cannot be listed", b.hour())
	b.qErr = nil
	want(t, "gone", b.hour(), "root@example.org|[SelfPost] The queue of mail.example.org holds nothing older than 3 days")
	want(t, "after", b.hour())
}

// A limit that refuses mail is said to the domain's users, at most once a day.
func TestLimitRefusalIsSaidOnceADay(t *testing.T) {
	b := newBench(t)
	b.now = time.Now().UTC() // refusals are dated by the store's clock
	refuse := func() {
		t.Helper()
		if err := b.st.InsertRejected(store.SendLogEntry{Domain: "example.com", AppLogin: "app1", From: "a@example.com"}); err != nil {
			t.Fatal(err)
		}
	}

	b.n.CheckLimits()
	want(t, "nothing refused", b.out.take())

	refuse()
	b.n.CheckLimits()
	subject := "[SelfPost] example.com: a rate limit is refusing mail"
	want(t, "first refusal", b.out.take(), "mine@example.org|"+subject, "root@example.org|"+subject)

	refuse()
	b.now = b.now.Add(2 * time.Hour)
	b.n.CheckLimits()
	want(t, "more of it two hours later", b.out.take())

	// A day after it was said, mail is still being refused: said again. The
	// refusals are dated by the store's clock, so it is the telling that is
	// moved back, not the clock forward.
	st, have, err := b.st.GetNotifyState("limit:example.com:app1")
	if err != nil || !have {
		t.Fatalf("no state for the limit: %v, %v", have, err)
	}
	b.now = time.Now().UTC()
	st.SentAt = b.now.Add(-25 * time.Hour)
	if err := b.st.SetNotifyState(st); err != nil {
		t.Fatal(err)
	}
	b.n.CheckLimits()
	want(t, "a day later, still refusing", b.out.take(), "mine@example.org|"+subject, "root@example.org|"+subject)
	if body := limitRefused("mail.example.org", store.LimitRefusal{Domain: "example.com", AppLogin: "app1", Count: 3, First: b.now}).body; !strings.Contains(body, "3 messages from the application app1 of example.com") {
		t.Errorf("the message does not say who was refused how much:\n%s", body)
	}

	// Nobody has hit the limit for two days: it is not a subject any more.
	st.SentAt = b.now.Add(-49 * time.Hour)
	if err := b.st.SetNotifyState(st); err != nil {
		t.Fatal(err)
	}
	if _, err := b.st.LimitRefusalsSince(b.now); err != nil {
		t.Fatal(err)
	}
	b.now = b.now.Add(25 * time.Hour) // past the window the refusals lie in
	b.n.CheckLimits()
	want(t, "long quiet", b.out.take())
	if _, have, _ := b.st.GetNotifyState("limit:example.com:app1"); have {
		t.Error("a limit nobody hits kept its state")
	}
}

func TestMailerComposesAndRefuses(t *testing.T) {
	var from, to string
	var msg []byte
	m := NewMailer("mail.example.org")
	m.run = func(f, t string, b []byte) error { from, to, msg = f, t, b; return nil }

	if err := m.Send("root@example.org", "[SelfPost] example.com:\r\nBcc: x@evil.example", "line one\n.\nline three\n"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if from != "postmaster@mail.example.org" || to != "root@example.org" {
		t.Errorf("envelope %s -> %s", from, to)
	}
	text := string(msg)
	head, body, ok := strings.Cut(text, "\r\n\r\n")
	if !ok {
		t.Fatalf("no header/body break:\n%s", text)
	}
	for _, h := range []string{
		"From: SelfPost <postmaster@mail.example.org>",
		"To: <root@example.org>",
		"Subject: [SelfPost] example.com: Bcc: x@evil.example",
		"Auto-Submitted: auto-generated",
		"Content-Type: text/plain; charset=utf-8",
	} {
		if !strings.Contains(head, h+"\r\n") && !strings.HasSuffix(head, h) {
			t.Errorf("header %q missing from:\n%s", h, head)
		}
	}
	if strings.Count(head, "\r\n")+1 != 9 {
		t.Errorf("a subject broke out of its line; headers:\n%s", head)
	}
	if body != "line one\r\n.\r\nline three\r\n" {
		t.Errorf("body = %q", body)
	}

	for _, bad := range []string{"", "no-at-sign", "-oQ/tmp@example.org", "a@example.org\nBcc: b@example.org", "a b@example.org", "<a@example.org>"} {
		if err := m.Send(bad, "s", "b"); err == nil {
			t.Errorf("address %q was accepted", bad)
		}
	}
}
