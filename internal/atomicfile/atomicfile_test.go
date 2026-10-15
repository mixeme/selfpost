package atomicfile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// leftovers lists what is in dir besides the names given.
func leftovers(t *testing.T, dir string, names ...string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	keep := map[string]bool{}
	for _, n := range names {
		keep[n] = true
	}
	var out []string
	for _, e := range entries {
		if !keep[e.Name()] {
			out = append(out, e.Name())
		}
	}
	return out
}

// A new file and a replaced one hold what was written, with the mode asked
// for, and nothing is left beside them.
func TestWriteCreatesAndReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "map")
	for _, body := range []string{"first\n", "second, longer than the first\n", ""} {
		if err := Write(path, []byte(body), 0o640); err != nil {
			t.Fatalf("Write %q: %v", body, err)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != body {
			t.Fatalf("file = %q, %v; want %q", got, err, body)
		}
		if runtime.GOOS != "windows" {
			if info, _ := os.Stat(path); info.Mode().Perm() != 0o640 {
				t.Errorf("mode = %v, want 0640", info.Mode().Perm())
			}
		}
		if rest := leftovers(t, dir, "map"); len(rest) != 0 {
			t.Errorf("Write left %v", rest)
		}
	}
}

// A reader that opened the old file reads the old file to its end: the new
// one takes the name, it does not overwrite the old one's bytes.
func TestAReaderKeepsTheOldFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not rename over an open file; the container is Linux")
	}
	path := filepath.Join(t.TempDir(), "map")
	if err := Write(path, []byte("old contents\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := Write(path, []byte("new\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	n, _ := f.Read(buf)
	if string(buf[:n]) != "old contents\n" {
		t.Errorf("the open reader read %q", buf[:n])
	}
}

// A write that fails leaves the old file and no temporary one.
func TestAFailedWriteLeavesTheOldFile(t *testing.T) {
	dir := t.TempDir()
	if err := Write(filepath.Join(dir, "missing", "map"), []byte("x"), 0o640); err == nil {
		t.Error("Write into a directory that is not there succeeded")
	}

	// The name is a directory: the rename fails after the temporary file
	// was written.
	target := filepath.Join(dir, "taken")
	if err := os.MkdirAll(filepath.Join(target, "inside"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := Write(target, []byte("x"), 0o640); err == nil {
		t.Error("Write over a directory succeeded")
	}
	if rest := leftovers(t, dir, "taken"); len(rest) != 0 {
		t.Errorf("the failed Write left %v", rest)
	}
	if _, err := os.Stat(filepath.Join(target, "inside")); err != nil {
		t.Errorf("the failed Write changed the directory in its way: %v", err)
	}
}

// The directory is synced where the system allows it; on Linux, where the
// container runs, it does.
func TestSyncDir(t *testing.T) {
	err := SyncDir(t.TempDir())
	if runtime.GOOS == "linux" && err != nil {
		t.Errorf("SyncDir on Linux = %v", err)
	}
	if err := SyncDir(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("SyncDir of a directory that is not there = nil")
	}
}
