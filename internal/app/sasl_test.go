package app

import (
	"encoding/hex"
	"errors"
	"fmt"
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

// makeDump builds a db_dump byte-value document from key/value byte pairs, the
// same shape `db_dump <sasldb2>` emits.
func makeDump(pairs [][2][]byte) []byte {
	var b strings.Builder
	b.WriteString("VERSION=3\nformat=bytevalue\ntype=hash\nHEADER=END\n")
	for _, p := range pairs {
		fmt.Fprintf(&b, " %s\n", hex.EncodeToString(p[0]))
		fmt.Fprintf(&b, " %s\n", hex.EncodeToString(p[1]))
	}
	b.WriteString("DATA=END\n")
	return []byte(b.String())
}

func saslKey(login, realm, prop string) []byte {
	return []byte(login + "\x00" + realm + "\x00" + prop)
}

func TestSecretExtractsPassword(t *testing.T) {
	s := NewSASLDB("/data/sasl/sasldb2", "mail.example.com")
	s.dump = func(path string) ([]byte, error) {
		if path != "/data/sasl/sasldb2" {
			t.Errorf("dump path = %q", path)
		}
		return makeDump([][2][]byte{
			{saslKey("other", "mail.example.com", "userPassword"), []byte("otherpw")},
			{saslKey("alerts", "mail.example.com", "userPassword"), []byte("hunter2-pass")},
		}), nil
	}
	got, err := s.Secret("alerts")
	if err != nil {
		t.Fatalf("Secret: %v", err)
	}
	if got != "hunter2-pass" {
		t.Errorf("Secret = %q, want %q", got, "hunter2-pass")
	}
}

func TestSecretRealmMismatchNotFound(t *testing.T) {
	s := NewSASLDB("/data/sasl/sasldb2", "mail.example.com")
	s.dump = func(string) ([]byte, error) {
		// Same login but a different realm must not match.
		return makeDump([][2][]byte{
			{saslKey("alerts", "other.host", "userPassword"), []byte("hunter2")},
		}), nil
	}
	if _, err := s.Secret("alerts"); !errors.Is(err, ErrSecretNotFound) {
		t.Errorf("Secret err = %v, want ErrSecretNotFound", err)
	}
}

func TestSecretMissingLoginNotFound(t *testing.T) {
	s := NewSASLDB("/data/sasl/sasldb2", "mail.example.com")
	s.dump = func(string) ([]byte, error) {
		return makeDump(nil), nil
	}
	if _, err := s.Secret("ghost"); !errors.Is(err, ErrSecretNotFound) {
		t.Errorf("Secret err = %v, want ErrSecretNotFound", err)
	}
}

func TestSecretRejectsInvalidLoginBeforeDump(t *testing.T) {
	s := NewSASLDB("/data/sasl/sasldb2", "mail.example.com")
	called := false
	s.dump = func(string) ([]byte, error) { called = true; return nil, nil }
	if _, err := s.Secret("bad login"); err == nil {
		t.Error("Secret accepted invalid login")
	}
	if called {
		t.Error("db_dump invoked for an invalid login")
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
