package e2e

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testFullBackupRestore takes a full backup from the panel, throws the
// installation away and brings it back the way the guide tells an operator to
// (§ Full backup and restore): unpack the archive into an empty project
// directory, put the private key back, start the same image. What has to come back is what an operator
// cannot recreate - the administrator, the domain with its DKIM key, the
// application with its password - and the proof is the same one the first
// send gave: a message signed with the restored key verifies against the
// record that was published before the backup.
func testFullBackupRestore(t *testing.T, sc *scenario) {
	resp, archive, err := sc.panel.postForm("/server/backup", url.Values{})
	if err != nil {
		t.Fatalf("full backup: %v", err)
	}
	if ct := resp.Header.Get("Content-Type"); resp.StatusCode != 200 || ct != "application/gzip" {
		t.Fatalf("full backup: status %d, Content-Type %q, want an application/gzip download:\n%.600s", resp.StatusCode, ct, archive)
	}

	// A download that broke off half way is still a 200: the panel streams the
	// archive and can only log what went wrong. Read it through once while the
	// container that wrote it can still say why.
	if _, err := untar(t.TempDir(), []byte(archive)); err != nil {
		t.Fatalf("the backup the panel sent (%d bytes) is not a complete archive: %v\n%s", len(archive), err, logTail(h.logs("selfpost"), "backup"))
	}

	// The installation goes: containers down, then everything the archive is
	// supposed to bring back. The fake zone and the sink's directory are the
	// outside world and stay.
	// The private key is the operator's to keep: the panel may not read it (it
	// is the host user's, mode 0600, here as on a real server) and its archive
	// comes without it.
	keyPath := filepath.Join(h.stageDir, "certs", "privkey.pem")
	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read the stand's private key: %v", err)
	}
	h.reclaimData()
	h.down()
	for _, name := range []string{"data", "certs", "docker-compose.yml", ".env"} {
		if err := removeAllBestEffort(filepath.Join(h.stageDir, name)); err != nil {
			t.Fatalf("remove %s before restore: %v", name, err)
		}
	}
	names, err := untar(h.stageDir, []byte(archive))
	if err != nil {
		t.Fatalf("unpack the backup into the empty project directory: %v", err)
	}
	for _, want := range []string{"docker-compose.yml", ".env", "data/manifest.json", "data/selfpost.db", "certs/fullchain.pem"} {
		if !names[want] {
			t.Fatalf("the backup has no %s (it has %d entries)", want, len(names))
		}
	}
	// The panel's archive is the one without the Postfix queue (guide § Taking
	// a full backup): the restored instance has to build an empty one.
	for name := range names {
		if name == "data/postfix/queue" || strings.HasPrefix(name, "data/postfix/queue/") {
			t.Fatalf("the panel's backup holds %s", name)
		}
	}
	if !names["certs/privkey.pem"] {
		if err := os.WriteFile(keyPath, key, 0o600); err != nil {
			t.Fatalf("put the private key back: %v", err)
		}
	}

	if err := h.up(); err != nil {
		t.Fatalf("start the restored installation: %v", err)
	}
	if err := waitForPanelReady(); err != nil {
		t.Fatalf("the restored installation did not come up: %v\n%s", err, h.logs("selfpost"))
	}
	if err := waitForSMTPSReady(60 * time.Second); err != nil {
		t.Fatal(err)
	}

	// The manifest guards the first start only and is removed by it.
	if err := waitFor("manifest.json to be removed by the first start", 30*time.Second, 500*time.Millisecond, func() (bool, error) {
		out, _ := h.execIn("selfpost", "sh", "-c", "test -e /data/manifest.json && echo present || echo gone")
		return strings.TrimSpace(out) == "gone", fmt.Errorf("manifest.json is %s", strings.TrimSpace(out))
	}); err != nil {
		t.Fatal(err)
	}

	p, err := newPanelClient()
	if err != nil {
		t.Fatal(err)
	}
	if err := p.login(adminUser, adminPassword); err != nil {
		t.Fatalf("the administrator cannot sign in after the restore: %v", err)
	}
	sc.panel = p

	_, value, err := p.dkimRecord(sc.domainID)
	if err != nil {
		t.Fatalf("the domain is not there after the restore: %v", err)
	}
	if value != sc.dkimValue {
		t.Fatalf("the domain came back with another DKIM key:\n before %s\n after  %s", sc.dkimValue, value)
	}

	token := uniqueToken("restored")
	res := attemptSend(sendAttempt{
		authLogin: sc.appLogin, authPassword: sc.appPassword,
		from: "alerts@" + senderDomain, to: recipient,
		subject: "e2e " + token, body: "sent by the restored installation",
	})
	if !res.ok() {
		t.Fatalf("the application cannot send after the restore: %v", res.firstErr())
	}
	raw, err := findSinkMessage(h.stageDir, token, 30*time.Second)
	if err != nil {
		t.Fatalf("the restored installation did not deliver: %v", err)
	}
	if err := verifyDKIM(raw, senderDomain); err != nil {
		t.Fatalf("mail signed after the restore does not verify against the record published before it: %v", err)
	}
}

// untar unpacks a .tar.gz into dir and returns the names it held. Only
// directories and regular files are expected in a backup; anything else, or a
// name that leaves dir, is an error rather than something to skip quietly.
func untar(dir string, archive []byte) (map[string]bool, error) {
	zr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return names, nil
		}
		if err != nil {
			return nil, err
		}
		name := filepath.ToSlash(filepath.Clean(hdr.Name))
		if name == ".." || strings.HasPrefix(name, "../") || filepath.IsAbs(hdr.Name) {
			return nil, fmt.Errorf("entry %q leaves the directory", hdr.Name)
		}
		target := filepath.Join(dir, filepath.FromSlash(name))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return nil, err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return nil, err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, hdr.FileInfo().Mode().Perm())
			if err != nil {
				return nil, err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return nil, err
			}
			if err := f.Close(); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("entry %q has type %q, neither a directory nor a regular file", hdr.Name, hdr.Typeflag)
		}
		names[name] = true
	}
}

// logTail returns the lines of log that contain filter.
func logTail(log, filter string) string {
	var b strings.Builder
	for _, line := range strings.Split(log, "\n") {
		if strings.Contains(line, filter) {
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}
