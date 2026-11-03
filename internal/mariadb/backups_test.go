package mariadb

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeCopy makes a folder in BackupDir that looks enough like a data folder
// for Restore, holding one file with the given contents.
func fakeCopy(t *testing.T, s *Server, name, contents string) Backup {
	t.Helper()
	path := filepath.Join(s.backupDir(), name)
	if err := os.MkdirAll(filepath.Join(path, "mysql"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "mysql", "marker"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	taken, _ := snapshotTime(name)
	return Backup{Path: path, Kind: kindOf(name), Taken: taken}
}

func readMarker(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "mysql", "marker"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestBackupsListsNewestFirst(t *testing.T) {
	s := &Server{DataDir: filepath.Join(t.TempDir(), "data")}
	for _, name := range []string{
		"pre-recovery-2026-11-01-090000",
		"backup-2026-11-03-120000",
		"backup-2026-11-03-120000-2",
		"pre-restore-2026-11-02-080000",
		"pre-repair-2026-11-02-100000",
		"backup-2026-11-04-000000.partial", // unfinished
		"holiday-photos",                   // not Keel's
	} {
		if err := os.MkdirAll(filepath.Join(s.backupDir(), name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.Backups()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, b := range list {
		got = append(got, b.Name()+" "+b.Kind.Label())
	}
	want := []string{
		"backup-2026-11-03-120000-2 Backup",
		"backup-2026-11-03-120000 Backup",
		"pre-repair-2026-11-02-100000 Before repair",
		"pre-restore-2026-11-02-080000 Before restore",
		"pre-recovery-2026-11-01-090000 Before recovery",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("Backups() =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	empty := &Server{DataDir: filepath.Join(t.TempDir(), "data")}
	if list, err := empty.Backups(); err != nil || len(list) != 0 {
		t.Errorf("with no backups folder: %v, %v", list, err)
	}
}

func TestPruneNeverDeletesTheUsersBackups(t *testing.T) {
	s := &Server{DataDir: filepath.Join(t.TempDir(), "data")}
	old := func(d time.Duration) string { return time.Now().Add(-d).Format(snapshotLayout) }
	day := 24 * time.Hour
	keep := map[string]bool{
		"backup-" + old(400*day):            true,  // the user's, however old
		"backup-" + old(30*day):             true,  // the user's
		"pre-restore-" + old(30*day):        false, // Keel's, old
		"pre-restore-" + old(20*day):        true,  // Keel's, old, but the newest of its kind
		"pre-repair-" + old(10*day):         true,  // newest of its kind
		"pre-recovery-" + old(9*day):        false, // old
		"pre-recovery-" + old(1*day):        true,  // recent
		"backup-" + old(3*day) + ".partial": false, // abandoned half-copy, even of a backup
	}
	for name := range keep {
		if err := os.MkdirAll(filepath.Join(s.backupDir(), name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ago := time.Now().Add(-2 * time.Hour)
	for name := range keep {
		if strings.HasSuffix(name, ".partial") {
			os.Chtimes(filepath.Join(s.backupDir(), name), ago, ago)
		}
	}
	s.pruneSnapshots("")
	for name, want := range keep {
		_, err := os.Stat(filepath.Join(s.backupDir(), name))
		if got := err == nil; got != want {
			t.Errorf("%s: kept = %v, want %v", name, got, want)
		}
	}
}

func TestRestoreRejectsBadCopies(t *testing.T) {
	dir := t.TempDir()
	s := &Server{DataDir: filepath.Join(dir, "data")}
	if err := os.MkdirAll(filepath.Join(s.DataDir, "mysql"), 0o755); err != nil {
		t.Fatal(err)
	}
	notData := filepath.Join(s.backupDir(), "backup-2026-11-03-120000")
	os.MkdirAll(notData, 0o755)
	partial := fakeCopy(t, s, "backup-2026-11-03-130000.partial", "x")
	outside := filepath.Join(dir, "elsewhere")
	os.MkdirAll(filepath.Join(outside, "mysql"), 0o755)

	for _, b := range []Backup{
		{Path: outside},
		{Path: s.backupDir()},
		{Path: filepath.Join(s.backupDir(), "..", "data")},
		{Path: notData},
		partial,
	} {
		if _, err := s.Restore(b); err == nil {
			t.Errorf("Restore(%s) succeeded", b.Path)
		}
	}
	entries, _ := os.ReadDir(s.backupDir())
	if len(entries) != 2 {
		t.Errorf("a refused restore saved a copy anyway: %d entries in backups", len(entries))
	}
}

func TestRestoreSwapsAndCanBeUndone(t *testing.T) {
	s := &Server{DataDir: filepath.Join(t.TempDir(), "data")}
	os.MkdirAll(filepath.Join(s.DataDir, "mysql"), 0o755)
	os.WriteFile(filepath.Join(s.DataDir, "mysql", "marker"), []byte("today"), 0o644)
	b := fakeCopy(t, s, "backup-2026-11-01-090000", "last week")

	saved, err := s.Restore(b)
	if err != nil {
		t.Fatal(err)
	}
	if got := readMarker(t, s.DataDir); got != "last week" {
		t.Errorf("data folder after restore holds %q", got)
	}
	if got := readMarker(t, b.Path); got != "last week" {
		t.Error("the restored copy itself must stay as it was")
	}
	if !strings.HasPrefix(filepath.Base(saved), preRestorePrefix) || readMarker(t, saved) != "today" {
		t.Errorf("the pre-restore copy %s should hold the data from before", saved)
	}
	if clean, why := s.LastShutdown(); clean || !strings.Contains(why, "restored") {
		t.Errorf("after a restore the next start must check everything: clean=%v %q", clean, why)
	}
	if j, _ := readJournal(s.journalFile()); j.Snapshot != b.Path {
		t.Errorf("the journal should offer the restored copy as the fallback, has %q", j.Snapshot)
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(s.DataDir), ".keel-*"))
	if len(leftovers) > 0 {
		t.Errorf("restore left %v behind", leftovers)
	}

	// Undo: restore the copy Restore saved.
	list, _ := s.Backups()
	var undo Backup
	for _, c := range list {
		if c.Path == saved {
			undo = c
		}
	}
	if _, err := s.Restore(undo); err != nil {
		t.Fatal(err)
	}
	if got := readMarker(t, s.DataDir); got != "today" {
		t.Errorf("after undoing, the data folder holds %q", got)
	}
}

func TestRestoreIntoMissingDataFolder(t *testing.T) {
	s := &Server{DataDir: filepath.Join(t.TempDir(), "data")}
	b := fakeCopy(t, s, "pre-recovery-2026-11-01-090000", "saved")
	saved, err := s.Restore(b)
	if err != nil {
		t.Fatal(err)
	}
	if saved != "" || readMarker(t, s.DataDir) != "saved" {
		t.Errorf("saved %q, marker %q", saved, readMarker(t, s.DataDir))
	}
}

func TestInterruptedRestoreIsExplained(t *testing.T) {
	dir := t.TempDir()
	s := &Server{DataDir: filepath.Join(dir, "data"), Port: freePort(t), Socket: filepath.Join(dir, "s.sock")}
	old := filepath.Join(dir, replacedPrefix+"2026-11-03-120000")
	os.MkdirAll(old, 0o755)
	err := s.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "restore was interrupted") || !strings.Contains(err.Error(), old) {
		t.Errorf("Start with the data folder missing mid-restore: %v", err)
	}
}

// A real round trip with MariaDB: back up, change data, restore, and the
// next start recovers and checks the restored data, using the backup as its
// fallback instead of copying again.
func TestBackUpAndRestoreWithMariaDB(t *testing.T) {
	s := startTestServer(t)
	ctx := context.Background()
	db := openDB(t, s)
	mustExec(t, db, "CREATE DATABASE lab")
	mustExec(t, db, "CREATE TABLE lab.notes (id INT PRIMARY KEY, note VARCHAR(20))")
	mustExec(t, db, "INSERT INTO lab.notes VALUES (1, 'first draft')")
	db.Close()
	if _, err := s.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	b, err := s.BackUp()
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	db = openDB(t, s)
	mustExec(t, db, "UPDATE lab.notes SET note = 'oops' WHERE id = 1")
	db.Close()
	if _, err := s.Stop(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Restore(b); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopForCleanup(t, s) })
	rec := s.LastRecovery()
	if rec == nil || rec.Snapshot != b.Path || len(rec.Problems) > 0 || rec.CheckErr != nil {
		t.Errorf("start after restore: %+v", rec)
	}
	var note string
	if err := openDB(t, s).QueryRow("SELECT note FROM lab.notes WHERE id = 1").Scan(&note); err != nil || note != "first draft" {
		t.Errorf("note = %q (%v), want the backed-up %q", note, err, "first draft")
	}
	if backups, _ := s.Backups(); len(backups) != 2 { // the backup, and the copy saved before restoring
		t.Errorf("want 2 copies (backup + pre-restore), got %d", len(backups))
	}
}
