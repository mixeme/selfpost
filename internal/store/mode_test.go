package store

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The database and its WAL and shared-memory files are the panel's alone:
// a file that was 0644 is 0600 after Open, and the -wal and -shm files
// SQLite opens are 0600 too (keepPrivate).
func TestOpenKeepsTheDatabasePrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are POSIX")
	}
	path := filepath.Join(t.TempDir(), "selfpost.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.db.Exec(`PRAGMA user_version`); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatalf("%s: %v", filepath.Base(p), err)
		}
		if m := fi.Mode().Perm(); m != 0o600 {
			t.Errorf("%s mode %o, want 600", filepath.Base(p), m)
		}
	}
}
