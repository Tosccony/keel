package mariadb

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Tosccony/keel/internal/ports"
)

// RepairReport says what Repair did.
type RepairReport struct {
	Copy      string         // the copy saved before anything changed
	AriaReset bool           // Aria's log files were damaged and were rebuilt
	Checked   int            // tables checked at the end
	Repaired  []string       // tables that were damaged and now pass their check
	Remaining []TableProblem // tables still damaged, and why
	Clean     bool           // repair mode shut down cleanly
}

// Repair fixes the damage that stops MariaDB from starting, or that the
// table check found. MariaDB must be stopped.
//
//  1. It saves a copy of the data folder, and changes nothing if it can't.
//  2. It starts MariaDB in repair mode: on Keel's port, without checking
//     accounts, so damaged account tables can't stop it starting. (That's
//     XAMPP's "Can't open and lock privilege tables" error.) Crash recovery
//     runs as part of this start.
//  3. If MariaDB won't start because Aria's log files are damaged (XAMPP's
//     "Aria recovery failed"), it rebuilds them the way MariaDB's own error
//     message asks: deletes them and zerofills every Aria table with aria_chk.
//  4. It checks every table, repairs each damaged one with REPAIR TABLE, and
//     if that's not enough, REPAIR TABLE ... USE_FRM, which rebuilds the
//     table's index from its definition. A table counts as repaired only if
//     it then passes CHECK TABLE. InnoDB tables can't be repaired in place.
//  5. It shuts repair mode down cleanly.
//
// If MariaDB can't start even in repair mode, Repair returns an error and
// the copy is the way back.
func (s *Server) Repair(ctx context.Context) (*RepairReport, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	unlock, err := s.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	if pid := s.RunningPID(); pid != 0 {
		return nil, fmt.Errorf("mariadb: MariaDB is running (PID %d): %w", pid, ErrMustStop)
	}
	if !ports.Free(s.Port) {
		return nil, fmt.Errorf("mariadb: port %d: %w", s.Port, ErrPortInUse)
	}

	rep := &RepairReport{}
	s.progress("Saving a copy of the data folder before repairing…")
	copyPath, err := s.saveCopy(preRepairPrefix)
	if err != nil {
		return nil, fmt.Errorf("mariadb: %w (%v). Nothing was changed", ErrSnapshotFailed, err)
	}
	rep.Copy = copyPath
	s.progress("Saved a copy to " + copyPath)
	if err := s.setJournal(stateRepairing, 0); err != nil {
		return rep, fmt.Errorf("mariadb: can't record the repair in %s (%v). Nothing was changed", s.journalFile(), err)
	}

	s.progress("Starting MariaDB in repair mode…")
	logStart := fileSize(s.LogFile)
	err = s.launch(ctx, repairModeArgs...)
	if err != nil && s.child == nil && ariaLogDamaged(readLinesFrom(s.LogFile, logStart)) {
		s.progress("Aria's log files are damaged. Rebuilding them…")
		if err := s.resetAriaLog(); err != nil {
			return rep, fmt.Errorf("%w\nYour data from before the repair is in %s", err, copyPath)
		}
		rep.AriaReset = true
		s.progress("Starting MariaDB in repair mode again…")
		err = s.launch(ctx, repairModeArgs...)
	}
	if err != nil {
		if s.child != nil { // up, but not answering in time; leave it to finish
			return rep, err
		}
		return rep, fmt.Errorf("mariadb: MariaDB couldn't start even in repair mode, so Keel can't repair it. "+
			"Restore a copy instead; your data from before the repair is in %s.\n%w", copyPath, err)
	}

	workErr := s.repairTables(ctx, rep)

	s.progress("Leaving repair mode…")
	res, err := s.shutdown(ctx, s.PID())
	if err != nil {
		return rep, fmt.Errorf("mariadb: repair mode didn't stop: %w", err)
	}
	rep.Clean = res.Confirmed
	s.pruneSnapshots(copyPath)
	if workErr != nil {
		return rep, workErr
	}
	switch {
	case len(rep.Remaining) > 0:
		s.progress(fmt.Sprintf("Repaired %s; %s still damaged.", tables(len(rep.Repaired)), tables(len(rep.Remaining))))
	case len(rep.Repaired) > 0:
		s.progress(fmt.Sprintf("Repaired %s. All %d tables pass their check.", tables(len(rep.Repaired)), rep.Checked))
	default:
		s.progress(fmt.Sprintf("No damaged tables found. All %d tables pass their check.", rep.Checked))
	}
	return rep, nil
}

// repairModeArgs are added to MariaDB's options in repair mode.
var repairModeArgs = []string{"--skip-grant-tables"}

// repairTables checks every table, repairs the damaged ones, and checks
// again. It fills in rep.
func (s *Server) repairTables(ctx context.Context, rep *RepairReport) error {
	s.progress("Checking every table…")
	_, problems, err := s.CheckTables(ctx)
	if err != nil {
		return fmt.Errorf("mariadb: check tables in repair mode: %w", err)
	}
	if len(problems) > 0 {
		db, _, err := s.connect(ctx, s.Port)
		if err != nil {
			return err
		}
		defer db.Close()
		db.SetMaxOpenConns(1)
		for _, p := range problems {
			s.progress("Repairing " + p.Table + "…")
			repairTable(ctx, db, p)
		}
	}

	// The final check is the only judge: REPAIR TABLE can say "Operation
	// failed" about a repair that worked.
	checked, after, err := s.CheckTables(ctx)
	if err != nil {
		return fmt.Errorf("mariadb: check tables after repairing: %w", err)
	}
	rep.Checked = checked
	still := map[string]bool{}
	for _, p := range after {
		still[p.Table] = true
		if strings.EqualFold(p.Engine, "InnoDB") {
			p.Message += " (InnoDB tables can't be repaired in place; restore a copy from before the damage)"
		}
		rep.Remaining = append(rep.Remaining, p)
	}
	for _, p := range problems {
		if !still[p.Table] {
			rep.Repaired = append(rep.Repaired, p.Table)
		}
	}
	return nil
}

// repairTable runs REPAIR TABLE, then REPAIR TABLE ... USE_FRM if the table
// still fails its check. USE_FRM rebuilds the index from the table's
// definition when the index file is unreadable; it's only tried second
// because it's the heavier fix. InnoDB tables are left alone: REPAIR
// TABLE doesn't support them.
func repairTable(ctx context.Context, db *sql.DB, p TableProblem) {
	if strings.EqualFold(p.Engine, "InnoDB") {
		return
	}
	q := quoteIdent(p.Schema) + "." + quoteIdent(p.Name)
	for _, stmt := range []string{"REPAIR TABLE " + q, "REPAIR TABLE " + q + " USE_FRM"} {
		if rows, err := db.QueryContext(ctx, stmt); err == nil {
			for rows.Next() {
			}
			rows.Close()
		}
		if msg, err := checkTable(ctx, db, p.Schema, p.Name); err == nil && msg == "" {
			return
		}
	}
}

// ariaLogDamaged reports whether MariaDB's log lines from a failed start
// show that Aria's log or control file is damaged.
func ariaLogDamaged(lines []string) bool {
	for _, l := range lines {
		l = strings.ToLower(l)
		for _, sign := range []string{
			"aria control file",
			"aria recovery failed",
			"plugin 'aria' registration",
			"unknown storage engine 'aria'",
			"aria_log",
		} {
			if strings.Contains(l, sign) {
				return true
			}
		}
	}
	return false
}

// resetAriaLog deletes aria_log_control and the aria_log.* files, then
// runs aria_chk --zerofill on every Aria table so none still refers to the
// old log. This is what MariaDB's "Aria recovery failed" message asks for.
// Changes to Aria tables that were only in the damaged log are lost; the
// copy Repair saved first still has the old files.
func (s *Server) resetAriaLog() error {
	logs, _ := filepath.Glob(filepath.Join(s.DataDir, "aria_log*"))
	for _, f := range logs {
		if err := os.Remove(f); err != nil {
			return fmt.Errorf("mariadb: remove %s: %w", f, err)
		}
	}
	var tables []string
	err := filepath.WalkDir(s.DataDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".MAI") {
			rel, err := filepath.Rel(s.DataDir, strings.TrimSuffix(path, ".MAI"))
			if err != nil {
				return err
			}
			tables = append(tables, rel)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("mariadb: find Aria tables: %w", err)
	}
	if len(tables) == 0 {
		return nil
	}
	cmd := exec.Command(filepath.Join(s.BinDir, exe("aria_chk")), append([]string{"--zerofill", "--silent"}, tables...)...)
	cmd.Dir = s.DataDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mariadb: aria_chk --zerofill: %v\n%s", err, lastLines(string(out), 10))
	}
	return nil
}

// tables says "1 table" or "n tables".
func tables(n int) string {
	if n == 1 {
		return "1 table"
	}
	return fmt.Sprintf("%d tables", n)
}

// exe adds ".exe" to a program name on Windows.
func exe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}
