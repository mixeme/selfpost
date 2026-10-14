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
	postfix := func(started time.Time, running bool) func() postfixState {
		return func() postfixState { return postfixState{started: started, running: running, known: true} }
	}
	silent := func() postfixState { return postfixState{} }
	down := func(ask func() postfixState) (time.Duration, bool) { return senderCheckDowntime(mark, now, ask) }

	if d, _ := down(postfix(now.Add(-time.Hour), true)); d != 0 {
		t.Errorf("no mark: %s", d)
	}
	set(now.Add(-40 * time.Second))
	if d, est := down(postfix(now.Add(-time.Hour), true)); d != 40*time.Second || est {
		t.Errorf("Postfix up throughout: %s %v, want 40s, not estimated", d, est)
	}
	if d, _ := down(postfix(now.Add(-10*time.Second), true)); d != 10*time.Second {
		t.Errorf("Postfix started within the gap: %s, want 10s", d)
	}
	if d, _ := down(postfix(time.Time{}, false)); d != 0 {
		t.Errorf("Postfix not running: %s", d)
	}
	// supervisord did not answer: the time since the mark, an estimate.
	if d, est := down(silent); d != 40*time.Second || !est {
		t.Errorf("supervisord silent: %s %v, want 40s, estimated", d, est)
	}
	set(now.Add(time.Minute)) // a clock set back
	if d, _ := down(postfix(now.Add(-time.Hour), true)); d != 0 {
		t.Errorf("a mark in the future: %s", d)
	}
}

// Downtime is booked on the days it fell on.
func TestByDay(t *testing.T) {
	at := func(d, h, m int) time.Time { return time.Date(2026, 10, d, h, m, 0, 0, time.UTC) }
	got := byDay(at(12, 23, 50), at(14, 0, 10))
	want := []dayPart{{at(12, 23, 50), 10 * time.Minute}, {at(13, 0, 0), 24 * time.Hour}, {at(14, 0, 0), 10 * time.Minute}}
	if len(got) != len(want) {
		t.Fatalf("parts = %+v", got)
	}
	for i := range want {
		if !got[i].day.Equal(want[i].day) || got[i].down != want[i].down {
			t.Errorf("part %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if one := byDay(at(12, 10, 0), at(12, 10, 0).Add(40*time.Second)); len(one) != 1 || one[0].down != 40*time.Second {
		t.Errorf("within a day: %+v", one)
	}
	if none := byDay(at(12, 10, 0), at(12, 10, 0)); len(none) != 0 {
		t.Errorf("no time: %+v", none)
	}
}
