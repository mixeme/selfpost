package view

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// panel.js run in node against a stand-in for the parts of the DOM and htmx
// it uses (testdata/*_test.js): the refreshes of the monitoring boxes
// survive a failure and back off (audit 2026-10-12, M11), and a button that
// leaves the page asks first when a field holds an unsaved edit (L19).
// Skipped where node is not installed - the test server's Go has none; the
// workstation runs it.
func TestPanelJS(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	for _, script := range []string{"poll_test.js", "unsaved_test.js"} {
		t.Run(script, func(t *testing.T) {
			out, err := exec.Command(node, filepath.Join("testdata", script), filepath.Join("static", "panel.js")).CombinedOutput()
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
		})
	}
}
