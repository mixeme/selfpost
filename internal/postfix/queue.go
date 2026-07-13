package postfix

import (
	"fmt"
	"os/exec"
	"strings"
)

// Queue returns Postfix's own human-readable mail-queue listing (spec 7.2.11):
// active, deferred and held messages, exactly as an administrator would see
// via the CLI. The command takes a single fixed flag and no user input, so it
// never goes through a shell (spec 7.6.3). The panel is responsible for
// escaping the output before display (spec 7.6.7); this function returns it
// as-is.
func Queue() (string, error) {
	cmd := exec.Command("postqueue", "-p")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("postqueue -p: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
