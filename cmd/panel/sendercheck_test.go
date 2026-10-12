package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The check's downtime is the time since its last mark, under a Postfix that
// was running: a new container has no mark, and a Postfix that is not up, or
// came up later than the mark, was not kept waiting by the check for all of
// that time.
func TestSenderCheckDowntime(t *testing.T) {
	now := time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC)
	mark := filepath.Join(t.TempDir(), "sender.sock.alive")
	set := func(at time.Time) {
		t.Helper()
		if err := os.WriteFile(mark, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(mark, at, at); err != nil {
			t.Fatal(err)
		}
	}
	postfix := func(started time.Time, running bool) func() (time.Time, bool) {
		return func() (time.Time, bool) { return started, running }
	}

	if d := senderCheckDowntime(mark, now, postfix(now.Add(-time.Hour), true)); d != 0 {
		t.Errorf("no mark: %s", d)
	}
	set(now.Add(-40 * time.Second))
	if d := senderCheckDowntime(mark, now, postfix(now.Add(-time.Hour), true)); d != 40*time.Second {
		t.Errorf("Postfix up throughout: %s, want 40s", d)
	}
	if d := senderCheckDowntime(mark, now, postfix(now.Add(-10*time.Second), true)); d != 10*time.Second {
		t.Errorf("Postfix started within the gap: %s, want 10s", d)
	}
	if d := senderCheckDowntime(mark, now, postfix(time.Time{}, false)); d != 0 {
		t.Errorf("Postfix not running: %s", d)
	}
	set(now.Add(time.Minute)) // a clock set back
	if d := senderCheckDowntime(mark, now, postfix(now.Add(-time.Hour), true)); d != 0 {
		t.Errorf("a mark in the future: %s", d)
	}
}
