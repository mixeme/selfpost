package logtail

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mixeme/selfpost/internal/store"
)

func TestParseDelivery(t *testing.T) {
	cases := []struct {
		name                       string
		line                       string
		wantOK                     bool
		queueID, recipient, status string
	}{
		{
			name:      "sent",
			line:      "2026-07-11T11:55:34 host postfix/smtp[26]: 41E862C00D9E: to=<a@example.net>, relay=mx.example.net[203.0.113.9]:25, delay=0.5, dsn=2.0.0, status=sent (250 OK)",
			wantOK:    true,
			queueID:   "41E862C00D9E",
			recipient: "a@example.net",
			status:    store.StatusSent,
		},
		{
			name:      "deferred",
			line:      "host postfix/smtp[26]: 5900C2C00D9E: to=<y@example.net>, relay=none, delay=30, dsn=4.4.1, status=deferred (connect timed out)",
			wantOK:    true,
			queueID:   "5900C2C00D9E",
			recipient: "y@example.net",
			status:    store.StatusDeferred,
		},
		{
			name:      "bounced",
			line:      "host postfix/smtp[26]: ABC: to=<no@example.net>, relay=…, dsn=5.1.1, status=bounced (user unknown)",
			wantOK:    true,
			queueID:   "ABC",
			recipient: "no@example.net",
			status:    store.StatusBounced,
		},
		{
			name:      "expired maps to bounced",
			line:      "host postfix/smtp[26]: DEF: to=<slow@example.net>, relay=none, status=expired (delivery temporarily suspended)",
			wantOK:    true,
			queueID:   "DEF",
			recipient: "slow@example.net",
			status:    store.StatusBounced,
		},
		{
			name:   "qmgr from-line ignored",
			line:   "host postfix/qmgr[10]: 41E862C00D9E: from=<noreply@example.com>, size=500, nrcpt=1 (queue active)",
			wantOK: false,
		},
		{
			name:   "smtpd client-line ignored",
			line:   "host postfix/smtpd[10]: 41E862C00D9E: client=unknown[203.0.113.7]",
			wantOK: false,
		},
		{
			// The remote server's reply is quoted verbatim at the end of the
			// line and is entirely attacker-influenced text. A "status=" that
			// appears in there must not win over the real field, or a bounce
			// would be filed as a success.
			name:      "status= quoted in the remote reply does not win",
			line:      "host postfix/smtp[26]: 9F1A2C00D9E: to=<a@example.net>, relay=mx.example.net[203.0.113.9]:25, dsn=5.1.1, status=bounced (host mx.example.net said: 550 5.1.1 unknown status=sent (in reply to RCPT TO command))",
			wantOK:    true,
			queueID:   "9F1A2C00D9E",
			recipient: "a@example.net",
			status:    store.StatusBounced,
		},
		{
			// Postfix logs the null sender's own delivery (double bounce) with
			// an empty recipient. It parses, and the empty recipient simply
			// matches no send-log row — the panel only ever records mail it
			// accepted from an authenticated client.
			name:      "null recipient parses with an empty address",
			line:      "host postfix/smtp[26]: A1B2C3: to=<>, relay=none, delay=0.1, dsn=2.0.0, status=sent (250 OK)",
			wantOK:    true,
			queueID:   "A1B2C3",
			recipient: "",
			status:    store.StatusSent,
		},
		{
			// An alias/virtual expansion carries orig_to= as well; the address
			// the message was actually delivered to is the one in to=.
			name:      "orig_to is ignored in favour of to",
			line:      "host postfix/lmtp[26]: 4Xk9tS1abcz: to=<real@example.net>, orig_to=<alias@example.net>, relay=x, dsn=2.0.0, status=sent (ok)",
			wantOK:    true,
			queueID:   "4Xk9tS1abcz",
			recipient: "real@example.net",
			status:    store.StatusSent,
		},
		{
			// Postfix's own delivery agents write these two, but neither is a
			// final result we model: "deliverable" comes from address
			// verification probes, and anything unrecognised is dropped rather
			// than guessed at, leaving the row in its previous state.
			name:   "unknown status word is not a delivery result",
			line:   "host postfix/smtp[26]: BEEF01: to=<a@example.net>, relay=x, status=deliverable (ok)",
			wantOK: false,
		},
		{
			name:   "status matching is case-sensitive, as Postfix writes it",
			line:   "host postfix/smtp[26]: BEEF02: to=<a@example.net>, relay=x, dsn=4.0.0, status=Deferred (connect timed out)",
			wantOK: false,
		},
		{
			name:   "cleanup message-id line ignored",
			line:   "host postfix/cleanup[12]: BEEF03: message-id=<x@example.com>",
			wantOK: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q, r, s, ok := parseDelivery(c.line)
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v", ok, c.wantOK)
			}
			if !ok {
				return
			}
			if q != c.queueID || r != c.recipient || s != c.status {
				t.Fatalf("got (%q,%q,%q), want (%q,%q,%q)", q, r, s, c.queueID, c.recipient, c.status)
			}
		})
	}
}

// captureStore records UpdateStatus calls for the follow integration test and
// keeps the persisted read offset in memory, so a "restart" in a test is a
// second Run against the same captureStore.
type captureStore struct {
	mu    sync.Mutex
	calls []string

	state     store.LogtailState
	haveState bool
	stateErr  error
}

func (c *captureStore) UpdateStatus(queueID, recipient, status string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, queueID+"|"+recipient+"|"+status)
	return 1, nil
}

func (c *captureStore) DeleteSendLogBefore(time.Time) (int64, error) { return 0, nil }

func (c *captureStore) LogtailState(string) (store.LogtailState, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stateErr != nil {
		return store.LogtailState{}, false, c.stateErr
	}
	return c.state, c.haveState, nil
}

func (c *captureStore) SaveLogtailState(_ string, st store.LogtailState) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stateErr != nil {
		return c.stateErr
	}
	c.state, c.haveState = st, true
	return nil
}

func (c *captureStore) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.calls...)
}

func (c *captureStore) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = nil
}

// TestFollowTailsAndRotates writes delivery lines to a log file, then rotates
// it (rename + fresh create, as logrotate does) and writes more, asserting the
// tailer picks up lines from both the original and rotated file.
func TestFollowTailsAndRotates(t *testing.T) {
	old := pollInterval
	pollInterval = 10 * time.Millisecond
	t.Cleanup(func() { pollInterval = old })

	dir := t.TempDir()
	path := filepath.Join(dir, "mail.log")
	if err := os.WriteFile(path, []byte("preexisting line, ignored on start\n"), 0o644); err != nil {
		t.Fatalf("seed log: %v", err)
	}

	cs := &captureStore{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, path, cs, 90) }()

	// Give follow() time to open at EOF (it seeks to end immediately on start,
	// so the seed line above is ignored), then append a delivery line.
	time.Sleep(50 * time.Millisecond)
	appendLine(t, path, "host postfix/smtp[1]: Q1: to=<a@example.net>, dsn=2.0.0, status=sent (ok)")
	waitFor(t, func() bool { return contains(cs.snapshot(), "Q1|a@example.net|sent") })

	// Rotate: move the current file aside and create a fresh one (logrotate
	// "create"), then append to the new file.
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("recreate: %v", err)
	}
	appendLine(t, path, "host postfix/smtp[1]: Q2: to=<b@example.net>, dsn=5.1.1, status=bounced (nope)")
	waitFor(t, func() bool { return contains(cs.snapshot(), "Q2|b@example.net|bounced") })

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

// TestFollowResumesAfterRestart covers the persisted read offset: a restart
// must parse the delivery lines written while the tailer was down (rows that
// would otherwise stay "queued" forever), without re-parsing what it already
// read, and must fall back to reading the whole file when the log was rotated
// or recreated in the meantime.
func TestFollowResumesAfterRestart(t *testing.T) {
	old := pollInterval
	pollInterval = 10 * time.Millisecond
	t.Cleanup(func() { pollInterval = old })

	dir := t.TempDir()
	path := filepath.Join(dir, "mail.log")
	// A head longer than fingerprintSize, so the file stays identifiable across
	// the restart; the lines themselves predate the first start and are ignored.
	seed := strings.Repeat("host postfix/qmgr[1]: seed line, not a delivery\n", 20)
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed log: %v", err)
	}

	cs := &captureStore{}
	stop := startRun(t, path, cs)
	appendLine(t, path, "host postfix/smtp[1]: Q1: to=<a@example.net>, dsn=2.0.0, status=sent (ok)")
	waitFor(t, func() bool { return contains(cs.snapshot(), "Q1|a@example.net|sent") })
	stop() // persists the offset past Q1

	// Down: Postfix keeps delivering.
	appendLine(t, path, "host postfix/smtp[1]: Q2: to=<b@example.net>, dsn=2.0.0, status=sent (ok)")

	cs.reset()
	stop = startRun(t, path, cs)
	waitFor(t, func() bool { return contains(cs.snapshot(), "Q2|b@example.net|sent") })
	if contains(cs.snapshot(), "Q1|a@example.net|sent") {
		t.Fatal("resumed run re-parsed Q1: offset was not honoured")
	}
	stop()

	// Down again, and this time the log is replaced (logrotate + fresh create).
	// The stored offset belongs to a file that no longer exists, so the new one
	// must be read from the start.
	if err := os.WriteFile(path, []byte(strings.Repeat("host postfix/qmgr[1]: fresh log after rotation\n", 20)+
		"host postfix/smtp[1]: Q3: to=<c@example.net>, dsn=5.1.1, status=bounced (nope)\n"), 0o644); err != nil {
		t.Fatalf("recreate log: %v", err)
	}

	cs.reset()
	stop = startRun(t, path, cs)
	waitFor(t, func() bool { return contains(cs.snapshot(), "Q3|c@example.net|bounced") })
	stop()
}

// startRun launches the tailer and returns a function that cancels it and waits
// for a clean return, the way a panel restart bookends a run.
func startRun(t *testing.T, path string, cs *captureStore) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, path, cs, 90) }()
	// follow() opens and seeks on start; give it a moment before the caller
	// appends, so the append is not raced by the initial open.
	time.Sleep(50 * time.Millisecond)
	return func() {
		t.Helper()
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Run did not return after cancel")
		}
	}
}

func appendLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open for append: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatalf("append: %v", err)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}
