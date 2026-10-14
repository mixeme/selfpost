//go:build unix

package logtail

import (
	"fmt"
	"os"
	"syscall"
)

// fileID names the file itself - device and inode - which a rename keeps and
// a new file does not share. It is part of the log's fingerprint, so two log
// files whose first bytes happen to match are still told apart.
func fileID(f *os.File) string {
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%d.%d", st.Dev, st.Ino)
}
