package postfix

import (
	"fmt"
	"os/exec"
	"strings"
)

// Queue returns Postfix's own human-readable mail-queue listing
// (architecture.md § Panel HTTP surface): active, deferred and held messages,
// exactly as an administrator would see via the CLI. The command takes a
// single fixed flag and no user input, so it never goes through a shell
// (security.md). The panel is responsible for escaping the output before
// display (security.md); this function returns it as-is.
func Queue() (string, error) {
	cmd := exec.Command("postqueue", "-p")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("postqueue -p: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
