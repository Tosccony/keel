package mariadb

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Init creates the data folder with mariadb-install-db and records it as
// cleanly stopped. If the data folder already exists, Init does nothing,
// and doesn't touch the journal either: Keel can't know that folder's
// history, so its first start runs the recovery checks.
//
// Init builds the folder under a temporary name and renames it into place,
// so a failed or interrupted Init never leaves a half-made data folder.
//
// If the data folder is missing because a restore was interrupted, Init
// refuses: a new, empty folder in its place would hide the user's data.
func (s *Server) Init(ctx context.Context) error {
	if _, err := os.Stat(s.DataDir); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("mariadb: data folder: %w", err)
	}
	if old := s.interruptedRestore(); old != "" {
		return s.interruptedRestoreError(old)
	}
	parent := filepath.Dir(s.DataDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("mariadb: %w", err)
	}
	unlock, err := s.lock()
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := os.Stat(s.DataDir); err == nil {
		return nil // another Keel created it before we took the lock
	}
	tmp, err := os.MkdirTemp(parent, ".keel-init-")
	if err != nil {
		return fmt.Errorf("mariadb: %w", err)
	}
	defer os.RemoveAll(tmp) // only does anything if Init fails before the rename

	s.progress("Creating MariaDB's data folder.")
	cmd := exec.CommandContext(ctx, filepath.Join(s.BinDir, exe("mariadb-install-db")), installArgs(s.BinDir, tmp)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mariadb: mariadb-install-db failed: %w\n%s", err, lastLines(string(out), 10))
	}
	if err := os.Rename(tmp, s.DataDir); err != nil {
		return fmt.Errorf("mariadb: %w", err)
	}
	syncDir(parent)
	if err := writeJournal(s.journalFile(), journal{State: stateStopped}); err != nil {
		return fmt.Errorf("mariadb: record the new data folder: %w", err)
	}
	s.progress("Data folder ready: " + s.DataDir)
	return nil
}

// installArgs are mariadb-install-db's options. On Linux and macOS it's a
// script; on Windows it's a different program, mariadb-install-db.exe,
// with options of its own. Either way root gets no password, like XAMPP,
// and only works from this computer.
func installArgs(binDir, dataDir string) []string {
	if runtime.GOOS == "windows" {
		// It also writes a my.ini into the data folder, which Keel ignores:
		// mariadbd always runs with --no-defaults.
		return []string{"--datadir=" + dataDir}
	}
	return []string{
		"--no-defaults",
		"--basedir=" + filepath.Dir(binDir),
		"--datadir=" + dataDir,
		"--auth-root-authentication-method=normal",
		"--skip-test-db",
	}
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return "  " + strings.Join(lines, "\n  ")
}
