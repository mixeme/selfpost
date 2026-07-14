package milter

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/emersion/go-milter"

	"codeberg.org/mix/selfpost/internal/store"
)

// fakeRecorder captures inserts and can be made to fail, to prove the milter
// swallows recorder errors and still accepts the message. By default it reports
// no configured rate limit, so the level-2 check is inert unless a test sets
// limits (see fakeRecorder fields).
type fakeRecorder struct {
	entries  []store.SendLogEntry
	rejected []store.SendLogEntry
	fail     bool

	// limits, keyed by "scope|ref", drive the level-2 rate-limit tests. counts
	// gives the recent-message count returned for a "scope|ref". lookupErr and
	// countErr force the store errors that must fail open.
	limits    map[string]store.RateLimit
	counts    map[string]int64
	lookupErr error
	countErr  error
}

func (f *fakeRecorder) InsertQueued(e store.SendLogEntry) error {
	if f.fail {
		return errors.New("boom")
	}
	f.entries = append(f.entries, e)
	return nil
}

func (f *fakeRecorder) InsertRejected(e store.SendLogEntry) error {
	f.rejected = append(f.rejected, e)
	return nil
}

func (f *fakeRecorder) RateLimit(scope, ref string) (store.RateLimit, bool, error) {
	if f.lookupErr != nil {
		return store.RateLimit{}, false, f.lookupErr
	}
	rl, ok := f.limits[scope+"|"+ref]
	return rl, ok, nil
}

func (f *fakeRecorder) CountMessages(scope, ref string, _ time.Time) (int64, error) {
	if f.countErr != nil {
		return 0, f.countErr
	}
	return f.counts[scope+"|"+ref], nil
}

func mods(kv map[string]string) *milter.Modifier {
	return &milter.Modifier{Macros: kv}
}

// drive replays a typical message through one session and returns the recorder.
func drive(t *testing.T, rec Store) *session {
	t.Helper()
	s := &session{rec: rec}
	if _, err := s.Connect("localhost", "tcp4", 0, net.ParseIP("203.0.113.7"), mods(nil)); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if _, err := s.MailFrom("noreply@example.com", mods(map[string]string{"auth_authen": "app1"})); err != nil {
		t.Fatalf("MailFrom: %v", err)
	}
	if _, err := s.RcptTo("<a@example.net>", mods(nil)); err != nil {
		t.Fatalf("RcptTo: %v", err)
	}
	if _, err := s.RcptTo("b@example.net", mods(nil)); err != nil {
		t.Fatalf("RcptTo: %v", err)
	}
	if _, err := s.Header("Subject", "Hello there", mods(nil)); err != nil {
		t.Fatalf("Header: %v", err)
	}
	if _, err := s.Body(mods(map[string]string{"i": "ABC123"})); err != nil {
		t.Fatalf("Body: %v", err)
	}
	return s
}

func TestSessionRecordsRowPerRecipient(t *testing.T) {
	rec := &fakeRecorder{}
	s := drive(t, rec)

	if s.clientIP != "203.0.113.7" {
		t.Fatalf("clientIP = %q, want 203.0.113.7", s.clientIP)
	}
	if len(rec.entries) != 2 {
		t.Fatalf("want 2 entries, got %d: %+v", len(rec.entries), rec.entries)
	}
	got := rec.entries[0]
	want := store.SendLogEntry{
		QueueID:  "ABC123",
		Domain:   "example.com",
		AppLogin: "app1",
		From:     "noreply@example.com",
		To:       "a@example.net", // angle brackets stripped
		Subject:  "Hello there",
	}
	if got != want {
		t.Fatalf("entry[0]\n got %+v\nwant %+v", got, want)
	}
	if rec.entries[1].To != "b@example.net" {
		t.Fatalf("entry[1].To = %q", rec.entries[1].To)
	}
}

func TestBodyAcceptsEvenWhenRecorderFails(t *testing.T) {
	rec := &fakeRecorder{fail: true}
	s := &session{rec: rec}
	_, _ = s.MailFrom("x@example.com", mods(map[string]string{"auth_authen": "app1"}))
	_, _ = s.RcptTo("y@example.net", mods(nil))
	resp, err := s.Body(mods(map[string]string{"i": "Q9"}))
	if err != nil {
		t.Fatalf("Body returned error, must fail open: %v", err)
	}
	if resp != milter.RespAccept {
		t.Fatalf("Body response = %v, want Accept", resp)
	}
}

// A single connection may carry several messages; the second must not inherit
// the first's recipients or subject.
func TestSessionResetsBetweenMessages(t *testing.T) {
	rec := &fakeRecorder{}
	s := &session{rec: rec}

	_, _ = s.MailFrom("a@example.com", mods(map[string]string{"auth_authen": "app1"}))
	_, _ = s.RcptTo("one@example.net", mods(nil))
	_, _ = s.Header("Subject", "first", mods(nil))
	_, _ = s.Body(mods(map[string]string{"i": "Q1"}))

	_, _ = s.MailFrom("b@example.com", mods(map[string]string{"auth_authen": "app2"}))
	_, _ = s.RcptTo("two@example.net", mods(nil))
	_, _ = s.Body(mods(map[string]string{"i": "Q2"}))

	if len(rec.entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(rec.entries))
	}
	second := rec.entries[1]
	if second.QueueID != "Q2" || second.To != "two@example.net" || second.Subject != "" || second.AppLogin != "app2" {
		t.Fatalf("second message leaked state: %+v", second)
	}
}

// Postfix sends multi-character macro names wrapped in braces ({auth_authen},
// {i} for some versions), so the milter must resolve those too — this is the
// case the SASL-less spike missed and that produced empty app_login at first.
func TestBracedMacros(t *testing.T) {
	rec := &fakeRecorder{}
	s := &session{rec: rec}
	_, _ = s.MailFrom("app@example.com", mods(map[string]string{"{auth_authen}": "app1"}))
	_, _ = s.RcptTo("to@example.net", mods(nil))
	_, _ = s.Body(mods(map[string]string{"{i}": "QBRACE"}))

	if len(rec.entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(rec.entries))
	}
	e := rec.entries[0]
	if e.AppLogin != "app1" {
		t.Fatalf("AppLogin = %q, want app1 (braced {auth_authen} not resolved)", e.AppLogin)
	}
	if e.QueueID != "QBRACE" {
		t.Fatalf("QueueID = %q, want QBRACE (braced {i} not resolved)", e.QueueID)
	}
}

// limitAt is the client IP the rate-limit tests connect from; the limits below
// register it so the differentiated check applies.
const limitIP = "203.0.113.7"

func activeLimit(ips ...string) store.RateLimit {
	return store.RateLimit{AllowedIPs: ips, MaxMessages: 5, WindowSeconds: 3600}
}

// mailFrom drives just the connect + MAIL FROM stages and returns the response,
// which is where the level-2 limit is enforced.
func mailFrom(t *testing.T, rec Store, ip, from, login string) milter.Response {
	t.Helper()
	s := &session{rec: rec}
	if _, err := s.Connect("h", "tcp4", 0, net.ParseIP(ip), mods(nil)); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	resp, err := s.MailFrom(from, mods(map[string]string{"auth_authen": login}))
	if err != nil {
		t.Fatalf("MailFrom: %v", err)
	}
	return resp
}

func TestRateLimitRefusesWhenDomainOverLimit(t *testing.T) {
	rec := &fakeRecorder{
		limits: map[string]store.RateLimit{
			store.RateLimitScopeDomain + "|example.com": activeLimit(limitIP),
		},
		counts: map[string]int64{store.RateLimitScopeDomain + "|example.com": 5}, // == max
	}
	if resp := mailFrom(t, rec, limitIP, "a@example.com", "app1"); resp != milter.RespTempFail {
		t.Fatalf("over-limit MAIL FROM = %v, want TempFail (4xx)", resp)
	}
	if len(rec.rejected) != 1 || rec.rejected[0].Domain != "example.com" {
		t.Fatalf("want one rejected send-log row for example.com, got %+v", rec.rejected)
	}
}

func TestRateLimitRefusesWhenAppOverLimit(t *testing.T) {
	rec := &fakeRecorder{
		limits: map[string]store.RateLimit{
			store.RateLimitScopeApp + "|app1": activeLimit(limitIP),
		},
		counts: map[string]int64{store.RateLimitScopeApp + "|app1": 9}, // over max
	}
	if resp := mailFrom(t, rec, limitIP, "a@example.com", "app1"); resp != milter.RespTempFail {
		t.Fatalf("over app limit = %v, want TempFail", resp)
	}
}

func TestRateLimitAllowsUnderLimit(t *testing.T) {
	rec := &fakeRecorder{
		limits: map[string]store.RateLimit{
			store.RateLimitScopeDomain + "|example.com": activeLimit(limitIP),
		},
		counts: map[string]int64{store.RateLimitScopeDomain + "|example.com": 4}, // < max
	}
	if resp := mailFrom(t, rec, limitIP, "a@example.com", "app1"); resp != milter.RespContinue {
		t.Fatalf("under limit = %v, want Continue", resp)
	}
	if len(rec.rejected) != 0 {
		t.Fatalf("under limit must not record a rejection: %+v", rec.rejected)
	}
}

func TestRateLimitIgnoresUnregisteredIP(t *testing.T) {
	rec := &fakeRecorder{
		limits: map[string]store.RateLimit{
			store.RateLimitScopeDomain + "|example.com": activeLimit("198.51.100.1"), // not limitIP
		},
		counts: map[string]int64{store.RateLimitScopeDomain + "|example.com": 999},
	}
	// The sender's IP is not in the domain's registered set, so level-2 does not
	// apply even though the count is huge (level-1 anvil would still cover it).
	if resp := mailFrom(t, rec, limitIP, "a@example.com", "app1"); resp != milter.RespContinue {
		t.Fatalf("unregistered IP = %v, want Continue (level-2 n/a)", resp)
	}
}

func TestRateLimitInactiveWithoutCeiling(t *testing.T) {
	rec := &fakeRecorder{
		// IP registered but no ceiling/window: an inert draft, must not enforce.
		limits: map[string]store.RateLimit{
			store.RateLimitScopeDomain + "|example.com": {AllowedIPs: []string{limitIP}},
		},
		counts: map[string]int64{store.RateLimitScopeDomain + "|example.com": 999},
	}
	if resp := mailFrom(t, rec, limitIP, "a@example.com", "app1"); resp != milter.RespContinue {
		t.Fatalf("inactive limit = %v, want Continue", resp)
	}
}

func TestRateLimitFailsOpenOnLookupError(t *testing.T) {
	rec := &fakeRecorder{lookupErr: errors.New("db down")}
	if resp := mailFrom(t, rec, limitIP, "a@example.com", "app1"); resp != milter.RespContinue {
		t.Fatalf("lookup error = %v, want Continue (fail-open)", resp)
	}
}

func TestRateLimitFailsOpenOnCountError(t *testing.T) {
	rec := &fakeRecorder{
		limits: map[string]store.RateLimit{
			store.RateLimitScopeDomain + "|example.com": activeLimit(limitIP),
		},
		countErr: errors.New("db down"),
	}
	if resp := mailFrom(t, rec, limitIP, "a@example.com", "app1"); resp != milter.RespContinue {
		t.Fatalf("count error = %v, want Continue (fail-open)", resp)
	}
}

func TestRateLimitNoIPKeyDoesNotApply(t *testing.T) {
	rec := &fakeRecorder{
		limits: map[string]store.RateLimit{
			store.RateLimitScopeDomain + "|example.com": activeLimit(limitIP),
		},
		counts: map[string]int64{store.RateLimitScopeDomain + "|example.com": 999},
	}
	// A session with no client IP (e.g. local submission) cannot be keyed.
	s := &session{rec: rec}
	resp, err := s.MailFrom("a@example.com", mods(map[string]string{"auth_authen": "app1"}))
	if err != nil {
		t.Fatalf("MailFrom: %v", err)
	}
	if resp != milter.RespContinue {
		t.Fatalf("no-IP session = %v, want Continue", resp)
	}
}

func TestDomainOf(t *testing.T) {
	cases := map[string]string{
		"user@Example.COM": "example.com",
		"no-domain":        "",
		"":                 "",
		"a@b@c.com":        "c.com",
	}
	for in, want := range cases {
		if got := domainOf(in); got != want {
			t.Fatalf("domainOf(%q) = %q, want %q", in, got, want)
		}
	}
}
