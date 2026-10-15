package main

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/backup"
	"github.com/mixeme/selfpost/internal/store"
)

// seedProject builds the minimum operator project tree a backup can be taken from.
func seedProject(t *testing.T) (deployRoot, dataDir string) {
	t.Helper()
	deployRoot = t.TempDir()
	dataDir = filepath.Join(deployRoot, "data")
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
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
	writeFile(t, filepath.Join(deployRoot, backup.ComposeFileName), "services:\n  selfpost:\n    image: test\n")
	writeFile(t, filepath.Join(deployRoot, backup.EnvFileName), "SELFPOST_HOSTNAME=mail.example.com\n")
	writeFile(t, filepath.Join(deployRoot, backup.CertsDirName, "fullchain.pem"), "CERT")
	writeFile(t, filepath.Join(deployRoot, backup.CertsDirName, "privkey.pem"), "KEY")
	t.Setenv("SELFPOST_DATA_DIR", dataDir)
	t.Setenv("SELFPOST_DB_PATH", filepath.Join(dataDir, "selfpost.db"))
	t.Setenv("SELFPOST_DEPLOY_ROOT", deployRoot)
	return deployRoot, dataDir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// An encrypted backup is only worth having if the container it came from can
// hand it back as an ordinary archive during a restore, so the two halves of
// the CLI are tested as the one round trip an operator actually performs.
func TestEncryptedBackupRoundTrip(t *testing.T) {
	seedProject(t)
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
	for _, want := range []string{
		backup.DataArchivePrefix + backup.ManifestName,
		backup.DataArchivePrefix + "selfpost.db",
		backup.ComposeFileName,
		backup.EnvFileName,
	} {
		if !names[want] {
			t.Errorf("decrypted archive has no %s (entries: %v)", want, names)
		}
	}
}

// Without a password the CLI keeps producing the plain archive that existing
// backup scripts consume.
func TestUnencryptedBackupStaysPlain(t *testing.T) {
	seedProject(t)
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

// An encrypted backup is held to the panel's password floor (audit
// 2026-10-12, M5); a refused one writes nothing.
func TestEncryptionPasswordFloor(t *testing.T) {
	seedProject(t)
	out := filepath.Join(t.TempDir(), "backup.spbk")
	err := run(out, "eleven-char")
	if err == nil || !strings.Contains(err.Error(), "at least 12 characters") {
		t.Fatalf("an 11-character password = %v, want refused", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("the refused backup wrote %s", out)
	}
}

// A wrong SELFPOST_DB_PATH makes no backup and no database (audit
// 2026-10-12, M3).
func TestBackupOfAMissingDatabaseIsRefused(t *testing.T) {
	_, dataDir := seedProject(t)
	missing := filepath.Join(dataDir, "typo.db")
	t.Setenv("SELFPOST_DB_PATH", missing)
	out := filepath.Join(t.TempDir(), "backup.tar.gz")
	if err := run(out, ""); err == nil {
		t.Fatal("a backup of a database that is not there was made")
	}
	for _, p := range []string{missing, out} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("the refused backup left %s (stat err = %v)", p, err)
		}
	}
}

// -decrypt -o writes its file whole or not at all: a wrong password or an
// envelope cut short leaves the file that had the name before, and nothing
// else in its directory (audit 2026-10-12, L1 and stage 3).
func TestDecryptLeavesTheOutputAloneOnFailure(t *testing.T) {
	seedProject(t)
	dir := t.TempDir()
	encrypted := filepath.Join(dir, "backup.spbk")
	const password = "a long enough password"
	if err := run(encrypted, password); err != nil {
		t.Fatalf("create encrypted backup: %v", err)
	}
	sealed, err := os.ReadFile(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	cut := filepath.Join(dir, "cut.spbk")
	writeFile(t, cut, string(sealed[:len(sealed)-100]))

	outDir := t.TempDir()
	out := filepath.Join(outDir, "backup.tar.gz")
	const before = "the archive that was here"
	for _, c := range []struct{ name, in, password, want string }{
		{"wrong password", encrypted, "the wrong password", "wrong password"},
		{"cut short", cut, password, "corrupted"},
	} {
		t.Run(c.name, func(t *testing.T) {
			writeFile(t, out, before)
			err := runDecrypt(c.in, out, c.password)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("runDecrypt = %v, want %q", err, c.want)
			}
			if got, _ := os.ReadFile(out); string(got) != before {
				t.Errorf("the failed decryption changed %s to %d bytes", out, len(got))
			}
			entries, _ := os.ReadDir(outDir)
			if len(entries) != 1 {
				t.Errorf("the failed decryption left files beside its output: %v", entries)
			}
		})
	}
}
