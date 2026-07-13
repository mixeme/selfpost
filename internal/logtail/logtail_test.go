package logtail

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"codeberg.org/mix/selfpost/internal/store"
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

// captureStore records UpdateStatus calls for the follow integration test.
type captureStore struct {
	mu    sync.Mutex
	calls []string
}

func (c *captureStore) UpdateStatus(queueID, recipient, status string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, queueID+"|"+recipient+"|"+status)
	return 1, nil
}

func (c *captureStore) DeleteSendLogBefore(time.Time) (int64, error) { return 0, nil }

func (c *captureStore) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.calls...)
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
