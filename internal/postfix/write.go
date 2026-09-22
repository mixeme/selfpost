package postfix

import (
	"os"

	"github.com/mixeme/selfpost/internal/atomicfile"
)

// writeFileAtomic writes data to path via a temp file in the same directory
// followed by a rename, so a concurrent Postfix reload only ever sees the
// complete old or new map, never a partial write (security.md).
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	return atomicfile.Write(path, data, perm)
}
