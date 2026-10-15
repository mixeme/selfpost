package view

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// The refreshes of the monitoring boxes survive a failure and back off,
// checked by running panel.js in node against a stand-in for the DOM and
// htmx (testdata/poll_test.js; audit 2026-10-12, M11). Skipped where node is
// not installed - the test server's Go has none; the workstation runs it.
func TestPanelJSPolling(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	out, err := exec.Command(node, filepath.Join("testdata", "poll_test.js"), filepath.Join("static", "panel.js")).CombinedOutput()
	if err != nil {
		t.Fatalf("panel.js polling: %v\n%s", err, out)
	}
}
