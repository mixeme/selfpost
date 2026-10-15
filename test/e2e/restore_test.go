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
// directory, start the same image. What has to come back is what an operator
// cannot recreate - the administrator, the domain with its DKIM key, the
// application with its password - and the proof is the same one the first
// send gave: a message signed with the restored key verifies against the
// record that was published before the backup.
func testFullBackupRestore(t *testing.T, sc *scenario) {
	// A private key the panel may not read - the state a server is in until
	// its operator gives the key to group 998. The panel does not make an
	// archive without it: it refuses, names the file and says what to do.
	keyPath := filepath.Join(h.stageDir, "certs", "privkey.pem")
	if err := os.Chmod(keyPath, 0o600); err != nil {
		t.Fatal(err)
	}
	resp, body, err := sc.panel.postForm("/server/backup", url.Values{})
	if err != nil {
		t.Fatalf("full backup with an unreadable key: %v", err)
	}
	if resp.StatusCode != 500 || !strings.Contains(body, "not allowed to read certs/privkey.pem") || !strings.Contains(body, "chgrp 998") {
		t.Fatalf("full backup with an unreadable key: status %d, want a refusal that names the file and the way out:\n%.800s", resp.StatusCode, body)
	}
	if err := os.Chmod(keyPath, 0o644); err != nil {
		t.Fatal(err)
	}

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
	for _, want := range []string{"docker-compose.yml", ".env", "data/manifest.json", "data/selfpost.db", "data/contents.json", "certs/fullchain.pem", "certs/privkey.pem"} {
		if !names[want] {
			t.Fatalf("the backup has no %s (it has %d entries)", want, len(names))
		}
	}
	// No backup holds the Postfix queue (guide § Full backup and restore): the
	// restored instance has to build an empty one.
	for name := range names {
		if name == "data/postfix/queue" || strings.HasPrefix(name, "data/postfix/queue/") {
			t.Fatalf("the backup holds %s", name)
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

	// The manifest guards the first start only and is removed by it, with the
	// record of the archive's files that start compared /data with - after
	// the entrypoint had set owners and modes and the daemons had started,
	// which is what this run shows the comparison survives.
	if err := waitFor("manifest.json to be removed by the first start", 30*time.Second, 500*time.Millisecond, func() (bool, error) {
		out, _ := h.execIn("selfpost", "sh", "-c", "ls /data/manifest.json /data/contents.json /data/contents.checked.json 2>/dev/null; true")
		return strings.TrimSpace(out) == "", fmt.Errorf("still there: %s", strings.TrimSpace(out))
	}); err != nil {
		t.Fatalf("%v\n%s", err, logTail(h.logs("selfpost"), "backup"))
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
