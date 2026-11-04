package mariadb

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ErrMustStop means MariaDB has to be stopped before this can run.
var ErrMustStop = errors.New("stop MariaDB first")

// BackupKind says why a copy of the data folder was made.
type BackupKind string

const (
	BeforeRecovery BackupKind = "pre-recovery" // by Start, before crash recovery
	Manual         BackupKind = "backup"       // by BackUp, when the user asked
	BeforeRestore  BackupKind = "pre-restore"  // by Restore, so the restore can be undone
	BeforeRepair   BackupKind = "pre-repair"   // by Repair, before it changed anything
)

// Label names the kind for a person.
func (k BackupKind) Label() string {
	switch k {
	case BeforeRecovery:
		return "Before recovery"
	case Manual:
		return "Backup"
	case BeforeRestore:
		return "Before restore"
	case BeforeRepair:
		return "Before repair"
	}
	return string(k)
}

// kindOf returns the kind a copy's folder name starts with, or "".
func kindOf(name string) BackupKind {
	for _, k := range []BackupKind{BeforeRecovery, Manual, BeforeRestore, BeforeRepair} {
		if strings.HasPrefix(name, string(k)+"-") {
			return k
		}
	}
	return ""
}

// Backup is a copy of the data folder in BackupDir.
type Backup struct {
	Path  string
	Kind  BackupKind
	Taken time.Time
}

// Name is the copy's folder name.
func (b Backup) Name() string { return filepath.Base(b.Path) }

// Size adds up the copy's files. A clone shares its disk space with the
// data folder until either changes, so this can be more than it takes up.
func (b Backup) Size() (int64, error) { return dirSize(b.Path) }

// BackupFolder is where the copies are kept.
func (s *Server) BackupFolder() string { return s.backupDir() }

// Backups lists the finished copies in BackupDir, newest first.
func (s *Server) Backups() ([]Backup, error) {
	entries, err := os.ReadDir(s.backupDir())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var list []Backup
	for _, e := range entries {
		name := e.Name()
		kind := kindOf(name)
		taken, ok := snapshotTime(name)
		if !e.IsDir() || kind == "" || !ok || strings.HasSuffix(name, ".partial") {
			continue
		}
		list = append(list, Backup{Path: filepath.Join(s.backupDir(), name), Kind: kind, Taken: taken})
	}
	sort.SliceStable(list, func(i, j int) bool {
		if !list[i].Taken.Equal(list[j].Taken) {
			return list[i].Taken.After(list[j].Taken)
		}
		return list[i].Name() > list[j].Name() // "-2" was made after the plain name
	})
	return list, nil
}

// BackUp saves a copy of the data folder to BackupDir/backup-<time>.
// MariaDB must be stopped, so the copy is complete. Keel never deletes
// these copies itself.
func (s *Server) BackUp() (Backup, error) {
	if err := s.validate(); err != nil {
		return Backup{}, err
	}
	unlock, err := s.lock()
	if err != nil {
		return Backup{}, err
	}
	defer unlock()
	if pid := s.RunningPID(); pid != 0 {
		return Backup{}, fmt.Errorf("mariadb: MariaDB is running (PID %d): %w", pid, ErrMustStop)
	}
	s.progress("Saving a copy of the data folder…")
	began := time.Now()
	path, err := s.saveCopy(backupPrefix)
	if err != nil {
		return Backup{}, fmt.Errorf("mariadb: couldn't save a copy: %w", err)
	}
	s.progress("Saved a copy to " + path)
	return Backup{Path: path, Kind: Manual, Taken: began}, nil
}

// Restore replaces the data folder with a copy from BackupDir. MariaDB must
// be stopped.
//
// It first saves the current data folder as a pre-restore copy and returns
// its path, so the restore can be undone by restoring that. The next start
// runs crash recovery and checks every table, because a copy made before
// recovery holds data from a crash.
//
// The swap is two renames. If Keel dies between them, the data folder is
// missing and the next start says where the old one is.
func (s *Server) Restore(b Backup) (saved string, err error) {
	unlock, err := s.lock()
	if err != nil {
		return "", err
	}
	defer unlock()
	if pid := s.RunningPID(); pid != 0 {
		return "", fmt.Errorf("mariadb: MariaDB is running (PID %d): %w", pid, ErrMustStop)
	}
	if err := s.checkBackup(b); err != nil {
		return "", err
	}
	parent := filepath.Dir(s.DataDir)
	stamp := time.Now().Format(snapshotLayout)
	removeLeftovers(parent, ".keel-restore-")

	if dirExists(s.DataDir) {
		s.progress("Saving a copy of the current data first, so this can be undone…")
		saved, err = s.saveCopy(preRestorePrefix)
		if err != nil {
			return "", fmt.Errorf("mariadb: %w (%v). Nothing was changed", ErrSnapshotFailed, err)
		}
		s.progress("Saved a copy to " + saved)
	}

	s.progress("Copying " + b.Name() + "…")
	incoming := filepath.Join(parent, ".keel-restore-"+stamp)
	if err := copyDataDir(b.Path, incoming); err != nil {
		return saved, fmt.Errorf("mariadb: couldn't copy %s (%v). Nothing was changed", b.Name(), err)
	}

	before, _ := readJournal(s.journalFile())
	if err := writeJournal(s.journalFile(), journal{State: stateRestoring, Snapshot: b.Path}); err != nil {
		os.RemoveAll(incoming)
		return saved, fmt.Errorf("mariadb: can't record the restore in %s (%v). Nothing was changed", s.journalFile(), err)
	}
	var replaced string
	if dirExists(s.DataDir) {
		replaced = filepath.Join(parent, replacedPrefix+stamp)
		if err := os.Rename(s.DataDir, replaced); err != nil {
			os.RemoveAll(incoming)
			writeJournal(s.journalFile(), before)
			return saved, fmt.Errorf("mariadb: couldn't move the data folder aside (%v). Nothing was changed", err)
		}
	}
	if err := os.Rename(incoming, s.DataDir); err != nil {
		if replaced != "" {
			os.Rename(replaced, s.DataDir)
		}
		os.RemoveAll(incoming)
		writeJournal(s.journalFile(), before)
		return saved, fmt.Errorf("mariadb: couldn't put the copy in place (%v). Nothing was changed", err)
	}
	syncDir(parent)
	if replaced != "" {
		os.RemoveAll(replaced) // saved holds the same data
	}

	// The copy stays in BackupDir untouched, so the next start can use it
	// as its fallback instead of copying the restored data again.
	if err := writeJournal(s.journalFile(), journal{State: stateRestored, Snapshot: b.Path}); err != nil {
		s.progress("Couldn't update Keel's journal: " + err.Error())
	}
	s.progress(fmt.Sprintf("Restored %s. The next start will check every table.", b.Name()))
	return saved, nil
}

// replacedPrefix names the old data folder while Restore swaps folders.
const replacedPrefix = ".keel-replaced-"

// checkBackup makes sure b is a finished copy in BackupDir that looks like
// a MariaDB data folder.
func (s *Server) checkBackup(b Backup) error {
	rel, err := filepath.Rel(s.backupDir(), b.Path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || strings.ContainsRune(rel, filepath.Separator) {
		return fmt.Errorf("mariadb: %s isn't one of Keel's copies in %s", b.Path, s.backupDir())
	}
	if strings.HasSuffix(b.Path, ".partial") {
		return fmt.Errorf("mariadb: %s is an unfinished copy", b.Name())
	}
	if !dirExists(filepath.Join(b.Path, "mysql")) {
		return fmt.Errorf("mariadb: %s doesn't look like a MariaDB data folder (it has no mysql folder)", b.Name())
	}
	return nil
}

// removeLeftovers deletes folders in dir whose names start with prefix:
// the remains of an operation that was interrupted. Only call it while
// holding the lock, so the operation can't still be running.
func removeLeftovers(dir, prefix string) {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			os.RemoveAll(filepath.Join(dir, e.Name()))
		}
	}
}

func (s *Server) interruptedRestoreError(old string) error {
	return fmt.Errorf("mariadb: data folder %s is missing because a restore was interrupted. "+
		"The data from before the restore is in %s; restore a copy, or rename that folder back to %s",
		s.DataDir, old, filepath.Base(s.DataDir))
}

// interruptedRestore returns the old data folder left behind by a restore
// that was interrupted between its two renames, or "".
func (s *Server) interruptedRestore() string {
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(s.DataDir), replacedPrefix+"*"))
	sort.Strings(matches)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1]
}
