package app

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A change is made to a copy and renamed over the database: saslpasswd2
// never gets the live file, and a reader that opened the old file keeps
// reading it whole (assumptions C-S3).
func TestSASLChangeGoesThroughACopy(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "sasldb2")
	if err := os.WriteFile(db, []byte("old database"), 0o640); err != nil {
		t.Fatal(err)
	}
	// smtpd in the middle of a lookup. Windows refuses a rename over an open
	// file; the container is Linux, where the reader keeps the old inode.
	var reader *os.File
	if runtime.GOOS != "windows" {
		var err error
		if reader, err = os.Open(db); err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
	}

	s := NewSASLDB(db, "mail.example.com")
	var gotPath string
	s.run = func(args []string, stdin []byte) error {
		gotPath = args[3]
		before, err := os.ReadFile(gotPath)
		if err != nil {
			return err
		}
		if string(before) != "old database" {
			t.Errorf("the copy holds %q, want the database's bytes", before)
		}
		return os.WriteFile(gotPath, []byte("new database"), 0o600)
	}
	if err := s.Set("alerts", "pw"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if gotPath == db {
		t.Fatal("saslpasswd2 was given the live database")
	}
	if got, _ := os.ReadFile(db); string(got) != "new database" {
		t.Fatalf("database after the change: %q", got)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(db); fi.Mode().Perm() != 0o640 {
			t.Fatalf("database mode %v, want 0640 for Postfix's group", fi.Mode().Perm())
		}
		old := make([]byte, 64)
		n, _ := reader.ReadAt(old, 0)
		if string(old[:n]) != "old database" {
			t.Fatalf("a reader of the old file read %q", old[:n])
		}
	}
	if _, err := os.Stat(db + ".new"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the copy is left behind: %v", err)
	}
}

func TestSASLFailedChangeLeavesTheDatabase(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "sasldb2")
	if err := os.WriteFile(db, []byte("old database"), 0o640); err != nil {
		t.Fatal(err)
	}
	// A copy a killed panel left behind is not taken for the database.
	if err := os.WriteFile(db+".new", []byte("stale"), 0o640); err != nil {
		t.Fatal(err)
	}
	s := NewSASLDB(db, "mail.example.com")
	s.run = func(args []string, stdin []byte) error {
		if b, _ := os.ReadFile(args[2]); string(b) != "old database" {
			t.Errorf("the copy holds %q", b)
		}
		_ = os.WriteFile(args[2], []byte("half"), 0o640)
		return errors.New("saslpasswd2: exit status 1")
	}
	if err := s.Delete("alerts"); err == nil {
		t.Fatal("Delete succeeded with saslpasswd2 failing")
	}
	if got, _ := os.ReadFile(db); string(got) != "old database" {
		t.Fatalf("database after a failed change: %q", got)
	}
	if _, err := os.Stat(db + ".new"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the copy is left behind: %v", err)
	}
}

// The first account: there is no database to copy, and saslpasswd2 creates
// one at the path it is given.
func TestSASLFirstAccountCreatesTheDatabase(t *testing.T) {
	db := filepath.Join(t.TempDir(), "sasldb2")
	s := NewSASLDB(db, "mail.example.com")
	s.run = func(args []string, stdin []byte) error {
		if _, err := os.Stat(args[3]); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("saslpasswd2 given an existing file %s: %v", args[3], err)
		}
		return os.WriteFile(args[3], []byte("created"), 0o660)
	}
	if err := s.Set("alerts", "pw"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got, _ := os.ReadFile(db); string(got) != "created" {
		t.Fatalf("database: %q", got)
	}
}
