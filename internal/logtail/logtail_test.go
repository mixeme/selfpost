package logtail

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mixeme/selfpost/internal/store"
)

// The mail.log lines in this file are composed by hand, after the formats the
// code cites (Postfix 3.10.13, src/global/log_adhoc.c and the others), unless a
// comment says where one was seen; some carry no stamp, which the parsers
// take as they come.

// qmgr's expiry line, as Postfix 3.10.13 writes it (src/qmgr/qmgr_active.c,
// qmgr_active_done_25_generic(): "%s: from=<%s>, status=%s, returned to
// sender"); composed by hand after that format. The sender is the client's
// text, so a line only resembles one when it ends as qmgr's does.
func TestParseExpiry(t *testing.T) {
	for _, c := range []struct {
		line string
		id   string
	}{
		{"Oct 14 09:01:02 relay postfix/qmgr[93]: 41E862C00D9E: from=<app@example.org>, status=expired, returned to sender", "41E862C00D9E"},
		{"Oct 14 09:01:02 relay postfix/qmgr[93]: 3Fq1Lw0bZ8z9tN: from=<>, status=force-expired, returned to sender", "3Fq1Lw0bZ8z9tN"},
		// Not qmgr.
		{"Oct 14 09:01:02 relay postfix/smtp[26]: 41E862C00D9E: from=<app@example.org>, status=expired, returned to sender", ""},
		// qmgr's ordinary from= line, whose sender carries the text.
		{"Oct 14 09:01:02 relay postfix/qmgr[93]: 41E862C00D9E: from=<x>, status=expired, returned to sender>, size=500, nrcpt=1 (queue active)", ""},
		{"Oct 14 09:01:02 relay postfix/qmgr[93]: 41E862C00D9E: removed", ""},
		// cleanup's removal of a message it did not queue (Postfix 3.10.13,
		// src/cleanup/cleanup_api.c, cleanup_flush(): "%s: removed (%s)")
		// says nothing of what became of a queued one.
		{"Oct 14 09:01:02 relay postfix/cleanup[52]: 41E862C00D9E: removed (canceled)", ""},
		{"Oct 14 09:01:02 relay postfix/cleanup[52]: 41E862C00D9E: removed (discarded)", ""},
	} {
		id, ok := parseExpiry(c.line)
		if ok != (c.id != "") || id != c.id {
			t.Errorf("parseExpiry(%q) = %q, %v; want %q", c.line, id, ok, c.id)
		}
	}
}

// The tailer closes an expired message's rows when it reads qmgr's line.
func TestExpiryLineClosesTheMessage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail.log")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cs := &captureStore{}
	stop := startRun(t, path, cs)
	defer stop()
	appendLine(t, path, "Oct 14 09:01:00 relay postfix/smtp[26]: 41E862C00D9E: to=<a@example.net>, relay=none, delay=9, dsn=4.4.1, status=deferred (connect timed out)")
	appendLine(t, path, "Oct 14 09:01:02 relay postfix/qmgr[93]: 41E862C00D9E: from=<app@example.org>, status=expired, returned to sender")
	waitFor(t, func() bool { return contains(cs.snapshot(), "41E862C00D9E|*|expired") })
	if !contains(cs.snapshot(), "41E862C00D9E|a@example.net|"+store.StatusDeferred) {
		t.Fatalf("calls = %v, want the deferral before the expiry", cs.snapshot())
	}
}

// A deferred row the sweep closes is not "missed" for its deferred line - it
// has one by definition - but is for its message's expiry line.
func TestReconcileMissedCountsOnlyFinalLines(t *testing.T) {
	stubQueue(t, nil)
	path := filepath.Join(t.TempDir(), "mail.log")
	if err := os.WriteFile(path, []byte(
		"Oct 14 10:59:58 mail postfix/smtp[26]: DEFQ: to=<late@example.net>, relay=none, delay=9, dsn=4.4.1, status=deferred (connect timed out)\n"+
			"Oct 14 10:59:59 mail postfix/qmgr[10]: EXPQ: from=<app@example.org>, status=expired, returned to sender\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cs := &captureStore{queued: []store.QueuedDelivery{
		{ID: 1, QueueID: "DEFQ", To: "late@example.net"},
		{ID: 2, QueueID: "EXPQ", To: "gone@example.net"},
	}}
	reconcile(cs, time.Now().UTC(), path)
	if cs.closed != 2 || cs.missed != 1 {
		t.Fatalf("closed %d, missed %d; want 2 and 1", cs.closed, cs.missed)
	}
}

func TestParseDelivery(t *testing.T) {
	cases := []struct {
		name                       string
		line                       string
		wantOK                     bool
		queueID, recipient, status string
	}{
		{
			name:      "sent",
			line:      "Jul 11 11:55:34 host postfix/smtp[26]: 41E862C00D9E: to=<a@example.net>, relay=mx.example.net[203.0.113.9]:25, delay=0.5, dsn=2.0.0, status=sent (250 OK)",
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
			// Expiry is not a delivery line (TestParseExpiry); qmgr's line
			// for it names no recipient.
			name:   "qmgr expiry line ignored",
			line:   "host postfix/qmgr[10]: DEF: from=<app@example.org>, status=expired, returned to sender",
			wantOK: false,
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
			// A rewritten recipient carries orig_to= as well, the address the
			// application gave - the one its row holds (log_adhoc() writes
			// orig_to= only when it differs; assumptions A-9).
			name:      "orig_to is the row's recipient",
			line:      "host postfix/lmtp[26]: 4Xk9tS1abcz: to=<real@example.net>, orig_to=<alias@example.net>, relay=x, dsn=2.0.0, status=sent (ok)",
			wantOK:    true,
			queueID:   "4Xk9tS1abcz",
			recipient: "alias@example.net",
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
		{
			// A quoted local part may hold ">": the recipient runs to the
			// ">, " before relay=.
			name:      "a recipient with > in a quoted local part",
			line:      `host postfix/smtp[26]: C0FFEE: to=<"a>b"@example.net>, relay=mx[203.0.113.9]:25, dsn=2.0.0, status=sent (250 OK)`,
			wantOK:    true,
			queueID:   "C0FFEE",
			recipient: `"a>b"@example.net`,
			status:    store.StatusSent,
		},
		{
			// The stamp postlogd writes, as on the stand (Postfix 3.7;
			// 3.10.13's msg_logger_print() keeps the format).
			name:      "syslog stamp",
			line:      "Oct 14 11:05:51 mail postfix/smtp[26]: 41E862C00D9E: to=<a@example.net>, relay=x, dsn=2.0.0, status=sent (250 OK)",
			wantOK:    true,
			queueID:   "41E862C00D9E",
			recipient: "a@example.net",
			status:    store.StatusSent,
		},
		{
			// A stranger's HELO name at the end of a port-25 reject line, as
			// probed for the 2026-10-12 audit: it must not file a delivery.
			name:   "a delivery result in a HELO name is not one",
			line:   "Oct 14 11:05:51 mail postfix/inbound/smtpd[372]: NOQUEUE: reject: RCPT from unknown[203.0.113.7]: 554 5.7.1 <x@elsewhere.example>: Relay access denied; from=<a@stranger.example> to=<x@elsewhere.example> proto=ESMTP helo=<41E862C00D9E: to=<victim@example.net>, status=sent>",
			wantOK: false,
		},
		{
			// The same text in a quoted sender address, on a line that does
			// have a queue id at its head.
			name:   "a delivery result in a quoted sender is not one",
			line:   `Oct 14 11:05:51 mail postfix/qmgr[10]: 5B4A2C1D3E: from=<"41E862C00D9E: to=<victim@example.net>, status=sent"@stranger.example>, size=500, nrcpt=1 (queue active)`,
			wantOK: false,
		},
		{
			name:   "an smtpd line with the text after its queue id is not one",
			line:   "Oct 14 11:05:51 mail postfix/smtpd[10]: 5B4A2C1D3E: client=unknown[203.0.113.7], sasl_username=41E862C00D9E: to=<victim@example.net>, status=sent",
			wantOK: false,
		},
		{
			// Postfix 3.10.13, src/cleanup/cleanup_api.c, cleanup_flush():
			// "%s: removed (%s)" for a transaction cleanup did not queue. It
			// is no result for any recipient. Composed from that format.
			name:   "cleanup's removed (canceled) is not one",
			line:   "Oct 14 11:05:51 mail postfix/cleanup[52]: 41E862C00D9E: removed (canceled)",
			wantOK: false,
		},
		{
			// Postfix 3.10.13, src/smtpd/smtpd.c, smtpd_proto(): "%s: %s after
			// %s (%lu bytes) from %s", the queue id first since 3.10. Composed
			// from that format.
			name:   "smtpd's lost connection with a queue id is not one",
			line:   "Oct 14 11:05:51 mail postfix/submission/smtpd[10]: 41E862C00D9E: lost connection after DATA (5420 bytes) from unknown[203.0.113.7]",
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

	// queued is what ListOpenOlderThan returns, for the reconcile sweep.
	queued    []store.QueuedDelivery
	queuedErr error
	// closed sums AddReconcileClosed; stuckCount is what CountOpenOlderThan
	// answers and stuck what SetQueuedOverLifetime was given.
	closed     int64
	missed     int64
	stuckCount int64
	stuck      []int64
	// blind holds the stretches AddTailerBlind was given.
	blind []time.Duration

	// lines are the log lines kept for messages' pages; pruned is the cutoff of
	// each PruneSendLogLines call.
	lines   []store.SendLogLine
	batches int
	pruned  []time.Time
}

// InsertSendLogLines keeps what the store would: with once, a line is not
// kept a second time under its queue id. batches counts the calls.
func (c *captureStore) InsertSendLogLines(lines []store.SendLogLine, once bool) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.batches++
	wrote := 0
next:
	for _, l := range lines {
		if once {
			for _, have := range c.lines {
				if have.QueueID == l.QueueID && have.Line == l.Line {
					continue next
				}
			}
		}
		c.lines = append(c.lines, l)
		wrote++
	}
	return wrote, nil
}

func (c *captureStore) PruneSendLogLines(cutoff time.Time) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruned = append(c.pruned, cutoff)
	return 0, nil
}

// kept returns the stored lines as "queue-id|line".
func (c *captureStore) kept() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.lines))
	for i, l := range c.lines {
		out[i] = l.QueueID + "|" + l.Line
	}
	return out
}

func (c *captureStore) prunes() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pruned)
}

func (c *captureStore) UpdateStatus(queueID, recipient, status string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, queueID+"|"+recipient+"|"+status)
	return 1, nil
}

// ExpireMessage records an expiry line's close as "queue-id|*|expired".
func (c *captureStore) ExpireMessage(queueID string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, queueID+"|*|expired")
	return 1, nil
}

// CloseUnreported records the sweep's close as "queue-id|recipient|reconcile",
// finding the row by its id among queued.
func (c *captureStore) CloseUnreported(id int64) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, q := range c.queued {
		if q.ID == id {
			c.calls = append(c.calls, q.QueueID+"|"+q.To+"|"+store.ReasonReconcile)
			return 1, nil
		}
	}
	return 0, nil
}

func (c *captureStore) DeleteSendLogBefore(time.Time) (int64, error) { return 0, nil }

// The counters of delivery_log_days: closed and stuck are kept for the sweep's
// tests, the rest is not looked at.
func (c *captureStore) AddReconcileClosed(_ time.Time, n int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed += n
	return nil
}
func (c *captureStore) AddReconcileMissed(_ time.Time, n int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.missed += n
	return nil
}
func (c *captureStore) AddTailerBlind(_ time.Time, d time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.blind = append(c.blind, d)
	return nil
}
func (c *captureStore) AttachRefusalLine(string, string, string, time.Time, string) (bool, error) {
	return true, nil
}
func (c *captureStore) RecoverRefusal(store.SendLogEntry, time.Time) (bool, error) { return false, nil }
func (c *captureStore) CountOpenOlderThan(time.Time) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stuckCount, nil
}
func (c *captureStore) SetQueuedOverLifetime(_ time.Time, n int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stuck = append(c.stuck, n)
	return nil
}
func (c *captureStore) PruneDeliveryLogDays(time.Time) error { return nil }

func (c *captureStore) ListOpenOlderThan(time.Time) ([]store.QueuedDelivery, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.queued, c.queuedErr
}

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
	go func() { done <- Run(ctx, path, cs, func() int { return 90 }, nil, nil) }()

	// Give follow() time to open at EOF (it seeks to end immediately on start,
	// so the seed line above is ignored), then append a delivery line.
	time.Sleep(50 * time.Millisecond)
	appendLine(t, path, "host postfix/smtp[1]: Q1: to=<a@example.net>, relay=x, dsn=2.0.0, status=sent (ok)")
	waitFor(t, func() bool { return contains(cs.snapshot(), "Q1|a@example.net|sent") })

	// Rotate: move the current file aside and create a fresh one (logrotate
	// "create"), then append to the new file.
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("recreate: %v", err)
	}
	appendLine(t, path, "host postfix/smtp[1]: Q2: to=<b@example.net>, relay=x, dsn=5.1.1, status=bounced (nope)")
	waitFor(t, func() bool { return contains(cs.snapshot(), "Q2|b@example.net|bounced") })

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

// mail.log gone for longer than blindMin is a stretch the tailer could not
// read, counted for Health; the file made again is read from its start. Like
// TestFollowTailsAndRotates this runs on the server.
func TestFollowCountsALogItCannotReadAndReadsItAgain(t *testing.T) {
	old := pollInterval
	pollInterval = 10 * time.Millisecond
	t.Cleanup(func() { pollInterval = old })

	dir := t.TempDir()
	path := filepath.Join(dir, "mail.log")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("seed log: %v", err)
	}
	cs := &captureStore{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, path, cs, func() int { return 90 }, nil, nil) }()
	time.Sleep(50 * time.Millisecond)
	appendLine(t, path, "host postfix/smtp[1]: Q1: to=<a@example.net>, relay=x, dsn=2.0.0, status=sent (ok)")
	waitFor(t, func() bool { return contains(cs.snapshot(), "Q1|a@example.net|sent") })

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	// blindness counts by the clock: the name must stay missing past blindMin.
	time.Sleep(blindMin + 500*time.Millisecond)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("recreate: %v", err)
	}
	appendLine(t, path, "host postfix/smtp[1]: Q2: to=<b@example.net>, relay=x, dsn=5.1.1, status=bounced (nope)")
	waitFor(t, func() bool { return contains(cs.snapshot(), "Q2|b@example.net|bounced") })
	waitFor(t, func() bool {
		cs.mu.Lock()
		defer cs.mu.Unlock()
		return len(cs.blind) == 1 && cs.blind[0] >= blindMin
	})

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
// postlogd goes on writing to the renamed file until "postfix reload" ends
// it; the tailer reads that file for a while after it switched to the new
// one, so a line written there in that gap is not lost (assumptions A-33).
// The rename of an open file is what logrotate does on Linux; like
// TestFollowTailsAndRotates this runs on the server.
func TestFollowReadsTheRotatedFileAfterTheSwitch(t *testing.T) {
	old := pollInterval
	pollInterval = 10 * time.Millisecond
	t.Cleanup(func() { pollInterval = old })

	path := filepath.Join(t.TempDir(), "mail.log")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cs := &captureStore{}
	stop := startRun(t, path, cs)
	defer stop()
	appendLine(t, path, "host postfix/smtp[1]: Q1: to=<a@example.net>, relay=x, dsn=2.0.0, status=sent (ok)")
	waitFor(t, func() bool { return contains(cs.snapshot(), "Q1|a@example.net|sent") })

	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	appendLine(t, path, "host postfix/smtp[1]: Q2: to=<b@example.net>, relay=x, dsn=2.0.0, status=sent (ok)")
	waitFor(t, func() bool { return contains(cs.snapshot(), "Q2|b@example.net|sent") })
	// The switch is made; postlogd has not reloaded yet.
	appendLine(t, path+".1", "host postfix/smtp[1]: Q3: to=<c@example.net>, relay=x, dsn=2.0.0, status=sent (ok)")
	waitFor(t, func() bool { return contains(cs.snapshot(), "Q3|c@example.net|sent") })
}

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
	appendLine(t, path, "host postfix/smtp[1]: Q1: to=<a@example.net>, relay=x, dsn=2.0.0, status=sent (ok)")
	waitFor(t, func() bool { return contains(cs.snapshot(), "Q1|a@example.net|sent") })
	stop() // persists the offset past Q1

	// Down: Postfix keeps delivering.
	appendLine(t, path, "host postfix/smtp[1]: Q2: to=<b@example.net>, relay=x, dsn=2.0.0, status=sent (ok)")

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
		"host postfix/smtp[1]: Q3: to=<c@example.net>, relay=x, dsn=5.1.1, status=bounced (nope)\n"), 0o644); err != nil {
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
	go func() { done <- Run(ctx, path, cs, func() int { return 90 }, nil, nil) }()
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

// stubQueue makes the reconcile sweep answer from a fixed list of queue ids
// instead of a running Postfix, or fail if err is non-nil.
func stubQueue(t *testing.T, err error, ids ...string) *int {
	t.Helper()
	calls := 0
	old := queueIDs
	queueIDs = func() (map[string]struct{}, error) {
		calls++
		if err != nil {
			return nil, err
		}
		set := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			set[id] = struct{}{}
		}
		return set, nil
	}
	t.Cleanup(func() { queueIDs = old })
	return &calls
}

// A row stays "queued" forever when its delivery lines are gone for good. The
// queue is what settles it: a message Postfix still holds is simply in flight
// and must be left alone, while one it no longer holds will never be reported
// on and is closed.
func TestReconcileClosesOnlyWhatPostfixNoLongerHolds(t *testing.T) {
	stubQueue(t, nil, "STILLQ")

	cs := &captureStore{queued: []store.QueuedDelivery{
		{ID: 1, QueueID: "STILLQ", To: "inflight@example.net"},
		{ID: 2, QueueID: "GONEQ", To: "lost@example.net"},
	}}
	reconcile(cs, time.Now().UTC(), "")

	got := cs.snapshot()
	if len(got) != 1 || got[0] != "GONEQ|lost@example.net|"+store.ReasonReconcile {
		t.Fatalf("got %v, want only the message Postfix dropped closed by the sweep", got)
	}
	if cs.closed != 1 || cs.missed != 0 {
		t.Fatalf("the day counts %d closed and %d missed, want 1 and 0", cs.closed, cs.missed)
	}
}

// A closed row whose delivery line is in the log after all - here in the file
// rotated last - is counted as missed: the tailer did not read it.
func TestReconcileCountsALineTheTailerMissed(t *testing.T) {
	stubQueue(t, nil)
	dir := t.TempDir()
	path := filepath.Join(dir, "mail.log")
	if err := os.WriteFile(path, []byte("Oct 14 11:00:00 mail postfix/qmgr[10]: OTHERQ: removed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".1", []byte("Oct 14 10:59:58 mail postfix/smtp[26]: SEENQ: to=<Seen@example.net>, relay=mx[203.0.113.9]:25, dsn=2.0.0, status=sent (250 OK)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cs := &captureStore{queued: []store.QueuedDelivery{
		{ID: 1, QueueID: "SEENQ", To: "seen@example.net"},
		{ID: 2, QueueID: "GONEQ", To: "lost@example.net"},
	}}
	reconcile(cs, time.Now().UTC(), path)
	if cs.closed != 2 || cs.missed != 1 {
		t.Fatalf("closed %d, missed %d; want 2 and 1", cs.closed, cs.missed)
	}
}

// Each sweep records how many rows are queued past the queue lifetime, as a
// level for the day.
func TestCountStuckRecordsTheLevel(t *testing.T) {
	cs := &captureStore{stuckCount: 3}
	countStuck(cs, time.Now(), time.Hour)
	cs.stuckCount = 0
	countStuck(cs, time.Now(), time.Hour)
	if len(cs.stuck) != 2 || cs.stuck[0] != 3 || cs.stuck[1] != 0 {
		t.Fatalf("levels recorded = %v, want [3 0]", cs.stuck)
	}
}

// The tailer's blind time: a rotation's one-tick gap is not counted, a longer
// stretch is, a stretch that lasts is stored a minute at a time from where the
// last part ended, and reading again ends it.
func TestBlindnessCountsStretchesThatMatter(t *testing.T) {
	type part struct {
		from time.Time
		d    time.Duration
	}
	var got []part
	b := &blindness{add: func(from time.Time, d time.Duration) error {
		got = append(got, part{from, d})
		return nil
	}}
	t0 := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }

	b.see(at(0), true) // rotation: gone for a tick
	b.see(at(1), false)
	b.see(at(2), false)
	if len(got) != 0 {
		t.Fatalf("a one-second gap was counted: %v", got)
	}

	for s := 10; s <= 80; s++ { // blind for 75 s
		b.see(at(s), true)
	}
	b.see(at(85), false)
	want := []part{{at(10), 60 * time.Second}, {at(70), 15 * time.Second}}
	if len(got) != len(want) {
		t.Fatalf("parts = %v, want %v", got, want)
	}
	for i := range want {
		if !got[i].from.Equal(want[i].from) || got[i].d != want[i].d {
			t.Errorf("part %d = %v, want %v", i, got[i], want[i])
		}
	}
}

// A queue that cannot be listed says nothing about any message — treating the
// failure as an empty queue would close every stale row at once.
func TestReconcileLeavesRowsAloneWhenTheQueueCannotBeRead(t *testing.T) {
	stubQueue(t, errors.New("postqueue: Permission denied"))

	cs := &captureStore{queued: []store.QueuedDelivery{
		{ID: 1, QueueID: "GONEQ", To: "lost@example.net"},
	}}
	reconcile(cs, time.Now().UTC(), "")

	if got := cs.snapshot(); len(got) != 0 {
		t.Fatalf("got %v, want no row touched", got)
	}
}

// With nothing stale to explain there is no reason to shell out to postqueue at
// all — which is the normal state of a relay that is keeping up.
func TestReconcileSkipsTheQueueWhenNoRowIsStuck(t *testing.T) {
	calls := stubQueue(t, nil)

	reconcile(&captureStore{}, time.Now().UTC(), "")

	if *calls != 0 {
		t.Fatalf("queue listed %d time(s), want none", *calls)
	}
}

// A delivery's page shows what Postfix wrote about that one message, so the
// read has to pick its queue-id's lines out of everything else the mail path
// logged around them — and only its own: queue ids are hexadecimal runs, and a
// shorter one is regularly the tail of a longer one.
func TestQueueLinesPicksOutOneMessage(t *testing.T) {
	path := writeLog(t,
		"host postfix/smtpd[20]: 41E862C00D9E: client=mail.example.com[203.0.113.4]",
		"host postfix/qmgr[10]: 5900C2C00D9E: from=<other@example.com>, size=500, nrcpt=1 (queue active)",
		"host postfix/cleanup[15]: 41E862C00D9E: message-id=<abc@example.com>",
		// Same run of characters, longer id: not this message.
		"host postfix/smtp[26]: FF41E862C00D9E: to=<z@example.net>, status=sent (250 OK)",
		"host postfix/qmgr[10]: 41E862C00D9E: from=<a@example.com>, size=500, nrcpt=1 (queue active)",
		"host postfix/smtp[26]: 41E862C00D9E: to=<a@example.net>, relay=mx.example.net[203.0.113.9]:25, dsn=2.0.0, status=sent (250 OK)",
	)

	lines, err := QueueLines(path, "41E862C00D9E", 200)
	if err != nil {
		t.Fatalf("QueueLines: %v", err)
	}
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	// Oldest first: the page reads the message's history downwards.
	if !strings.Contains(lines[0], "client=") || !strings.Contains(lines[3], "status=sent") {
		t.Errorf("lines are not in the order they were logged:\n%s", strings.Join(lines, "\n"))
	}
	for _, line := range lines {
		if strings.Contains(line, "FF41E862C00D9E") || strings.Contains(line, "5900C2C00D9E") {
			t.Errorf("another message's line came back: %q", line)
		}
	}
}

// A message the milter refused has no queue id, so there is nothing to match
// on — every line in the log would be someone else's.
func TestQueueLinesWithoutAQueueIDMatchesNothing(t *testing.T) {
	path := writeLog(t, "host postfix/smtp[26]: 41E862C00D9E: to=<a@example.net>, status=sent (250 OK)")

	lines, err := QueueLines(path, "", 200)
	if err != nil {
		t.Fatalf("QueueLines: %v", err)
	}
	if lines != nil {
		t.Errorf("got %v, want no lines", lines)
	}
}

// The cap keeps the newest lines, not the first ones: what a message did last
// is what its page is opened for.
func TestQueueLinesCapKeepsTheLatest(t *testing.T) {
	var log []string
	for i := 0; i < 10; i++ {
		log = append(log, "host postfix/smtp[26]: ABC123: attempt "+itoa(i))
	}
	path := writeLog(t, log...)

	lines, err := QueueLines(path, "ABC123", 3)
	if err != nil {
		t.Fatalf("QueueLines: %v", err)
	}
	if len(lines) != 3 || !strings.HasSuffix(lines[0], "attempt 7") || !strings.HasSuffix(lines[2], "attempt 9") {
		t.Errorf("cap did not keep the last three:\n%s", strings.Join(lines, "\n"))
	}
}

// Send-log rows outlive mail.log — retention is ninety days and rotation keeps
// fourteen files — so a message whose lines are gone, or a log that is between
// rotations and absent altogether, is an empty answer for the page to explain,
// not an error for it to report.
func TestQueueLinesOnAMessageWithNoLinesLeft(t *testing.T) {
	path := writeLog(t, "host postfix/smtp[26]: 5900C2C00D9E: to=<z@example.net>, status=sent (250 OK)")

	lines, err := QueueLines(path, "41E862C00D9E", 200)
	if err != nil {
		t.Fatalf("QueueLines: %v", err)
	}
	if lines != nil {
		t.Errorf("got %v, want no lines", lines)
	}
}

// writeLog creates a mail.log holding the given lines and returns its path.
func writeLog(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mail.log")
	body := ""
	for _, line := range lines {
		body += line + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	return path
}

func itoa(n int) string { return strconv.Itoa(n) }

// The read is bounded, so on a log that has grown all day it starts in the
// middle of a line. That fragment is not a line and must not come back as one,
// and anything before the budget is out of reach — which the page reports the
// same way as a message whose lines have rotated away.
func TestQueueLinesReadsABoundedTail(t *testing.T) {
	old := queueScanBytes
	queueScanBytes = 120
	t.Cleanup(func() { queueScanBytes = old })

	path := writeLog(t,
		"host postfix/smtp[26]: ABC123: too far back to reach, padded out past the budget with this run of filler text",
		"host postfix/smtp[26]: ABC123: within the budget",
	)

	lines, err := QueueLines(path, "ABC123", 200)
	if err != nil {
		t.Fatalf("QueueLines: %v", err)
	}
	if len(lines) != 1 || !strings.HasSuffix(lines[0], "within the budget") {
		t.Errorf("got %d lines, want only the one inside the budget:\n%s", len(lines), strings.Join(lines, "\n"))
	}
}

// The delivery page shows a message's lines as a table of when and what, so the
// timestamp has to come off the head of the line — in either of the two formats
// a mail log arrives in — and nothing may be lost doing it.
func TestSplitTimestamp(t *testing.T) {
	cases := []struct {
		name        string
		line        string
		stamp, rest string
	}{
		{
			// An RFC 3339 stamp is not one Postfix 3.10 writes (assumptions
			// A-12): it is not taken off.
			name:  "RFC 3339 is not a stamp Postfix writes",
			line:  "2026-08-03T05:15:52.219218+00:00 mail postfix/qmgr[10]: 4A1B2C3D: removed",
			stamp: "",
			rest:  "2026-08-03T05:15:52.219218+00:00 mail postfix/qmgr[10]: 4A1B2C3D: removed",
		},
		{
			name:  "syslog's traditional format, space-padded day",
			line:  "Aug  3 05:15:52 mail postfix/smtpd[20]: 4A1B2C3D: client=app.example.ru[203.0.113.4]",
			stamp: "Aug  3 05:15:52",
			rest:  "mail postfix/smtpd[20]: 4A1B2C3D: client=app.example.ru[203.0.113.4]",
		},
		{
			// Copied off the live relay (Postfix 3.7, which has no
			// maillog_file_format), so this is the shape the panel actually
			// meets: zero-padded day, and the host is the container's name.
			// Seen on the live relay, date not recorded. The other cases
			// here are composed.
			name:  "syslog's traditional format as the live relay writes it",
			line:  "Aug 08 07:26:41 selfpost postfix/master[231]: daemon started -- version 3.7.11, configuration /etc/postfix",
			stamp: "Aug 08 07:26:41",
			rest:  "selfpost postfix/master[231]: daemon started -- version 3.7.11, configuration /etc/postfix",
		},
		{
			name:  "unrecognised head keeps the whole line",
			line:  "mail postfix/smtp[26]: 4A1B2C3D: to=<a@example.net>, status=sent (250 OK)",
			stamp: "",
			rest:  "mail postfix/smtp[26]: 4A1B2C3D: to=<a@example.net>, status=sent (250 OK)",
		},
		{
			// A date-like run that is not the head of the line is not a stamp.
			name:  "date inside the text is left alone",
			line:  "mail postfix/smtp[26]: ABC: 220 mx.example.net ready at 2026-08-03T05:15:52+00:00",
			stamp: "",
			rest:  "mail postfix/smtp[26]: ABC: 220 mx.example.net ready at 2026-08-03T05:15:52+00:00",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stamp, rest := SplitTimestamp(c.line)
			if stamp != c.stamp || rest != c.rest {
				t.Errorf("SplitTimestamp(%q) = (%q, %q), want (%q, %q)", c.line, stamp, rest, c.stamp, c.rest)
			}
			// Whatever the split, the line's own text survives it whole.
			if !strings.Contains(c.line, rest) {
				t.Errorf("the text column is not part of the line it came from: %q", rest)
			}
		})
	}
}

// A log rotated while the panel was down is read to its end before the new
// one: what Postfix wrote between the stop and the rotation is in mail.log.1
// (audit 2026-10-12, stage 2b). Both halves are read, once each.
func TestALogRotatedWhileDownIsReadToItsEnd(t *testing.T) {
	old := pollInterval
	pollInterval = 10 * time.Millisecond
	t.Cleanup(func() { pollInterval = old })

	dir := t.TempDir()
	path := filepath.Join(dir, "mail.log")
	seed := strings.Repeat("host postfix/qmgr[1]: seed line, not a delivery\n", 20)
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	cs := &captureStore{}
	stop := startRun(t, path, cs)
	appendLine(t, path, "host postfix/smtp[1]: Q1: to=<a@example.net>, relay=x, dsn=2.0.0, status=sent (ok)")
	waitFor(t, func() bool { return contains(cs.snapshot(), "Q1|a@example.net|sent") })
	stop()

	// Down: Postfix writes, then logrotate renames and creates, and Postfix
	// goes on in the new file.
	appendLine(t, path, "host postfix/smtp[1]: Q2: to=<b@example.net>, relay=x, dsn=2.0.0, status=sent (ok)")
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("host postfix/qmgr[1]: the new log\n", 20)+
		"host postfix/smtp[1]: Q3: to=<c@example.net>, relay=x, dsn=5.1.1, status=bounced (nope)\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cs.reset()
	stop = startRun(t, path, cs)
	waitFor(t, func() bool {
		got := cs.snapshot()
		return contains(got, "Q2|b@example.net|sent") && contains(got, "Q3|c@example.net|bounced")
	})
	stop()
	n := 0
	for _, c := range cs.snapshot() {
		if strings.HasPrefix(c, "Q1|") {
			t.Errorf("Q1 was read again: %s", c)
		}
		if strings.HasPrefix(c, "Q2|") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("Q2 read %d times, want once", n)
	}
}

// A mail.log.1 that is not the file the saved place belongs to is not read
// from that place: the new file is read from its start and the gap is said.
func TestARotatedLogThatIsNotTheSavedOneIsNotRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mail.log")
	if err := os.WriteFile(path+".1", []byte(strings.Repeat("host postfix/qmgr[1]: another file altogether\n", 20)+
		"host postfix/smtp[1]: QX: to=<x@example.net>, relay=x, dsn=2.0.0, status=sent (ok)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tr := &tracker{path: path, behind: true, prev: store.LogtailState{Fingerprint: "not-this-one", Offset: 10}}
	var read []string
	if tr.readRotated(func(l string) { read = append(read, l) }) || len(read) != 0 {
		t.Fatalf("read %d lines of a rotated file that is not the saved one", len(read))
	}
}

// A fingerprint saved before it carried the file's identity still matches the
// file by its hash.
func TestSameLogAcceptsAFingerprintOfBefore(t *testing.T) {
	if !sameLog("abc", "abc:1.2") || !sameLog("abc:1.2", "abc:1.2") || sameLog("abc:1.2", "abc:1.3") || sameLog("", "abc") {
		t.Fatal("sameLog")
	}
}
