package mariadb

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	snapshotPrefix = "pre-recovery-"
	snapshotLayout = "2006-01-02-150405"
	snapshotKeep   = 7 * 24 * time.Hour
	// snapshotMargin is free space to leave on top of the copy itself, so
	// the copy never fills the disk MariaDB needs for recovery.
	snapshotMargin = 256 << 20
)

// Hooks that tests replace.
var (
	freeSpace = diskFree  // free bytes on the disk holding a path
	cloneDir  = cloneTree // instant copy-on-write copy, where the file system supports it
)

var errNoClone = errors.New("copy-on-write cloning isn't available here")

func (s *Server) backupDir() string {
	if s.BackupDir != "" {
		return s.BackupDir
	}
	return filepath.Join(filepath.Dir(s.DataDir), "backups")
}

// snapshot copies the data folder to BackupDir/pre-recovery-<time> and
// returns the copy's path. MariaDB must not be running.
//
// The copy is made under a ".partial" name and renamed when complete, so an
// interrupted copy is never mistaken for a good one.
func (s *Server) snapshot() (string, error) {
	dir := s.backupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := uniquePath(filepath.Join(dir, snapshotPrefix+time.Now().Format(snapshotLayout)))
	tmp := dst + ".partial"
	os.RemoveAll(tmp)

	if err := cloneDir(s.DataDir, tmp); err != nil {
		os.RemoveAll(tmp)
		size, err := dirSize(s.DataDir)
		if err != nil {
			return "", err
		}
		free, err := freeSpace(dir)
		if err != nil {
			return "", fmt.Errorf("check free space: %w", err)
		}
		if need := uint64(size) + snapshotMargin; free < need {
			return "", fmt.Errorf("not enough free space in %s: the copy needs %s and %s is free", dir, humanBytes(need), humanBytes(free))
		}
		if err := copyTree(s.DataDir, tmp); err != nil {
			os.RemoveAll(tmp)
			return "", err
		}
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	syncDir(dir)
	return dst, nil
}

// uniquePath adds -2, -3, ... if path already exists.
func uniquePath(path string) string {
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		return path
	}
	for i := 2; ; i++ {
		p := fmt.Sprintf("%s-%d", path, i)
		if _, err := os.Lstat(p); errors.Is(err, fs.ErrNotExist) {
			return p
		}
	}
}

// copyTree copies a folder: regular files byte for byte and flushed to
// disk, subfolders, and symlinks as symlinks. Anything else, such as a
// socket, is skipped; it isn't data.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info() // doesn't follow symlinks
		if err != nil {
			return err
		}
		switch mode := info.Mode(); {
		case mode.IsDir():
			return os.MkdirAll(target, mode.Perm()|0o700)
		case mode&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case mode.IsRegular():
			return copyFile(path, target, info)
		default:
			return nil
		}
	})
}

func copyFile(src, dst string, info fs.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if cloneFile(out, in) != nil {
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}

func dirSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// pruneSnapshots deletes pre-recovery copies older than a week, and
// ".partial" copies abandoned more than an hour ago. It always keeps the
// newest copy and the one passed as keep (the journal's).
func (s *Server) pruneSnapshots(keep string) {
	dir := s.backupDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var snaps []string
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !strings.HasPrefix(name, snapshotPrefix) {
			continue
		}
		if strings.HasSuffix(name, ".partial") {
			if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > time.Hour {
				os.RemoveAll(filepath.Join(dir, name))
			}
			continue
		}
		snaps = append(snaps, name)
	}
	sort.Strings(snaps) // names start with a sortable timestamp
	for i, name := range snaps {
		path := filepath.Join(dir, name)
		if i == len(snaps)-1 || (keep != "" && sameDir(path, keep)) {
			continue
		}
		if taken, ok := snapshotTime(name); ok && time.Since(taken) > snapshotKeep {
			os.RemoveAll(path)
		}
	}
}

// snapshotTime reads the time from a name like pre-recovery-2026-11-03-111652-2.
func snapshotTime(name string) (time.Time, bool) {
	stamp := strings.TrimPrefix(name, snapshotPrefix)
	if len(stamp) < len(snapshotLayout) {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation(snapshotLayout, stamp[:len(snapshotLayout)], time.Local)
	return t, err == nil
}

func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
