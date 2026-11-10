package mariadb

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNeedsRecovery(t *testing.T) {
	tests := []struct {
		name      string
		j         journal
		jerr      error
		ev        logEvidence
		cleanEnd  bool // MariaDB's log confirms the journal's run shut down cleanly
		wantDirty bool
		wantWhy   string
	}{
		{"no journal", journal{}, errNoJournal, logNoEvidence, false, true, "no record"},
		{"unreadable journal", journal{}, errors.New("unreadable journal: bad"), logNoEvidence, false, true, "can't be read"},
		{"clean stop", journal{State: stateStopped}, nil, logNoEvidence, false, false, ""},
		{"clean stop, log agrees", journal{State: stateStopped}, nil, logCleanShutdown, false, false, ""},
		{"clean stop, but log shows a later start", journal{State: stateStopped}, nil, logStartedNotStopped, false, true, "started again"},
		{"was running", journal{State: stateRunning}, nil, logCleanShutdown, false, true, "still running"},
		{"was stopping", journal{State: stateStopping}, nil, logCleanShutdown, false, true, "middle of stopping"},
		{"was killed", journal{State: stateKilled}, nil, logNoEvidence, false, true, "force-stopped"},
		{"stop not confirmed", journal{State: stateUnconfirmed}, nil, logNoEvidence, false, true, "wasn't confirmed"},
		{"empty state", journal{}, nil, logNoEvidence, false, true, "unknown state"},
		{"unknown state", journal{State: "paused"}, nil, logNoEvidence, false, true, "unknown state"},
		// The computer shut down, or someone ran SHUTDOWN: MariaDB stopped
		// cleanly by itself, and its log says so for the very run Keel recorded.
		{"was running, MariaDB's log confirms that run stopped cleanly", journal{State: stateRunning, PID: 7}, nil, logCleanShutdown, true, false, ""},
		{"was stopping, and the stop finished cleanly", journal{State: stateStopping, PID: 7}, nil, logCleanShutdown, true, false, ""},
		{"stop confirmed in the log later", journal{State: stateUnconfirmed, PID: 7}, nil, logCleanShutdown, true, false, ""},
		// A kill can't write "Shutdown complete", and the special flows keep
		// their own rules, whatever the log says.
		{"killed, even if the log looks clean", journal{State: stateKilled, PID: 7}, nil, logCleanShutdown, true, true, "force-stopped"},
		{"restored, even if the log looks clean", journal{State: stateRestored, PID: 7}, nil, logCleanShutdown, true, true, "restored"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dirty, why := needsRecovery(tt.j, tt.jerr, tt.ev, tt.cleanEnd)
			if dirty != tt.wantDirty || !strings.Contains(why, tt.wantWhy) {
				t.Errorf("needsRecovery = %v, %q; want %v and a reason containing %q", dirty, why, tt.wantDirty, tt.wantWhy)
			}
		})
	}
}

func TestJournalRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "j.json")
	want := journal{State: stateRunning, PID: 4242, Snapshot: "/backups/pre-recovery-x"}
	if err := writeJournal(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := readJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != want.State || got.PID != want.PID || got.Snapshot != want.Snapshot || got.Updated.IsZero() {
		t.Errorf("read back %+v, want %+v with a time", got, want)
	}
}

func TestWriteJournalReplacesAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "j.json")
	for _, state := range []string{stateRunning, stateStopping, stateStopped} {
		if err := writeJournal(path, journal{State: state}); err != nil {
			t.Fatal(err)
		}
	}
	if j, _ := readJournal(path); j.State != stateStopped {
		t.Errorf("state = %q, want the last write (stopped)", j.State)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("leftover files next to the journal: %v", names)
	}
}

func TestReadJournalProblems(t *testing.T) {
	dir := t.TempDir()
	if _, err := readJournal(filepath.Join(dir, "missing.json")); !errors.Is(err, errNoJournal) {
		t.Errorf("missing journal: got %v, want errNoJournal", err)
	}
	for name, content := range map[string]string{
		"empty":     "",
		"garbage":   "\x00\x01not json",
		"truncated": `{"state": "stop`,
	} {
		path := filepath.Join(dir, name+".json")
		os.WriteFile(path, []byte(content), 0o644)
		_, err := readJournal(path)
		if err == nil || errors.Is(err, errNoJournal) {
			t.Errorf("%s journal: got %v, want an unreadable-journal error", name, err)
		}
	}
}

func TestWriteJournalFailsInReadOnlyFolder(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs Unix permissions and a non-root user")
	}
	dir := t.TempDir()
	os.Chmod(dir, 0o500)
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	if err := writeJournal(filepath.Join(dir, "j.json"), journal{State: stateRunning}); err == nil {
		t.Error("writing into a read-only folder succeeded")
	}
}

func TestReadLogEvidence(t *testing.T) {
	const (
		ready    = "2026-11-03 10:39:09 0 [Note] /opt/homebrew/bin/mariadbd: ready for connections."
		innodb   = "2026-11-03 10:50:26 0 [Note] InnoDB: Shutdown completed; log sequence number 47983; transaction id 24"
		shutdown = "2026-11-03 10:50:26 0 [Note] /opt/homebrew/bin/mariadbd: Shutdown complete"
		other    = "2026-11-03 10:39:09 0 [Note] InnoDB: Buffer pool(s) load completed"
	)
	tests := []struct {
		name  string
		lines []string
		want  logEvidence
	}{
		{"empty log", nil, logNoEvidence},
		{"no start or stop lines", []string{other, other}, logNoEvidence},
		{"started, never stopped", []string{other, ready, other}, logStartedNotStopped},
		{"started then stopped", []string{ready, other, innodb, shutdown}, logCleanShutdown},
		{"InnoDB's line alone isn't a clean stop", []string{ready, innodb}, logStartedNotStopped},
		{"stopped, then started again", []string{ready, shutdown, ready}, logStartedNotStopped},
		{"two clean runs", []string{ready, shutdown, ready, shutdown}, logCleanShutdown},
		{"Windows line endings", []string{ready + "\r", shutdown + "\r"}, logCleanShutdown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "error.log")
			os.WriteFile(path, []byte(strings.Join(tt.lines, "\n")), 0o644)
			if got := readLogEvidence(path); got != tt.want {
				t.Errorf("readLogEvidence = %v, want %v", got, tt.want)
			}
		})
	}
	if got := readLogEvidence(filepath.Join(t.TempDir(), "missing.log")); got != logNoEvidence {
		t.Errorf("missing log: got %v, want logNoEvidence", got)
	}
}

func TestReadLogEvidenceReadsOnlyTheTail(t *testing.T) {
	// A big log: a start far back (outside the tail), then lots of noise,
	// then a clean stop at the end. Only the end matters.
	path := filepath.Join(t.TempDir(), "error.log")
	noise := strings.Repeat("2026-11-03 10:39:09 0 [Note] noise noise noise noise noise noise\n", 60000) // ~4 MB
	content := "x mariadbd: ready for connections.\n" + noise + "x mariadbd: Shutdown complete\n"
	os.WriteFile(path, []byte(content), 0o644)
	if got := readLogEvidence(path); got != logCleanShutdown {
		t.Errorf("clean stop at the end of a big log: got %v", got)
	}
	os.WriteFile(path, []byte(content+noise+"x mariadbd: ready for connections.\n"), 0o644)
	if got := readLogEvidence(path); got != logStartedNotStopped {
		t.Errorf("start at the end of a big log: got %v", got)
	}
}

func TestLastShutdown(t *testing.T) {
	dir := t.TempDir()
	s := &Server{DataDir: filepath.Join(dir, "data"), LogFile: filepath.Join(dir, "error.log")}
	os.Mkdir(s.DataDir, 0o755) // an existing data folder; before setup there's nothing to recover
	if clean, why := s.LastShutdown(); clean || !strings.Contains(why, "no record") {
		t.Errorf("no journal: clean=%v (%s)", clean, why)
	}
	writeJournal(s.journalFile(), journal{State: stateStopped})
	if clean, _ := s.LastShutdown(); !clean {
		t.Error("journal says stopped, but LastShutdown says unclean")
	}
	s.setJournal(stateKilled, 0)
	if clean, why := s.LastShutdown(); clean || !strings.Contains(why, "force-stopped") {
		t.Errorf("after a kill: clean=%v (%s)", clean, why)
	}
}

func TestSetJournalKeepsSnapshotUntilCleanStop(t *testing.T) {
	dir := t.TempDir()
	s := &Server{DataDir: filepath.Join(dir, "data")}
	writeJournal(s.journalFile(), journal{State: stateRunning, Snapshot: "/backups/one"})
	s.setJournal(stateStopping, 7)
	if j, _ := readJournal(s.journalFile()); j.Snapshot != "/backups/one" || j.PID != 7 {
		t.Errorf("stopping: %+v, want the snapshot kept and PID 7", j)
	}
	s.setJournal(stateStopped, 0)
	if j, _ := readJournal(s.journalFile()); j.Snapshot != "" {
		t.Errorf("a clean stop should forget the snapshot, got %q", j.Snapshot)
	}
}

// runEndedCleanly only trusts the log for the run Keel recorded. A crash
// early in a new start must not borrow the previous run's clean shutdown.
func TestRunEndedCleanly(t *testing.T) {
	const (
		start100 = "2026-11-10 10:00:00 0 [Note] Starting MariaDB 11.8.9-MariaDB source revision abc server_uid x as process 100"
		start200 = "2026-11-10 11:00:00 0 [Note] Starting MariaDB 11.8.9-MariaDB source revision abc server_uid x as process 200"
		ready    = "2026-11-10 10:00:01 0 [Note] /keel/mariadb/bin/mariadbd: ready for connections."
		down     = "2026-11-10 10:30:00 0 [Note] /keel/mariadb/bin/mariadbd: Shutdown complete"
	)
	for _, tt := range []struct {
		name string
		log  []string
		pid  int
		want bool
	}{
		{"the recorded run stopped cleanly", []string{start100, ready, down}, 100, true},
		{"a different process's clean stop", []string{start100, ready, down}, 200, false},
		{"crash early in the next start", []string{start100, ready, down, start200}, 200, false},
		{"and the old run doesn't count once a new one began", []string{start100, ready, down, start200}, 100, false},
		{"the recorded run never stopped", []string{start100, ready}, 100, false},
		{"no PID recorded", []string{start100, ready, down}, 0, false},
		{"Windows line endings", []string{start100 + "\r", ready + "\r", down + "\r"}, 100, true},
	} {
		path := filepath.Join(t.TempDir(), "mariadb.log")
		os.WriteFile(path, []byte(strings.Join(tt.log, "\n")+"\n"), 0o644)
		if got := runEndedCleanly(path, tt.pid); got != tt.want {
			t.Errorf("%s: runEndedCleanly = %v, want %v", tt.name, got, tt.want)
		}
	}
	if runEndedCleanly(filepath.Join(t.TempDir(), "missing.log"), 100) {
		t.Error("a missing log can't confirm anything")
	}
}
