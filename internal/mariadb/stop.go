package mariadb

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

// ErrNotRunning means no mariadbd is running for this data folder.
var ErrNotRunning = errors.New("server is not running")

// ErrStillRunning means the server ignored both requests to shut down. The
// only step left is a hard kill, which needs the user's OK first (see Kill).
var ErrStillRunning = errors.New("server is still running after two requests to shut down")

// StopResult describes how a stop went.
type StopResult struct {
	PID       int
	Nudged    bool // the first request wasn't enough
	Confirmed bool // MariaDB's log says "Shutdown complete"
	Took      time.Duration
}

// Stop shuts the server down cleanly, whether this Server started it or it
// was already running (found through its PID file):
//
//  1. Ask: run SHUTDOWN over SQL, then wait for the process to exit.
//  2. Ask again: if it's still running, send SIGTERM on Linux and macOS
//     (MariaDB treats it as a normal shutdown) or a second SHUTDOWN on
//     Windows, and wait again.
//  3. Give up with ErrStillRunning. Stop never kills; that's Kill's job,
//     and only after the user agrees.
//
// If ctx ends first, Stop returns ctx's error and the server carries on
// shutting down by itself.
func (s *Server) Stop(ctx context.Context) (StopResult, error) {
	unlock, err := s.lock()
	if err != nil {
		return StopResult{}, err
	}
	defer unlock()
	pid := s.RunningPID()
	if pid == 0 {
		return StopResult{}, ErrNotRunning
	}
	return s.shutdown(ctx, pid)
}

// shutdown is Stop without the lock, for callers that already hold it.
func (s *Server) shutdown(ctx context.Context, pid int) (StopResult, error) {
	res := StopResult{PID: pid}
	began := time.Now()
	logStart := fileSize(s.LogFile)

	// If Keel crashes from here on, the next start knows a stop was underway.
	if err := s.setJournal(stateStopping, pid); err != nil {
		s.progress("Couldn't update Keel's journal: " + err.Error())
	}
	s.progress("Asking MariaDB to shut down.")
	askErr := s.askShutdown(ctx, pid)
	if askErr != nil {
		s.progress("Couldn't ask over SQL (" + askErr.Error() + ").")
	} else {
		s.progress("Waiting for MariaDB to finish writing to disk…")
		if s.waitExit(ctx, pid, orDefault(s.StopTimeout, defaultStopTimeout)) {
			return s.stopped(res, began, logStart), nil
		}
	}
	if err := ctx.Err(); err != nil {
		return res, stillShuttingDown(err)
	}

	res.Nudged = true
	if !isServerFor(pid, s.DataDir) { // it finished just now
		return s.stopped(res, began, logStart), nil
	}
	if askErr != nil {
		// For example, root has a password Keel doesn't know.
		s.progress("Asking MariaDB another way.")
	} else {
		s.progress("MariaDB is still running. Asking again.")
	}
	if err := s.nudge(ctx, pid); err != nil {
		s.progress("The second request failed (" + err.Error() + ").")
	}
	if s.waitExit(ctx, pid, orDefault(s.NudgeTimeout, defaultNudgeTimeout)) {
		return s.stopped(res, began, logStart), nil
	}
	if err := ctx.Err(); err != nil {
		return res, stillShuttingDown(err)
	}
	return res, fmt.Errorf("mariadb: PID %d: %w", pid, ErrStillRunning)
}

// Kill force-stops the server: TerminateProcess on Windows, SIGKILL on Linux
// and macOS. This is exactly what corrupts XAMPP's databases, so call it only
// after Stop returned ErrStillRunning and the user agreed. The next start
// runs crash recovery.
func (s *Server) Kill() error {
	unlock, err := s.lock()
	if err != nil {
		return err
	}
	defer unlock()
	pid := s.RunningPID()
	if pid == 0 {
		return ErrNotRunning
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("mariadb: find PID %d: %w", pid, err)
	}
	if err := p.Kill(); err != nil {
		return fmt.Errorf("mariadb: kill PID %d: %w", pid, err)
	}
	if s.child != nil && s.child.cmd.Process.Pid == pid {
		<-s.child.exited
		s.child = nil
	}
	if err := s.setJournal(stateKilled, 0); err != nil {
		s.progress("Couldn't update Keel's journal: " + err.Error())
	}
	s.progress(fmt.Sprintf("Force-stopped MariaDB (PID %d). The next start will check it and run recovery.", pid))
	return nil
}

// RunningPID returns the process ID of the mariadbd using this data folder,
// or 0 if none is running.
//
// A PID from a PID file is only trusted after checking that the process
// really is mariadbd with this data folder. After a crash the file stays
// behind, and its number may since belong to another program.
func (s *Server) RunningPID() int {
	if s.child != nil {
		select {
		case <-s.child.exited:
			s.child = nil
		default:
			return s.child.cmd.Process.Pid
		}
	}
	files := []string{s.pidFile()}
	// A server started by hand writes <hostname>.pid instead.
	if more, err := filepath.Glob(filepath.Join(s.DataDir, "*.pid")); err == nil {
		files = append(files, more...)
	}
	for _, f := range files {
		if pid := readPID(f); isServerFor(pid, s.DataDir) {
			return pid
		}
	}
	return 0
}

// askShutdown runs SHUTDOWN over SQL, after checking that the server on the
// port is the one using our data folder.
//
// It connects to the port the running server was started with, which can
// differ from s.Port: keel-lab -port, or a port Keel moved to since. On
// Windows a wrong port would leave nothing but the force-stop.
func (s *Server) askShutdown(ctx context.Context, pid int) error {
	ctx, cancel := context.WithTimeout(ctx, min(10*time.Second, orDefault(s.StopTimeout, defaultStopTimeout)))
	defer cancel()

	port := s.Port
	if p := serverPort(pid); p != 0 {
		port = p
	}
	db, _, err := s.connect(ctx, port)
	if err != nil {
		return err
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := s.checkDataDir(ctx, conn); err != nil {
		return err
	}

	_, err = conn.ExecContext(ctx, "SHUTDOWN")
	var refused *mysql.MySQLError
	if errors.As(err, &refused) {
		return err // for example, the account lacks the SHUTDOWN privilege
	}
	// Any other error means the server closed the connection while shutting
	// down, which is what we asked for.
	return nil
}

// waitExit waits up to timeout for the server to exit.
func (s *Server) waitExit(ctx context.Context, pid int, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var exited <-chan struct{} // stays nil (blocks forever) for a server we didn't start
	if s.child != nil && s.child.cmd.Process.Pid == pid {
		exited = s.child.exited
	}
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		if exited == nil && !isServerFor(pid, s.DataDir) {
			return true
		}
		select {
		case <-exited:
			return true
		case <-ctx.Done():
			return false
		case <-tick.C:
		}
	}
}

// stopped fills in the result once the server has exited.
func (s *Server) stopped(res StopResult, began time.Time, logStart int64) StopResult {
	res.Took = time.Since(began)
	res.Confirmed = logSaysShutdownComplete(s.LogFile, logStart)
	if s.child != nil && s.child.cmd.Process.Pid == res.PID {
		s.child = nil
	}
	// Only a stop MariaDB's log confirms counts as clean.
	state, pid := stateStopped, 0
	if !res.Confirmed {
		state, pid = stateUnconfirmed, res.PID // kept, so a confirmation logged later still counts
	}
	if err := s.setJournal(state, pid); err != nil {
		s.progress("Couldn't update Keel's journal: " + err.Error())
	}
	s.pruneSnapshots("")
	s.progress("MariaDB stopped.")
	return res
}

func stillShuttingDown(err error) error {
	return fmt.Errorf("mariadb: stopped waiting, but MariaDB is still shutting down on its own: %w", err)
}

// logSaysShutdownComplete looks for MariaDB's last shutdown line, which ends
// in "mariadbd: Shutdown complete". InnoDB's "Shutdown completed; ..." line
// comes earlier and doesn't count.
func logSaysShutdownComplete(path string, from int64) bool {
	for _, line := range readLinesFrom(path, from) {
		if strings.HasSuffix(strings.TrimSpace(line), ": Shutdown complete") {
			return true
		}
	}
	return false
}

// logSaysReady reports whether MariaDB's log, from offset from, says the
// server is ready for connections.
func logSaysReady(path string, from int64) bool {
	for _, line := range readLinesFrom(path, from) {
		if strings.Contains(line, "ready for connections") {
			return true
		}
	}
	return false
}

func readPID(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0
	}
	return pid
}
