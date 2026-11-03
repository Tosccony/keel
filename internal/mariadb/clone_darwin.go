package mariadb

import (
	"os"

	"golang.org/x/sys/unix"
)

// cloneTree copies a whole folder with APFS cloning: instant, and it takes
// no extra space until either copy changes.
func cloneTree(src, dst string) error {
	return unix.Clonefile(src, dst, unix.CLONE_NOFOLLOW)
}

// cloneFile isn't needed on macOS, because cloneTree clones whole folders.
func cloneFile(_, _ *os.File) error {
	return errNoClone
}
