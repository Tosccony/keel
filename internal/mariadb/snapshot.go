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

// Copies in BackupDir are named <prefix><time>, like
// pre-recovery-2026-11-03-121500.
const (
	snapshotPrefix   = "pre-recovery-" // made by Start before crash recovery
	backupPrefix     = "backup-"       // made by BackUp, when the user asks
	preRestorePrefix = "pre-restore-"  // made by Restore, so a restore can be undone
	preRepairPrefix  = "pre-repair-"   // made by Repair before it changes anything

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

// snapshot saves the copy Start makes before crash recovery.
func (s *Server) snapshot() (string, error) { return s.saveCopy(snapshotPrefix) }

// saveCopy copies the data folder to BackupDir/<prefix><time> and returns
// the copy's path. MariaDB must not be running.
func (s *Server) saveCopy(prefix string) (string, error) {
	dir := s.backupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := uniquePath(filepath.Join(dir, prefix+time.Now().Format(snapshotLayout)))
	if err := copyDataDir(s.DataDir, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// copyDataDir copies a data folder to dst, which must not exist: an
// instant copy-on-write clone where the file system allows, otherwise a
// plain copy after checking there's room for it.
//
// The copy is made under a ".partial" name and renamed when complete, so an
// interrupted copy is never mistaken for a good one.
func copyDataDir(src, dst string) error {
	parent := filepath.Dir(dst)
	tmp := dst + ".partial"
	os.RemoveAll(tmp)

	if err := cloneDir(src, tmp); err != nil {
		os.RemoveAll(tmp)
		size, err := dirSize(src)
		if err != nil {
			return err
		}
		free, err := freeSpace(parent)
		if err != nil {
			return fmt.Errorf("check free space: %w", err)
		}
		if need := uint64(size) + snapshotMargin; free < need {
			return fmt.Errorf("not enough free space in %s: the copy needs %s and %s is free", parent, humanBytes(need), humanBytes(free))
		}
		if err := copyTree(src, tmp); err != nil {
			os.RemoveAll(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	syncDir(parent)
	return nil
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

// pruneSnapshots deletes the copies Keel made by itself (before recovery,
// restore or repair) once they're more than a week old, keeping the newest
// of each kind and the one passed as keep (the journal's). Copies made
// with BackUp are the user's, and are never deleted. ".partial" copies
// abandoned more than an hour ago are deleted whatever their kind.
func (s *Server) pruneSnapshots(keep string) {
	dir := s.backupDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	byKind := map[BackupKind][]string{}
	for _, e := range entries {
		name := e.Name()
		kind := kindOf(name)
		if !e.IsDir() || kind == "" {
			continue
		}
		if strings.HasSuffix(name, ".partial") {
			if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > time.Hour {
				os.RemoveAll(filepath.Join(dir, name))
			}
			continue
		}
		if kind != Manual {
			byKind[kind] = append(byKind[kind], name)
		}
	}
	for _, names := range byKind {
		sort.Strings(names) // names end in a sortable timestamp
		for i, name := range names {
			path := filepath.Join(dir, name)
			if i == len(names)-1 || (keep != "" && sameDir(path, keep)) {
				continue
			}
			if taken, ok := snapshotTime(name); ok && time.Since(taken) > snapshotKeep {
				os.RemoveAll(path)
			}
		}
	}
}

// snapshotTime reads the time from a name like pre-recovery-2026-11-03-111652-2.
func snapshotTime(name string) (time.Time, bool) {
	kind := kindOf(name)
	if kind == "" {
		return time.Time{}, false
	}
	stamp := strings.TrimPrefix(name, string(kind)+"-")
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
