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
