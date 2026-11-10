//go:build darwin

package setup

import (
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const quarantine = "com.apple.quarantine"

// ClearQuarantine removes macOS's "downloaded from the internet" mark from
// the files inside Keel.app. Everything copied out of a downloaded disk
// image carries the mark, and macOS can refuse to run a marked program
// that isn't notarized (MariaDB, Apache, PHP) even though the user already
// chose to open Keel itself. It does nothing outside an app bundle, works
// only once (when the MariaDB server is still marked), and skips files it
// can't change.
func ClearQuarantine() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	clearQuarantine(exe)
}

// clearQuarantine returns how many files it cleared.
func clearQuarantine(exe string) int {
	macos := filepath.Dir(exe)
	contents := filepath.Dir(macos)
	if filepath.Base(macos) != "MacOS" || filepath.Base(contents) != "Contents" {
		return 0
	}
	resources := filepath.Join(contents, "Resources")
	if !quarantined(filepath.Join(resources, "mariadb", "bin", "mariadbd")) {
		return 0
	}
	n := 0
	filepath.WalkDir(resources, func(path string, _ fs.DirEntry, err error) error {
		if err == nil && unix.Lremovexattr(path, quarantine) == nil {
			n++
		}
		return nil
	})
	return n
}

func quarantined(path string) bool {
	_, err := unix.Lgetxattr(path, quarantine, nil)
	return err == nil
}
