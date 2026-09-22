package e2e

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// runEntrypoint runs the selfpost:e2e image's entrypoint standalone (no
// compose, no DNS/sink sidecars — the SELFPOST_HOSTNAME gate in
// build/entrypoint.sh runs and fails before anything else needs them) with
// hostnameEnv substituted for SELFPOST_HOSTNAME, and returns its combined
// output and whether it exited zero.
func runEntrypoint(t *testing.T, hostnameEnv string) (output string, exitedZero bool) {
	t.Helper()
	dataDir := t.TempDir()

	args := []string{
		"run", "--rm",
		"-e", "SELFPOST_HOSTNAME=" + hostnameEnv,
		"-v", dataDir + ":/data",
		"selfpost:e2e",
	}
	cmd := exec.Command("docker", args...)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

// TestHostnameGate is plan C.4 negative check 7: an empty or syntactically
// invalid SELFPOST_HOSTNAME must fail the container fast with an explanatory
// message (plan B.3), never a silent bad fallback. It runs the image directly
// rather than through the shared stand — the whole point is to exercise
// entrypoint.sh's gate before anything else in the container has a chance to
// start, so it does not depend on TestE2E's stack at all.
func TestHostnameGate(t *testing.T) {
	cases := []struct {
		name     string
		hostname string
		want     string
	}{
		{"empty", "", "SELFPOST_HOSTNAME is not set"},
		{"scheme_and_port", "https://mail.example.com:465", "must be a bare hostname"},
		{"bare_word_no_dot", "localhost", "fully-qualified domain name"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, ok := runEntrypoint(t, c.hostname)
			if ok {
				t.Fatalf("container started with SELFPOST_HOSTNAME=%q, want a fatal exit\noutput:\n%s", c.hostname, out)
			}
			if !strings.Contains(out, c.want) {
				t.Fatalf("SELFPOST_HOSTNAME=%q: output does not mention %q:\n%s", c.hostname, c.want, out)
			}
		})
	}

	t.Run("valid_hostname_starts", func(t *testing.T) {
		out, ok := runEntrypointBackground(t, "mail.example.test")
		if !ok {
			t.Fatalf("container with a valid SELFPOST_HOSTNAME failed to start:\n%s", out)
		}
	})
}

// runEntrypointBackground starts the container detached and confirms
// supervisord came up (rather than crash-looping) within a short timeout, then
// removes it — the positive control for the gate cases above: a valid
// hostname must not be caught by the same checks.
func runEntrypointBackground(t *testing.T, hostnameEnv string) (output string, started bool) {
	t.Helper()
	dataDir := t.TempDir()
	certDir := t.TempDir()
	if err := writeSelfSignedCert(certDir+"/fullchain.pem", certDir+"/privkey.pem"); err != nil {
		t.Fatalf("generate throwaway TLS cert: %v", err)
	}
	name := "selfpost-e2e-hostname-check"
	_ = exec.Command("docker", "rm", "-f", name).Run()
	defer exec.Command("docker", "rm", "-f", name).Run()

	up := exec.Command("docker", "run", "-d", "--name", name,
		"-e", "SELFPOST_HOSTNAME="+hostnameEnv,
		"-v", dataDir+":/data",
		"-v", certDir+":/etc/postfix/tls:ro",
		"selfpost:e2e")
	if out, err := up.CombinedOutput(); err != nil {
		return string(out), false
	}

	err := waitFor("supervisord to report RUNNING processes", 20*time.Second, 300*time.Millisecond, func() (bool, error) {
		out, err := exec.Command("docker", "exec", name, "supervisorctl", "-c", "/etc/supervisor/supervisord.conf", "status").CombinedOutput()
		if strings.Contains(string(out), "RUNNING") {
			return true, nil
		}
		return false, err
	})
	logs, _ := exec.Command("docker", "logs", name).CombinedOutput()
	return string(logs), err == nil
}
