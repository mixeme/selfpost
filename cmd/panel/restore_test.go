package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/backup"
	"github.com/mixeme/selfpost/internal/buildinfo"
	"github.com/mixeme/selfpost/internal/secretfile"
	"github.com/mixeme/selfpost/internal/store"
	"golang.org/x/crypto/bcrypt"
)

// Restoring a SelfPost backup is not a code path in the panel: the operator
// extracts the archive into the /data bind mount and starts the image, and the
// panel is expected to come up on it (architecture.md § Persistence). Nothing
// below stubs that story out — the archive is downloaded from a running panel
// through /backup, unpacked the way `tar -xzf` unpacks it, and a second panel
// is started on the result through the same startup sequence run() uses:
// CheckRestore, store.Open, newPanel, Start.

const (
	restorePassword = "correct-horse-battery"
	restoreDomain   = "bs.example.ru"
	restoreSubject  = "Order confirmation"
)

// restored is the outcome of a full backup-and-restore round trip.
type restored struct {
	panel   http.Handler // panel booted on the restored data directory
	dataDir string       // the restored /data
	session *http.Cookie // a session opened before the backup was taken
}

// restoreFromOwnBackup runs the operator's path end to end: seed a panel that
// has been in use, sign in, download a backup from it, extract that archive
// into an empty directory and boot a second panel there. A non-empty password
// takes the encrypted download and decrypts it on the way in, which is what an
// operator does with a .spbk file.
func restoreFromOwnBackup(t *testing.T, password string) restored {
	t.Helper()

	live := seedPanelData(t)
	panel := bootPanel(t, live)
	session := signIn(t, panel)
	archive := downloadBackup(t, panel, session, password)

	target := t.TempDir()
	extract(t, archive, target)

	return restored{panel: bootPanel(t, target), dataDir: target, session: session}
}

// The panel has to come up on the restored directory and show the state that
// was in the archive, without the operator touching anything else: the domain
// and its journal are in the database the archive carried, and the credentials
// that worked before the restore still work after it.
func TestPanelBootsOnADataDirectoryRestoredFromItsOwnBackup(t *testing.T) {
	r := restoreFromOwnBackup(t, "")

	body := getPage(t, r.panel, "/deliveries", signIn(t, r.panel))
	for _, want := range []string{restoreDomain, restoreSubject} {
		if !strings.Contains(body, want) {
			t.Errorf("the restored panel's send log does not show %q:\n%s", want, body)
		}
	}

	// The daemons read their own state from the archive rather than from
	// SQLite, so the files have to land where the panel's configuration says
	// they are — that is the whole reason restore needs no regeneration step.
	for path, want := range map[string]string{
		filepath.Join("opendkim", "keys", restoreDomain, "selfpost.private"): "PRIVATE KEY",
		filepath.Join("sasl", "sasldb2"):                                     "SASLDB",
		filepath.Join("postfix", "sender_login_maps"):                        "@" + restoreDomain + " shop",
	} {
		got, err := os.ReadFile(filepath.Join(r.dataDir, path))
		if err != nil {
			t.Errorf("the restored data directory has no %s: %v", path, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
}

// The one-time setup link is closed by the presence of a panel user, and the
// restored database has one. A restore that reopened it would publish a link
// that creates a second global administrator on a server holding live mail
// credentials (security.md).
func TestARestoreDoesNotReopenTheSetupLink(t *testing.T) {
	r := restoreFromOwnBackup(t, "")

	if _, err := os.Stat(filepath.Join(r.dataDir, "setup-token")); !os.IsNotExist(err) {
		t.Errorf("the restored panel wrote a setup token (stat err = %v)", err)
	}
	body := getPage(t, r.panel, "/login", nil)
	if strings.Contains(body, "No administrator has been created yet") {
		t.Errorf("the restored panel offers first-run setup:\n%s", body)
	}
}

// Sessions live in the database, so they travel in the archive: a cookie that
// was valid when the backup was taken is valid again on the restored panel.
// That is the documented consequence of restoring an older backup (guide §
// Backup and restore) — stated here so it cannot change by accident.
func TestARestoredPanelHonoursSessionsFromTheArchive(t *testing.T) {
	r := restoreFromOwnBackup(t, "")

	rec := request(t, r.panel, http.MethodGet, "/deliveries", nil, r.session)
	if rec.Code != http.StatusOK {
		t.Errorf("a session from before the backup = %d on the restored panel, want 200", rec.Code)
	}
}

// An encrypted download is the same archive inside an envelope, so it restores
// the same way once the password is supplied. The archive is never written to
// disk in the clear by the panel, so this is the only place the two paths can
// be shown to agree.
func TestAnEncryptedBackupRestoresTheSameWay(t *testing.T) {
	r := restoreFromOwnBackup(t, "a-long-enough-password")

	body := getPage(t, r.panel, "/deliveries", signIn(t, r.panel))
	if !strings.Contains(body, restoreSubject) {
		t.Errorf("the panel restored from an encrypted backup lost the send log:\n%s", body)
	}
}

// The version guard is what stops a restore from being silently corrupted by
// schema skew, and it runs before anything opens the database. The manifest
// stays put on a mismatch: the operator's next move is to start the image the
// backup names, and it has to be there when they do.
func TestPanelRefusesADataDirectoryRestoredFromAnotherVersion(t *testing.T) {
	live := seedPanelData(t)
	var archive bytes.Buffer
	if err := backup.Create(&archive, backup.Params{
		DataDir: live,
		DBPath:  filepath.Join(live, "selfpost.db"),
		Version: "9.9.9",
	}); err != nil {
		t.Fatalf("create backup: %v", err)
	}
	target := t.TempDir()
	extract(t, archive.Bytes(), target)

	cfg := panelConfig(t, target)
	err := backup.CheckRestore(cfg.manifestPath, buildinfo.Version)
	if err == nil {
		t.Fatal("the panel booted on a data directory left by another version")
	}
	for _, want := range []string{"9.9.9", buildinfo.Version} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q, so the operator cannot tell which image to run: %v", want, err)
		}
	}
	if _, statErr := os.Stat(cfg.manifestPath); statErr != nil {
		t.Errorf("the manifest was consumed by a refused restore: %v", statErr)
	}
}

// seedPanelData builds the /data tree of a panel that has been in use: an
// administrator, a sending domain with an application and one logged message,
// and the daemon state the mail path needs (a DKIM key, the SASL database and
// Postfix's sender map).
func seedPanelData(t *testing.T) string {
	t.Helper()
	dataDir := t.TempDir()

	st, err := store.Open(filepath.Join(dataDir, "selfpost.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(restorePassword), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := st.CreateGlobalUser("admin", string(hash)); err != nil {
		t.Fatalf("create user: %v", err)
	}
	dom, err := st.AddDomain(restoreDomain, "selfpost")
	if err != nil {
		t.Fatalf("add domain: %v", err)
	}
	if _, err := st.AddApplication(dom.ID, "shop", store.AddressModeWildcard, nil); err != nil {
		t.Fatalf("add application: %v", err)
	}
	if err := st.InsertQueued(store.SendLogEntry{
		QueueID: "4A1B2C3D", Domain: restoreDomain, AppLogin: "shop",
		From: "noreply@" + restoreDomain, To: "customer@example.net", Subject: restoreSubject,
	}); err != nil {
		t.Fatalf("insert send-log row: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	for path, content := range map[string]string{
		filepath.Join("opendkim", "keys", restoreDomain, "selfpost.private"): "PRIVATE KEY",
		filepath.Join("sasl", "sasldb2"):                                     "SASLDB",
		filepath.Join("postfix", "sender_login_maps"):                        "@" + restoreDomain + " shop",
		filepath.Join("log", "mail.log"):                                     "postfix/smtp[1]: 4A1B2C3D: status=sent",
	} {
		full := filepath.Join(dataDir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", full, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o640); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	return dataDir
}

// panelConfig resolves the panel's own configuration for a data directory, so
// the test finds the files where the running binary would look for them rather
// than where it put them. Cookies are marked insecure for the same reason the
// e2e stand does it: the test client speaks plain HTTP.
func panelConfig(t *testing.T, dataDir string) config {
	t.Helper()
	t.Setenv("SELFPOST_DATA_DIR", dataDir)
	t.Setenv("PANEL_COOKIE_SECURE", "false")
	t.Setenv("SELFPOST_HOSTNAME", "mail.example.ru")
	// MAIL_LOG's default is an absolute path, not one derived from the data
	// directory; without this the panel would read the host's /data.
	t.Setenv("MAIL_LOG", filepath.Join(dataDir, "log", "mail.log"))
	return loadConfig()
}

// bootPanel performs the startup sequence run() performs, in the same order,
// and returns the panel's HTTP handler.
func bootPanel(t *testing.T, dataDir string) http.Handler {
	t.Helper()
	cfg := panelConfig(t, dataDir)

	if err := backup.CheckRestore(cfg.manifestPath, buildinfo.Version); err != nil {
		t.Fatalf("the panel refused to start on %s: %v", dataDir, err)
	}
	if _, err := os.Stat(cfg.manifestPath); err == nil {
		t.Errorf("the restore manifest was not consumed, so the next start is gated by it too")
	}

	st, err := store.Open(cfg.dbPath)
	if err != nil {
		t.Fatalf("open the restored database: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	panel, err := newPanel(cfg, st)
	if err != nil {
		t.Fatalf("build the panel: %v", err)
	}
	if err := panel.Start(); err != nil {
		t.Fatalf("start the panel: %v", err)
	}
	return panel.Handler()
}

// signIn signs in as the seeded administrator and returns the session cookie.
func signIn(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	form := url.Values{"username": {"admin"}, "password": {restorePassword}}
	rec := request(t, h, http.MethodPost, "/login", strings.NewReader(form.Encode()), nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("sign in = %d, want 303:\n%s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("sign in issued no session cookie")
	}
	return cookies[0]
}

// downloadBackup takes a backup through the panel's own /backup route, the way
// the operator does. An empty password downloads the plain archive; otherwise
// the response is a .spbk envelope, which is decrypted here.
func downloadBackup(t *testing.T, h http.Handler, session *http.Cookie, password string) []byte {
	t.Helper()
	form := url.Values{}
	if password != "" {
		form.Set("encrypt", "1")
		form.Set("password", password)
		form.Set("password_confirm", password)
	}
	rec := request(t, h, http.MethodPost, "/backup", strings.NewReader(form.Encode()), session)
	if rec.Code != http.StatusOK {
		t.Fatalf("download a backup = %d, want 200:\n%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q; an archive of every secret on the server must not be cached", got)
	}

	body := rec.Body.Bytes()
	if password == "" {
		if secretfile.HasMagic(body) {
			t.Fatal("an unencrypted download came back as an envelope")
		}
		return body
	}

	if !secretfile.HasMagic(body) {
		t.Fatal("the download is not an encrypted envelope, so the archive left the panel in the clear")
	}
	r, err := secretfile.NewReader(bytes.NewReader(body), password)
	if err != nil {
		t.Fatalf("open the encrypted backup: %v", err)
	}
	plain, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("decrypt the backup: %v", err)
	}
	return plain
}

// extract unpacks a backup archive into dir, as `tar -xzf` does onto the /data
// bind mount before the image is started.
func extract(t *testing.T, archive []byte, dir string) {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatalf("the download is not a gzip stream: %v", err)
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read the archive: %v", err)
		}
		name := filepath.Clean(filepath.FromSlash(hdr.Name))
		if strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			t.Fatalf("the archive escapes the directory it is extracted into: %q", hdr.Name)
		}
		path := filepath.Join(dir, name)

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, hdr.FileInfo().Mode().Perm()); err != nil {
				t.Fatalf("mkdir %s: %v", path, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
				t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
			}
			f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, hdr.FileInfo().Mode().Perm())
			if err != nil {
				t.Fatalf("create %s: %v", path, err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				t.Fatalf("write %s: %v", path, err)
			}
			if err := f.Close(); err != nil {
				t.Fatalf("close %s: %v", path, err)
			}
		}
	}
}

// getPage performs a GET and returns the body, failing on any non-200.
func getPage(t *testing.T, h http.Handler, target string, session *http.Cookie) string {
	t.Helper()
	rec := request(t, h, http.MethodGet, target, nil, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200:\n%s", target, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// request drives the panel's real handler chain, including the origin check,
// with the headers a browser on the panel's own page would send.
func request(t *testing.T, h http.Handler, method, target string, body io.Reader, session *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "http://mail.example.ru"+target, body)
	req.Host = "mail.example.ru"
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	if session != nil {
		req.AddCookie(session)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
