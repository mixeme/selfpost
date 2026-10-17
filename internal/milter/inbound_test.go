package milter

import (
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/emersion/go-milter"

	"github.com/mixeme/selfpost/internal/store"
)

// fakeInbound is the journal and the inbound-domain list of one test.
type fakeInbound struct {
	domains   map[string]bool
	rows      []store.InboundSpamEntry
	insertErr error
	lookupErr error
	forgot    []string
}

func (f *fakeInbound) InsertInboundSpamMessage(e store.InboundSpamEntry, rcpts []store.InboundRecipient) error {
	if f.insertErr != nil {
		return f.insertErr
	}
	for _, r := range rcpts {
		e.InboundDomain, e.To = r.Domain, r.To
		f.rows = append(f.rows, e)
	}
	return nil
}

func (f *fakeInbound) ForgetInboundQueueID(queueID string) error {
	f.forgot = append(f.forgot, queueID)
	return nil
}

func (f *fakeInbound) IsInboundDomain(name string) (bool, error) {
	if f.lookupErr != nil {
		return false, f.lookupErr
	}
	return f.domains[name], nil
}

// A header in the format of the documented rspamd sidecar, composed by hand:
// rspamd 3.11's milter_headers.lua writes the same fields, but sorts the
// symbols by absolute weight (sort_by = 'score'), which this order is not.
const spamdResult = "default: False [7.20 / 15.00];\n\tBAYES_SPAM(5.10)[100.00%];\n\tMIME_GOOD(-0.10)[text/plain];\n\tARC_NA(0.00)[];\n\tRCVD_COUNT_ZERO(0.00)[0];\n\tR_DKIM_NA(2.20)[]"

// message replays one message through s: envelope, headers in order, end.
func message(t *testing.T, s *inboundSession, from string, rcpts []string, headers [][2]string) milter.Response {
	t.Helper()
	if _, err := s.MailFrom(from, mods(nil)); err != nil {
		t.Fatalf("MailFrom: %v", err)
	}
	for _, r := range rcpts {
		if _, err := s.RcptTo(r, mods(nil)); err != nil {
			t.Fatalf("RcptTo: %v", err)
		}
	}
	for _, h := range headers {
		if _, err := s.Header(h[0], h[1], mods(nil)); err != nil {
			t.Fatalf("Header: %v", err)
		}
	}
	resp, err := s.Body(mods(map[string]string{"i": "ABC123"}))
	if err != nil {
		t.Fatalf("Body: %v", err)
	}
	return resp
}

// testKey is the key the tests' filter signs with; the messages they replay
// have the queue id ABC123.
const testKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

var testSig = [2]string{SignatureHeader, FilterSignature(testKey, "ABC123")}

func inboundSessionFor(t *testing.T, rec InboundStore, filter bool) *inboundSession {
	t.Helper()
	s := &inboundSession{rec: rec}
	if filter {
		s.key = testKey
	}
	if _, err := s.Connect("mx.example.net", "tcp4", 0, net.ParseIP("203.0.113.7"), mods(nil)); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	return s
}

// With no filter attached an accepted message is a plain row, one per
// recipient, decided by nobody.
func TestInboundRecordsAcceptedMessage(t *testing.T) {
	rec := &fakeInbound{domains: map[string]bool{"in.example.com": true}}
	s := inboundSessionFor(t, rec, false)
	// As go-milter hands them: brackets trimmed, ESMTP parameters left out
	// (go-milter v0.4.1, session.go, case CodeMail/CodeRcpt).
	resp := message(t, s, "sender@example.net",
		[]string{"a@in.example.com", "b@IN.example.com"},
		[][2]string{{"Subject", "=?utf-8?Q?Sch=C3=B6n?="}})
	if resp != milter.RespContinue {
		t.Errorf("end-of-message response = %v, want continue", resp)
	}
	if len(rec.rows) != 2 {
		t.Fatalf("rows = %+v, want one per recipient", rec.rows)
	}
	want := store.InboundSpamEntry{
		InboundDomain: "in.example.com", ClientIP: "203.0.113.7",
		From: "sender@example.net", To: "a@in.example.com", Subject: "Schön",
		Decision: store.SpamDecisionAccept, Engine: store.SpamEngineSelfPost,
		// The queue id of end-of-message, for the delivery lines to come.
		QueueID: "ABC123", Upstream: store.UpstreamQueued,
	}
	if rec.rows[0] != want {
		t.Errorf("row = %+v\nwant  %+v", rec.rows[0], want)
	}
	if rec.rows[1].To != "b@IN.example.com" || rec.rows[1].InboundDomain != "in.example.com" {
		t.Errorf("second row = %+v", rec.rows[1])
	}
}

// Port 25 also takes DMARC reports for the server's own name. They are not
// the relay's mail and the journal says nothing about them.
func TestInboundSkipsRecipientsOutsideInboundDomains(t *testing.T) {
	rec := &fakeInbound{domains: map[string]bool{"in.example.com": true}}
	s := inboundSessionFor(t, rec, false)
	message(t, s, "noreply-dmarc@google.com",
		[]string{"dmarc-example.com@mail.example.org", "a@in.example.com"}, nil)
	if len(rec.rows) != 1 || rec.rows[0].To != "a@in.example.com" {
		t.Errorf("rows = %+v, want the inbound recipient alone", rec.rows)
	}
}

// With the filter attached its verdict headers become the engine and detail.
func TestInboundReadsTheFiltersVerdict(t *testing.T) {
	rec := &fakeInbound{domains: map[string]bool{"in.example.com": true}}
	s := inboundSessionFor(t, rec, true)
	message(t, s, "sender@example.net", []string{"a@in.example.com"}, [][2]string{
		{"Subject", "Cheap pills"},
		{"X-Rspamd-Action", "add header"},
		{"X-Spamd-Result", spamdResult},
		testSig,
	})
	if len(rec.rows) != 1 {
		t.Fatalf("rows = %+v", rec.rows)
	}
	got := rec.rows[0]
	if got.Engine != store.SpamEngineRspamd || got.Decision != store.SpamDecisionAccept {
		t.Errorf("engine %q, decision %q", got.Engine, got.Decision)
	}
	if want := "add header, score 7.20 / 15.00; BAYES_SPAM(5.10) MIME_GOOD(-0.10) R_DKIM_NA(2.20)"; got.Detail != want {
		t.Errorf("detail = %q\nwant     %q", got.Detail, want)
	}
}

func TestInboundQuarantineAction(t *testing.T) {
	rec := &fakeInbound{domains: map[string]bool{"in.example.com": true}}
	s := inboundSessionFor(t, rec, true)
	message(t, s, "sender@example.net", []string{"a@in.example.com"},
		[][2]string{{"x-rspamd-action", "quarantine"}, {"x-selfpost-filter", strings.ToUpper(testSig[1])}})
	if len(rec.rows) != 1 || rec.rows[0].Decision != store.SpamDecisionQuarantine || rec.rows[0].Detail != "quarantine" {
		t.Errorf("rows = %+v", rec.rows)
	}
}

// Without a filter in front, a verdict header is the sender's own writing.
func TestInboundIgnoresVerdictHeadersWithoutAFilter(t *testing.T) {
	rec := &fakeInbound{domains: map[string]bool{"in.example.com": true}}
	s := inboundSessionFor(t, rec, false)
	message(t, s, "sender@example.net", []string{"a@in.example.com"}, [][2]string{
		{"X-Rspamd-Action", "no action"},
		{"X-Spamd-Result", spamdResult},
	})
	if len(rec.rows) != 1 || rec.rows[0].Engine != store.SpamEngineSelfPost || rec.rows[0].Detail != "" {
		t.Errorf("rows = %+v", rec.rows)
	}
}

// The filter is attached fail-open, so with it down the headers are again the
// sender's: nothing of them reaches the journal except through the patterns.
func TestRspamdVerdictKeepsOnlyWhatItRecognises(t *testing.T) {
	action, detail := rspamdVerdict("<script>alert(1)</script>", "default: False [1 / 15]; <b>HTML</b> lower_case(9.9) \x1b[31m")
	if action != "" {
		t.Errorf("action = %q, want it dropped", action)
	}
	if detail != "score 1 / 15" {
		t.Errorf("detail = %q", detail)
	}

	var many strings.Builder
	for i := 0; i < 400; i++ {
		many.WriteString("SYMBOL_WITH_A_LONG_NAME_" + strings.Repeat("X", 30) + "(1.00)[];")
	}
	if _, detail := rspamdVerdict("reject", many.String()); len(detail) > verdictMaxBytes || strings.Count(detail, "(") > verdictMaxSymbols {
		t.Errorf("detail of %d bytes with %d symbols", len(detail), strings.Count(detail, "("))
	}
	if action, _ := rspamdVerdict(strings.Repeat("a", 33), ""); action != "" {
		t.Errorf("an action of 33 letters was kept")
	}
}

// A second message on the same connection starts clean.
func TestInboundResetsBetweenMessages(t *testing.T) {
	rec := &fakeInbound{domains: map[string]bool{"in.example.com": true}}
	s := inboundSessionFor(t, rec, true)
	message(t, s, "one@example.net", []string{"a@in.example.com"}, [][2]string{
		{"Subject", "First"}, {"X-Rspamd-Action", "add header"}, testSig,
	})
	message(t, s, "two@example.net", []string{"b@in.example.com"}, nil)
	if len(rec.rows) != 2 {
		t.Fatalf("rows = %+v", rec.rows)
	}
	want := store.InboundSpamEntry{
		InboundDomain: "in.example.com", ClientIP: "203.0.113.7",
		From: "two@example.net", To: "b@in.example.com",
		Decision: store.SpamDecisionAccept, Engine: store.SpamEngineSelfPost,
		QueueID: "ABC123", Upstream: store.UpstreamQueued,
	}
	if rec.rows[1] != want {
		t.Errorf("second row = %+v\nwant         %+v", rec.rows[1], want)
	}

	// An aborted transaction leaves nothing behind for the next one either.
	if _, err := s.MailFrom("three@example.net", mods(nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RcptTo("c@in.example.com", mods(nil)); err != nil {
		t.Fatal(err)
	}
	if err := s.Abort(mods(nil)); err != nil {
		t.Fatal(err)
	}
	if len(rec.rows) != 2 || len(s.rcpts) != 0 {
		t.Errorf("after an abort: rows %d, pending recipients %v", len(rec.rows), s.rcpts)
	}
}

// The journal failing is never the mail's problem.
func TestInboundAcceptsWhenTheStoreFails(t *testing.T) {
	for name, rec := range map[string]*fakeInbound{
		"insert": {domains: map[string]bool{"in.example.com": true}, insertErr: errors.New("boom")},
		"lookup": {lookupErr: errors.New("boom")},
	} {
		s := inboundSessionFor(t, rec, false)
		if resp := message(t, s, "sender@example.net", []string{"a@in.example.com"}, nil); resp != milter.RespContinue {
			t.Errorf("%s failure: response = %v, want continue", name, resp)
		}
	}
}

// A composed header with the allow entry's -100 behind six others. rspamd
// 3.11 itself sorts by absolute weight and would put it first
// (milter_headers.lua, x-spamd-result, sort_by = 'score'); the order here is
// the hard case for the reader. Filter lists counts an entry's matches by
// finding that symbol in the journal: it is kept whatever else there is.
func TestRspamdVerdictKeepsTheListSymbols(t *testing.T) {
	result := "default: False [-91.00 / 15.00]; SYM_A(3.00)[]; SYM_B(2.00)[]; SYM_C(1.50)[]; SYM_D(1.00)[]; SYM_E(0.80)[]; SYM_F(0.50)[]; SYM_G(0.20)[]; ARC_NA(0.00)[]; SELFPOST_ALLOW_FROM(-100.00)[billing@vendor.example]"
	_, detail := rspamdVerdict("no action", result)
	want := "no action, score -91.00 / 15.00; SELFPOST_ALLOW_FROM(-100.00) SYM_A(3.00) SYM_B(2.00) SYM_C(1.50) SYM_D(1.00) SYM_E(0.80)"
	if detail != want {
		t.Errorf("detail = %q\nwant     %q", detail, want)
	}
}

// The client address of a connection's later messages comes from the MAIL
// macro: a session the library made after a non-continue reply saw no Connect.
func TestInboundClientAddressFromTheMailMacro(t *testing.T) {
	rec := &fakeInbound{domains: map[string]bool{"in.example.com": true}}
	s := &inboundSession{rec: rec} // no Connect
	if _, err := s.MailFrom("sender@example.net", mods(map[string]string{"{client_addr}": "IPv6:2001:db8::7"})); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RcptTo("a@in.example.com", mods(nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Body(mods(nil)); err != nil {
		t.Fatal(err)
	}
	if len(rec.rows) != 1 || rec.rows[0].ClientIP != "2001:db8::7" {
		t.Errorf("rows = %+v", rec.rows)
	}
}

// No callback of the inbound milter answers anything but "continue": it must
// never be the reason a message is held up, and a non-continue reply would
// cost it its session.
func TestInboundOnlyEverContinues(t *testing.T) {
	rec := &fakeInbound{domains: map[string]bool{"in.example.com": true}}
	s := &inboundSession{rec: rec, key: testKey}
	check := func(stage string, resp milter.Response, err error) {
		t.Helper()
		if err != nil || resp != milter.RespContinue {
			t.Errorf("%s: %v, %v", stage, resp, err)
		}
	}
	resp, err := s.Connect("mx", "tcp4", 0, net.ParseIP("203.0.113.7"), mods(nil))
	check("connect", resp, err)
	resp, err = s.MailFrom("a@example.net", mods(nil))
	check("mail", resp, err)
	resp, err = s.RcptTo("a@in.example.com", mods(nil))
	check("rcpt", resp, err)
	resp, err = s.Header("X-Rspamd-Action", "reject", mods(nil))
	check("header", resp, err)
	resp, err = s.Body(mods(nil))
	check("end of message", resp, err)
}

// The filter is attached so that mail flows when it is down, and then the
// verdict headers of a message are its sender's own. A verdict is recorded only
// next to the filter's signature for this message's queue id.
func TestInboundReadsAVerdictOnlyWithTheFiltersSignature(t *testing.T) {
	verdict := [][2]string{{"X-Rspamd-Action", "quarantine"}, {"X-Spamd-Result", spamdResult}}
	for name, c := range map[string]struct {
		extra  [][2]string
		signed bool
	}{
		"signed":                            {[][2]string{testSig}, true},
		"no signature":                      {nil, false},
		"a made-up signature":               {[][2]string{{SignatureHeader, strings.Repeat("ab", 32)}}, false},
		"another message's signature":       {[][2]string{{SignatureHeader, FilterSignature(testKey, "OTHER1")}}, false},
		"a signature under another key":     {[][2]string{{SignatureHeader, FilterSignature(strings.Repeat("f", 64), "ABC123")}}, false},
		"the sender's first, then the real": {[][2]string{{SignatureHeader, "forged"}, testSig}, true},
	} {
		rec := &fakeInbound{domains: map[string]bool{"in.example.com": true}}
		s := inboundSessionFor(t, rec, true)
		message(t, s, "sender@example.net", []string{"a@in.example.com"}, append(append([][2]string{}, verdict...), c.extra...))
		if len(rec.rows) != 1 {
			t.Fatalf("%s: rows = %+v", name, rec.rows)
		}
		got := rec.rows[0]
		if c.signed && (got.Engine != store.SpamEngineRspamd || got.Decision != store.SpamDecisionQuarantine) {
			t.Errorf("%s: a signed verdict was not read: %+v", name, got)
		}
		if !c.signed && (got.Engine != store.SpamEngineSelfPost || got.Decision != store.SpamDecisionAccept || got.Detail != "") {
			t.Errorf("%s: an unsigned verdict was recorded: %+v", name, got)
		}
	}

	// A signature is of no use to the next message on the connection.
	rec := &fakeInbound{domains: map[string]bool{"in.example.com": true}}
	s := inboundSessionFor(t, rec, true)
	message(t, s, "one@example.net", []string{"a@in.example.com"}, append(append([][2]string{}, verdict...), testSig))
	message(t, s, "two@example.net", []string{"a@in.example.com"}, verdict)
	if len(rec.rows) != 2 || rec.rows[1].Engine != store.SpamEngineSelfPost {
		t.Errorf("rows = %+v", rec.rows)
	}

	// With no queue id nothing can be checked, so nothing is trusted.
	rec = &fakeInbound{domains: map[string]bool{"in.example.com": true}}
	s = inboundSessionFor(t, rec, true)
	_, _ = s.MailFrom("sender@example.net", mods(nil))
	_, _ = s.RcptTo("a@in.example.com", mods(nil))
	_, _ = s.Header("X-Rspamd-Action", "reject", mods(nil))
	_, _ = s.Header(SignatureHeader, FilterSignature(testKey, ""), mods(nil))
	_, _ = s.Body(mods(nil))
	if len(rec.rows) != 1 || rec.rows[0].Engine != store.SpamEngineSelfPost {
		t.Errorf("no queue id: rows = %+v", rec.rows)
	}
}

// What rspamd 3.11 wrote for this key and queue id in a throwaway container
// (dev trial, 2026-10-12): the two ends must compute the same thing.
func TestFilterSignatureIsWhatTheFilterWrites(t *testing.T) {
	if got := FilterSignature(testKey, "4A1B2C3D5E"); got != "2aa1ca6dd1545d3f16c8c0c29ba59bfc28c050224ac2c5c953bd2617f73b6de2" {
		t.Errorf("FilterSignature = %s", got)
	}
}

// The row is the delivery lines' to find: a recipient is kept as Postfix logs
// it, a held message starts held, older rows of a reused queue id are
// detached first - once per message - and a message with no queue id has no
// result, since nothing could ever report one.
func TestInboundRowsMeetTheirDeliveryLines(t *testing.T) {
	rec := &fakeInbound{domains: map[string]bool{"in.example.com": true}}
	s := inboundSessionFor(t, rec, true)
	message(t, s, "sender@example.net", []string{`<"owner"@in.example.com>`, "b@in.example.com"}, nil)
	if len(rec.rows) != 2 || rec.rows[0].To != "owner@in.example.com" {
		t.Fatalf("rows = %+v, want the quoted recipient as logged", rec.rows)
	}
	if len(rec.forgot) != 1 || rec.forgot[0] != "ABC123" {
		t.Errorf("detached %q, want ABC123 once", rec.forgot)
	}

	rec.rows = nil
	message(t, s, "sender@example.net", []string{"a@in.example.com"},
		[][2]string{{"X-Rspamd-Action", "quarantine"}, testSig})
	if len(rec.rows) != 1 || rec.rows[0].Upstream != store.UpstreamHeld {
		t.Errorf("a held message's row = %+v, want held", rec.rows)
	}

	rec.rows, rec.forgot = nil, nil
	if _, err := s.MailFrom("sender@example.net", mods(nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RcptTo("a@in.example.com", mods(nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Body(mods(nil)); err != nil {
		t.Fatal(err)
	}
	if len(rec.rows) != 1 || rec.rows[0].QueueID != "" || rec.rows[0].Upstream != "" || len(rec.forgot) != 0 {
		t.Errorf("with no queue id: rows %+v, detached %q", rec.rows, rec.forgot)
	}
}

// A recipient given again - in any case - is one row: Postfix accepts a
// repeated RCPT up to smtpd_recipient_limit (1 000) and sends each, and one
// message used to be a thousand rows (security audit 2026-10-17, M3).
func TestInboundRecipientRepeatedIsOneRow(t *testing.T) {
	rec := &fakeInbound{domains: map[string]bool{"in.example.com": true}}
	s := inboundSessionFor(t, rec, false)
	rcpts := make([]string, 0, 1000)
	for i := 0; i < 1000; i++ {
		if i%2 == 0 {
			rcpts = append(rcpts, "listed@in.example.com")
		} else {
			rcpts = append(rcpts, "LISTED@in.example.com")
		}
	}
	message(t, s, "sender@example.net", rcpts, nil)
	if len(rec.rows) != 1 || rec.rows[0].To != "listed@in.example.com" {
		t.Errorf("rows = %d (%+v), want one, spelt as first given", len(rec.rows), rec.rows)
	}
}
