package mariadb

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests cover the crash journal end to end with real MariaDB servers:
// how Keel notices an unclean shutdown, protects the data, recovers, and
// checks every table.

func TestInitCreatesWorkingDataFolder(t *testing.T) {
	dir := t.TempDir()
	s := &Server{
		BinDir:  binDir(t),
		DataDir: filepath.Join(dir, "data"),
		Port:    freePort(t),
		Socket:  shortSocketPath(t, dir),
		LogFile: filepath.Join(dir, "error.log"),
	}
	if err := s.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if clean, why := s.LastShutdown(); !clean {
		t.Errorf("a new data folder should count as cleanly stopped: %s", why)
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, ".keel-init-*")); len(matches) != 0 {
		t.Errorf("Init left temporary folders: %v", matches)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start after Init: %v", err)
	}
	t.Cleanup(func() { stopForCleanup(t, s) })
	if s.LastRecovery() != nil {
		t.Error("the first start of a new data folder ran recovery")
	}
}

func TestInitLeavesExistingDataAlone(t *testing.T) {
	dir := t.TempDir()
	s := &Server{BinDir: binDir(t), DataDir: filepath.Join(dir, "data")}
	os.MkdirAll(s.DataDir, 0o755)
	marker := filepath.Join(s.DataDir, "my-database-files")
	os.WriteFile(marker, []byte("keep me"), 0o644)

	if err := s.Init(context.Background()); err != nil {
		t.Fatalf("Init on an existing folder: %v", err)
	}
	if b, _ := os.ReadFile(marker); string(b) != "keep me" {
		t.Error("Init changed an existing data folder")
	}
	if _, err := readJournal(s.journalFile()); !errors.Is(err, errNoJournal) {
		t.Error("Init wrote a journal for a folder whose history it doesn't know")
	}
}

func TestInitFailureLeavesNothing(t *testing.T) {
	dir := t.TempDir()
	s := &Server{BinDir: t.TempDir(), DataDir: filepath.Join(dir, "data")} // no MariaDB there
	if err := s.Init(context.Background()); err == nil {
		t.Fatal("Init succeeded without mariadb-install-db")
	}
	// Only the lock file may stay: it's never deleted, so two Keels always
	// lock the same file.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "keel.lock" {
			t.Errorf("a failed Init left %s behind", e.Name())
		}
	}
}

func TestKillThenStartRecovers(t *testing.T) {
	s := startTestServer(t)
	db := openDB(t, s)
	mustExec(t, db, "CREATE DATABASE lab")
	mustExec(t, db, "CREATE TABLE lab.notes (id INT PRIMARY KEY, note VARCHAR(50))")
	mustExec(t, db, "INSERT INTO lab.notes VALUES (1, 'before the kill')")

	if err := s.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if clean, why := s.LastShutdown(); clean || !strings.Contains(why, "force-stopped") {
		t.Errorf("after Kill: clean=%v (%s)", clean, why)
	}
	before := treeFingerprint(t, s.DataDir)

	rec := startAndRecover(t, s)
	if !strings.Contains(rec.Reason, "force-stopped") {
		t.Errorf("reason = %q", rec.Reason)
	}
	if strings.Join(treeFingerprint(t, rec.Snapshot), "\n") != strings.Join(before, "\n") {
		t.Error("the snapshot doesn't match the data folder as it was before recovery")
	}
	var note string
	if err := openDB(t, s).QueryRow("SELECT note FROM lab.notes WHERE id = 1").Scan(&note); err != nil || note != "before the kill" {
		t.Errorf("row written before the kill: %q, %v", note, err)
	}
}

func TestOutsideKillIsDetected(t *testing.T) {
	s := startTestServer(t)
	killOutside(t, s) // like kill -9 or Task Manager: the journal still says running

	rec := startAndRecover(t, s)
	if !strings.Contains(rec.Reason, "still running when it went away") {
		t.Errorf("reason = %q", rec.Reason)
	}
}

func TestStopAfterCrashKeepsJournalDirty(t *testing.T) {
	s := startTestServer(t)
	killOutside(t, s)

	if _, err := s.Stop(context.Background()); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("Stop on a crashed server: got %v, want ErrNotRunning", err)
	}
	if clean, _ := s.LastShutdown(); clean {
		t.Error("stopping an already-crashed server made the journal say clean")
	}
	startAndRecover(t, s)
}

func TestUnconfirmedStopCountsAsUnclean(t *testing.T) {
	s := startTestServer(t)
	realLog := s.LogFile
	s.LogFile = filepath.Join(t.TempDir(), "elsewhere.log") // Stop won't find "Shutdown complete"
	res, err := s.Stop(context.Background())
	s.LogFile = realLog
	if err != nil {
		t.Fatal(err)
	}
	if res.Confirmed {
		t.Fatal("expected an unconfirmed stop")
	}
	if j, _ := readJournal(s.journalFile()); j.State != stateUnconfirmed {
		t.Errorf("journal state = %q, want %q", j.State, stateUnconfirmed)
	}
	// Make it truly unconfirmed: the real log loses its "Shutdown
	// complete", as if MariaDB had died partway through shutting down.
	b, _ := os.ReadFile(realLog)
	var kept []string
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasSuffix(strings.TrimSpace(line), ": Shutdown complete") {
			kept = append(kept, line)
		}
	}
	os.WriteFile(realLog, []byte(strings.Join(kept, "\n")), 0o644)
	rec := startAndRecover(t, s)
	if !strings.Contains(rec.Reason, "wasn't confirmed") {
		t.Errorf("reason = %q", rec.Reason)
	}
}

// If Stop couldn't confirm the shutdown but MariaDB's log shows that very
// run did shut down cleanly, it was a clean stop.
func TestConfirmationInTheLogCountsLater(t *testing.T) {
	s := startTestServer(t)
	realLog := s.LogFile
	s.LogFile = filepath.Join(t.TempDir(), "elsewhere.log") // Stop won't find "Shutdown complete"
	res, err := s.Stop(context.Background())
	s.LogFile = realLog
	if err != nil || res.Confirmed {
		t.Fatalf("stop: %+v, %v", res, err)
	}
	if clean, why := s.LastShutdown(); !clean {
		t.Errorf("MariaDB's own log confirms the run shut down cleanly, but: %s", why)
	}
}

func TestJournalProblemsTriggerRecovery(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(s *Server)
		wantWhy string
	}{
		{"journal missing", func(s *Server) { os.Remove(s.journalFile()) }, "no record"},
		{"journal corrupt", func(s *Server) { os.WriteFile(s.journalFile(), []byte("{garbage"), 0o644) }, "can't be read"},
		{"journal says stopping", func(s *Server) { s.setJournal(stateStopping, 0) }, "middle of stopping"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t)
			t.Cleanup(func() { stopForCleanup(t, s) })
			tt.setup(s)
			rec := startAndRecover(t, s)
			if !strings.Contains(rec.Reason, tt.wantWhy) {
				t.Errorf("reason = %q, want it to mention %q", rec.Reason, tt.wantWhy)
			}
		})
	}
}

func TestLogCatchesKillTheJournalMissed(t *testing.T) {
	// A server started with the same log file by something that doesn't
	// keep Keel's journal, then killed. The journal says "stopped", but
	// MariaDB's log shows a start with no clean shutdown after it.
	s := startTestServer(t)
	s.setJournal(stateStopped, 0)
	killOutside(t, s)

	rec := startAndRecover(t, s)
	if !strings.Contains(rec.Reason, "log shows") {
		t.Errorf("reason = %q", rec.Reason)
	}
}

func TestSnapshotFailureBlocksStart(t *testing.T) {
	s := newTestServer(t)
	s.setJournal(stateKilled, 0)
	cloneDir = func(string, string) error { return errNoClone }
	freeSpace = func(string) (uint64, error) { return 0, nil }
	t.Cleanup(func() { cloneDir, freeSpace = cloneTree, diskFree })

	err := s.Start(context.Background())
	if !errors.Is(err, ErrSnapshotFailed) {
		t.Fatalf("Start with no room for a copy: got %v, want ErrSnapshotFailed", err)
	}
	if !strings.Contains(err.Error(), "nothing has changed") {
		t.Errorf("the error should say nothing changed: %v", err)
	}
	if s.RunningPID() != 0 {
		t.Fatal("MariaDB was started even though the copy failed")
	}
	if j, _ := readJournal(s.journalFile()); j.State != stateKilled {
		t.Errorf("journal changed to %q; it should still say killed", j.State)
	}

	if err := s.StartWithoutSnapshot(context.Background()); err != nil {
		t.Fatalf("StartWithoutSnapshot: %v", err)
	}
	t.Cleanup(func() { stopForCleanup(t, s) })
	if rec := s.LastRecovery(); rec == nil || rec.Snapshot != "" || rec.Checked == 0 {
		t.Errorf("recovery without a copy: %+v", rec)
	}
}

func TestRecoveryCheckFindsDamagedTable(t *testing.T) {
	s := startTestServer(t)
	db := openDB(t, s)
	mustExec(t, db, "CREATE DATABASE shop")
	mustExec(t, db, "CREATE TABLE shop.orders (id INT PRIMARY KEY) ENGINE=MyISAM")
	mustExec(t, db, "INSERT INTO shop.orders VALUES (1), (2), (3)")
	db.Close()
	if _, err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Damage the table while the server is down, then report a crash.
	if err := os.Remove(filepath.Join(s.DataDir, "shop", "orders.MYD")); err != nil {
		t.Fatal(err)
	}
	s.setJournal(stateKilled, 0)

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	rec := s.LastRecovery()
	if rec == nil || rec.CheckErr != nil {
		t.Fatalf("recovery report: %+v", rec)
	}
	found := false
	for _, p := range rec.Problems {
		if p.Table == "shop.orders" {
			found = true
		}
	}
	if !found {
		t.Errorf("the check missed the damaged table; problems: %+v", rec.Problems)
	}
}

func TestCheckTablesHandlesOddNamesAndViews(t *testing.T) {
	s := startTestServer(t)
	before, problems, err := s.CheckTables(context.Background())
	if err != nil || len(problems) != 0 || before == 0 {
		t.Fatalf("baseline check: %d tables, %v, %v", before, problems, err)
	}
	db := openDB(t, s)
	mustExec(t, db, "CREATE DATABASE `we``ird db`")
	mustExec(t, db, "CREATE TABLE `we``ird db`.`café ✓ table` (id INT) ENGINE=InnoDB")
	mustExec(t, db, "CREATE TABLE `we``ird db`.`aria t` (id INT) ENGINE=Aria")
	mustExec(t, db, "CREATE VIEW `we``ird db`.`a view` AS SELECT 1 AS one")

	after, problems, err := s.CheckTables(context.Background())
	if err != nil || len(problems) != 0 {
		t.Fatalf("check with odd names: %v, %v", problems, err)
	}
	if after != before+2 {
		t.Errorf("checked %d tables, want %d (two new tables, views skipped)", after, before+2)
	}
}

func TestRecoveryReportClearedOnCleanStart(t *testing.T) {
	s := startTestServer(t)
	killOutside(t, s)
	startAndRecover(t, s)
	if _, err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.LastRecovery() != nil {
		t.Error("a clean start still reports the earlier recovery")
	}
}

func TestCommittedRowsSurviveRepeatedKills(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	const cycles = 5
	s := startTestServer(t)
	setup := openDB(t, s)
	mustExec(t, setup, "CREATE DATABASE lab")
	mustExec(t, setup, "CREATE TABLE lab.t (id INT PRIMARY KEY) ENGINE=InnoDB")
	setup.Close()

	committed := map[int]bool{} // rows MariaDB said were committed
	for cycle := 0; cycle < cycles; cycle++ {
		db, err := sql.Open("mysql", s.dsn())
		if err != nil {
			t.Fatal(err)
		}
		var next int
		db.QueryRow("SELECT COALESCE(MAX(id), 0) + 1 FROM lab.t").Scan(&next)

		var written []int
		done := make(chan struct{})
		go func() {
			defer close(done)
			for id := next; ; id++ {
				if _, err := db.Exec("INSERT INTO lab.t (id) VALUES (?)", id); err != nil {
					return // the server died mid-write
				}
				written = append(written, id)
			}
		}()
		time.Sleep(300 * time.Millisecond)
		killOutside(t, s)
		<-done
		db.Close()
		if len(written) == 0 {
			t.Fatalf("cycle %d: nothing was written before the kill", cycle)
		}
		for _, id := range written {
			committed[id] = true
		}

		rec := startAndRecover(t, s)
		if rec.Checked == 0 {
			t.Fatalf("cycle %d: no tables were checked", cycle)
		}
		present := map[int]bool{}
		rows, err := openDB(t, s).Query("SELECT id FROM lab.t")
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var id int
			rows.Scan(&id)
			present[id] = true
		}
		rows.Close()
		for id := range committed {
			if !present[id] {
				t.Fatalf("cycle %d: committed row %d is missing after recovery", cycle, id)
			}
		}
	}
	snaps, _ := filepath.Glob(filepath.Join(s.backupDir(), snapshotPrefix+"*"))
	if len(snaps) != cycles {
		t.Errorf("%d snapshots for %d crashes; each crash needs its own", len(snaps), cycles)
	}
	t.Logf("%d committed rows survived %d kills", len(committed), cycles)
}

// startAndRecover starts a server whose last shutdown wasn't clean and
// checks that recovery ran, saved a copy, and found no problems.
func startAndRecover(t *testing.T, s *Server) *Recovery {
	t.Helper()
	if clean, _ := s.LastShutdown(); clean {
		t.Fatal("LastShutdown says clean before a recovery start")
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	rec := s.LastRecovery()
	switch {
	case rec == nil:
		t.Fatal("Start didn't run recovery")
	case rec.Snapshot == "" || !dirExists(rec.Snapshot):
		t.Fatalf("no snapshot was saved: %q", rec.Snapshot)
	case rec.CheckErr != nil:
		t.Fatalf("table check failed to run: %v", rec.CheckErr)
	case len(rec.Problems) != 0:
		t.Fatalf("tables with problems after recovery: %+v", rec.Problems)
	}
	if j, _ := readJournal(s.journalFile()); j.State != stateRunning || j.Snapshot != "" {
		t.Errorf("journal after a successful recovery = %+v, want running with no snapshot", j)
	}
	return rec
}

func mustExec(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.Exec(query); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}
