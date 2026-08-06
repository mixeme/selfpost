package main

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeberg.org/mix/selfpost/internal/store"
)

// seedDataDir builds the minimum /data tree a backup can be taken from.
func seedDataDir(t *testing.T) string {
	t.Helper()
	dataDir := t.TempDir()
	st, err := store.Open(filepath.Join(dataDir, "selfpost.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := st.AddDomain("example.com", "selfpost"); err != nil {
		t.Fatalf("add domain: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	t.Setenv("SELFPOST_DATA_DIR", dataDir)
	t.Setenv("SELFPOST_DB_PATH", filepath.Join(dataDir, "selfpost.db"))
	return dataDir
}

// An encrypted backup is only worth having if the container it came from can
// hand it back as an ordinary archive during a restore, so the two halves of
// the CLI are tested as the one round trip an operator actually performs.
func TestEncryptedBackupRoundTrip(t *testing.T) {
	seedDataDir(t)
	dir := t.TempDir()
	encrypted := filepath.Join(dir, "backup.spbk")
	plain := filepath.Join(dir, "backup.tar.gz")
	const password = "a long enough password"

	if err := run(encrypted, password); err != nil {
		t.Fatalf("create encrypted backup: %v", err)
	}
	head, err := os.ReadFile(encrypted)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if !strings.HasPrefix(string(head), "SELFPOST") {
		t.Fatalf("encrypted backup does not start with the envelope magic")
	}

	if err := runDecrypt(encrypted, plain, "the wrong password"); err == nil {
		t.Fatal("decryption with the wrong password succeeded")
	}
	if err := runDecrypt(encrypted, plain, password); err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	// What comes out must be the same gzip tar the plain path produces.
	f, err := os.Open(plain)
	if err != nil {
		t.Fatalf("open decrypted archive: %v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	names := map[string]bool{}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		names[hdr.Name] = true
	}
	for _, want := range []string{"manifest.json", "selfpost.db"} {
		if !names[want] {
			t.Errorf("decrypted archive has no %s (entries: %v)", want, names)
		}
	}
}

// Without a password the CLI keeps producing the plain archive that existing
// backup scripts consume.
func TestUnencryptedBackupStaysPlain(t *testing.T) {
	seedDataDir(t)
	out := filepath.Join(t.TempDir(), "backup.tar.gz")
	if err := run(out, ""); err != nil {
		t.Fatalf("create backup: %v", err)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()
	if _, err := gzip.NewReader(f); err != nil {
		t.Fatalf("plain backup is not a gzip archive: %v", err)
	}
}

// Decrypting needs a password, and it must come from a file or the environment
// — never an argument, which the process list would expose.
func TestReadPassword(t *testing.T) {
	dir := t.TempDir()
	pwFile := filepath.Join(dir, "pw")
	if err := os.WriteFile(pwFile, []byte("from the file\nignored second line\n"), 0o600); err != nil {
		t.Fatalf("write password file: %v", err)
	}

	t.Setenv(passwordEnv, "from the environment")
	got, err := readPassword("")
	if err != nil || got != "from the environment" {
		t.Errorf("readPassword(\"\") = %q, %v", got, err)
	}
	got, err = readPassword(pwFile)
	if err != nil || got != "from the file" {
		t.Errorf("readPassword(file) = %q, %v", got, err)
	}

	if err := os.WriteFile(pwFile, nil, 0o600); err != nil {
		t.Fatalf("truncate password file: %v", err)
	}
	if _, err := readPassword(pwFile); err == nil {
		t.Error("an empty password file was accepted")
	}

	os.Unsetenv(passwordEnv)
	if err := runDecrypt("", "", ""); err == nil {
		t.Error("-decrypt without a password was accepted")
	}
}
