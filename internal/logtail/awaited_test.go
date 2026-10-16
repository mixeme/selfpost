package logtail

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The test message's row learns its queue id from cleanup's Message-ID line
// - once, for the id awaited, and from no other program's line or shape.
func TestAwaitedMessageID(t *testing.T) {
	id := "<9f3c01.1760600000@mail.example.org>"
	AwaitMessageID(id, 42)
	for _, line := range []string{
		"selfpost postfix/smtpd[52]: 4j5qN53nwRz791P4: message-id=" + id,
		"selfpost postfix/cleanup[52]: 4j5qN53nwRz791P4: message-id=" + id + " trailing",
		"selfpost postfix/cleanup[52]: 4j5qN53nwRz791P4: message-id=<other@mail.example.org>",
	} {
		if q, row, ok := takeAwaited(line); ok {
			t.Fatalf("%q took row %d with %s", line, row, q)
		}
	}
	q, row, ok := takeAwaited("selfpost postfix/cleanup[52]: 4j5qN53nwRz791P4: message-id=" + id)
	if !ok || q != "4j5qN53nwRz791P4" || row != 42 {
		t.Fatalf("cleanup's line = %q, %d, %v", q, row, ok)
	}
	if _, _, ok := takeAwaited("selfpost postfix/cleanup[52]: 4j5qN53nwRz791P4: message-id=" + id); ok {
		t.Fatal("the same id was taken twice")
	}
	AwaitMessageID(id, 43)
	ForgetMessageID(id)
	if _, _, ok := takeAwaited("selfpost postfix/cleanup[52]: X: message-id=" + id); ok {
		t.Fatal("a forgotten id was taken")
	}
}

// Run puts the queue id on the row as the line is read.
func TestRunGivesTheTestMessageItsQueueID(t *testing.T) {
	old := pollInterval
	pollInterval = 10 * time.Millisecond
	t.Cleanup(func() { pollInterval = old })
	path := filepath.Join(t.TempDir(), "mail.log")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cs := &captureStore{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, path, cs, func() int { return 90 }, nil, nil) }()
	time.Sleep(50 * time.Millisecond)
	AwaitMessageID("<t1@mail.example.org>", 7)
	appendLine(t, path, "Oct 17 09:00:00 host postfix/cleanup[3]: Q7long: message-id=<t1@mail.example.org>")
	waitFor(t, func() bool { return contains(cs.snapshot(), "7|Q7long|queue-id") })
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
