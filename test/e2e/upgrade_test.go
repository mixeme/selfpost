package e2e

import (
	"context"
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

// upgradeData is a /data an earlier image left, laid out for host: the
// server's key under /data/dkim as 2.8.x keeps it (unless only27), the old
// key tree /data/opendkim as 2.7.x kept it - 2.8.x left it for a rollback -
// and, with a queue, a Postfix queue owned by numbers that are not this
// image's postfix and postdrop, and a log rotation due at the start. It
// returns the directory and the key's PEM.
func upgradeData(t *testing.T, host string, only27 bool) (string, []byte) {
	t.Helper()
	dataDir := t.TempDir()
	if err := os.Chmod(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	write := func(rel string, data []byte) {
		p := filepath.Join(dataDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join("opendkim", "keys", host, "selfpost.private"), pemKey)
	write(filepath.Join("opendkim", "KeyTable"), []byte(host+" "+host+":selfpost:/data/opendkim/keys/"+host+"/selfpost.private\n"))
	if !only27 {
		write(filepath.Join("dkim", "keys", host, "selfpost.private"), pemKey)
		write(filepath.Join("dkim", "selectors"), []byte(host+" selfpost\n"))
	}

	// The owners and modes the earlier images left - 2.8.x made both key
	// trees the panel's alone, 2.7.x the old one group selfpost's with the
	// set-group-ID bit - and a queue of foreign numbers, laid out as root in
	// a throwaway container of the image.
	layout := `set -e
if [ -d /data/dkim ]; then
	chown -R 10999:10999 /data/dkim /data/opendkim
	find /data/dkim /data/opendkim -type d -exec chmod 0700 {} +
	find /data/dkim /data/opendkim -type f -exec chmod 0600 {} +
else
	chown -R 10999:10998 /data/opendkim
	find /data/opendkim -type d -exec chmod 2750 {} +
	find /data/opendkim -type f -exec chmod 0640 {} +
fi
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
		t.Fatalf("lay out the earlier /data: %v\n%s", err, out)
	}
	return dataDir, pemKey
}

// TestStartsOnA28Data: the image starts on a /data 2.8.x left - the keys
// under /data/dkim, the old tree /data/opendkim still there for a rollback to
// 2.7.x, which 2.9.0 ends - and a Postfix queue owned by numbers that are not
// this image's postfix and postdrop. The old tree is removed and the key
// under /data/dkim is the one kept, unchanged, so the published record holds.
// The queue is handed to Postfix's users by name before "postfix check": the
// first rollout of 2.8.0 crash-looped on it when removing opendkim shifted
// the package accounts' numbers (2026-10-16); a removed package can shift
// them again.
func TestStartsOnA28Data(t *testing.T) {
	const host = "mail.example.test"
	dataDir, pemKey := upgradeData(t, host, false)
	certDir := t.TempDir()
	if err := writeSelfSignedCert(certDir+"/fullchain.pem", certDir+"/privkey.pem"); err != nil {
		t.Fatalf("generate throwaway TLS cert: %v", err)
	}

	// A dangling link where root creates an empty DMARC map: the panel owns
	// /data/postfix and could plant one. The entrypoint removes it and
	// creates nothing at its target (audit 2026-10-17, I2).
	const planted = "/etc/selfpost-planted-by-the-panel"
	if err := os.Symlink(planted, filepath.Join(dataDir, "postfix", "dmarc_transport")); err != nil {
		t.Fatal(err)
	}

	name := "selfpost-e2e-upgrade"
	_ = exec.Command("docker", "rm", "-f", name).Run()
	defer func() {
		_ = exec.Command("docker", "exec", name, "sh", "-c",
			"chown -R root:root /data && chmod -R a+rwX /data").Run()
		_ = exec.Command("docker", "rm", "-f", name).Run()
	}()
	if out, err := exec.Command("docker", "run", "-d", "--name", name,
		"-e", "SELFPOST_HOSTNAME="+host, "-e", "MILTER_WAIT_TIMEOUT=10", "-e", "DMARC_REPORTS_ENABLE=true",
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

	if got := exec1(`test -e /data/opendkim && echo there || echo gone`); got != "gone" {
		t.Errorf("/data/opendkim is %s after the start", got)
	}
	if got := exec1(`cat /data/dkim/keys/` + host + `/selfpost.private`); got != strings.TrimSpace(string(pemKey)) {
		t.Error("the key under /data/dkim is not the one 2.8.x left")
	}
	if got := exec1(`cat /data/dkim/selectors`); got != host+" selfpost" {
		t.Errorf("selector table = %q", got)
	}
	if got := exec1(`stat -c '%U:%G' /data/postfix/queue/active /data/postfix/queue/maildrop /data/postfix/queue/pid`); got != "postfix:postfix\npostfix:postdrop\nroot:postfix" {
		t.Errorf("the queue is not Postfix's by name: %q", got)
	}
	if !strings.Contains(logs(), "entrypoint: removed /data/opendkim") {
		t.Errorf("the entrypoint did not say it removed the old tree:\n%s", logs())
	}
	// A rotation due at the start runs before Postfix does: it rotates, and
	// its postrotate asks no reload of a Postfix not running yet (the test
	// server's rollout of 2026-10-16 logged "fatal: the Postfix mail system
	// is not running"; build/logrotate-mail.conf).
	if !strings.Contains(logs(), "entrypoint: removing the symbolic link /data/postfix/dmarc_transport") {
		t.Errorf("the entrypoint did not remove the planted link:\n%s", logs())
	}
	if got := exec1(`test -e ` + planted + ` && echo created || echo absent; test -f /data/postfix/dmarc_transport && ! test -L /data/postfix/dmarc_transport && echo file`); got != "absent\nfile" {
		t.Errorf("planted link: %q, want the target absent and the map a regular file", got)
	}
	if got := exec1(`test -f /data/log/mail.log.1 && echo rotated`); got != "rotated" {
		t.Errorf("the rotation due at the start did not run: %q", got)
	}
	if l := logs(); strings.Contains(l, "error running non-shared postrotate script") || strings.Contains(l, "logrotate failed") {
		t.Errorf("the rotation at the start failed:\n%s", l)
	}
}

// TestRefusesA27Data: a /data that never ran 2.8.x keeps the keys under
// /data/opendkim only. The start stops and says to run 2.8.x first, which
// copies them, rather than let the panel make the server's own name a new key
// that its published record would not match; the old tree is left as it was.
func TestRefusesA27Data(t *testing.T) {
	const host = "mail.example.test"
	dataDir, _ := upgradeData(t, host, true)

	name := "selfpost-e2e-upgrade-27"
	_ = exec.Command("docker", "rm", "-f", name).Run()
	defer exec.Command("docker", "rm", "-f", name).Run()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "run", "--rm", "--name", name,
		"-e", "SELFPOST_HOSTNAME="+host, "-v", dataDir+":/data", "selfpost:e2e").CombinedOutput()
	if err == nil || ctx.Err() != nil {
		t.Fatalf("the image started on a 2.7 /data (err %v, %v):\n%s", err, ctx.Err(), out)
	}
	if !strings.Contains(string(out), "the DKIM keys are only under /data/opendkim") {
		t.Errorf("the refusal does not say why:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "opendkim", "keys", host, "selfpost.private")); err != nil {
		t.Errorf("the old key is gone after the refusal: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "dkim", "keys", host)); err == nil {
		t.Error("a key directory was made under /data/dkim before the refusal")
	}
}
