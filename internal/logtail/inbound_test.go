package logtail

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mixeme/selfpost/internal/store"
)

type fakeInboundStore struct {
	domains   map[string]bool
	rows      []store.InboundSpamEntry
	lookupErr error
	pruned    []int

	// upstream records the results put on rows, as
	// "queue-id|recipient|status|relay|reply|time" or "queue-id|expired|time";
	// Run writes it from its own goroutine.
	mu       sync.Mutex
	upstream []string
}

func (f *fakeInboundStore) UpdateInboundUpstream(queueID, recipient, status, relay, reply string, at time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upstream = append(f.upstream, strings.Join([]string{queueID, recipient, status, relay, reply, at.Format(time.RFC3339)}, "|"))
	return 1, nil
}

func (f *fakeInboundStore) ExpireInboundUpstream(queueID string, at time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upstream = append(f.upstream, queueID+"|expired|"+at.Format(time.RFC3339))
	return 1, nil
}

func (f *fakeInboundStore) ForgetInboundQueueID(queueID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upstream = append(f.upstream, queueID+"|forgot")
	return nil
}

func (f *fakeInboundStore) upstreamCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.upstream...)
}

func (f *fakeInboundStore) InsertInboundSpam(e store.InboundSpamEntry) error {
	f.rows = append(f.rows, e)
	return nil
}

// InsertInboundSpamOnce writes unless an equal row is there, like the store's.
func (f *fakeInboundStore) InsertInboundSpamOnce(e store.InboundSpamEntry) (bool, error) {
	for _, r := range f.rows {
		if r == e {
			return false, nil
		}
	}
	f.rows = append(f.rows, e)
	return true, nil
}

func (f *fakeInboundStore) IsInboundDomain(name string) (bool, error) {
	if f.lookupErr != nil {
		return false, f.lookupErr
	}
	return f.domains[name], nil
}

func (f *fakeInboundStore) PruneInboundSpam(_ time.Time, keep int) (int64, error) {
	f.pruned = append(f.pruned, keep)
	return 0, nil
}

func inboundFixture() (*inboundJournal, *fakeInboundStore) {
	st := &fakeInboundStore{domains: map[string]bool{"in.example.com": true}}
	return newInboundJournal(st), st
}

var tailNow = time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC)

// The mail.log lines in this file are composed by hand, after
// src/smtpd/smtpd_check.c log_whatsup(), src/cleanup/cleanup_milter.c
// cleanup_milter_apply() and src/cleanup/cleanup_api.c cleanup_flush()
// (Postfix 3.10.13), with the stamp Postfix 3.10 writes
// (src/util/msg_logger.c, "%b %d %H:%M:%S").
const (
	head       = "Oct 11 12:00:05 mail "
	unknownRcp = head + "postfix/inbound/smtpd[41]: NOQUEUE: reject: RCPT from mx.example.net[203.0.113.9]: 550 5.1.1 <nobody@in.example.com>: Recipient address rejected: User unknown in relay recipient table; from=<a@example.net> to=<nobody@in.example.com> proto=ESMTP helo=<mx.example.net>"
)

// Postfix's own refusal of a recipient at an inbound domain is a row, dated by
// its line.
func TestInboundRejectByPostfix(t *testing.T) {
	j, st := inboundFixture()
	j.handle(unknownRcp, tailNow, false)
	if len(st.rows) != 1 {
		t.Fatalf("rows = %+v", st.rows)
	}
	want := store.InboundSpamEntry{
		InboundDomain: "in.example.com", ClientIP: "203.0.113.9",
		From: "a@example.net", To: "nobody@in.example.com",
		Decision: store.SpamDecisionReject, Engine: store.SpamEnginePostfix,
		Detail: "550 5.1.1 <nobody@in.example.com>: Recipient address rejected: User unknown in relay recipient table",
		At:     time.Date(2026, 10, 11, 12, 0, 5, 0, time.UTC),
	}
	if st.rows[0] != want {
		t.Errorf("row = %+v\nwant  %+v", st.rows[0], want)
	}
}

func TestInboundTempfailByPostfix(t *testing.T) {
	j, st := inboundFixture()
	j.handle(head+"postfix/inbound/smtpd[41]: NOQUEUE: reject: RCPT from unknown[2001:db8::9]: 450 4.1.8 <a@nx.example>: Sender address rejected: Domain not found; from=<a@nx.example> to=<B@IN.example.com> proto=ESMTP helo=<x>", tailNow, false)
	if len(st.rows) != 1 || st.rows[0].Decision != store.SpamDecisionTempfail || st.rows[0].ClientIP != "2001:db8::9" || st.rows[0].InboundDomain != "in.example.com" {
		t.Errorf("rows = %+v", st.rows)
	}
}

// Postfix 3.10's log_whatsup() appends " sasl_method=%s", " sasl_username=%s"
// and " sasl_sender=%s" after the HELO name for a session that has them
// (src/smtpd/smtpd_check.c; RELEASE_NOTES, [Incompat 20241023]). Port 25 has
// SASL off, so it never writes them there; a line that had them, with the
// HELO name or without it, is still read for what comes before. Composed
// from that format.
func TestInboundRejectTakesTheSASLTail(t *testing.T) {
	for name, line := range map[string]string{
		"after the HELO name": head + "postfix/inbound/smtpd[41]: NOQUEUE: reject: RCPT from mx.example.net[203.0.113.9]: 550 5.1.1 <nobody@in.example.com>: Recipient address rejected: User unknown in relay recipient table; from=<a@example.net> to=<nobody@in.example.com> proto=ESMTP helo=<mx.example.net> sasl_method=PLAIN sasl_username=u sasl_sender=s",
		"with no HELO name":   head + "postfix/inbound/smtpd[41]: NOQUEUE: reject: RCPT from mx.example.net[203.0.113.9]: 550 5.1.1 <nobody@in.example.com>: Recipient address rejected: User unknown in relay recipient table; from=<a@example.net> to=<nobody@in.example.com> proto=ESMTP sasl_method=PLAIN sasl_username=u",
	} {
		j, st := inboundFixture()
		j.handle(line, tailNow, false)
		if len(st.rows) != 1 || st.rows[0].From != "a@example.net" || st.rows[0].To != "nobody@in.example.com" ||
			st.rows[0].Detail != "550 5.1.1 <nobody@in.example.com>: Recipient address rejected: User unknown in relay recipient table" {
			t.Errorf("%s: rows = %+v", name, st.rows)
		}
	}
}

// The filter's verdict is written by cleanup, under one name for every
// listener: it is the journal's only for a queue id port 25 opened.
func TestInboundMilterVerdictNeedsAPort25QueueID(t *testing.T) {
	const verdict = "postfix/cleanup[52]: 4A1B2C3D: milter-reject: END-OF-MESSAGE from mx.example.net[203.0.113.9]: 5.7.1 Spam message rejected; from=<a@example.net> to=<b@in.example.com> proto=ESMTP helo=<mx.example.net>"

	j, st := inboundFixture()
	// Opened by a submission port: postfix/smtpd, not postfix/inbound/smtpd.
	j.handle(head+"postfix/smtpd[20]: 4A1B2C3D: client=app.example.com[203.0.113.4], sasl_method=PLAIN, sasl_username=app", tailNow, false)
	j.handle(head+verdict, tailNow, false)
	if len(st.rows) != 0 {
		t.Fatalf("a submission-port verdict was journalled: %+v", st.rows)
	}

	j.handle(head+"postfix/inbound/smtpd[41]: 4A1B2C3D: client=mx.example.net[203.0.113.9]", tailNow, false)
	j.handle(head+verdict, tailNow, false)
	if len(st.rows) != 1 {
		t.Fatalf("rows = %+v", st.rows)
	}
	got := st.rows[0]
	if got.Decision != store.SpamDecisionReject || got.Engine != store.SpamEngineRspamd || got.Detail != "5.7.1 Spam message rejected" {
		t.Errorf("row = %+v", got)
	}
	// One verdict per message: the id is forgotten with it.
	j.handle(head+verdict, tailNow, false)
	if len(st.rows) != 1 {
		t.Errorf("the same queue id was journalled twice")
	}
}

func TestInboundMilterDecisions(t *testing.T) {
	for kind, c := range map[string]struct{ reply, decision string }{
		"milter-reject":  {"4.7.1 Try again later", store.SpamDecisionTempfail},
		"milter-discard": {"milter triggers DISCARD action", store.SpamDecisionReject},
	} {
		j, st := inboundFixture()
		j.handle(head+"postfix/inbound/smtpd[41]: Q1: client=mx.example.net[203.0.113.9]", tailNow, false)
		j.handle(head+"postfix/cleanup[52]: Q1: "+kind+": END-OF-MESSAGE from mx.example.net[203.0.113.9]: "+c.reply+"; from=<a@example.net> to=<b@in.example.com> proto=ESMTP helo=<mx.example.net>", tailNow, false)
		if len(st.rows) != 1 || st.rows[0].Decision != c.decision || st.rows[0].Engine != store.SpamEngineRspamd {
			t.Errorf("%s: rows = %+v", kind, st.rows)
		}
	}
}

// What is not the relay's mail, or is not a refusal at all, leaves no row.
func TestInboundIgnores(t *testing.T) {
	for name, line := range map[string]string{
		"another domain (relay access denied)": head + "postfix/inbound/smtpd[41]: NOQUEUE: reject: RCPT from x[203.0.113.9]: 554 5.7.1 <c@elsewhere.example>: Relay access denied; from=<a@example.net> to=<c@elsewhere.example> proto=ESMTP helo=<x>",
		"no recipient (size limit at MAIL)":    head + "postfix/inbound/smtpd[41]: NOQUEUE: reject: MAIL from x[203.0.113.9]: 552 5.3.4 Message size exceeds fixed limit; proto=ESMTP helo=<x>",
		"a submission port":                    head + "postfix/smtpd[20]: NOQUEUE: reject: RCPT from x[203.0.113.9]: 553 5.7.1 <b@in.example.com>: Sender address rejected: not owned by user app; from=<a@example.net> to=<b@in.example.com> proto=ESMTP helo=<x> sasl_method=PLAIN sasl_username=app",
		"a delivery line":                      head + "postfix/smtp[26]: 41E862C00D9E: to=<b@in.example.com>, relay=mx[203.0.113.9]:25, dsn=2.0.0, status=sent (250 OK)",
		"a connect line":                       head + "postfix/inbound/smtpd[41]: connect from mx.example.net[203.0.113.9]",
		// The HELO name is the sender's; a line cannot be started from inside one.
		"a refusal spelled inside another line": head + "postfix/inbound/smtpd[41]: warning: hostname x does not resolve: mail postfix/inbound/smtpd[1]: NOQUEUE: reject: RCPT from x[203.0.113.9]: 550 no; from=<a@example.net> to=<b@in.example.com> proto=ESMTP helo=<x>",
		"an address with a bracket in it":       head + "postfix/inbound/smtpd[41]: NOQUEUE: reject: RCPT from x[203.0.113.9]: 554 5.7.1 <b@elsewhere.example>: Relay access denied; from=<\"a> to=<v@in.example.com> proto=X helo=<y\"@example.net> to=<b@elsewhere.example> proto=ESMTP helo=<x>",
	} {
		j, st := inboundFixture()
		j.handle(line, tailNow, false)
		if len(st.rows) != 0 {
			t.Errorf("%s: journalled %+v", name, st.rows)
		}
	}
}

func TestInboundDetailIsCutAndPrintable(t *testing.T) {
	j, st := inboundFixture()
	j.handle(head+"postfix/inbound/smtpd[41]: NOQUEUE: reject: RCPT from x[203.0.113.9]: 550 5.7.1 caf\xc3\xa9 \x1b[31m"+strings.Repeat("x", 400)+"; from=<a@example.net> to=<b@in.example.com> proto=ESMTP helo=<x>", tailNow, false)
	if len(st.rows) != 1 {
		t.Fatalf("rows = %+v", st.rows)
	}
	d := st.rows[0].Detail
	if len(d) != inboundDetailMax || !strings.HasPrefix(d, "550 5.7.1 caf?? ?[31mxxx") {
		t.Errorf("detail (%d bytes) = %q", len(d), d)
	}
}

func TestInboundLookupFailureLeavesNoRow(t *testing.T) {
	j, st := inboundFixture()
	st.lookupErr = errors.New("boom")
	j.handle(unknownRcp, tailNow, false)
	if len(st.rows) != 0 {
		t.Errorf("rows = %+v", st.rows)
	}
}

// A message the filter holds goes on to the inbound journal milter, which
// writes its row; a second one from the log would be the same message twice.
func TestInboundHoldIsTheMiltersRow(t *testing.T) {
	// Postfix 3.10.13 logs a quarantine with the milter's reason, or
	// "default_action" when milter_default_action asked for it, where 3.7
	// wrote "milter triggers HOLD action" (src/cleanup/cleanup_milter.c,
	// cleanup_milter_apply(), and src/smtpd/smtpd.c, check_milter_reply():
	// text = reply[1] ? reply + 1 : "milter triggers HOLD action";
	// src/milter/milter8.c, SMFIR_QUARANTINE "H%.*s" and milter8_comm_error()
	// "Hdefault_action"). Whatever the reason says, the line is a hold.
	// Composed from those formats.
	for _, hold := range []string{
		"postfix/cleanup[52]: Q1: milter-hold: END-OF-MESSAGE from mx.example.net[203.0.113.9]: milter triggers HOLD action; from=<a@example.net> to=<b@in.example.com> proto=ESMTP helo=<mx.example.net>",
		"postfix/cleanup[52]: Q1: milter-hold: END-OF-MESSAGE from mx.example.net[203.0.113.9]: Gtube pattern; from=<a@example.net> to=<b@in.example.com> proto=ESMTP helo=<mx.example.net>",
		"postfix/cleanup[52]: Q1: milter-hold: END-OF-MESSAGE from mx.example.net[203.0.113.9]: default_action; from=<a@example.net> to=<b@in.example.com> proto=ESMTP helo=<mx.example.net>",
		// A reason that spells a reject is still the reason of a hold.
		"postfix/cleanup[52]: Q1: milter-hold: END-OF-MESSAGE from mx.example.net[203.0.113.9]: x: milter-reject: END-OF-MESSAGE from y[192.0.2.1]: 5.7.1 no; from=<a@example.net> to=<b@in.example.com> proto=ESMTP helo=<mx.example.net>",
		"postfix/inbound/smtpd[41]: Q1: milter-hold: RCPT from mx.example.net[203.0.113.9]: held for review; from=<a@example.net> to=<b@in.example.com> proto=ESMTP helo=<mx.example.net>",
	} {
		j, st := inboundFixture()
		j.handle(head+"postfix/inbound/smtpd[41]: Q1: client=mx.example.net[203.0.113.9]", tailNow, false)
		j.handle(head+hold, tailNow, false)
		if len(st.rows) != 0 {
			t.Errorf("%s: rows = %+v", hold, st.rows)
		}
	}
}

// The HELO name is the last thing on the line. Postfix logs it with a space
// and ` <>()\";@` turned into "?" (src/smtpd/smtpd.c, helo_cmd()), so one
// that tried to spell another refusal arrives neutered and the row is
// Postfix's own; a HELO name with those characters left in is no line
// Postfix writes, and makes no row.
func TestInboundHeloCannotWriteTheRow(t *testing.T) {
	j, st := inboundFixture()
	j.handle(head+"postfix/inbound/smtpd[41]: NOQUEUE: reject: RCPT from x[203.0.113.9]: 550 5.1.1 <nobody@in.example.com>: Recipient address rejected: User unknown in relay recipient table; from=<real@example.net> to=<nobody@in.example.com> proto=ESMTP helo=<x?5.7.1?Refused?by?this?server's?deny?list??from=?victim?bank.example??to=?ceo?in.example.com??proto=ESMTP?helo=?y?>", tailNow, false)
	if len(st.rows) != 1 {
		t.Fatalf("rows = %+v", st.rows)
	}
	got := st.rows[0]
	if got.From != "real@example.net" || got.To != "nobody@in.example.com" ||
		got.Detail != "550 5.1.1 <nobody@in.example.com>: Recipient address rejected: User unknown in relay recipient table" {
		t.Errorf("row = %+v", got)
	}

	// Un-neutered, which Postfix never writes: a line with two tails makes
	// no row, and above all not the one the HELO name spells.
	for _, line := range []string{
		head + "postfix/inbound/smtpd[41]: NOQUEUE: reject: RCPT from x[203.0.113.9]: 550 5.1.1 <nobody@in.example.com>: Recipient address rejected: User unknown in relay recipient table; from=<real@example.net> to=<nobody@in.example.com> proto=ESMTP helo=<x 5.7.1 Refused by this server's deny list; from=<victim@bank.example> to=<ceo@in.example.com> proto=ESMTP helo=<y>",
		head + "postfix/inbound/smtpd[41]: NOQUEUE: reject: RCPT from x[203.0.113.9]: 550 5.1.1 <nobody@in.example.com>: no; from=<real@example.net> to=<nobody@in.example.com> proto=ESMTP helo=<a>b>",
	} {
		j, st = inboundFixture()
		j.handle(line, tailNow, false)
		if len(st.rows) != 0 {
			t.Errorf("a line with an un-neutered HELO name wrote %+v", st.rows)
		}
	}

	// The same on a verdict line: the filter's reply is not extended by the
	// HELO name, so a stranger cannot make a refusal look like the deny list's.
	j, st = inboundFixture()
	j.handle(head+"postfix/inbound/smtpd[41]: Q1: client=x[203.0.113.9]", tailNow, false)
	j.handle(head+"postfix/cleanup[52]: Q1: milter-reject: END-OF-MESSAGE from x[203.0.113.9]: 5.7.1 Spam message rejected; from=<real@example.net> to=<b@in.example.com> proto=ESMTP helo=<x?this?server's?deny?list??from=?spoofed?example.org??to=?b?in.example.com??proto=ESMTP?helo=?y?>", tailNow, false)
	if len(st.rows) != 1 || st.rows[0].Detail != "5.7.1 Spam message rejected" || st.rows[0].From != "real@example.net" {
		t.Errorf("rows = %+v", st.rows)
	}
}

// The refused recipient is quoted in the reply in internal form - quotes
// removed, "; < > =" kept - so it can spell a whole "; from=<…> to=<…>
// proto=…" there. The line below is the shape mail.log on the test server
// held on 2026-10-17 for such a RCPT TO sent from outside (the audit's probe
// used a domain that is not inbound; here it is one). It writes no row in
// the stranger's words (security audit 2026-10-17, M2).
func TestInboundRecipientCannotWriteTheRow(t *testing.T) {
	forged := `; from=<ceo@example.net> to=<board@in.example.com> proto=ESMTP helo=<x>; `
	for name, line := range map[string]string{
		"recipient unknown":               head + `postfix/inbound/smtpd[41]: NOQUEUE: reject: RCPT from x[203.0.113.9]: 550 5.1.1 <` + forged + `@in.example.com>: Recipient address rejected: User unknown in relay recipient table; from=<probe@example.net> to=<"` + forged + `"@in.example.com> proto=ESMTP helo=<audit.example>`,
		"forged sender also in MAIL FROM": head + `postfix/inbound/smtpd[41]: NOQUEUE: reject: RCPT from x[203.0.113.9]: 550 5.1.1 <` + forged + `@in.example.com>: Recipient address rejected: User unknown in relay recipient table; from=<"x; from=<ceo@example.net"@a.example> to=<"` + forged + `"@in.example.com> proto=ESMTP helo=<audit.example>`,
	} {
		j, st := inboundFixture()
		j.handle(line, tailNow, false)
		if len(st.rows) != 0 {
			t.Errorf("%s: a line with two tails wrote %+v", name, st.rows)
		}
	}

	// A plain refusal is still read whole, its reply up to Postfix's tail.
	j, st := inboundFixture()
	j.handle(unknownRcp, tailNow, false)
	if len(st.rows) != 1 || st.rows[0].From != "a@example.net" || st.rows[0].To != "nobody@in.example.com" {
		t.Errorf("rows = %+v", st.rows)
	}
}

// A queue id is port 25's only until the message is queued, cleanup removes
// it unqueued, or the id is opened again by another listener - Postfix reuses
// ids.
func TestInboundQueueIDIsForgotten(t *testing.T) {
	const verdict = "postfix/cleanup[52]: Q1: milter-reject: END-OF-MESSAGE from app[203.0.113.4]: 4.7.1 Service unavailable - try again later; from=<a@example.net> to=<b@in.example.com> proto=ESMTP helo=<app>"
	for name, forget := range map[string]string{
		"queued":                        "postfix/qmgr[58]: Q1: from=<a@example.net>, size=600, nrcpt=1 (queue active)",
		"reused by the submission port": "postfix/smtpd[20]: Q1: client=app.example.com[203.0.113.4], sasl_method=PLAIN, sasl_username=app",
		// Postfix 3.10's cleanup_flush(): "%s: removed (%s)", for a message
		// whose client went away in DATA, or that was discarded.
		"canceled by cleanup":  "postfix/cleanup[52]: Q1: removed (canceled)",
		"discarded by cleanup": "postfix/cleanup[52]: Q1: removed (discarded)",
	} {
		j, st := inboundFixture()
		j.handle(head+"postfix/inbound/smtpd[41]: Q1: client=mx.example.net[203.0.113.9]", tailNow, false)
		j.handle(head+forget, tailNow, false)
		j.handle(head+verdict, tailNow, false)
		if len(st.rows) != 0 || len(j.port25) != 0 {
			t.Errorf("%s: rows %+v, remembered %v", name, st.rows, j.port25)
		}
	}
}

// An id that was forgotten and opened again is not dropped by its own stale
// place in the queue of arrivals.
func TestInboundStaleArrivalDoesNotDropALiveID(t *testing.T) {
	j, _ := inboundFixture()
	j.remember("Q1")
	delete(j.port25, "Q1") // its verdict came
	for i := 0; i < inboundQueueIDs-2; i++ {
		j.remember("F" + time.Duration(i).String())
	}
	j.remember("Q1") // reused; order is now full, with the stale Q1 first
	j.remember("LAST")
	if _, ok := j.port25["Q1"]; !ok {
		t.Error("the live Q1 was dropped by its stale arrival")
	}
}

// The queue ids waiting for a verdict are bounded.
func TestInboundQueueIDsAreBounded(t *testing.T) {
	j, _ := inboundFixture()
	for i := 0; i < inboundQueueIDs+10; i++ {
		j.remember("Q" + strings.Repeat("A", i%7) + string(rune('0'+i%10)) + time.Duration(i).String())
	}
	if len(j.port25) != inboundQueueIDs || len(j.order) != inboundQueueIDs {
		t.Errorf("remembered %d ids (%d in order), want %d", len(j.port25), len(j.order), inboundQueueIDs)
	}
}

func TestLineTime(t *testing.T) {
	now := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		line string
		want time.Time
		rest string
	}{
		{"Jan 02 09:00:00 mail postfix/x: y", time.Date(2026, 1, 2, 9, 0, 0, 0, time.Local).UTC(), "mail postfix/x: y"},
		{"Jan  2 09:30:00 mail postfix/x: y", time.Date(2026, 1, 2, 9, 30, 0, 0, time.Local).UTC(), "mail postfix/x: y"},
		// December read in January is last year's.
		{"Dec 31 23:59:00 mail postfix/x: y", time.Date(2025, 12, 31, 23, 59, 0, 0, time.Local).UTC(), "mail postfix/x: y"},
		{"no stamp here", now, "no stamp here"},
	} {
		got, rest := lineTime(c.line, now)
		if !got.Equal(c.want) || rest != c.rest {
			t.Errorf("lineTime(%q) = %v, %q; want %v, %q", c.line, got, rest, c.want, c.rest)
		}
	}
}

func TestInboundRetentionPrunesAtStart(t *testing.T) {
	old := retentionInterval
	retentionInterval = time.Hour
	defer func() { retentionInterval = old }()

	st := &fakeInboundStore{}
	ctx, cancel := contextWithCancel()
	done := make(chan struct{})
	go func() {
		inboundRetentionLoop(ctx, &Inbound{Store: st, Retention: func() int { return 0 }})
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for len(st.pruned) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	if len(st.pruned) != 1 || st.pruned[0] != inboundSpamLogMaxRows {
		t.Errorf("prune calls = %v", st.pruned)
	}
}

func contextWithCancel() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}

// The lines the tailer reads when it starts may be ones it read before its
// last stop: a refusal among them is one row, not two. Lines read as they are
// written are always new.
func TestInboundCatchUpDoesNotWriteARowTwice(t *testing.T) {
	j, st := inboundFixture()
	j.handle(unknownRcp, tailNow, false) // before the stop
	j.handle(unknownRcp, tailNow, true)  // the same line, read again on start
	if len(st.rows) != 1 {
		t.Fatalf("rows = %d, want the one refusal", len(st.rows))
	}
	j.handle(unknownRcp, tailNow, false) // a second refusal in the same second
	if len(st.rows) != 2 {
		t.Errorf("rows = %d, want two: a line read as it is written is new", len(st.rows))
	}
}
