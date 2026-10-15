package e2e

import (
	"context"
	"fmt"
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
func runEntrypoint(t *testing.T, hostnameEnv string, env ...string) (output string, exitedZero bool) {
	t.Helper()
	dataDir := t.TempDir()
	// Entrypoint also chmod 755 /data; mirror that here so a pre-fix image
	// still gets a traversable bind mount under Go's 0700 TempDir (testing.TempDir
	// uses os.MkdirTemp, src/os/tempfile.go: "created with mode 0o700").
	if err := os.Chmod(dataDir, 0o755); err != nil {
		t.Fatalf("chmod data dir: %v", err)
	}

	// A name of its own, so a run that outlives its minute is removed.
	name := fmt.Sprintf("selfpost-gate-%d", time.Now().UnixNano())
	defer exec.Command("docker", "rm", "-f", name).Run()
	args := []string{"run", "--rm", "--name", name, "-e", "SELFPOST_HOSTNAME=" + hostnameEnv}
	for _, e := range env {
		args = append(args, "-e", e)
	}
	args = append(args, "-v", dataDir+":/data", "selfpost:e2e")
	// A start that gets past every gate runs on: it is ended after a minute
	// rather than holding the run until the test binary's own timeout.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", args...)
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
	if err := os.Chmod(dataDir, 0o755); err != nil {
		t.Fatalf("chmod data dir: %v", err)
	}
	certDir := t.TempDir()
	if err := writeSelfSignedCert(certDir+"/fullchain.pem", certDir+"/privkey.pem"); err != nil {
		t.Fatalf("generate throwaway TLS cert: %v", err)
	}
	name := "selfpost-e2e-hostname-check"
	_ = exec.Command("docker", "rm", "-f", name).Run()
	defer func() {
		// Make bind-mounted /data deletable by Go's TempDir cleanup: the panel
		// leaves setup-token/db/dkim owned by container UIDs.
		_ = exec.Command("docker", "exec", name, "sh", "-c",
			"chown -R root:root /data && chmod -R a+rwX /data").Run()
		_ = exec.Command("docker", "rm", "-f", name).Run()
	}()

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

// A setting Postfix takes as a number or a time value stops the start when it
// is neither, naming the variable: before, it passed `postfix check` and every
// smtpd died at its first connection (audit 2026-10-12, L31). A sensible
// value of the same form gets past the check (and the start fails later, for
// what this bare run lacks).
func TestNumericSettingsGate(t *testing.T) {
	for _, c := range []struct{ env, want string }{
		{"RATE_LIMIT_MESSAGES_PER_IP=100/h", "RATE_LIMIT_MESSAGES_PER_IP must be a whole number, got '100/h'"},
		{"INBOUND_MESSAGE_SIZE_LIMIT=10MB", "INBOUND_MESSAGE_SIZE_LIMIT must be a whole number, got '10MB'"},
		{"MILTER_CONTENT_TIMEOUT=thirty", "MILTER_CONTENT_TIMEOUT must be a time value"},
		// Under the parameter's minimum: anvil and smtpd die on a zero.
		{"RATE_LIMIT_WINDOW_SECONDS=0", "RATE_LIMIT_WINDOW_SECONDS must be from 1 to 2147483647, got '0'"},
		{"MILTER_COMMAND_TIMEOUT=0s", "MILTER_COMMAND_TIMEOUT must be a time value"},
		{"INBOUND_MESSAGE_SIZE_LIMIT=99999999999", "INBOUND_MESSAGE_SIZE_LIMIT must be from 0 to 2147483647"},
	} {
		t.Run(c.env, func(t *testing.T) {
			out, ok := runEntrypoint(t, "mail.example.com", c.env)
			if ok || !strings.Contains(out, c.want) {
				t.Fatalf("%s: exited zero %v, want the refusal %q\noutput:\n%s", c.env, ok, c.want, out)
			}
		})
	}
	// Good values beside a bad one checked after them: the refusal names the
	// bad one alone. (A start with good values only would run on.)
	out, _ := runEntrypoint(t, "mail.example.com", "RATE_LIMIT_MESSAGES_PER_IP=50", "MILTER_CONNECT_TIMEOUT=2m", "MILTER_CONTENT_TIMEOUT=bad")
	if !strings.Contains(out, "MILTER_CONTENT_TIMEOUT must be") || strings.Contains(out, "RATE_LIMIT_MESSAGES_PER_IP must") || strings.Contains(out, "MILTER_CONNECT_TIMEOUT must") {
		t.Errorf("valid values refused:\n%s", out)
	}
}
