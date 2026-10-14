//go:build !unix

package logtail

import "os"

// fileID is empty where the system has no inode to give (the panel runs on
// Linux; this keeps the package building elsewhere for its tests).
func fileID(*os.File) string { return "" }
