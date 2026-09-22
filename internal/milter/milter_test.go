package milter

import (
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-milter"

	"github.com/mixeme/selfpost/internal/store"
)

// fakeRecorder captures inserts and can be made to fail, to prove the milter
// swallows recorder errors and still accepts the message. By default it reports
// no configured rate limit, so the level-2 check is inert unless a test sets
// limits (see fakeRecorder fields).
// mu guards the recorded slices so several sessions may drive one recorder
// concurrently, as they do in the real server.
type fakeRecorder struct {
	mu       sync.Mutex
	entries  []store.SendLogEntry
	rejected []store.SendLogEntry
	fail     bool

	// limits, keyed by "scope|ref", drive the level-2 rate-limit tests. counts
	// gives the recent-message count returned for a "scope|ref". apps supplies
	// application rows for client-IP authorization tests. lookupErr and countErr
	// force the store errors that must fail open.
	limits    map[string]store.RateLimit
	counts    map[string]int64
	apps      map[string]store.Application
	lookupErr error
	countErr  error

	// onCount, if set, runs inside CountMessages. It lets a test hold every
	// racing session at the store lookup until they can all proceed together.
	onCount func()
}

func (f *fakeRecorder) InsertQueued(e store.SendLogEntry) error {
	if f.fail {
		return errors.New("boom")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, e)
	return nil
}

func (f *fakeRecorder) InsertRejected(e store.SendLogEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rejected = append(f.rejected, e)
	return nil
}

func (f *fakeRecorder) RateLimit(scope, ref string) (store.RateLimit, bool, error) {
	if f.lookupErr != nil {
		return store.RateLimit{}, false, f.lookupErr
	}
	rl, ok := f.limits[scope+"|"+ref]
	if ok {
		rl.Scope = scope
	}
	return rl, ok, nil
}

func (f *fakeRecorder) CountMessages(scope, ref string, _ time.Time) (int64, error) {
	if f.countErr != nil {
		return 0, f.countErr
	}
	if f.onCount != nil {
		f.onCount()
	}
	return f.counts[scope+"|"+ref], nil
}

func (f *fakeRecorder) ApplicationByLogin(login string) (store.Application, error) {
	if f.lookupErr != nil {
		return store.Application{}, f.lookupErr
	}
	a, ok := f.apps[login]
	if !ok {
		return store.Application{}, store.ErrApplicationNotFound
	}
	return a, nil
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

// A subject in any non-ASCII alphabet reaches the milter as RFC 2047
// encoded-words; the journal stores the text, not the encoding. The decoding
// itself is mailhdr's; what this checks is that Header runs it.
func TestHeaderDecodesEncodedSubject(t *testing.T) {
	s := &session{rec: &fakeRecorder{}}
	if _, err := s.Header("Subject", "=?utf-8?Q?=D0=9F=D1=80=D0=BE=D0=B2=D0=B5=D1=80=D0=BA=D0=B0?=", mods(nil)); err != nil {
		t.Fatalf("Header: %v", err)
	}
	if s.subject != "Проверка" {
		t.Fatalf("subject = %q, want %q", s.subject, "Проверка")
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

// limitIP is the client IP rate-limit tests connect from.
const limitIP = "203.0.113.7"

func domainLimit() store.RateLimit {
	return store.RateLimit{MaxMessages: 5, WindowSeconds: 3600}
}

func appLimit() store.RateLimit {
	return store.RateLimit{MaxMessages: 5, WindowSeconds: 3600}
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
			store.RateLimitScopeDomain + "|example.com": domainLimit(),
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
			store.RateLimitScopeApp + "|app1": appLimit(),
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
			store.RateLimitScopeDomain + "|example.com": domainLimit(),
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

func TestRateLimitDomainAppliesToAnyIP(t *testing.T) {
	rec := &fakeRecorder{
		limits: map[string]store.RateLimit{
			store.RateLimitScopeDomain + "|example.com": domainLimit(),
		},
		counts: map[string]int64{store.RateLimitScopeDomain + "|example.com": 999},
	}
	// Domain ceilings apply to every client IP; leftover AllowedIPs on the row
	// are ignored.
	if resp := mailFrom(t, rec, limitIP, "a@example.com", "app1"); resp != milter.RespTempFail {
		t.Fatalf("domain over limit from any IP = %v, want TempFail", resp)
	}
}

func TestRateLimitAppSkipsDomainWhenActive(t *testing.T) {
	rec := &fakeRecorder{
		limits: map[string]store.RateLimit{
			store.RateLimitScopeDomain + "|example.com": {MaxMessages: 1, WindowSeconds: 3600},
			store.RateLimitScopeApp + "|app1":           appLimit(),
		},
		counts: map[string]int64{
			store.RateLimitScopeDomain + "|example.com": 5, // over domain
			store.RateLimitScopeApp + "|app1":           2, // under app
		},
	}
	if resp := mailFrom(t, rec, limitIP, "a@example.com", "app1"); resp != milter.RespContinue {
		t.Fatalf("app under its ceiling = %v, want Continue (domain not checked)", resp)
	}
}

func TestAuthIPRestrictBlocksUnlisted(t *testing.T) {
	rec := &fakeRecorder{
		apps: map[string]store.Application{
			"app1": {
				Login:          "app1",
				AuthIPRestrict: true,
				AuthAllowedIPs: []string{"198.51.100.1"},
			},
		},
	}
	if resp := mailFrom(t, rec, limitIP, "a@example.com", "app1"); resp != milter.RespTempFail {
		t.Fatalf("unlisted IP = %v, want TempFail", resp)
	}
	if len(rec.rejected) != 1 {
		t.Fatalf("want one rejected row, got %+v", rec.rejected)
	}
}

func TestAuthIPRestrictAllowsListed(t *testing.T) {
	rec := &fakeRecorder{
		apps: map[string]store.Application{
			"app1": {
				Login:          "app1",
				AuthIPRestrict: true,
				AuthAllowedIPs: []string{limitIP},
			},
		},
	}
	if resp := mailFrom(t, rec, limitIP, "a@example.com", "app1"); resp != milter.RespContinue {
		t.Fatalf("listed IP = %v, want Continue", resp)
	}
}

func TestAuthIPRestrictOffAllowsAnyIP(t *testing.T) {
	rec := &fakeRecorder{
		apps: map[string]store.Application{
			"app1": {Login: "app1"},
		},
	}
	if resp := mailFrom(t, rec, limitIP, "a@example.com", "app1"); resp != milter.RespContinue {
		t.Fatalf("restriction off = %v, want Continue", resp)
	}
}

func TestRateLimitInactiveWithoutCeiling(t *testing.T) {
	rec := &fakeRecorder{
		limits: map[string]store.RateLimit{
			store.RateLimitScopeDomain + "|example.com": {AllowedIPs: []string{limitIP}}, // no max/window
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
			store.RateLimitScopeDomain + "|example.com": domainLimit(),
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
			store.RateLimitScopeDomain + "|example.com": domainLimit(),
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

// mailFromIn is mailFrom with an explicit shared in-flight registry, so a test
// can play several concurrent SMTP sessions of one process against each other.
func mailFromIn(t *testing.T, rec Store, fl *inflight, ip, from, login string) (*session, milter.Response) {
	t.Helper()
	s := &session{rec: rec, flight: fl}
	if _, err := s.Connect("h", "tcp4", 0, net.ParseIP(ip), mods(nil)); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	resp, err := s.MailFrom(from, mods(map[string]string{"auth_authen": login}))
	if err != nil {
		t.Fatalf("MailFrom: %v", err)
	}
	return s, resp
}

func limitedRecorder(count int64) *fakeRecorder {
	return &fakeRecorder{
		limits: map[string]store.RateLimit{
			store.RateLimitScopeDomain + "|example.com": domainLimit(),
		},
		counts: map[string]int64{store.RateLimitScopeDomain + "|example.com": count},
	}
}

// Messages between MAIL FROM and end-of-message are not in the send log yet, so
// counting the stored rows alone lets concurrent sessions each pass the same
// check and overshoot the ceiling. The last free slot may only be taken once.
func TestRateLimitCountsInFlightMessages(t *testing.T) {
	rec := limitedRecorder(4) // one below the ceiling of 5
	fl := &inflight{}

	if _, resp := mailFromIn(t, rec, fl, limitIP, "a@example.com", "app1"); resp != milter.RespContinue {
		t.Fatalf("first message = %v, want Continue (4/5 stored)", resp)
	}
	// Same window, nothing written yet: the first message holds the fifth slot.
	if _, resp := mailFromIn(t, rec, fl, limitIP, "b@example.com", "app1"); resp != milter.RespTempFail {
		t.Fatalf("concurrent message = %v, want TempFail (would overshoot)", resp)
	}
	if len(rec.rejected) != 1 {
		t.Fatalf("want one rejected send-log row, got %+v", rec.rejected)
	}
}

// The case above is sequential: the second session reads the stored count after
// the first has already reserved. Here every session reads it first — the gate
// holds them all inside the lookup — which is what concurrent SMTP connections
// actually do. However many then race for the single free slot, exactly one may
// pass. (TestTryAdmitHandsOutEachSlotOnce is the test that fails when counting
// and reserving are not one step; this one pins the session-level behaviour.)
func TestRateLimitAdmitsOnlyOneRacingSession(t *testing.T) {
	rec := limitedRecorder(4) // one below the ceiling of 5
	fl := &inflight{}
	gate := make(chan struct{})

	// Every session is held inside the stored-count lookup until all of them
	// have read it, which is the state the race needs: none of them can see
	// another's reservation, because none has been taken yet.
	const racers = 32
	var atCount, done sync.WaitGroup
	atCount.Add(racers)
	go func() { atCount.Wait(); close(gate) }()
	rec.onCount = func() { atCount.Done(); <-gate }

	responses := make([]milter.Response, racers)
	for i := range racers {
		done.Add(1)
		go func() {
			defer done.Done()
			// Connect is skipped so every goroutine starts from the same point;
			// the client IP is what Connect would have captured.
			s := &session{rec: rec, flight: fl, clientIP: limitIP}
			resp, err := s.MailFrom("a@example.com", mods(map[string]string{"auth_authen": "app1"}))
			if err != nil {
				resp = nil // reported as a missing Continue below
			}
			responses[i] = resp
		}()
	}
	done.Wait()

	admitted := 0
	for _, resp := range responses {
		if resp == milter.RespContinue {
			admitted++
		}
	}
	if admitted != 1 {
		t.Fatalf("%d of %d racing sessions admitted, want exactly 1 — the last slot was handed out twice",
			admitted, racers)
	}
	if n := fl.count(store.RateLimitScopeDomain+"|example.com", time.Now().Add(-time.Hour)); n != 1 {
		t.Fatalf("in-flight reservations = %d, want 1", n)
	}
}

// Once the message is recorded the stored count sees it, so its reservation
// must be given back — otherwise it would be counted twice and the ceiling
// would drift closed.
func TestReservationReleasedAtEndOfMessage(t *testing.T) {
	rec := limitedRecorder(4)
	fl := &inflight{}

	s, resp := mailFromIn(t, rec, fl, limitIP, "a@example.com", "app1")
	if resp != milter.RespContinue {
		t.Fatalf("first message = %v, want Continue", resp)
	}
	if _, err := s.Body(mods(map[string]string{"i": "Q1"})); err != nil {
		t.Fatalf("Body: %v", err)
	}
	if n := fl.count(store.RateLimitScopeDomain+"|example.com", time.Now().Add(-time.Hour)); n != 0 {
		t.Fatalf("in-flight count after EOM = %d, want 0", n)
	}
}

// A transaction the client abandons (RSET, or a Postfix-side rejection) never
// reaches the send log, so its slot must not stay claimed.
func TestReservationReleasedOnAbort(t *testing.T) {
	rec := limitedRecorder(4)
	fl := &inflight{}

	s, resp := mailFromIn(t, rec, fl, limitIP, "a@example.com", "app1")
	if resp != milter.RespContinue {
		t.Fatalf("first message = %v, want Continue", resp)
	}
	if err := s.Abort(mods(nil)); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if _, resp := mailFromIn(t, rec, fl, limitIP, "b@example.com", "app1"); resp != milter.RespContinue {
		t.Fatalf("after abort = %v, want Continue (slot released)", resp)
	}
}

// A trusted app at its ceiling refuses without touching the domain counter;
// no domain reservation should linger after the refusal.
func TestRefusalDoesNotLeaveDomainReservation(t *testing.T) {
	rec := &fakeRecorder{
		limits: map[string]store.RateLimit{
			store.RateLimitScopeDomain + "|example.com": domainLimit(),
			store.RateLimitScopeApp + "|app1":           appLimit(),
		},
		counts: map[string]int64{
			store.RateLimitScopeDomain + "|example.com": 0,
			store.RateLimitScopeApp + "|app1":           5, // app at ceiling
		},
	}
	fl := &inflight{}
	if _, resp := mailFromIn(t, rec, fl, limitIP, "a@example.com", "app1"); resp != milter.RespTempFail {
		t.Fatalf("app over limit = %v, want TempFail", resp)
	}
	if n := fl.count(store.RateLimitScopeDomain+"|example.com", time.Now().Add(-time.Hour)); n != 0 {
		t.Fatalf("domain reservation left behind after refusal: %d", n)
	}
	if n := fl.count(store.RateLimitScopeApp+"|app1", time.Now().Add(-time.Hour)); n != 0 {
		t.Fatalf("app reservation left behind after refusal: %d", n)
	}
}

// The ceiling is handed out exactly max times however the sessions interleave.
// Counting and reserving in two critical sections passes the sequential tests
// above and still overshoots here, because between one session's count and its
// reservation any number of others can pass the same check.
func TestTryAdmitHandsOutEachSlotOnce(t *testing.T) {
	const (
		max     = 500
		workers = 8
	)
	fl := &inflight{}
	since := time.Now().Add(-time.Hour)
	start := make(chan struct{})
	admitted := make([]int, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for {
				_, _, ok := fl.tryAdmit("domain|example.com", since, 0, max)
				if !ok {
					return
				}
				admitted[i]++
			}
		}()
	}
	close(start)
	wg.Wait()

	total := 0
	for _, n := range admitted {
		total += n
	}
	if total != max {
		t.Fatalf("admitted %d messages under a ceiling of %d", total, max)
	}
}

// The in-flight count only covers the limit's own window: a reservation older
// than it (a session stuck mid-DATA for longer than the window) must not be
// counted against a window it no longer belongs to.
func TestInflightIgnoresReservationsOutsideWindow(t *testing.T) {
	fl := &inflight{}
	r, _, ok := fl.tryAdmit("domain|example.com", time.Now().Add(-time.Hour), 0, 1)
	if !ok {
		t.Fatal("tryAdmit refused the first message under a ceiling of 1")
	}
	r.at = time.Now().Add(-time.Minute)

	if n := fl.count("domain|example.com", time.Now().Add(-time.Hour)); n != 1 {
		t.Fatalf("count inside window = %d, want 1", n)
	}
	if n := fl.count("domain|example.com", time.Now().Add(-time.Second)); n != 0 {
		t.Fatalf("count outside window = %d, want 0", n)
	}
	// Past the TTL the reservation is dropped even for a wide window, so a
	// client that vanished after MAIL FROM cannot hold a slot forever.
	r.at = time.Now().Add(-2 * reservationTTL)
	if n := fl.count("domain|example.com", time.Now().Add(-3*reservationTTL)); n != 0 {
		t.Fatalf("expired reservation still counted: %d", n)
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
