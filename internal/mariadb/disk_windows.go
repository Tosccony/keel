package mariadb

import "golang.org/x/sys/windows"

// diskFree returns the bytes available to this user on the disk holding path.
func diskFree(path string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, nil, nil); err != nil {
		return 0, err
	}
	return free, nil
}

// syncDir does nothing on Windows: NTFS commits renames on its own, and
// folders can't be opened for flushing the way they can on Linux and macOS.
func syncDir(string) {}
