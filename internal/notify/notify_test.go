package notify

import (
	"errors"
	"fmt"
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
	sent   []string // "to|subject"
	body   []string
	fail   bool
	failTo map[string]bool // addresses whose copy fails
}

func (o *outbox) Send(to, subject, body string) error {
	if o.fail || o.failTo[to] {
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
	problem := "[SelfPost] example.com: DKIM does not match"
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

// The certificate Postfix serves nears its end while a renewal waits in the
// mount: nothing is said while Postfix's check for it is still to come, and
// the usual notice once the renewal has waited past it (A-65).
func TestCertificateWithARenewalWaiting(t *testing.T) {
	b := newBench(t)
	expiry := b.now.Add(10 * 24 * time.Hour)
	b.cert = health.Certificate{NotAfter: expiry, Renewal: &health.Renewal{NotAfter: expiry.Add(80 * 24 * time.Hour)}}
	want(t, "renewal waiting", b.hour())
	b.cert.Renewal.Late = true
	want(t, "renewal late", b.hour(), "root@example.org|[SelfPost] TLS certificate of mail.example.org expires in 10 days")
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

// A limit that refuses mail is said to the domain's users, at most once a day
// for the domain, whichever of its applications are refused.
func TestLimitRefusalIsSaidOnceADay(t *testing.T) {
	b := newBench(t)
	b.now = time.Now().UTC() // refusals are dated by the store's clock
	domains, _ := b.st.ListDomains()
	var domainID int64
	for _, d := range domains {
		if d.Name == "example.com" {
			domainID = d.ID
		}
	}
	for _, login := range []string{"app1", "app2"} {
		a, err := b.st.AddApplication(domainID, login, store.AddressModeWildcard, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := b.st.SetRateLimit(store.RateLimit{Scope: store.RateLimitScopeApp, RefID: a.ID, MaxMessages: 1, WindowSeconds: 3600}); err != nil {
			t.Fatal(err)
		}
	}
	refuse := func(login, mailFromDomain string) {
		t.Helper()
		if err := b.st.InsertRejected(store.SendLogEntry{Domain: mailFromDomain, AppLogin: login, From: "a@" + mailFromDomain}); err != nil {
			t.Fatal(err)
		}
	}

	b.n.CheckLimits()
	want(t, "nothing refused", b.out.take())

	refuse("app1", "example.com")
	b.n.CheckLimits()
	subject := "[SelfPost] example.com: a rate limit is refusing mail"
	want(t, "first refusal", b.out.take(), "mine@example.org|"+subject, "root@example.org|"+subject)

	// The second application of the domain is refused, and the first goes on
	// under domains it makes up in MAIL FROM - another tenant's among them.
	// It is all the same subject: example.com, told today.
	refuse("app2", "example.com")
	refuse("app1", "other.example")
	refuse("app1", "made-up.example")
	b.now = b.now.Add(2 * time.Hour)
	b.n.CheckLimits()
	want(t, "more of it two hours later", b.out.take())
	if keys, _ := b.st.ListNotifyStateKeys("limit:"); strings.Join(keys, " ") != "limit:example.com" {
		t.Errorf("limit subjects = %v, want the one registered domain", keys)
	}

	// A day after it was said, mail is still being refused: said again. The
	// refusals are dated by the store's clock, so it is the telling that is
	// moved back, not the clock forward.
	st, have, err := b.st.GetNotifyState("limit:example.com")
	if err != nil || !have {
		t.Fatalf("no state for the limit: %v, %v", have, err)
	}
	b.now = time.Now().UTC()
	st.SentAt = b.now.Add(-25 * time.Hour)
	if err := b.st.SetNotifyState(st); err != nil {
		t.Fatal(err)
	}
	b.n.CheckLimits()
	if len(b.out.sent) != 2 || !strings.Contains(b.out.body[0], "4 messages from the applications app1, app2 of example.com") {
		t.Fatalf("a day later, still refusing: %v\n%s", b.out.sent, strings.Join(b.out.body, "\n---\n"))
	}
	b.out.take()

	// Nobody was refused for two days: it is not a subject any more.
	st.SentAt = b.now.Add(-49 * time.Hour)
	if err := b.st.SetNotifyState(st); err != nil {
		t.Fatal(err)
	}
	b.now = b.now.Add(25 * time.Hour) // past the window the refusals lie in
	b.n.CheckLimits()
	want(t, "long quiet", b.out.take())
	if _, have, _ := b.st.GetNotifyState("limit:example.com"); have {
		t.Error("a limit nobody hits kept its state")
	}
}

// Two checks minutes apart - the last before a restart and the first after it
// - are not "an hour apart": the mismatch is said when a check finds it again
// with the time passed.
func TestDNSChecksMustBeApart(t *testing.T) {
	b := newBench(t)
	b.dns["example.com"] = badDKIM()

	b.n.Check()
	b.now = b.now.Add(3 * time.Minute) // the process restarted
	b.n.Check()
	want(t, "two checks three minutes apart", b.out.take())
	b.now = b.now.Add(time.Hour)
	b.n.Check()
	if got := b.out.take(); len(got) != 2 {
		t.Fatalf("an hour later: %v, want the two messages", got)
	}

	// The same for its going: two good checks minutes apart do not end it.
	b.dns["example.com"] = good()
	b.now = b.now.Add(time.Hour)
	b.n.Check()
	b.now = b.now.Add(3 * time.Minute)
	b.n.Check()
	want(t, "two good checks three minutes apart", b.out.take())
	b.now = b.now.Add(time.Hour)
	b.n.Check()
	if got := b.out.take(); len(got) != 2 || !strings.Contains(got[0], "match again") {
		t.Errorf("an hour later: %v, want the two messages that it is over", got)
	}
}

// What arises while there is nobody to tell is told when there is somebody:
// a thing is said only when it was sent. Its going, with nobody left to tell,
// is simply over.
func TestNobodyToTellIsNotSaid(t *testing.T) {
	b := newBench(t)
	users, _ := b.st.ListUsers()
	for _, u := range users {
		if err := b.st.SetUserNotify(u.ID, false); err != nil {
			t.Fatal(err)
		}
	}
	expiry := b.now.Add(10 * 24 * time.Hour)
	b.cert = health.Certificate{NotAfter: expiry}
	b.dns["example.com"] = badDKIM()
	b.q = []postfix.QueuedMessage{{ID: "OLD00001", Sender: "app@example.com", Arrival: b.now.Add(-5 * 24 * time.Hour)}}

	want(t, "nobody listens", b.hour())
	want(t, "nobody listens", b.hour())

	for _, u := range users {
		if u.Username == "root" {
			if err := b.st.SetUserNotify(u.ID, true); err != nil {
				t.Fatal(err)
			}
		}
	}
	got := b.hour()
	if len(got) != 3 {
		t.Fatalf("somebody listens now: %v, want the DNS mismatch, the certificate and the queue", got)
	}

	// Everything is put right while nobody listens again: no state is left to
	// announce an ending later.
	for _, u := range users {
		if err := b.st.SetUserNotify(u.ID, false); err != nil {
			t.Fatal(err)
		}
	}
	b.dns["example.com"] = good()
	b.q = nil
	want(t, "put right", b.hour())
	want(t, "put right", b.hour())
	for _, key := range []string{"dns:example.com", "queue"} {
		if _, have, _ := b.st.GetNotifyState(key); have {
			t.Errorf("state %s left behind", key)
		}
	}
}

// The notifier's own mail in the queue - a notification to a mailbox that does
// not answer, or its bounce coming back to this server's postmaster - is not
// the mail the queue is watched for.
func TestQueueLeavesOutTheNotifiersOwnMail(t *testing.T) {
	b := newBench(t)
	old := b.now.Add(-4 * 24 * time.Hour)
	b.q = []postfix.QueuedMessage{
		{ID: "NOTE0001", Sender: "postmaster@mail.example.org", Recipients: []postfix.QueuedRecipient{{Address: "gone@example.net"}}, Arrival: old},
		{ID: "BOUNCE01", Sender: "", Recipients: []postfix.QueuedRecipient{{Address: "Postmaster@Mail.Example.Org"}}, Arrival: old},
	}
	want(t, "only its own mail is old", b.hour())

	b.q = append(b.q, postfix.QueuedMessage{ID: "APP00001", Sender: "", Recipients: []postfix.QueuedRecipient{{Address: "app@example.com"}}, Arrival: old})
	if got := b.hour(); len(got) != 1 {
		t.Errorf("an application's bounce is old too: %v, want one message", got)
	}
}

// Whatever the rules arrive at, no more than the ceiling leaves in an hour:
// these messages bypass every rate limit.
func TestHourlyCeiling(t *testing.T) {
	b := newBench(t)
	for i := 0; i < hourlyCeiling+5; i++ {
		b.n.open(fmt.Sprintf("test:%d", i), "", message{subject: "s", body: "b"})
	}
	if got := len(b.out.take()); got != hourlyCeiling {
		t.Errorf("%d messages left in one hour, ceiling is %d", got, hourlyCeiling)
	}
	if b.n.open("test:x", "", message{subject: "s", body: "b"}) != failed {
		t.Error("a message over the ceiling was counted as said")
	}
	b.now = b.now.Add(61 * time.Minute)
	if b.n.open("test:y", "", message{subject: "s", body: "b"}) != sent {
		t.Error("the ceiling did not lift after an hour")
	}
}

// The subject of a certificate message counts in days as a person would:
// found an hour past the threshold it still has "14 days".
func TestCertificateSubjectRoundsDays(t *testing.T) {
	for _, c := range []struct {
		left time.Duration
		want string
	}{
		{14*24*time.Hour - time.Hour, "expires in 14 days"},
		{3*24*time.Hour - 20*time.Minute, "expires in 3 days"},
		{30 * time.Hour, "expires in 1 day"},
		{-time.Hour, "has expired"},
	} {
		m := certExpiring("mail.example.org", time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), c.left)
		if !strings.Contains(m.subject, c.want) {
			t.Errorf("%v left: subject %q, want %q", c.left, m.subject, c.want)
		}
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

// A notification is owed to each recipient (owner, 2026-10-14): a copy that
// failed goes out at the next check, once, and the subject's end goes to
// those who were told.
func TestACopyThatFailedGoesOutAtTheNextCheck(t *testing.T) {
	b := newBench(t)
	b.dns["example.com"] = badDKIM()
	problem := "[SelfPost] example.com: DKIM does not match"
	b.out.failTo = map[string]bool{"mine@example.org": true}
	want(t, "first bad check", b.hour())
	want(t, "said, one copy failing", b.hour(), "root@example.org|"+problem)
	b.out.failTo = nil
	want(t, "the next check", b.hour(), "mine@example.org|"+problem)
	want(t, "and not again", b.hour())

	b.dns["example.com"] = good()
	want(t, "first good check", b.hour())
	cleared := "[SelfPost] example.com: DNS records match again"
	want(t, "over", b.hour(), "mine@example.org|"+cleared, "root@example.org|"+cleared)
}

// The hourly ceiling holds a copy back instead of dropping it: it goes out at
// a check after the hour.
func TestTheCeilingHoldsACopyBack(t *testing.T) {
	b := newBench(t)
	b.dns["example.com"] = badDKIM()
	want(t, "first bad check", b.hour())
	// All but one of the hour's messages are spent.
	for i := 0; i < hourlyCeiling-1; i++ {
		b.n.sentAt = append(b.n.sentAt, b.now)
	}
	problem := "[SelfPost] example.com: DKIM does not match"
	got := b.hour()
	if len(got) != 1 {
		t.Fatalf("under the ceiling with room for one: sent %v", got)
	}
	want(t, "the next check, the hour over", b.hour(), map[string]string{
		"mine@example.org|" + problem: "root@example.org|" + problem,
		"root@example.org|" + problem: "mine@example.org|" + problem,
	}[got[0]])
	want(t, "and not again", b.hour())
}

// A user who comes to hear of a subject while it is open is not mailed about
// it, nor about its end: the message was owed to those there when it was
// said.
func TestAUserAddedWhileASubjectIsOpenIsNotMailed(t *testing.T) {
	b := newBench(t)
	b.dns["example.com"] = badDKIM()
	want(t, "first bad check", b.hour())
	problem := "[SelfPost] example.com: DKIM does not match"
	want(t, "said", b.hour(), "mine@example.org|"+problem, "root@example.org|"+problem)

	id, err := b.st.CreateUser("late", "hash", store.RoleGlobal, store.Reach{})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.st.UpdateUser(id, "late", "hash", "late@example.org"); err != nil {
		t.Fatal(err)
	}
	want(t, "still wrong, a user added", b.hour())
	b.dns["example.com"] = good()
	want(t, "first good check", b.hour())
	cleared := "[SelfPost] example.com: DNS records match again"
	want(t, "over", b.hour(), "mine@example.org|"+cleared, "root@example.org|"+cleared)
	if owed, _ := b.st.NotifyAddressees("dns:example.com"); len(owed) != 0 {
		t.Errorf("who the subject was owed to outlived it: %v", owed)
	}
}

// A domain is reported broken on what is wrong, never on what is not known: a
// check whose SPF lookup did not answer beside a wrong DKIM says DKIM only,
// and one with SPF unknown alone says nothing (audit 2026-10-12, M14/M15).
func TestDNSUnknownIsNeverReported(t *testing.T) {
	b := newBench(t)
	unknownSPF := good()
	unknownSPF.SPF = dnscheck.Result{Status: health.StatusUnknown, Detail: "Could not check SPF: the DNS lookup failed (timed out)."}
	b.dns["example.com"] = unknownSPF
	want(t, "SPF unknown", b.hour())
	want(t, "SPF unknown again", b.hour())
	want(t, "SPF unknown a third time", b.hour())

	mixed := badDKIM()
	mixed.SPF = unknownSPF.SPF
	b.dns["example.com"] = mixed
	want(t, "first bad check", b.hour())
	problem := "[SelfPost] example.com: DKIM does not match"
	b.n.Check()
	body := strings.Join(b.out.body, "")
	want(t, "DKIM wrong, SPF unknown", b.out.take(), "mine@example.org|"+problem, "root@example.org|"+problem)
	if body == "" || strings.Contains(body, "SPF") {
		t.Errorf("the message speaks of SPF, which was not known:\n%s", body)
	}
}

// A subject opened before 2.5.4 has no record of who was told: no follow-up
// is sent, and its end goes to everyone who is to hear of it, as it did.
func TestASubjectFromBeforeTheTableEndsForEveryone(t *testing.T) {
	b := newBench(t)
	since := b.now.Add(-3 * time.Hour)
	if err := b.st.SetNotifyState(store.NotifyState{Key: "dns:example.com", Phase: phaseOpen, Since: since, SentAt: since}); err != nil {
		t.Fatal(err)
	}
	b.dns["example.com"] = badDKIM()
	want(t, "still wrong", b.hour())
	b.dns["example.com"] = good()
	want(t, "first good check", b.hour())
	cleared := "[SelfPost] example.com: DNS records match again"
	want(t, "over", b.hour(), "mine@example.org|"+cleared, "root@example.org|"+cleared)
}
