package mariadb

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrBusy means another Keel (a second window, or keel-lab) is starting,
// stopping or setting up this MariaDB right now.
var ErrBusy = errors.New("another Keel is starting or stopping this MariaDB right now")

// errLocked is what lockFile returns when someone else holds the lock.
var errLocked = errors.New("locked")

func (s *Server) lockFile() string {
	if s.LockFile != "" {
		return s.LockFile
	}
	return filepath.Join(filepath.Dir(s.DataDir), "keel.lock")
}

// lock takes the data folder's lock without waiting, so two Keels can't
// start, stop or recover the same MariaDB at once. The OS drops the lock
// when Keel exits or crashes, so it never goes stale. The file itself is
// left in place: deleting it would let two Keels lock different files.
func (s *Server) lock() (unlock func(), err error) {
	path := s.lockFile()
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("mariadb: open lock file: %w", err)
	}
	if err := lockFile(f); err != nil {
		f.Close()
		if errors.Is(err, errLocked) {
			return nil, fmt.Errorf("mariadb: %w", ErrBusy)
		}
		return nil, fmt.Errorf("mariadb: lock %s: %w", path, err)
	}
	return func() {
		unlockFile(f)
		f.Close()
	}, nil
}
