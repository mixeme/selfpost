package e2e

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testCertbotLayout: certs/ as the guide now has an operator leave it with
// certbot - two links into /etc/letsencrypt/live/<host>/, whose files are
// relative links into archive/ - and /etc/letsencrypt mounted at the same
// path (compose.override.yml mounts the stage's ./letsencrypt there). Postfix
// serves the certificate the links lead to, Health shows it with nothing
// waiting, and a full backup carries the files, not the links (assumptions
// C-A1, C-Q4). Until 2.5.2 the guide had certs/ linked to live/<host>, and
// the container got links into a directory it did not have.
func testCertbotLayout(t *testing.T, sc *scenario) {
	le := filepath.Join(h.stageDir, "letsencrypt")
	archive := filepath.Join(le, "archive", selfpostHostname)
	live := filepath.Join(le, "live", selfpostHostname)
	for _, d := range []string{archive, live} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A new pair, so what is served afterwards can only have come through
	// the links.
	if err := writeSelfSignedCert(filepath.Join(archive, "fullchain1.pem"), filepath.Join(archive, "privkey1.pem")); err != nil {
		t.Fatal(err)
	}
	certs := filepath.Join(h.stageDir, "certs")
	for _, f := range []string{"fullchain", "privkey"} {
		link := filepath.Join(live, f+".pem")
		_ = os.Remove(link)
		if err := os.Symlink("../../archive/"+selfpostHostname+"/"+f+"1.pem", link); err != nil {
			t.Fatal(err)
		}
		mounted := filepath.Join(certs, f+".pem")
		if err := os.Remove(mounted); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("/etc/letsencrypt/live/"+selfpostHostname+"/"+f+".pem", mounted); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.recreateSelfpost(); err != nil {
		t.Fatalf("recreate selfpost over certbot's layout: %v", err)
	}
	if err := waitForPanelReady(); err != nil {
		t.Fatalf("%v\n%s", err, h.logs("selfpost"))
	}
	if err := waitForSMTPSReady(60 * time.Second); err != nil {
		t.Fatalf("%v\n%s", err, h.logs("selfpost"))
	}

	want, err := fileSerial(filepath.Join(archive, "fullchain1.pem"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := servedSerial()
	if err != nil {
		t.Fatal(err)
	}
	if got.Cmp(want) != 0 {
		t.Fatalf("port 465 serves serial %s, want %s, the certificate certs/ links to", got, want)
	}

	_, body, err := sc.panel.get("/server/health")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "<dt>Renewal expires</dt>") {
		t.Fatalf("Health names a renewal waiting, but Postfix serves what certs/ leads to")
	}

	resp, archived, err := sc.panel.postForm("/server/backup", nil)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("full backup over certbot's layout: %v, status %v\n%.600s", err, resp, archived)
	}
	dir := t.TempDir()
	names, err := untar(dir, []byte(archived))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"fullchain", "privkey"} {
		name := "certs/" + f + ".pem"
		if !names[name] {
			t.Fatalf("the backup has no %s", name)
		}
		fi, err := os.Lstat(filepath.Join(dir, name))
		if err != nil || !fi.Mode().IsRegular() {
			t.Fatalf("%s in the backup: %v, %v; want the file, not a link", name, fi, err)
		}
		inBackup, _ := os.ReadFile(filepath.Join(dir, name))
		original, _ := os.ReadFile(filepath.Join(archive, f+"1.pem"))
		if !bytes.Equal(inBackup, original) {
			t.Fatalf("%s in the backup is not the file certbot's link leads to", name)
		}
	}
}
