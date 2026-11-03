package mariadb

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Journal states. Anything but stateStopped means the last shutdown wasn't
// clean, and the next Start runs the recovery steps.
const (
	stateStopped     = "stopped"     // stopped, and MariaDB's log confirmed "Shutdown complete"
	stateRunning     = "running"     // written just before launching mariadbd
	stateStopping    = "stopping"    // a stop was in progress
	stateKilled      = "killed"      // force-stopped with Kill
	stateUnconfirmed = "unconfirmed" // exited after a stop request, but the log didn't confirm it
	stateRepairing   = "repairing"   // Repair was running MariaDB in repair mode
	stateRestoring   = "restoring"   // Restore was swapping the data folder for a copy
	stateRestored    = "restored"    // a copy was restored and hasn't been started since
)

// journal is Keel's record of how MariaDB last stopped. It lives next to
// the data folder, not inside it, so copies of the data folder don't
// carry it along.
type journal struct {
	State string `json:"state"`
	PID   int    `json:"pid,omitempty"`
	// Snapshot is the copy taken before recovering from the current unclean
	// shutdown. While it's set, a failed recovery doesn't take another copy:
	// the first one holds the data from before any recovery attempt.
	Snapshot string    `json:"snapshot,omitempty"`
	Updated  time.Time `json:"updated"`
}

var errNoJournal = errors.New("no journal")

func (s *Server) journalFile() string {
	if s.JournalFile != "" {
		return s.JournalFile
	}
	return filepath.Join(filepath.Dir(s.DataDir), "keel-journal.json")
}

func readJournal(path string) (journal, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return journal{}, errNoJournal
	}
	if err != nil {
		return journal{}, err
	}
	var j journal
	if err := json.Unmarshal(b, &j); err != nil {
		return journal{}, fmt.Errorf("unreadable journal: %w", err)
	}
	return j, nil
}

// writeJournal replaces the journal atomically. It writes a temporary file,
// flushes it to disk, and renames it over the old one, so a crash mid-write
// leaves the old journal or the new one, never a mix.
func writeJournal(path string, j journal) error {
	j.Updated = time.Now().UTC()
	b, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".keel-journal-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	done := false
	defer func() {
		if !done {
			os.Remove(tmp)
		}
	}()
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	done = true
	syncDir(dir)
	return nil
}

// setJournal records a new state, keeping the recorded snapshot unless the
// server stopped cleanly.
func (s *Server) setJournal(state string, pid int) error {
	j, _ := readJournal(s.journalFile())
	j.State, j.PID = state, pid
	if state == stateStopped {
		j.Snapshot = ""
	}
	return writeJournal(s.journalFile(), j)
}

// logEvidence is what MariaDB's own log says about the last shutdown.
type logEvidence int

const (
	logNoEvidence        logEvidence = iota // no start or shutdown lines found
	logCleanShutdown                        // the last start was followed by "Shutdown complete"
	logStartedNotStopped                    // the last start was never followed by "Shutdown complete"
)

// logTailBytes is how much of the end of the log readLogEvidence reads.
const logTailBytes = 256 << 10

// readLogEvidence checks the end of MariaDB's log. If the last "ready for
// connections" comes after the last "Shutdown complete", the server went
// away without shutting down cleanly. This catches unclean stops that
// Keel's journal can't know about, such as a server started by hand with
// the same log file and then killed.
func readLogEvidence(path string) logEvidence {
	from := fileSize(path) - logTailBytes
	if from < 0 {
		from = 0
	}
	lastReady, lastDown := -1, -1
	for i, line := range readLinesFrom(path, from) {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasSuffix(line, "ready for connections."):
			lastReady = i
		case strings.HasSuffix(line, ": Shutdown complete"):
			lastDown = i
		}
	}
	switch {
	case lastReady < 0 && lastDown < 0:
		return logNoEvidence
	case lastReady > lastDown:
		return logStartedNotStopped
	default:
		return logCleanShutdown
	}
}

// needsRecovery decides whether the last shutdown was clean, and says why
// not in plain words. It's deliberately suspicious: anything other than a
// confirmed clean stop counts as unclean. An unneeded check costs a few
// seconds; a missed one can cost a damaged database.
func needsRecovery(j journal, jerr error, ev logEvidence) (bool, string) {
	switch {
	case errors.Is(jerr, errNoJournal):
		return true, "Keel has no record of how MariaDB last stopped."
	case jerr != nil:
		return true, "Keel's record of the last shutdown can't be read."
	}
	switch j.State {
	case stateStopped:
		if ev == logStartedNotStopped {
			return true, "MariaDB's log shows it started again after its last clean stop and never shut down cleanly."
		}
		return false, ""
	case stateRunning:
		return true, "MariaDB was still running when it went away (a crash, a power cut, or a kill)."
	case stateStopping:
		return true, "MariaDB went away in the middle of stopping."
	case stateKilled:
		return true, "MariaDB was force-stopped."
	case stateUnconfirmed:
		return true, "The last stop wasn't confirmed in MariaDB's log."
	case stateRepairing:
		return true, "A repair was interrupted."
	case stateRestoring:
		return true, "A restore was interrupted."
	case stateRestored:
		return true, "The data was just restored from a copy."
	default:
		return true, fmt.Sprintf("Keel's record of the last shutdown has an unknown state (%q).", j.State)
	}
}

// LastShutdown reports whether MariaDB last shut down cleanly, according to
// Keel's journal and MariaDB's log, and why not if it didn't. Start runs
// the recovery steps whenever this says unclean. While the server is
// running, the answer describes the current run, which hasn't stopped yet.
func (s *Server) LastShutdown() (clean bool, reason string) {
	j, err := readJournal(s.journalFile())
	dirty, why := needsRecovery(j, err, readLogEvidence(s.LogFile))
	return !dirty, why
}
