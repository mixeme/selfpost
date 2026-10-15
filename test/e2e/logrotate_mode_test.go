package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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

	// Past the entrypoint, which would want a hostname and the rest: only the
	// file's mode is asked. (Until stage 6 of the 2026-10-12 audit this test
	// sat in a non-test file and go test never ran it.)
	run := exec.Command("docker", "run", "--rm", "--entrypoint", "stat", tag, "-c", "%a", "/etc/logrotate.d/mail")
	run.Env = os.Environ()
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("stat in image: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) != "644" {
		t.Fatalf("image logrotate config mode is %q, want 644", strings.TrimSpace(string(out)))
	}
}
