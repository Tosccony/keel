//go:build !windows

package mariadb

import (
	"os"

	"golang.org/x/sys/unix"
)

// diskFree returns the bytes available to this user on the disk holding path.
func diskFree(path string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}

// syncDir flushes a folder's entry list, so a rename inside it survives a
// power cut. Errors are ignored: some file systems don't support it.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
}
