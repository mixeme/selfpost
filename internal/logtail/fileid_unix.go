//go:build unix

package logtail

import (
	"fmt"
	"os"
	"syscall"
)

// fileID names the file itself - its inode - which a rename keeps and a new
// file does not share. It is part of the log's fingerprint, so two log files
// whose first bytes happen to match are still told apart. The device number
// is left out: it can change across a host's reboot (LVM, btrfs, network
// file systems), and the same log would then pass for a rotated one (review
// of stage 2b).
func fileID(f *os.File) string {
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%d", st.Ino)
}
