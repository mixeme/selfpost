package domain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteLoadPrivateKeyRoundtrip(t *testing.T) {
	key, err := generateDKIMKey()
	if err != nil {
		t.Fatalf("generateDKIMKey: %v", err)
	}
	path := filepath.Join(t.TempDir(), "selfpost.private")
	if err := writePrivateKeyPEM(path, key); err != nil {
		t.Fatalf("writePrivateKeyPEM: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o640 {
		t.Errorf("key perm = %o, want 0640", perm)
	}

	loaded, err := loadPrivateKeyPEM(path)
	if err != nil {
		t.Fatalf("loadPrivateKeyPEM: %v", err)
	}
	if loaded.N.Cmp(key.N) != 0 || loaded.E != key.E {
		t.Error("loaded key does not match generated key")
	}
}

func TestLoadPrivateKeyRejectsGarbage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.private")
	if err := os.WriteFile(path, []byte("not a pem"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPrivateKeyPEM(path); err == nil {
		t.Error("expected error for non-PEM key file")
	}
}

func TestDKIMRecord(t *testing.T) {
	key, err := generateDKIMKey()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := dkimRecord("selfpost", "example.com", &key.PublicKey)
	if err != nil {
		t.Fatalf("dkimRecord: %v", err)
	}
	if rec.Name != "selfpost._domainkey.example.com" {
		t.Errorf("record name = %q", rec.Name)
	}
	for _, want := range []string{"v=DKIM1", "h=sha256", "k=rsa", "p="} {
		if !strings.Contains(rec.Value, want) {
			t.Errorf("record value %q missing %q", rec.Value, want)
		}
	}
}

func TestWriteFileAtomicOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := writeFileAtomic(path, []byte("one"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(path, []byte("two"), 0o640); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "two" {
		t.Errorf("content = %q, want %q", got, "two")
	}
	// No stray temp files left behind in the directory.
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("expected 1 file after atomic writes, found %d", len(entries))
	}
}
