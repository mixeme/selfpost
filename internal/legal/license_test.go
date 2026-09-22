package legal

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestEmbeddedLicenseMatchesRoot(t *testing.T) {
	root, err := os.ReadFile(filepath.Join("..", "..", "LICENSE"))
	if err != nil {
		t.Fatalf("read root LICENSE: %v", err)
	}
	if !bytes.Equal(root, License) {
		t.Fatal("internal/legal/LICENSE differs from the repository-root LICENSE; copy the root file over")
	}
	if len(License) == 0 {
		t.Fatal("embedded LICENSE is empty")
	}
}

// NOTICE used to tell modifiers to edit layout.html for the Source URL. The
// footer reads legal.SourceURL; a fork that only changed the template would
// still advertise the upstream repo.
func TestNoticePointsAtSourceURLConstant(t *testing.T) {
	notice, err := os.ReadFile(filepath.Join("..", "..", "NOTICE"))
	if err != nil {
		t.Fatalf("read NOTICE: %v", err)
	}
	if !bytes.Contains(notice, []byte("internal/legal/legal.go")) {
		t.Error("NOTICE must tell modifiers to update SourceURL in internal/legal/legal.go")
	}
	if bytes.Contains(notice, []byte("layout.html")) {
		t.Error("NOTICE still tells modifiers to edit layout.html for the Source URL")
	}
	if !bytes.Contains(notice, []byte("internal/web/view/static/OFL.txt")) {
		t.Error("NOTICE must point at the OFL text that travels with the Plex fonts")
	}
}
