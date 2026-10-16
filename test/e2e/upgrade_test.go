package e2e

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestStartsOnA27Data: the image starts on a /data an earlier image left -
// the DKIM keys under /data/opendkim, as 2.7.x kept them, and a Postfix queue
// owned by numbers that are not this image's postfix and postdrop. Without
// opendkim installed the package accounts get other numbers (postfix 101 ->
// 100, postdrop 103 -> 102 in 2.8.0), and the first rollout of 2.8.0 to the
// test server crash-looped on "postfix check" (2026-10-16): the queue is
// handed to Postfix's users by name before the check now. And the old key is
// the one signing: copied, not made anew, so the published record holds
// (build/entrypoint.sh; review of panel-dkim-signing, F5 - no test ran the
// copy).
func TestStartsOnA27Data(t *testing.T) {
	const host = "mail.example.test"
	dataDir := t.TempDir()
	if err := os.Chmod(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	certDir := t.TempDir()
	if err := writeSelfSignedCert(certDir+"/fullchain.pem", certDir+"/privkey.pem"); err != nil {
		t.Fatalf("generate throwaway TLS cert: %v", err)
	}

	// The server's key as 2.7.x kept it, and its KeyTable.
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	keyFile := filepath.Join(dataDir, "opendkim", "keys", host, "selfpost.private")
	if err := os.MkdirAll(filepath.Dir(keyFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pemKey, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "opendkim", "KeyTable"),
		[]byte(host+" "+host+":selfpost:/data/opendkim/keys/"+host+"/selfpost.private\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The owners and modes 2.7.x left, and a queue of foreign numbers, laid
	// out as root in a throwaway container of the image.
	layout := `set -e
chown -R 10999:10998 /data/opendkim
find /data/opendkim -type d -exec chmod 2750 {} +
find /data/opendkim -type f -exec chmod 0640 {} +
q=/data/postfix/queue
for d in active bounce corrupt defer deferred flush hold incoming private saved trace; do
	mkdir -p $q/$d; chown 4242:4243 $q/$d; chmod 700 $q/$d
done
mkdir -p $q/maildrop $q/public $q/pid
chown 4242:4244 $q/maildrop $q/public; chmod 730 $q/maildrop; chmod 710 $q/public
chown root:root $q $q/pid; chmod 755 $q $q/pid
mkdir -p /data/log
echo "Oct 14 00:00:00 mail postfix/master[1]: daemon started" > /data/log/mail.log
printf 'logrotate state -- version 2\n"/data/log/mail.log" 2026-10-14-0:0:0\n' > /data/log/.logrotate.status`
	if out, err := exec.Command("docker", "run", "--rm", "--entrypoint", "sh",
		"-v", dataDir+":/data", "selfpost:e2e", "-c", layout).CombinedOutput(); err != nil {
		t.Fatalf("lay out the 2.7 /data: %v\n%s", err, out)
	}

	name := "selfpost-e2e-upgrade"
	_ = exec.Command("docker", "rm", "-f", name).Run()
	defer func() {
		_ = exec.Command("docker", "exec", name, "sh", "-c",
			"chown -R root:root /data && chmod -R a+rwX /data").Run()
		_ = exec.Command("docker", "rm", "-f", name).Run()
	}()
	if out, err := exec.Command("docker", "run", "-d", "--name", name,
		"-e", "SELFPOST_HOSTNAME="+host, "-e", "MILTER_WAIT_TIMEOUT=10",
		"-v", dataDir+":/data", "-v", certDir+":/etc/postfix/tls:ro",
		"selfpost:e2e").CombinedOutput(); err != nil {
		t.Fatalf("docker run: %v\n%s", err, out)
	}
	logs := func() string {
		out, _ := exec.Command("docker", "logs", name).CombinedOutput()
		return string(out)
	}
	exec1 := func(script string) string {
		out, err := exec.Command("docker", "exec", name, "sh", "-c", script).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", script, err, out)
		}
		return strings.TrimSpace(string(out))
	}

	// postfix's startsecs is MILTER_WAIT_TIMEOUT plus ten: RUNNING after 20 s.
	if err := waitFor("postfix to be RUNNING", 90*time.Second, time.Second, func() (bool, error) {
		out, _ := exec.Command("docker", "exec", name, "supervisorctl", "-c", "/etc/supervisor/supervisord.conf", "status", "postfix").CombinedOutput()
		return strings.Contains(string(out), "RUNNING"), nil
	}); err != nil {
		t.Fatalf("%v\n%s", err, logs())
	}

	if got := exec1(`cmp /data/opendkim/keys/` + host + `/selfpost.private /data/dkim/keys/` + host + `/selfpost.private && echo same`); got != "same" {
		t.Errorf("the key under /data/dkim is not the one copied: %s", got)
	}
	if got := exec1(`cat /data/dkim/selectors`); got != host+" selfpost" {
		t.Errorf("selector table = %q", got)
	}
	if got := exec1(`stat -c '%U:%G %a' /data/opendkim/keys/` + host + `/selfpost.private /data/opendkim/keys`); got != "panel:panel 600\npanel:panel 700" {
		t.Errorf("the old tree is not the panel's alone: %q", got)
	}
	if got := exec1(`stat -c '%U:%G' /data/postfix/queue/active /data/postfix/queue/maildrop /data/postfix/queue/pid`); got != "postfix:postfix\npostfix:postdrop\nroot:postfix" {
		t.Errorf("the queue is not Postfix's by name: %q", got)
	}
	if !strings.Contains(logs(), "the DKIM keys were copied from /data/opendkim to /data/dkim") {
		t.Errorf("the entrypoint did not say it copied the keys:\n%s", logs())
	}
	// A rotation due at the start runs before Postfix does: it rotates, and
	// its postrotate asks no reload of a Postfix not running yet (the test
	// server's rollout of 2026-10-16 logged "fatal: the Postfix mail system
	// is not running"; build/logrotate-mail.conf).
	if got := exec1(`test -f /data/log/mail.log.1 && echo rotated`); got != "rotated" {
		t.Errorf("the rotation due at the start did not run: %q", got)
	}
	if l := logs(); strings.Contains(l, "error running non-shared postrotate script") || strings.Contains(l, "logrotate failed") {
		t.Errorf("the rotation at the start failed:\n%s", l)
	}
}
