package mariadb

import (
	"os"

	"golang.org/x/sys/unix"
)

// cloneTree has no whole-folder equivalent on Linux; copyTree clones file
// by file instead.
func cloneTree(_, _ string) error {
	return errNoClone
}

// cloneFile shares the file's blocks on Btrfs and XFS (a reflink), which is
// instant. Other file systems refuse, and copyFile falls back to copying.
func cloneFile(dst, src *os.File) error {
	return unix.IoctlFileClone(int(dst.Fd()), int(src.Fd()))
}
