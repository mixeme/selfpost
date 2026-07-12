package app

import (
	"strings"
	"testing"
)

type fakeRun struct {
	args  []string
	stdin string
	calls int
}

func newFakeSASL() (*SASLDB, *fakeRun) {
	fr := &fakeRun{}
	s := NewSASLDB("/data/sasl/sasldb2", "mail.example.com")
	s.run = func(args []string, stdin []byte) error {
		fr.calls++
		fr.args = args
		fr.stdin = string(stdin)
		return nil
	}
	return s, fr
}

func TestSASLSetPassesPasswordOnStdinNotArgv(t *testing.T) {
	s, fr := newFakeSASL()
	const secret = "s3cr3t-p4ss"
	if err := s.Set("alerts", secret); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if fr.stdin != secret {
		t.Errorf("password not passed on stdin: got %q", fr.stdin)
	}
	joined := strings.Join(fr.args, " ")
	if strings.Contains(joined, secret) {
		t.Errorf("password leaked into argv: %q", joined)
	}
	// Expected fixed flags and the login as its own trailing argument.
	want := []string{"-p", "-c", "-f", "/data/sasl/sasldb2", "-u", "mail.example.com", "alerts"}
	if len(fr.args) != len(want) {
		t.Fatalf("args = %v, want %v", fr.args, want)
	}
	for i := range want {
		if fr.args[i] != want[i] {
			t.Fatalf("args = %v, want %v", fr.args, want)
		}
	}
}

func TestSASLDeleteArgs(t *testing.T) {
	s, fr := newFakeSASL()
	if err := s.Delete("alerts"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	want := []string{"-d", "-f", "/data/sasl/sasldb2", "-u", "mail.example.com", "alerts"}
	if strings.Join(fr.args, " ") != strings.Join(want, " ") {
		t.Errorf("delete args = %v, want %v", fr.args, want)
	}
	if fr.stdin != "" {
		t.Errorf("delete should not send stdin, got %q", fr.stdin)
	}
}

func TestSASLRejectsInvalidLoginBeforeExec(t *testing.T) {
	s, fr := newFakeSASL()
	if err := s.Set("bad login", "pw"); err == nil {
		t.Error("Set accepted invalid login")
	}
	if err := s.Delete("bad@login"); err == nil {
		t.Error("Delete accepted invalid login")
	}
	if fr.calls != 0 {
		t.Errorf("saslpasswd2 invoked %d times for invalid logins, want 0", fr.calls)
	}
}
