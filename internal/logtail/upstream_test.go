package logtail

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The host and the reply of a delivery line, as log_adhoc() writes them: the
// relay= right after to=, the "(...)" after the first status= to the end of
// the line - also when the far end's reply holds a status= of its own.
func TestDeliveryReply(t *testing.T) {
	for _, c := range []struct{ line, relay, reply string }{
		{"selfpost postfix/smtp[315]: 82922153D147: to=<carrel@dev.mixdep.ru>, relay=mc-ent.mixfed.ru[188.242.125.61]:25, delay=498, delays=491/0.03/2.5/5, dsn=2.0.0, status=sent (250 2.0.0 Ok: queued as CE05D10261D)",
			"mc-ent.mixfed.ru[188.242.125.61]:25", "250 2.0.0 Ok: queued as CE05D10261D"},
		{"selfpost postfix/smtp[300]: D6E4F153D147: to=<a@in.example.com>, relay=none, delay=30, delays=0.1/0/30/0, dsn=4.4.1, status=deferred (connect to mx.up.example[198.51.100.5]:25: Connection refused)",
			"none", "connect to mx.up.example[198.51.100.5]:25: Connection refused"},
		{"selfpost postfix/smtp[312]: 1CACF153D147: to=<a@in.example.com>, orig_to=<A@in.example.com>, relay=mx.up.example[198.51.100.5]:25, conn_use=2, delay=1.9, dsn=5.7.1, status=bounced (host mx.up.example[198.51.100.5] said: 550 5.7.1 not status=sent (in reply to end of DATA command))",
			"mx.up.example[198.51.100.5]:25", "host mx.up.example[198.51.100.5] said: 550 5.7.1 not status=sent (in reply to end of DATA command)"},
		{"selfpost postfix/qmgr[286]: 1484E1539884: removed", "", ""},
	} {
		relay, reply := deliveryReply(c.line)
		if relay != c.relay || reply != c.reply {
			t.Errorf("deliveryReply(%q) =\n  %q, %q\nwant\n  %q, %q", c.line, relay, reply, c.relay, c.reply)
		}
	}
}

// The journal takes a delivery line's result with the host ("" for relay=none),
// the reply made printable and cut, and the line's own time.
func TestInboundJournalTakesTheUpstreamsAnswer(t *testing.T) {
	j, st := inboundFixture()
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	j.upstream("Oct 16 10:16:42 selfpost postfix/smtp[300]: P25A: to=<a@in.example.com>, relay=none, delay=30, dsn=4.4.1, status=deferred (connect to mx.up.example[198.51.100.5]:25: Connection refused\x01"+strings.Repeat("x", 300)+")",
		now, "P25A", "a@in.example.com", "deferred")
	calls := st.upstreamCalls()
	if len(calls) != 1 {
		t.Fatalf("calls = %q", calls)
	}
	f := strings.Split(calls[0], "|")
	if f[0] != "P25A" || f[1] != "a@in.example.com" || f[2] != "deferred" || f[3] != "" {
		t.Errorf("call = %q", calls[0])
	}
	if !strings.HasPrefix(f[4], "connect to mx.up.example[198.51.100.5]:25: Connection refused") || len(f[4]) > 255 || strings.ContainsRune(f[4], 1) {
		t.Errorf("reply = %q (%d bytes)", f[4], len(f[4]))
	}
	if at, _ := time.Parse(time.RFC3339, f[5]); !at.Equal(time.Date(2026, 10, 16, 10, 16, 42, 0, time.Local).UTC()) {
		t.Errorf("at = %s, want the line's time", f[5])
	}
}

// Run hands the journal what no send-log row took: a delivery line and an
// expiry line of a port-25 message (captureStore takes no queue id with
// "P25"), and nothing of the mail the send log knows.
func TestRunPutsTheUpstreamsAnswerOnTheJournal(t *testing.T) {
	old := pollInterval
	pollInterval = 10 * time.Millisecond
	t.Cleanup(func() { pollInterval = old })

	path := filepath.Join(t.TempDir(), "mail.log")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cs := &captureStore{}
	_, in := inboundFixture()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, path, cs, func() int { return 90 }, &Inbound{Store: in, Retention: func() int { return 90 }}, nil)
	}()
	time.Sleep(50 * time.Millisecond)
	appendLine(t, path, "host postfix/smtp[1]: Q1: to=<a@example.net>, relay=x, dsn=2.0.0, status=sent (ok)")
	appendLine(t, path, "host postfix/smtp[2]: P25A: to=<a@in.example.com>, relay=mx.up.example[198.51.100.5]:25, dsn=2.0.0, status=sent (250 2.0.0 Ok)")
	appendLine(t, path, "host postfix/qmgr[3]: P25B: from=<s@example.net>, status=expired, returned to sender")
	waitFor(t, func() bool { return len(in.upstreamCalls()) == 2 })
	calls := in.upstreamCalls()
	if !strings.HasPrefix(calls[0], "P25A|a@in.example.com|sent|mx.up.example[198.51.100.5]:25|250 2.0.0 Ok|") ||
		!strings.HasPrefix(calls[1], "P25B|expired|") {
		t.Errorf("journal calls = %q", calls)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	for _, c := range in.upstreamCalls() {
		if strings.HasPrefix(c, "Q1|") {
			t.Errorf("the journal was offered outbound mail: %q", c)
		}
	}
}

// A stranger's quoted RCPT TO may hold ">", "," and "=": read by Postfix's
// quoting, the address is his whole local part, and the line's own fields -
// the relay and status after it - are the ones taken, not those he wrote.
func TestDeliveryLineReadsAQuotedRecipientWhole(t *testing.T) {
	line := `Oct 16 10:16:42 selfpost postfix/smtp[300]: P25F: to=<"a>, orig_to=<b@in.example.com>, relay=F, dsn=5.0.0, status=bounced (FORGED"@in.example.com>, relay=mx.up.example[198.51.100.5]:25, delay=1, dsn=2.0.0, status=sent (250 2.0.0 Ok)`
	q, r, s, ok := parseDelivery(line)
	if !ok || q != "P25F" || s != "sent" || r != `"a>, orig_to=<b@in.example.com>, relay=F, dsn=5.0.0, status=bounced (FORGED"@in.example.com` {
		t.Errorf("parseDelivery = %q %q %q %v", q, r, s, ok)
	}
	_, rest := SplitTimestamp(line)
	if relay, reply := deliveryReply(rest); relay != "mx.up.example[198.51.100.5]:25" || reply != "250 2.0.0 Ok" {
		t.Errorf("deliveryReply = %q, %q", relay, reply)
	}
}

// cleanup's "removed (" is a queue file that was never queued: the rows the
// milter wrote for it are detached from its queue id.
func TestInboundJournalForgetsAMessageCleanupRemoved(t *testing.T) {
	j, st := inboundFixture()
	j.handle("Oct 16 10:16:42 selfpost postfix/cleanup[52]: 4A1B2C3D: removed (canceled)", tailNow, false)
	j.handle("Oct 16 10:16:42 selfpost postfix/qmgr[52]: 5A1B2C3D: removed", tailNow, false)
	if calls := st.upstreamCalls(); len(calls) != 1 || calls[0] != "4A1B2C3D|forgot" {
		t.Errorf("calls = %q", calls)
	}
}
