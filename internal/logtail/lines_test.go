package logtail

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Which lines are kept for a message's page, and under which queue id: the id
// at the head of the line and nothing the far end wrote further along it.
func TestQueueLineHead(t *testing.T) {
	cases := []struct {
		name, rest, want string
	}{
		{"delivery", "mail postfix/smtp[26]: 41E862C00D9E: to=<a@example.net>, relay=mx.example.net[203.0.113.9]:25, dsn=2.0.0, status=sent (250 OK)", "41E862C00D9E"},
		{"submission smtpd opens the id", "mail postfix/submission/smtpd[10]: 41E862C00D9E: client=unknown[203.0.113.7], sasl_method=PLAIN, sasl_username=app", "41E862C00D9E"},
		{"port-25 smtpd", "mail postfix/inbound/smtpd[41]: 4A1B2C3D: client=mx.example.net[203.0.113.9]", "4A1B2C3D"},
		{"cleanup", "mail postfix/cleanup[52]: 41E862C00D9E: message-id=<1@example.com>", "41E862C00D9E"},
		{"qmgr removes it", "mail postfix/qmgr[10]: 41E862C00D9E: removed", "41E862C00D9E"},
		{"a bounce is sent: the id at the head, not the new message's", "mail postfix/bounce[30]: 41E862C00D9E: sender non-delivery notification: 5F00AA12", "41E862C00D9E"},
		{"long queue id", "mail postfix/qmgr[10]: 3sJ8Xh2Cgqz4Xy1: removed", "3sJ8Xh2Cgqz4Xy1"},
		{"refused before queueing", "mail postfix/inbound/smtpd[41]: NOQUEUE: reject: RCPT from mx.example.net[203.0.113.9]: 550 5.1.1 <n@in.example.com>: Recipient address rejected; from=<a@example.net> to=<n@in.example.com> proto=ESMTP helo=<mx.example.net>", ""},
		{"a warning", "mail postfix/smtpd[10]: warning: hostname mx.example.net does not resolve to address 203.0.113.9", ""},
		{"a connection", "mail postfix/smtpd[10]: connect from unknown[203.0.113.7]", ""},
		{"no program", "preexisting line, ignored on start", ""},
		{
			// The reply is the receiving server's text. One that spells the head
			// of a line is still the tail of this one: the line is the message's
			// it was written about, and no other's.
			"a reply that quotes a line head",
			"mail postfix/smtp[26]: 9F1A2C00D9E: to=<a@example.net>, relay=mx.example.net[203.0.113.9]:25, dsn=5.1.1, status=bounced (host said: 550 mail postfix/smtp[1]: 41E862C00D9E: to=<b@example.net>, status=sent)",
			"9F1A2C00D9E",
		},
	}
	for _, c := range cases {
		got := ""
		if m := queueLineRe.FindStringSubmatch(c.rest); m != nil {
			got = m[2]
		}
		if got != c.want {
			t.Errorf("%s: queue id %q, want %q", c.name, got, c.want)
		}
	}
}

// A line is dated by its own stamp, not by when the tailer got to it, and is
// kept as the log has it. Nothing is written until the pass over the log ends.
func TestLineKeeperDatesByTheLine(t *testing.T) {
	cs := &captureStore{}
	k := newLineKeeper(cs)
	now := time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC)
	line := "Oct 12 09:00:00 mail postfix/qmgr[7]: 41E862C00D9E: removed"

	k.add(line, now, false)
	k.add("Oct 12 09:00:01 mail postfix/smtpd[7]: connect from unknown[203.0.113.7]", now, false)
	if len(cs.lines) != 0 {
		t.Fatalf("written before the pass ended: %v", cs.kept())
	}
	k.flush()

	if len(cs.lines) != 1 {
		t.Fatalf("kept %d lines, want 1: %v", len(cs.lines), cs.kept())
	}
	got := cs.lines[0]
	if got.QueueID != "41E862C00D9E" || got.Line != line {
		t.Errorf("kept %q under %q", got.Line, got.QueueID)
	}
	if want := time.Date(2026, 10, 12, 9, 0, 0, 0, time.Local).UTC(); !got.At.Equal(want) {
		t.Errorf("dated %v, want %v", got.At, want)
	}
}

// A backlog is written in batches of lineBatch, not held whole, and a pass
// that changes from catching up to following does not write the later lines
// under the earlier rule.
func TestLineKeeperWritesInBatches(t *testing.T) {
	cs := &captureStore{}
	k := newLineKeeper(cs)
	now := time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC)
	line := func(i int) string {
		return "host postfix/qmgr[7]: 41E862C00D9E: from=<a@example.com>, size=" + itoa(i) + ", nrcpt=1 (queue active)"
	}

	for i := 0; i < lineBatch+10; i++ {
		k.add(line(i), now, true)
	}
	if len(cs.lines) != lineBatch || cs.batches != 1 {
		t.Fatalf("after %d lines: %d written in %d batches, want %d in 1", lineBatch+10, len(cs.lines), cs.batches, lineBatch)
	}

	// The same line again, now as one read while following: it is a new line,
	// and the ten still held from the catch-up are written first, under theirs.
	k.add(line(0), now, false)
	k.flush()
	if got, want := len(cs.lines), lineBatch+11; got != want {
		t.Errorf("%d lines written, want %d", got, want)
	}
	if cs.batches != 3 {
		t.Errorf("%d batches, want 3", cs.batches)
	}
}

// Mail that comes in on port 25 is a stranger's and never gets a send-log row:
// none of its lines are kept, from the smtpd that opened the queue id to
// qmgr's "removed". The id is then anybody's again.
func TestLineKeeperLeavesOutPort25Mail(t *testing.T) {
	cs := &captureStore{}
	k := newLineKeeper(cs)
	now := time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC)

	const (
		outClient = "host postfix/submission/smtpd[10]: 41E862C00D9E: client=app.example.com[203.0.113.7], sasl_method=PLAIN, sasl_username=app"
		outSent   = "host postfix/smtp[26]: 41E862C00D9E: to=<a@example.net>, relay=mx.example.net[203.0.113.9]:25, dsn=2.0.0, status=sent (250 OK)"
		reused    = "host postfix/submission/smtpd[10]: 4A1B2C3D: client=app.example.com[203.0.113.7], sasl_method=PLAIN, sasl_username=app"
		bounce    = "host postfix/cleanup[52]: 5F00AA12: message-id=<20261012180000.5F00AA12@mail.example.com>"
	)
	for _, line := range []string{
		"host postfix/inbound/smtpd[41]: 4A1B2C3D: client=mx.example.net[198.51.100.9]",
		outClient,
		"host postfix/cleanup[52]: 4A1B2C3D: message-id=<whatever-the-stranger-wrote@example.net>",
		"host postfix/qmgr[10]: 4A1B2C3D: from=<a@example.net>, size=900, nrcpt=1 (queue active)",
		outSent,
		"host postfix/smtp[27]: 4A1B2C3D: to=<b@in.example.com>, relay=upstream.example.com[192.0.2.1]:25, dsn=2.0.0, status=sent (250 OK)",
		"host postfix/qmgr[10]: 4A1B2C3D: removed",
		// The id comes round again for a submitted message, and a bounce has no
		// smtpd at all: both are kept.
		reused,
		bounce,
	} {
		k.add(line, now, false)
	}
	k.flush()

	want := []string{"41E862C00D9E|" + outClient, "41E862C00D9E|" + outSent, "4A1B2C3D|" + reused, "5F00AA12|" + bounce}
	if got := cs.kept(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("kept:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// A port-25 id that a submission listener opens before qmgr's "removed" was
	// seen (the line was lost, the panel restarted) is the submitted message's.
	k.add("host postfix/inbound/smtpd[41]: 6C00BB34: client=mx.example.net[198.51.100.9]", now, false)
	again := "host postfix/smtps/smtpd[11]: 6C00BB34: client=app.example.com[203.0.113.7], sasl_method=PLAIN, sasl_username=app"
	k.add(again, now, false)
	k.flush()
	if got := cs.kept(); got[len(got)-1] != "6C00BB34|"+again {
		t.Errorf("a reused port-25 id was not kept for the submitted message: %v", got[len(got)-1])
	}

	// What is remembered of port 25 has a ceiling.
	for i := 0; i < port25IDs+50; i++ {
		k.rememberPort25("7D" + itoa(100000+i))
	}
	if len(k.port25) > port25IDs {
		t.Errorf("%d port-25 ids remembered, ceiling is %d", len(k.port25), port25IDs)
	}
}

// The tailer keeps queue-id lines as it follows the log, and after an unclean
// stop - its saved place lies before lines it had already read - it does not
// keep them a second time.
func TestRunKeepsQueueLinesOnce(t *testing.T) {
	old := pollInterval
	pollInterval = 10 * time.Millisecond
	t.Cleanup(func() { pollInterval = old })

	dir := t.TempDir()
	path := filepath.Join(dir, "mail.log")
	// Longer than a fingerprint, so the file is recognised across the restart.
	seed := strings.Repeat("host postfix/smtpd[1]: connect from unknown[203.0.113.7]\n", 20)
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed log: %v", err)
	}

	const (
		client = "host postfix/submission/smtpd[1]: 41E862C00D9E: client=unknown[203.0.113.7]"
		sent   = "host postfix/smtp[1]: 41E862C00D9E: to=<a@example.net>, dsn=2.0.0, status=sent (ok)"
		later  = "host postfix/qmgr[1]: 41E862C00D9E: removed"
	)

	cs := &captureStore{}
	stop := startRun(t, path, cs)
	appendLine(t, path, client)
	appendLine(t, path, "host postfix/smtpd[1]: disconnect from unknown[203.0.113.7]")
	appendLine(t, path, sent)
	waitFor(t, func() bool { return len(cs.kept()) == 2 })
	stop()

	// The process died before it saved its place: what it has is the place it
	// was at before those lines.
	cs.mu.Lock()
	cs.state.Offset = int64(len(seed))
	cs.mu.Unlock()
	appendLine(t, path, later)

	stop = startRun(t, path, cs)
	waitFor(t, func() bool { return len(cs.kept()) >= 3 })
	stop()

	want := []string{"41E862C00D9E|" + client, "41E862C00D9E|" + sent, "41E862C00D9E|" + later}
	if got := cs.kept(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("kept:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// Lines without a send-log row are removed at the start and after every
// send-log sweep, and only once they are old enough for the row to have been
// written.
func TestRetentionLoopPrunesLines(t *testing.T) {
	old := retentionInterval
	retentionInterval = 20 * time.Millisecond
	t.Cleanup(func() { retentionInterval = old })

	cs := &captureStore{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go retentionLoop(ctx, cs, func() int { return 90 })

	waitFor(t, func() bool { return cs.prunes() >= 3 })

	cs.mu.Lock()
	defer cs.mu.Unlock()
	for _, cutoff := range cs.pruned {
		if age := time.Since(cutoff); age < lineGrace || age > lineGrace+time.Minute {
			t.Errorf("prune cutoff %v ago, want %v", age, lineGrace)
		}
	}
}
