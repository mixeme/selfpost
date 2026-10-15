// Package atomicfile writes a file so that readers see the old file or the
// new one, whole, and a power loss after Write returns keeps the new one.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// Write writes data to path via a temp file in the same directory followed by
// a rename, so readers only ever see the complete old or new file. The file
// is synced before the rename and the directory after it: the rename is a
// change of the directory, and without its own sync a power loss may bring
// the old file back (fsync(2): syncing a file "does not necessarily ensure
// that the entry in the directory containing the file has also reached
// disk"; audit 2026-10-12, L7). Every map the panel writes for Postfix and
// OpenDKIM goes through here.
func Write(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", tmpName, path, err)
	}
	cleanup = false
	_ = SyncDir(dir)
	return nil
}

// SyncDir makes a rename in dir durable. It is best effort for its callers:
// the new file is in place and readable either way, and not every system
// syncs a directory - Go on Windows cannot open one for it - so a failure
// leaves the change as durable as it was before this existed.
func SyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
