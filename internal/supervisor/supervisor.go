// Package supervisor provides helpers for controlling processes through
// supervisord's control socket (architecture.md, security.md).
package supervisor

import (
	"fmt"
	"os/exec"
	"strings"
)

const confPath = "/etc/supervisor/supervisord.conf"

// Start issues a `supervisorctl start` for the named program. A program that
// is already running is not an error (the pending run covers the caller's
// intent).
func Start(program string) error {
	cmd := exec.Command("supervisorctl", "-c", confPath, "start", program)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "already started") {
			return nil
		}
		return fmt.Errorf("supervisor start %s: %w: %s", program, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Signal sends a signal to a supervised program via `supervisorctl signal`.
func Signal(sig, program string) error {
	cmd := exec.Command("supervisorctl", "-c", confPath, "signal", sig, program)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("supervisor signal %s %s: %w: %s", sig, program, err, strings.TrimSpace(string(out)))
	}
	return nil
}
