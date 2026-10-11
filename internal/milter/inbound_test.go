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
}

func (f *fakeInbound) InsertInboundSpam(e store.InboundSpamEntry) error {
	if f.insertErr != nil {
		return f.insertErr
	}
	f.rows = append(f.rows, e)
	return nil
}

func (f *fakeInbound) IsInboundDomain(name string) (bool, error) {
	if f.lookupErr != nil {
		return false, f.lookupErr
	}
	return f.domains[name], nil
}

// A header of the kind the documented rspamd sidecar adds.
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

func inboundSessionFor(t *testing.T, rec InboundStore, filter bool) *inboundSession {
	t.Helper()
	s := &inboundSession{rec: rec, filter: filter}
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
	resp := message(t, s, "<sender@example.net> SIZE=1200",
		[]string{"<a@in.example.com>", "b@IN.example.com"},
		[][2]string{{"Subject", "=?utf-8?Q?Sch=C3=B6n?="}})
	if resp != milter.RespAccept {
		t.Errorf("end-of-message response = %v, want accept", resp)
	}
	if len(rec.rows) != 2 {
		t.Fatalf("rows = %+v, want one per recipient", rec.rows)
	}
	want := store.InboundSpamEntry{
		InboundDomain: "in.example.com", ClientIP: "203.0.113.7",
		From: "sender@example.net", To: "a@in.example.com", Subject: "Schön",
		Decision: store.SpamDecisionAccept, Engine: store.SpamEngineSelfPost,
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
		[][2]string{{"x-rspamd-action", "quarantine"}})
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
		{"Subject", "First"}, {"X-Rspamd-Action", "add header"},
	})
	message(t, s, "two@example.net", []string{"b@in.example.com"}, nil)
	if len(rec.rows) != 2 {
		t.Fatalf("rows = %+v", rec.rows)
	}
	want := store.InboundSpamEntry{
		InboundDomain: "in.example.com", ClientIP: "203.0.113.7",
		From: "two@example.net", To: "b@in.example.com",
		Decision: store.SpamDecisionAccept, Engine: store.SpamEngineSelfPost,
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
		if resp := message(t, s, "sender@example.net", []string{"a@in.example.com"}, nil); resp != milter.RespAccept {
			t.Errorf("%s failure: response = %v, want accept", name, resp)
		}
	}
}

// rspamd writes symbols heaviest first, so an allow entry's -100 comes last.
// Filter lists counts an entry's matches by finding that symbol in the journal:
// it is kept whatever else there is.
func TestRspamdVerdictKeepsTheListSymbols(t *testing.T) {
	result := "default: False [-91.00 / 15.00]; SYM_A(3.00)[]; SYM_B(2.00)[]; SYM_C(1.50)[]; SYM_D(1.00)[]; SYM_E(0.80)[]; SYM_F(0.50)[]; SYM_G(0.20)[]; ARC_NA(0.00)[]; SELFPOST_ALLOW_FROM(-100.00)[billing@vendor.example]"
	_, detail := rspamdVerdict("no action", result)
	want := "no action, score -91.00 / 15.00; SELFPOST_ALLOW_FROM(-100.00) SYM_A(3.00) SYM_B(2.00) SYM_C(1.50) SYM_D(1.00) SYM_E(0.80)"
	if detail != want {
		t.Errorf("detail = %q\nwant     %q", detail, want)
	}
}
