package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/store"
)

// seedDataDir builds a realistic /data tree: a migrated SQLite database plus the
// DKIM key, SASL and transient files a backup must include or exclude. It also
// seeds a deploy root beside data/ with compose, .env, and certs/.
func seedDataDir(t *testing.T) (dataDir, dbPath, deployRoot string) {
	t.Helper()
	deployRoot = t.TempDir()
	dataDir = filepath.Join(deployRoot, "data")
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
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
	writeFile(t, filepath.Join(dataDir, "antispam", "deny_domain.map"), "evil.example\n")
	writeFile(t, filepath.Join(dataDir, "postfix", "queue", "deferred", "sample"), "queue-file")
	// Transient files that must NOT be archived.
	writeFile(t, filepath.Join(dataDir, "setup-token"), "secret-token")
	writeFile(t, filepath.Join(dataDir, "selfpost.db-wal"), "wal")
	writeFile(t, filepath.Join(dataDir, "selfpost.db-shm"), "shm")
	// Postfix's delivery log and its rotated files: diagnostic output, not
	// state, and the bulkiest thing under /data. The line is composed by hand;
	// only its absence from the archive matters.
	writeFile(t, filepath.Join(dataDir, "log", "mail.log"), "Aug  8 07:26:41 mail postfix/smtp[1]: ABC: to=<a@example.net>, status=sent (ok)")
	writeFile(t, filepath.Join(dataDir, "log", "mail.log.1"), "older")
	writeFile(t, filepath.Join(deployRoot, ComposeFileName), "services:\n  selfpost:\n    image: test\n")
	writeFile(t, filepath.Join(deployRoot, EnvFileName), "SELFPOST_HOSTNAME=mail.example.com\n")
	writeFile(t, filepath.Join(deployRoot, CertsDirName, "fullchain.pem"), "CERT")
	writeFile(t, filepath.Join(deployRoot, CertsDirName, "privkey.pem"), "KEY")
	return dataDir, dbPath, deployRoot
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
	dataDir, dbPath, deployRoot := seedDataDir(t)

	var buf bytes.Buffer
	if err := Create(&buf, Params{DataDir: dataDir, DBPath: dbPath, Version: "1.2.3", DeployRoot: deployRoot}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	files := readArchive(t, buf.Bytes())

	// Present.
	for _, name := range []string{
		DataArchivePrefix + ManifestName,
		DataArchivePrefix + "selfpost.db",
		DataArchivePrefix + "opendkim/keys/example.com/selfpost.private",
		DataArchivePrefix + "sasl/sasldb2",
		DataArchivePrefix + "postfix/sender_login_maps",
		// The filter lists as the spam filter reads them. They are rewritten
		// from the database on the first start after a restore; carrying them
		// means the filter has its lists before the panel is up.
		DataArchivePrefix + "antispam/deny_domain.map",
		ComposeFileName,
		EnvFileName,
		CertsDirName + "/fullchain.pem",
		CertsDirName + "/privkey.pem",
	} {
		if _, ok := files[name]; !ok {
			t.Errorf("archive missing %s", name)
		}
	}
	// Excluded.
	for _, name := range []string{
		DataArchivePrefix + "setup-token",
		DataArchivePrefix + "selfpost.db-wal",
		DataArchivePrefix + "selfpost.db-shm",
		DataArchivePrefix + "log/mail.log",
		DataArchivePrefix + "log/mail.log.1",
		DataArchivePrefix + "postfix/queue/deferred/sample",
	} {
		if _, ok := files[name]; ok {
			t.Errorf("archive should not contain %s", name)
		}
	}

	// Manifest is well-formed and carries the version.
	var m Manifest
	if err := json.Unmarshal([]byte(files[DataArchivePrefix+ManifestName]), &m); err != nil {
		t.Fatalf("manifest json: %v", err)
	}
	if m.Format != FormatFull || m.Version != "1.2.3" {
		t.Errorf("manifest = %+v, want format=%s version=1.2.3", m, FormatFull)
	}

	// The archived selfpost.db is a real, openable SQLite snapshot with our data.
	snapPath := filepath.Join(t.TempDir(), "restored.db")
	if err := os.WriteFile(snapPath, []byte(files[DataArchivePrefix+"selfpost.db"]), 0o640); err != nil {
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

func TestCreateRequiresDeployRoot(t *testing.T) {
	dataDir, dbPath, _ := seedDataDir(t)
	var buf bytes.Buffer
	if err := Create(&buf, Params{DataDir: dataDir, DBPath: dbPath, Version: "1.0.0"}); err == nil {
		t.Fatal("Create without DeployRoot succeeded")
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
	restored, err := CheckRestore(filepath.Join(t.TempDir(), "manifest.json"), "1.0.0")
	if err != nil {
		t.Errorf("CheckRestore with no manifest = %v, want nil", err)
	}
	if restored {
		t.Error("CheckRestore with no manifest reported a restore")
	}
}

// unpackData writes the data directory of a Create archive into a new
// directory and returns the manifest's path there.
func unpackData(t *testing.T, archive []byte) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range readArchive(t, archive) {
		if rel, ok := strings.CutPrefix(name, DataArchivePrefix); ok {
			writeFile(t, filepath.Join(dir, filepath.FromSlash(rel)), body)
		}
	}
	return filepath.Join(dir, ManifestName)
}

func createArchive(t *testing.T, version string) []byte {
	t.Helper()
	dataDir, dbPath, deployRoot := seedDataDir(t)
	var buf bytes.Buffer
	if err := Create(&buf, Params{DataDir: dataDir, DBPath: dbPath, Version: version, DeployRoot: deployRoot}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return buf.Bytes()
}

// A matching restore keeps its manifest until the start that runs it is done
// with it (audit 2026-10-12, M1): CheckRestore reports, ConsumeRestore ends.
func TestCheckRestoreMatchKeepsTheManifestUntilConsumed(t *testing.T) {
	path := unpackData(t, createArchive(t, "1.0.0"))
	restored, err := CheckRestore(path, "1.0.0")
	if err != nil {
		t.Fatalf("CheckRestore matching = %v, want nil", err)
	}
	if !restored {
		t.Fatal("CheckRestore matching did not report a restore")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("CheckRestore consumed the manifest before the start was done with it: %v", err)
	}
	if err := ConsumeRestore(path); err != nil {
		t.Fatalf("ConsumeRestore: %v", err)
	}
	for _, name := range []string{ManifestName, ContentsName, contentsCheckedName} {
		if _, err := os.Stat(filepath.Join(filepath.Dir(path), name)); !os.IsNotExist(err) {
			t.Errorf("%s left after ConsumeRestore (stat err = %v)", name, err)
		}
	}
	if err := ConsumeRestore(path); err != nil {
		t.Errorf("ConsumeRestore on a consumed restore = %v, want nil", err)
	}
}

// The archive's last entry records the data directory's files - every one,
// with its size and digest - and nothing of the deploy files, which the
// operator edits before the first start.
func TestCreateEndsWithTheRecordOfItsData(t *testing.T) {
	archive := createArchive(t, "1.0.0")
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	tr := tar.NewReader(gz)
	var last string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		last = hdr.Name
	}
	if last != DataArchivePrefix+ContentsName {
		t.Fatalf("the last entry is %s, want %s", last, DataArchivePrefix+ContentsName)
	}

	files := readArchive(t, archive)
	var c Contents
	if err := json.Unmarshal([]byte(files[DataArchivePrefix+ContentsName]), &c); err != nil {
		t.Fatalf("contents: %v", err)
	}
	recorded := map[string]FileSum{}
	for _, f := range c.Files {
		recorded[f.Name] = f
	}
	for name, body := range files {
		if name == DataArchivePrefix+ContentsName {
			continue
		}
		f, ok := recorded[name]
		if !strings.HasPrefix(name, DataArchivePrefix) {
			if ok {
				t.Errorf("the record names the deploy file %s", name)
			}
			continue
		}
		sum := sha256.Sum256([]byte(body))
		if !ok || f.Size != int64(len(body)) || f.SHA256 != hex.EncodeToString(sum[:]) {
			t.Errorf("%s recorded as %+v, want size %d sha256 %x", name, f, len(body), sum)
		}
		delete(recorded, name)
	}
	for name := range recorded {
		t.Errorf("the record names %s, which the archive does not hold", name)
	}
}

// Without the archive's last entry, or with a file that is not the one it
// holds, a restore is refused, and the manifest stays for the next try.
func TestCheckRestoreRefusesAnArchiveUnpackedInPart(t *testing.T) {
	sasldb := func(body string) func(string) {
		return func(dir string) { writeFile(t, filepath.Join(dir, "sasl", "sasldb2"), body) }
	}
	for _, c := range []struct {
		name   string
		break_ func(dir string)
		want   string
	}{
		{"no record", func(dir string) { os.Remove(filepath.Join(dir, ContentsName)) }, "contents.json, its last entry, is not there"},
		{"a record cut", func(dir string) { writeFile(t, filepath.Join(dir, ContentsName), `{"format":"selfpost-backup-con`) }, "is not a record"},
		{"a file missing", func(dir string) { os.Remove(filepath.Join(dir, "sasl", "sasldb2")) }, "data/sasl/sasldb2 is missing"},
		{"a file cut", sasldb("SASL"), "data/sasl/sasldb2 is not the file"},
		{"a file altered", sasldb("SASLDC"), "data/sasl/sasldb2 is not the file"},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := unpackData(t, createArchive(t, "1.0.0"))
			c.break_(filepath.Dir(path))
			restored, err := CheckRestore(path, "1.0.0")
			if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "not unpacked whole") {
				t.Fatalf("CheckRestore = %v, want a refusal saying %q", err, c.want)
			}
			if restored {
				t.Error("a refused restore reported a restore")
			}
			if _, err := os.Stat(path); err != nil {
				t.Errorf("the refusal removed the manifest: %v", err)
			}
		})
	}
}

// A record names files of the data directory only; one that names another
// path is refused, not followed.
func TestCheckRestoreRefusesARecordOutsideTheDataDirectory(t *testing.T) {
	for _, name := range []string{"data/../.env", "data/", "docker-compose.yml", "data//sasl/sasldb2", "data/./x", "data/.."} {
		path := unpackData(t, createArchive(t, "1.0.0"))
		body, _ := json.Marshal(Contents{Format: FormatContents, Files: []FileSum{{Name: name}}})
		writeFile(t, filepath.Join(filepath.Dir(path), ContentsName), string(body))
		if _, err := CheckRestore(path, "1.0.0"); err == nil || !strings.Contains(err.Error(), "not a file of the data directory") {
			t.Errorf("a record naming %q = %v, want it refused", name, err)
		}
	}
}

// The files are compared once per restore: a start that failed after the
// check had the database open, and the next start must not take the changes
// it made for damage. A different archive unpacked over the directory since
// is compared anew.
func TestCheckRestoreComparesTheFilesOncePerRestore(t *testing.T) {
	path := unpackData(t, createArchive(t, "1.0.0"))
	dir := filepath.Dir(path)
	if _, err := CheckRestore(path, "1.0.0"); err != nil {
		t.Fatalf("first CheckRestore: %v", err)
	}
	writeFile(t, filepath.Join(dir, "selfpost.db"), "changed by the start that failed")
	if _, err := CheckRestore(path, "1.0.0"); err != nil {
		t.Fatalf("the start after a failed one compared the files again: %v", err)
	}

	// Another archive's manifest, its record lost on the way.
	b, _ := json.Marshal(Manifest{Format: FormatFull, Version: "1.0.0", CreatedAt: "another time"})
	writeFile(t, path, string(b))
	if _, err := CheckRestore(path, "1.0.0"); err == nil || !strings.Contains(err.Error(), "not unpacked whole") {
		t.Fatalf("another archive's manifest over a checked restore = %v, want it compared anew and refused", err)
	}
}

func TestCheckRestoreVersionMismatchRefusesAndKeeps(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, FormatFull, "1.0.0")
	restored, err := CheckRestore(path, "2.0.0")
	if err == nil {
		t.Fatal("CheckRestore mismatch = nil, want error")
	}
	if restored {
		t.Error("CheckRestore mismatch reported a restore")
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
	restored, err := CheckRestore(path, "1.0.0")
	if err == nil {
		t.Error("CheckRestore accepted a non-backup manifest")
	}
	if restored {
		t.Error("CheckRestore wrong format reported a restore")
	}
}

// No archive holds the Postfix queue; the rest is there, the private key
// included, and Check agrees that it can be made.
func TestCreateLeavesTheQueueOut(t *testing.T) {
	dataDir, dbPath, deployRoot := seedDataDir(t)
	p := Params{DataDir: dataDir, DBPath: dbPath, Version: "1.2.3", DeployRoot: deployRoot}

	if err := Check(p); err != nil {
		t.Fatalf("Check: %v", err)
	}
	var buf bytes.Buffer
	if err := Create(&buf, p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	files := readArchive(t, buf.Bytes())
	for name := range files {
		if strings.HasPrefix(name, DataArchivePrefix+QueueDir) {
			t.Errorf("the panel's archive holds %s", name)
		}
	}
	for _, name := range []string{
		DataArchivePrefix + "selfpost.db",
		DataArchivePrefix + "opendkim/keys/example.com/selfpost.private",
		DataArchivePrefix + "sasl/sasldb2",
		DataArchivePrefix + "postfix/sender_login_maps",
		ComposeFileName, EnvFileName,
		CertsDirName + "/fullchain.pem",
		CertsDirName + "/privkey.pem",
	} {
		if _, ok := files[name]; !ok {
			t.Errorf("archive missing %s", name)
		}
	}
}

// A file under certs/ that may not be read is told apart from any other
// failure, so the panel can say what to do about it.
func TestUnreadableCert(t *testing.T) {
	root := filepath.Join("srv", "deploy")
	denied := func(path string) error {
		return fmt.Errorf("backup: open %s: %w", path, &fs.PathError{Op: "open", Path: path, Err: fs.ErrPermission})
	}
	if name, ok := UnreadableCert(denied(filepath.Join(root, "certs", "privkey.pem")), root); !ok || name != "certs/privkey.pem" {
		t.Errorf("certs/privkey.pem: got %q, %v", name, ok)
	}
	if name, ok := UnreadableCert(denied(filepath.Join(root, ".env")), root); ok {
		t.Errorf(".env was taken for a certificate file: %q", name)
	}
	if name, ok := UnreadableCert(denied(filepath.Join("data", "postfix", "x")), root); ok {
		t.Errorf("a data file was taken for a certificate file: %q", name)
	}
	missing := &fs.PathError{Op: "open", Path: filepath.Join(root, "certs", "privkey.pem"), Err: fs.ErrNotExist}
	if _, ok := UnreadableCert(missing, root); ok {
		t.Error("a missing file was taken for one that may not be read")
	}
}

// Check finds what Create would stumble over, before anything is written.
func TestCheckReportsWhatCannotBeRead(t *testing.T) {
	dataDir, dbPath, deployRoot := seedDataDir(t)
	if err := os.Remove(filepath.Join(deployRoot, EnvFileName)); err != nil {
		t.Fatal(err)
	}
	if err := Check(Params{DataDir: dataDir, DBPath: dbPath, Version: "1.2.3", DeployRoot: deployRoot}); err == nil {
		t.Fatal("Check passed with .env missing from the deploy root")
	}
}

// certs/ as certbot's advice leaves it: links into live/, which are relative
// links into archive/ (C-Q4). The archive holds the files, under the links'
// names; a link to nothing reachable stops the backup, naming it.
func TestCertsLinksAreFollowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need privileges on Windows; the container is Linux")
	}
	dataDir, dbPath, deployRoot := seedDataDir(t)
	le := t.TempDir()
	writeFile(t, filepath.Join(le, "archive", "mail.example.com", "fullchain1.pem"), "CERT FROM CERTBOT")
	writeFile(t, filepath.Join(le, "archive", "mail.example.com", "privkey1.pem"), "KEY FROM CERTBOT")
	live := filepath.Join(le, "live", "mail.example.com")
	if err := os.MkdirAll(live, 0o755); err != nil {
		t.Fatal(err)
	}
	certs := filepath.Join(deployRoot, CertsDirName)
	for _, f := range []string{"fullchain", "privkey"} {
		if err := os.Symlink("../../archive/mail.example.com/"+f+"1.pem", filepath.Join(live, f+".pem")); err != nil {
			t.Fatal(err)
		}
		os.Remove(filepath.Join(certs, f+".pem"))
		if err := os.Symlink(filepath.Join(live, f+".pem"), filepath.Join(certs, f+".pem")); err != nil {
			t.Fatal(err)
		}
	}
	p := Params{DataDir: dataDir, DBPath: dbPath, Version: "1.2.3", DeployRoot: deployRoot}
	if err := Check(p); err != nil {
		t.Fatalf("Check: %v", err)
	}
	var buf bytes.Buffer
	if err := Create(&buf, p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	files := readArchive(t, buf.Bytes())
	if files[CertsDirName+"/fullchain.pem"] != "CERT FROM CERTBOT" || files[CertsDirName+"/privkey.pem"] != "KEY FROM CERTBOT" {
		t.Fatalf("certs in the archive: %q, %q", files[CertsDirName+"/fullchain.pem"], files[CertsDirName+"/privkey.pem"])
	}

	// /etc/letsencrypt not mounted: the link leads nowhere.
	if err := os.RemoveAll(le); err != nil {
		t.Fatal(err)
	}
	err := Check(p)
	if err == nil || !strings.Contains(err.Error(), "certs/fullchain.pem is a link to a file this container cannot reach") {
		t.Fatalf("Check with a dangling link: %v", err)
	}
}

// A wrong SELFPOST_DB_PATH is refused, and nothing is created where it
// points: SQLite makes a database of a path that is not one, and an archive
// of that empty database restores a server that offers its first-run setup
// (audit 2026-10-12, M3). The snapshot opens the source read-only, which is
// what refuses it should the file go between the check and the open.
func TestSnapshotRefusesAMissingDatabase(t *testing.T) {
	dataDir, _, deployRoot := seedDataDir(t)
	missing := filepath.Join(dataDir, "elsewhere.db")
	var buf bytes.Buffer
	err := Create(&buf, Params{DataDir: dataDir, DBPath: missing, Version: "1.0.0", DeployRoot: deployRoot})
	if err == nil {
		t.Fatal("a backup of a database that is not there was made")
	}
	if _, statErr := os.Stat(missing); !os.IsNotExist(statErr) {
		t.Fatalf("the refused backup created %s (stat err = %v)", missing, statErr)
	}

	db, err := sql.Open("sqlite", "file:"+missing+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(); err == nil {
		t.Fatal("mode=ro opened a database that is not there")
	}
	if _, statErr := os.Stat(missing); !os.IsNotExist(statErr) {
		t.Fatalf("mode=ro created %s (stat err = %v)", missing, statErr)
	}
}

// certs/ there and empty is refused by Check and Create alike: the archive
// would restore a server without its certificate (audit 2026-10-12, L2).
func TestEmptyCertsIsRefused(t *testing.T) {
	dataDir, dbPath, deployRoot := seedDataDir(t)
	for _, f := range []string{"fullchain.pem", "privkey.pem"} {
		os.Remove(filepath.Join(deployRoot, CertsDirName, f))
	}
	p := Params{DataDir: dataDir, DBPath: dbPath, Version: "1.0.0", DeployRoot: deployRoot}
	if err := Check(p); err == nil || !strings.Contains(err.Error(), "certs/ holds no file") {
		t.Errorf("Check with an empty certs/ = %v", err)
	}
	if err := Create(io.Discard, p); err == nil || !strings.Contains(err.Error(), "certs/ holds no file") {
		t.Errorf("Create with an empty certs/ = %v", err)
	}
}

// What a backup leaves out it says it leaves out: a link under data/, and a
// link under certs/ to a directory (audit 2026-10-12, L2).
func TestBackupWarnsOfWhatItLeavesOut(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need privileges on Windows; the container is Linux")
	}
	dataDir, dbPath, deployRoot := seedDataDir(t)
	if err := os.Symlink("sasl/sasldb2", filepath.Join(dataDir, "a-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(deployRoot, CertsDirName, "a-directory")); err != nil {
		t.Fatal(err)
	}
	var warnings []string
	p := Params{DataDir: dataDir, DBPath: dbPath, Version: "1.0.0", DeployRoot: deployRoot,
		OnWarn: func(s string) { warnings = append(warnings, s) }}
	if err := Create(io.Discard, p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got := strings.Join(warnings, "\n")
	for _, want := range []string{
		"data/a-link is a link, not a file: the backup does not carry it",
		"certs/a-directory is a link to a directory, not a file: the backup does not carry it",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no warning %q in:\n%s", want, got)
		}
	}
}

// A link or a pipe where the record names a file is refused, not followed or
// read: a crafted archive must not make the start read outside /data, or
// wait (security review of audit stage 3).
func TestCheckRestoreReadsOnlyRegularFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need privileges on Windows; the container is Linux")
	}
	path := unpackData(t, createArchive(t, "1.0.0"))
	dir := filepath.Dir(path)
	sasl := filepath.Join(dir, "sasl", "sasldb2")
	outside := filepath.Join(t.TempDir(), "outside")
	writeFile(t, outside, "SASLDB")
	os.Remove(sasl)
	if err := os.Symlink(outside, sasl); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckRestore(path, "1.0.0"); err == nil || !strings.Contains(err.Error(), "data/sasl/sasldb2 is not the file") {
		t.Fatalf("a link to a file of the same contents = %v, want refused", err)
	}
}

// A journal file beside the restored database is from what the directory
// held before; SQLite would take it up, so the restore is refused.
func TestCheckRestoreRefusesAJournalBesideTheDatabase(t *testing.T) {
	for _, side := range []string{"-wal", "-shm", "-journal"} {
		path := unpackData(t, createArchive(t, "1.0.0"))
		writeFile(t, filepath.Join(filepath.Dir(path), "selfpost.db"+side), "old")
		if _, err := CheckRestore(path, "1.0.0"); err == nil || !strings.Contains(err.Error(), "selfpost.db"+side+" is beside the database") {
			t.Errorf("%s beside the database = %v, want refused", side, err)
		}
	}
}

// The archive unpacked again after a start that checked it is checked again:
// its record wins over the mark of the earlier check, and a file it cut short
// is refused. A finished restore leaves neither record.
func TestARecordUnpackedAgainIsChecked(t *testing.T) {
	archive := createArchive(t, "1.0.0")
	path := unpackData(t, archive)
	dir := filepath.Dir(path)
	if _, err := CheckRestore(path, "1.0.0"); err != nil {
		t.Fatal(err)
	}
	files := readArchive(t, archive)
	writeFile(t, filepath.Join(dir, ContentsName), files[DataArchivePrefix+ContentsName])
	writeFile(t, filepath.Join(dir, "sasl", "sasldb2"), "SAS")
	if _, err := CheckRestore(path, "1.0.0"); err == nil || !strings.Contains(err.Error(), "data/sasl/sasldb2 is not the file") {
		t.Fatalf("a second unpack cut short = %v, want refused", err)
	}
	writeFile(t, filepath.Join(dir, "sasl", "sasldb2"), files[DataArchivePrefix+"sasl/sasldb2"])
	if _, err := CheckRestore(path, "1.0.0"); err != nil {
		t.Fatalf("the second unpack whole: %v", err)
	}
	writeFile(t, filepath.Join(dir, ContentsName), files[DataArchivePrefix+ContentsName])
	if err := ConsumeRestore(path); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{ManifestName, ContentsName, contentsCheckedName} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s left after ConsumeRestore", name)
		}
	}
}
