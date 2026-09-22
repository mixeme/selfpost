package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// checkLogrotateConfigMode verifies the image pins /etc/logrotate.d/mail at 0644
// so logrotate will not silently ignore it (docs/development.md § Building
// binaries and the image).
func checkLogrotateConfigMode(s *stack) error {
	mode, err := s.execIn("selfpost", "stat", "-c", "%a", "/etc/logrotate.d/mail")
	if err != nil {
		return fmt.Errorf("stat logrotate config: %w", err)
	}
	mode = strings.TrimSpace(mode)
	if mode != "644" {
		return fmt.Errorf("/etc/logrotate.d/mail mode is %q, want 644", mode)
	}
	return nil
}

// checkLogrotateRotation forces a rotation and checks the recreated mail.log is
// panel-readable (0640 postfix:selfpost per build/logrotate-mail.conf).
func checkLogrotateRotation(s *stack) error {
	const logPath = "/data/log/mail.log"
	marker := "e2e-logrotate-marker\n"
	if _, err := s.execIn("selfpost", "sh", "-c",
		fmt.Sprintf("printf %q >> %s", marker, logPath)); err != nil {
		return fmt.Errorf("write mail.log: %w", err)
	}
	if _, err := s.execIn("selfpost", "logrotate", "-f", "/etc/logrotate.d/mail"); err != nil {
		return fmt.Errorf("logrotate -f: %w", err)
	}
	rotated, err := s.execIn("selfpost", "sh", "-c", "test -f /data/log/mail.log.1 && echo yes")
	if err != nil || strings.TrimSpace(rotated) != "yes" {
		return fmt.Errorf("expected /data/log/mail.log.1 after forced rotation (out=%q err=%v)", rotated, err)
	}
	mode, err := s.execIn("selfpost", "stat", "-c", "%a", logPath)
	if err != nil {
		return fmt.Errorf("stat rotated mail.log: %w", err)
	}
	if strings.TrimSpace(mode) != "640" {
		return fmt.Errorf("new %s mode is %q, want 640", logPath, strings.TrimSpace(mode))
	}
	body, err := s.execIn("selfpost", "grep", "-F", strings.TrimSpace(marker), "/data/log/mail.log.1")
	if err != nil || !strings.Contains(body, strings.TrimSpace(marker)) {
		return fmt.Errorf("rotated file does not contain marker (out=%q err=%v)", body, err)
	}
	return nil
}

// TestImageBuildPreservesLogrotateMode builds from a context where
// logrotate-mail.conf is group-writable and checks the image still ships 0644.
func TestImageBuildPreservesLogrotateMode(t *testing.T) {
	conf := filepath.Join(h.repoRoot, "build", "logrotate-mail.conf")
	info, err := os.Stat(conf)
	if err != nil {
		t.Fatalf("stat source config: %v", err)
	}
	origMode := info.Mode().Perm()

	if err := os.Chmod(conf, origMode|0o020); err != nil {
		t.Fatalf("chmod g+w source config: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(conf, origMode)
	})

	tag := "selfpost:e2e-logrotate-mode"
	build := exec.Command("docker", "build",
		"-f", filepath.Join(h.repoRoot, "build", "Dockerfile"),
		"-t", tag,
		"--build-arg", "VERSION=e2e",
		h.repoRoot,
	)
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("docker build with group-writable context config: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		_, _ = exec.Command("docker", "rmi", "-f", tag).CombinedOutput()
	})

	run := exec.Command("docker", "run", "--rm", tag, "stat", "-c", "%a", "/etc/logrotate.d/mail")
	run.Env = os.Environ()
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("stat in image: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) != "644" {
		t.Fatalf("image logrotate config mode is %q, want 644", strings.TrimSpace(string(out)))
	}
}
