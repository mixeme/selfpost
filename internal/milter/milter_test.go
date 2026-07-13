package milter

import (
	"errors"
	"net"
	"testing"

	"github.com/emersion/go-milter"

	"codeberg.org/mix/selfpost/internal/store"
)

// fakeRecorder captures inserts and can be made to fail, to prove the milter
// swallows recorder errors and still accepts the message.
type fakeRecorder struct {
	entries []store.SendLogEntry
	fail    bool
}

func (f *fakeRecorder) InsertQueued(e store.SendLogEntry) error {
	if f.fail {
		return errors.New("boom")
	}
	f.entries = append(f.entries, e)
	return nil
}

func mods(kv map[string]string) *milter.Modifier {
	return &milter.Modifier{Macros: kv}
}

// drive replays a typical message through one session and returns the recorder.
func drive(t *testing.T, rec Recorder) *session {
	t.Helper()
	s := &session{rec: rec}
	if _, err := s.Connect("localhost", "tcp4", 0, net.ParseIP("203.0.113.7"), mods(nil)); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if _, err := s.MailFrom("noreply@example.com", mods(map[string]string{"auth_authen": "app1"})); err != nil {
		t.Fatalf("MailFrom: %v", err)
	}
	if _, err := s.RcptTo("<a@example.net>", mods(nil)); err != nil {
		t.Fatalf("RcptTo: %v", err)
	}
	if _, err := s.RcptTo("b@example.net", mods(nil)); err != nil {
		t.Fatalf("RcptTo: %v", err)
	}
	if _, err := s.Header("Subject", "Hello there", mods(nil)); err != nil {
		t.Fatalf("Header: %v", err)
	}
	if _, err := s.Body(mods(map[string]string{"i": "ABC123"})); err != nil {
		t.Fatalf("Body: %v", err)
	}
	return s
}

func TestSessionRecordsRowPerRecipient(t *testing.T) {
	rec := &fakeRecorder{}
	s := drive(t, rec)

	if s.clientIP != "203.0.113.7" {
		t.Fatalf("clientIP = %q, want 203.0.113.7", s.clientIP)
	}
	if len(rec.entries) != 2 {
		t.Fatalf("want 2 entries, got %d: %+v", len(rec.entries), rec.entries)
	}
	got := rec.entries[0]
	want := store.SendLogEntry{
		QueueID:  "ABC123",
		Domain:   "example.com",
		AppLogin: "app1",
		From:     "noreply@example.com",
		To:       "a@example.net", // angle brackets stripped
		Subject:  "Hello there",
	}
	if got != want {
		t.Fatalf("entry[0]\n got %+v\nwant %+v", got, want)
	}
	if rec.entries[1].To != "b@example.net" {
		t.Fatalf("entry[1].To = %q", rec.entries[1].To)
	}
}

func TestBodyAcceptsEvenWhenRecorderFails(t *testing.T) {
	rec := &fakeRecorder{fail: true}
	s := &session{rec: rec}
	_, _ = s.MailFrom("x@example.com", mods(map[string]string{"auth_authen": "app1"}))
	_, _ = s.RcptTo("y@example.net", mods(nil))
	resp, err := s.Body(mods(map[string]string{"i": "Q9"}))
	if err != nil {
		t.Fatalf("Body returned error, must fail open: %v", err)
	}
	if resp != milter.RespAccept {
		t.Fatalf("Body response = %v, want Accept", resp)
	}
}

// A single connection may carry several messages; the second must not inherit
// the first's recipients or subject.
func TestSessionResetsBetweenMessages(t *testing.T) {
	rec := &fakeRecorder{}
	s := &session{rec: rec}

	_, _ = s.MailFrom("a@example.com", mods(map[string]string{"auth_authen": "app1"}))
	_, _ = s.RcptTo("one@example.net", mods(nil))
	_, _ = s.Header("Subject", "first", mods(nil))
	_, _ = s.Body(mods(map[string]string{"i": "Q1"}))

	_, _ = s.MailFrom("b@example.com", mods(map[string]string{"auth_authen": "app2"}))
	_, _ = s.RcptTo("two@example.net", mods(nil))
	_, _ = s.Body(mods(map[string]string{"i": "Q2"}))

	if len(rec.entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(rec.entries))
	}
	second := rec.entries[1]
	if second.QueueID != "Q2" || second.To != "two@example.net" || second.Subject != "" || second.AppLogin != "app2" {
		t.Fatalf("second message leaked state: %+v", second)
	}
}

// Postfix sends multi-character macro names wrapped in braces ({auth_authen},
// {i} for some versions), so the milter must resolve those too — this is the
// case the SASL-less spike missed and that produced empty app_login at first.
func TestBracedMacros(t *testing.T) {
	rec := &fakeRecorder{}
	s := &session{rec: rec}
	_, _ = s.MailFrom("app@example.com", mods(map[string]string{"{auth_authen}": "app1"}))
	_, _ = s.RcptTo("to@example.net", mods(nil))
	_, _ = s.Body(mods(map[string]string{"{i}": "QBRACE"}))

	if len(rec.entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(rec.entries))
	}
	e := rec.entries[0]
	if e.AppLogin != "app1" {
		t.Fatalf("AppLogin = %q, want app1 (braced {auth_authen} not resolved)", e.AppLogin)
	}
	if e.QueueID != "QBRACE" {
		t.Fatalf("QueueID = %q, want QBRACE (braced {i} not resolved)", e.QueueID)
	}
}

func TestDomainOf(t *testing.T) {
	cases := map[string]string{
		"user@Example.COM": "example.com",
		"no-domain":        "",
		"":                 "",
		"a@b@c.com":        "c.com",
	}
	for in, want := range cases {
		if got := domainOf(in); got != want {
			t.Fatalf("domainOf(%q) = %q, want %q", in, got, want)
		}
	}
}
