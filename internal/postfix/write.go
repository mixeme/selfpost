package postfix

import (
	"bytes"
	"os"

	"github.com/mixeme/selfpost/internal/atomicfile"
)

// writeFileAtomic writes data to path via a temp file in the same directory
// followed by a rename, so a concurrent Postfix reload only ever sees the
// complete old or new map, never a partial write (security.md).
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	return atomicfile.Write(path, data, perm)
}

// mapFile is one generated table and what it should hold.
type mapFile struct {
	path string
	data []byte
}

// writeChanged writes each file whose content differs from what is on disk
// and says whether any did. A save that changes nothing Postfix reads - a
// domain user saving a form as it was, or changing a field no map holds -
// then writes nothing and asks no reload: every save used to reload Postfix,
// at whatever rate a user cared to press (security audit 2026-10-17, W6).
func writeChanged(files ...mapFile) (bool, error) {
	changed := false
	for _, f := range files {
		if old, err := os.ReadFile(f.path); err == nil && bytes.Equal(old, f.data) {
			continue
		}
		if err := writeFileAtomic(f.path, f.data, 0o640); err != nil {
			return changed, err
		}
		changed = true
	}
	return changed, nil
}
