package mariadb

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// damage overwrites n bytes at the start of a file with zeros, or with
// garbage if garbage is set.
func damage(t *testing.T, path string, n int, garbage bool) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fill := bytes.Repeat([]byte{0}, n)
	if garbage {
		fill = bytes.Repeat([]byte{0xde, 0xad, 0xbe, 0xef}, n/4)
	}
	if _, err := f.WriteAt(fill, 0); err != nil {
		t.Fatal(err)
	}
}

// startFails starts s and expects MariaDB to quit during startup.
func startFails(t *testing.T, s *Server, logWants string) {
	t.Helper()
	err := s.Start(context.Background())
	if !errors.Is(err, ErrExitedDuringStartup) {
		t.Fatalf("Start on damaged data: err = %v, want ErrExitedDuringStartup", err)
	}
	if !strings.Contains(err.Error(), logWants) {
		t.Errorf("the error should quote MariaDB's log (%q):\n%v", logWants, err)
	}
}

// startsHealthy starts s after a repair and checks every table passes.
func startsHealthy(t *testing.T, s *Server) {
	t.Helper()
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start after repair: %v", err)
	}
	if rec := s.LastRecovery(); rec != nil {
		t.Errorf("repair mode stopped cleanly, so this start shouldn't need recovery: %+v", rec)
	}
	_, problems, err := s.CheckTables(context.Background())
	if err != nil || len(problems) > 0 {
		t.Errorf("after repair: problems %+v, err %v", problems, err)
	}
}

// XAMPP's best-known error: "Fatal error: Can't open and lock privilege
// tables". Here the index of mysql.global_priv, which holds the accounts,
// has its header wiped. aria_chk can't fix that; repair mode can.
func TestRepairFixesDamagedAccountsTable(t *testing.T) {
	s := newTestServer(t)
	t.Cleanup(func() { stopForCleanup(t, s) })
	index := filepath.Join(s.DataDir, "mysql", "global_priv.MAI")
	damage(t, index, 512, false)
	startFails(t, s, "privilege tables")

	rep, err := s.Repair(context.Background())
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if !slices.Contains(rep.Repaired, "mysql.global_priv") || len(rep.Remaining) > 0 || !rep.Clean || rep.AriaReset {
		t.Errorf("report: %+v", rep)
	}
	// The copy was taken before anything changed, damage included.
	if b, err := os.ReadFile(filepath.Join(rep.Copy, "mysql", "global_priv.MAI")); err != nil || !bytes.Equal(b[:512], make([]byte, 512)) {
		t.Errorf("the copy at %s should hold the data exactly as it was before the repair (err %v)", rep.Copy, err)
	}

	startsHealthy(t, s)
	db := openDB(t, s) // logs in as root, so the accounts are back
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM mysql.global_priv WHERE user = 'root'").Scan(&n); err != nil || n == 0 {
		t.Errorf("root accounts after repair: %d (%v)", n, err)
	}
}

// XAMPP's other famous error: "Aria recovery failed". Here Aria's control
// file is garbage, so the whole Aria engine fails and MariaDB can't start.
func TestRepairRebuildsDamagedAriaLog(t *testing.T) {
	s := newTestServer(t)
	t.Cleanup(func() { stopForCleanup(t, s) })
	damage(t, filepath.Join(s.DataDir, "aria_log_control"), 52, true)
	startFails(t, s, "Aria")

	rep, err := s.Repair(context.Background())
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if !rep.AriaReset || len(rep.Remaining) > 0 || !rep.Clean {
		t.Errorf("report: %+v", rep)
	}
	if _, err := os.Stat(filepath.Join(rep.Copy, "aria_log_control")); err != nil {
		t.Errorf("the copy should still have the old Aria control file: %v", err)
	}
	startsHealthy(t, s)
}

func TestRepairFixesMyISAMAndReportsInnoDB(t *testing.T) {
	s := startTestServer(t)
	db := openDB(t, s)
	mustExec(t, db, "CREATE DATABASE shop")
	mustExec(t, db, "CREATE TABLE shop.visits (id INT PRIMARY KEY, page VARCHAR(20)) ENGINE=MyISAM")
	mustExec(t, db, "INSERT INTO shop.visits VALUES (1, 'home'), (2, 'cart'), (3, 'checkout')")
	mustExec(t, db, "CREATE TABLE shop.orders (id INT PRIMARY KEY) ENGINE=InnoDB")
	mustExec(t, db, "INSERT INTO shop.orders VALUES (1)")
	db.Close()
	if _, err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	damage(t, filepath.Join(s.DataDir, "shop", "visits.MYI"), 1024, false)
	if err := os.Remove(filepath.Join(s.DataDir, "shop", "orders.ibd")); err != nil {
		t.Fatal(err)
	}

	rep, err := s.Repair(context.Background())
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if !slices.Contains(rep.Repaired, "shop.visits") {
		t.Errorf("the MyISAM table should be repaired: %+v", rep)
	}
	if len(rep.Remaining) != 1 || rep.Remaining[0].Table != "shop.orders" ||
		!strings.Contains(rep.Remaining[0].Message, "can't be repaired in place") {
		t.Errorf("the InnoDB table should be reported as needing a restore: %+v", rep.Remaining)
	}

	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopForCleanup(t, s) })
	var n int
	if err := openDB(t, s).QueryRow("SELECT COUNT(*) FROM shop.visits").Scan(&n); err != nil || n != 3 {
		t.Errorf("rows in the repaired table: %d (%v), want 3", n, err)
	}
}

func TestRepairWithNothingWrong(t *testing.T) {
	s := newTestServer(t)
	rep, err := s.Repair(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Repaired) > 0 || len(rep.Remaining) > 0 || !rep.Clean || rep.Checked == 0 {
		t.Errorf("report: %+v", rep)
	}
	if clean, why := s.LastShutdown(); !clean {
		t.Errorf("after a clean repair run, the last shutdown should be clean: %s", why)
	}
}

func TestRepairBackUpAndRestoreNeedMariaDBStopped(t *testing.T) {
	s := startTestServer(t)
	if _, err := s.Repair(context.Background()); !errors.Is(err, ErrMustStop) {
		t.Errorf("Repair while running: err = %v, want ErrMustStop", err)
	}
	if _, err := s.BackUp(); !errors.Is(err, ErrMustStop) {
		t.Errorf("BackUp while running: err = %v, want ErrMustStop", err)
	}
	if _, err := s.Restore(Backup{}); !errors.Is(err, ErrMustStop) {
		t.Errorf("Restore while running: err = %v, want ErrMustStop", err)
	}
	if entries, _ := os.ReadDir(s.backupDir()); len(entries) > 0 {
		t.Errorf("refused calls left %d entries in the backups folder", len(entries))
	}
}

// Stop must reach the server on the port it was started with, even if
// Keel's setting changed since. On Windows a wrong port would leave only
// the force-stop.
func TestStopUsesTheServersRealPort(t *testing.T) {
	s := startTestServer(t)
	other := sameServer(s)
	other.Port = freePort(t)
	res, err := other.Stop(context.Background())
	if err != nil || res.Nudged || !res.Confirmed {
		t.Errorf("Stop with a stale port setting: res %+v, err %v; want a clean first-try stop", res, err)
	}
}

func TestAriaLogDamaged(t *testing.T) {
	for line, want := range map[string]bool{
		"[ERROR] mariadbd: Got error 'Missing valid id at start of file. File is not a valid aria control file'":         true,
		"[ERROR] Aria recovery failed. Please run aria_chk -r on all Aria tables and delete all aria_log.######## files": true,
		"[ERROR] Plugin 'Aria' registration as a STORAGE ENGINE failed.":                                                 true,
		"[ERROR] Fatal error: Can't open and lock privilege tables: Incorrect file format 'global_priv'":                 false,
		"[Note] InnoDB: Completed initialization of buffer pool":                                                         false,
	} {
		if got := ariaLogDamaged([]string{line}); got != want {
			t.Errorf("ariaLogDamaged(%q) = %v, want %v", line, got, want)
		}
	}
}

// If Keel is killed during a repair, MariaDB stays in repair mode (no
// account checks) until it's stopped. The window must be able to tell.
func TestInRepairMode(t *testing.T) {
	s := newTestServer(t)
	t.Cleanup(func() { stopForCleanup(t, s) })
	if err := s.launch(context.Background(), repairModeArgs...); err != nil {
		t.Fatal(err)
	}
	if !s.InRepairMode(s.PID()) {
		t.Error("a server started in repair mode isn't recognised")
	}
	if _, err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.InRepairMode(s.PID()) || s.InRepairMode(0) {
		t.Error("a normal server, or no server, counts as repair mode")
	}
}
