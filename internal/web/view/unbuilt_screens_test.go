package view

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A screen the mockups draw and the panel does not build, or builds otherwise,
// is one the guards were told to let pass (deferredScreens, outlineExempt). It
// must be named in docs/roadmap.md, a row or an idea, so that it is not simply
// forgotten while every test is green: the queue as a table and the Inbound
// queue were (development.md § Panel pages: the component kit and its guards).
func TestUnbuiltScreensAreOnTheRoadmap(t *testing.T) {
	roadmap, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "roadmap.md"))
	if err != nil {
		t.Fatalf("read the roadmap: %v", err)
	}
	var names []string
	for name := range deferredScreens {
		names = append(names, name)
	}
	for name := range outlineExempt {
		names = append(names, name)
	}
	for _, name := range names {
		if !strings.Contains(string(roadmap), "`"+name+"`") {
			t.Errorf("screen %q is drawn and not built as drawn, and docs/roadmap.md does not name it (`%s`)", name, name)
		}
	}
}
