package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeberg.org/mix/selfpost/internal/store"
)

// seedDataDir builds a realistic /data tree: a migrated SQLite database plus the
// DKIM key, SASL and transient files a backup must include or exclude.
func seedDataDir(t *testing.T) (dataDir, dbPath string) {
	t.Helper()
	dataDir = t.TempDir()
	dbPath = filepath.Join(dataDir, "selfpost.db")

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := st.AddDomain("example.com", "selfpost"); err != nil {
		t.Fatalf("add domain: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	writeFile(t, filepath.Join(dataDir, "opendkim", "keys", "example.com", "selfpost.private"), "PRIVATE KEY")
	writeFile(t, filepath.Join(dataDir, "sasl", "sasldb2"), "SASLDB")
	writeFile(t, filepath.Join(dataDir, "postfix", "sender_login_maps"), "@example.com login")
	// Transient files that must NOT be archived.
	writeFile(t, filepath.Join(dataDir, "setup-token"), "secret-token")
	writeFile(t, filepath.Join(dataDir, "selfpost.db-wal"), "wal")
	writeFile(t, filepath.Join(dataDir, "selfpost.db-shm"), "shm")
	return dataDir, dbPath
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

// readArchive returns the set of regular-file entries (name -> content) in a
// gzip tar produced by Create.
func readArchive(t *testing.T, data []byte) map[string]string {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	tr := tar.NewReader(gz)
	out := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("tar read %s: %v", hdr.Name, err)
		}
		out[hdr.Name] = string(b)
	}
	return out
}

func TestCreateIncludesStateExcludesTransient(t *testing.T) {
	dataDir, dbPath := seedDataDir(t)

	var buf bytes.Buffer
	if err := Create(&buf, Params{DataDir: dataDir, DBPath: dbPath, Version: "1.2.3"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	files := readArchive(t, buf.Bytes())

	// Present.
	for _, name := range []string{
		ManifestName,
		"selfpost.db",
		"opendkim/keys/example.com/selfpost.private",
		"sasl/sasldb2",
		"postfix/sender_login_maps",
	} {
		if _, ok := files[name]; !ok {
			t.Errorf("archive missing %s", name)
		}
	}
	// Excluded.
	for _, name := range []string{"setup-token", "selfpost.db-wal", "selfpost.db-shm"} {
		if _, ok := files[name]; ok {
			t.Errorf("archive should not contain %s", name)
		}
	}

	// Manifest is well-formed and carries the version.
	var m Manifest
	if err := json.Unmarshal([]byte(files[ManifestName]), &m); err != nil {
		t.Fatalf("manifest json: %v", err)
	}
	if m.Format != FormatFull || m.Version != "1.2.3" {
		t.Errorf("manifest = %+v, want format=%s version=1.2.3", m, FormatFull)
	}

	// The archived selfpost.db is a real, openable SQLite snapshot with our data.
	snapPath := filepath.Join(t.TempDir(), "restored.db")
	if err := os.WriteFile(snapPath, []byte(files["selfpost.db"]), 0o640); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
	st, err := store.Open(snapPath)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	defer st.Close()
	domains, err := st.ListDomains()
	if err != nil {
		t.Fatalf("list domains from snapshot: %v", err)
	}
	if len(domains) != 1 || domains[0].Name != "example.com" {
		t.Errorf("snapshot domains = %+v, want one example.com", domains)
	}
}

func writeManifest(t *testing.T, dir, format, version string) string {
	t.Helper()
	path := filepath.Join(dir, ManifestName)
	b, _ := json.Marshal(Manifest{Format: format, Version: version, CreatedAt: "now"})
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

func TestCheckRestoreNoManifestIsNormalStart(t *testing.T) {
	if err := CheckRestore(filepath.Join(t.TempDir(), "manifest.json"), "1.0.0"); err != nil {
		t.Errorf("CheckRestore with no manifest = %v, want nil", err)
	}
}

func TestCheckRestoreMatchConsumesManifest(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, FormatFull, "1.0.0")
	if err := CheckRestore(path, "1.0.0"); err != nil {
		t.Fatalf("CheckRestore matching = %v, want nil", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("manifest should be consumed after a matching restore, stat err = %v", err)
	}
}

func TestCheckRestoreVersionMismatchRefusesAndKeeps(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, FormatFull, "1.0.0")
	err := CheckRestore(path, "2.0.0")
	if err == nil {
		t.Fatal("CheckRestore mismatch = nil, want error")
	}
	if !strings.Contains(err.Error(), "1.0.0") || !strings.Contains(err.Error(), "2.0.0") {
		t.Errorf("error should name both versions: %v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("manifest must be kept on mismatch so the operator can switch images: %v", statErr)
	}
}

func TestCheckRestoreWrongFormatRejected(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, "something-else", "1.0.0")
	if err := CheckRestore(path, "1.0.0"); err == nil {
		t.Error("CheckRestore accepted a non-backup manifest")
	}
}
