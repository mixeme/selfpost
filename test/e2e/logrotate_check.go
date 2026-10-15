package e2e

import (
	"fmt"
	"strings"
	"time"
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
	// The state file is beside the log, under /data, so a recreated container
	// keeps when mail.log was last rotated (audit 2026-10-12, M19); the loop
	// runs logrotate at once when it starts.
	return waitFor("logrotate's state under /data/log", 20*time.Second, 500*time.Millisecond, func() (bool, error) {
		out, _ := s.execIn("selfpost", "sh", "-c", "test -s /data/log/.logrotate.status && echo yes")
		out = strings.TrimSpace(out)
		return out == "yes", fmt.Errorf("state file: %q", out)
	})
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
