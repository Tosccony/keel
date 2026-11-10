// Package mariadb starts and stops a MariaDB server that Keel owns.
//
// The rule this package exists to enforce: the database is never killed by
// accident. Start and Stop never send a kill signal, a cancelled context
// never stops the server, and the server runs in its own process group so
// signals aimed at Keel (Ctrl+C, a crashing window) never reach it. The one
// hard kill, Kill, has to be called on purpose after the user agrees.
package mariadb

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/Tosccony/keel/internal/ports"
)

var (
	// ErrPortInUse means another program already accepts connections on
	// the server's port.
	ErrPortInUse = errors.New("port already in use")

	// ErrAlreadyRunning means a mariadbd for this data folder is already
	// running, so Start didn't launch a second one.
	ErrAlreadyRunning = errors.New("server is already running")

	// ErrSnapshotFailed means the last shutdown wasn't clean and Keel
	// couldn't copy the data folder first, so it didn't start MariaDB.
	// StartWithoutSnapshot starts anyway, if the user agrees.
	ErrSnapshotFailed = errors.New("couldn't save a copy of the data folder")
)

// Recovery describes what Start did after an unclean shutdown.
type Recovery struct {
	Reason   string         // why the last shutdown counts as unclean
	Snapshot string         // copy of the data folder from before recovery; "" if skipped
	Checked  int            // tables checked once MariaDB was running
	Problems []TableProblem // tables that failed their check
	CheckErr error          // set if the check itself couldn't run
}

const (
	defaultReadyTimeout = 30 * time.Second
	defaultStopTimeout  = 2 * time.Minute // a big buffer pool can take a while to flush
	defaultNudgeTimeout = time.Minute

	// maxSocketPath is the longest Unix socket path MariaDB accepts on
	// macOS. Linux allows a few more characters; using the smaller limit
	// everywhere keeps behaviour the same.
	maxSocketPath = 103
)

// Server is one MariaDB server and the files it uses. It isn't safe for
// concurrent use; Keel drives each server from one goroutine.
type Server struct {
	BinDir  string // folder that holds mariadbd
	DataDir string // data folder, created beforehand by mariadb-install-db
	Port    int
	Socket  string // Unix socket path; macOS limits it to about 100 characters
	LogFile string // MariaDB's error log
	PIDFile string // where mariadbd writes its process ID; default DataDir/mariadbd.pid

	// JournalFile records how MariaDB last stopped. Default: keel-journal.json
	// next to the data folder.
	JournalFile string
	// BackupDir holds the copies taken before recovery. Default: backups/
	// next to the data folder.
	BackupDir string
	// LockFile is locked while Keel starts, stops or sets up MariaDB.
	// Default: keel.lock next to the data folder.
	LockFile string

	ReadyTimeout time.Duration // how long Start waits for connections; default 30s
	StopTimeout  time.Duration // how long Stop waits after asking; default 2 minutes
	NudgeTimeout time.Duration // how long Stop waits after asking again; default 1 minute

	// Progress, if set, receives plain-language updates during Start and
	// Stop, ready for the window's activity log.
	Progress func(msg string)

	child        *child // the mariadbd this Server launched, while it runs
	lastRecovery *Recovery
}

// child is a mariadbd process this Server launched.
type child struct {
	cmd    *exec.Cmd
	exited chan struct{} // closed when the process exits
	err    error         // exit status; read only after exited is closed
}

// Start launches mariadbd and returns once the server accepts connections.
//
// If the last shutdown wasn't clean (see LastShutdown), Start first copies
// the data folder to BackupDir, then lets MariaDB recover, then runs CHECK
// TABLE on every table. LastRecovery describes the result. If the copy
// can't be made, Start returns ErrSnapshotFailed without starting MariaDB.
//
// If ctx is cancelled or the wait times out, Start returns an error but leaves
// the server running: a slow start, such as crash recovery on a big database,
// is still a healthy start, and killing it would cause the damage Keel exists
// to prevent.
// The MySQL driver logs dropped connections to stderr. A server that's
// stopping or failing to start drops them on purpose, and Keel reports
// what matters itself.
func init() { mysql.SetLogger(&mysql.NopLogger{}) }

// ErrExitedDuringStartup means mariadbd quit before accepting connections,
// usually because of damage it can't recover from by itself. Repair is the
// next step.
var ErrExitedDuringStartup = errors.New("server exited during startup")

func (s *Server) Start(ctx context.Context) error {
	return s.start(ctx, true)
}

// StartWithoutSnapshot is Start without the copy before recovery. Use it
// only when the user has agreed after Start returned ErrSnapshotFailed.
func (s *Server) StartWithoutSnapshot(ctx context.Context) error {
	return s.start(ctx, false)
}

// LastRecovery describes the recovery the last Start ran, or is nil if the
// last start was clean.
func (s *Server) LastRecovery() *Recovery {
	return s.lastRecovery
}

func (s *Server) start(ctx context.Context, takeSnapshot bool) error {
	if err := s.validate(); err != nil {
		return err
	}
	unlock, err := s.lock()
	if err != nil {
		return err
	}
	defer unlock()
	// RunningPID also forgets a server this Server started that has since
	// died, so a crashed server can be started again.
	if pid := s.RunningPID(); pid != 0 {
		return fmt.Errorf("mariadb: PID %d: %w", pid, ErrAlreadyRunning)
	}
	if !ports.Free(s.Port) {
		return fmt.Errorf("mariadb: port %d: %w", s.Port, ErrPortInUse)
	}
	s.lastRecovery = nil

	// Was the last shutdown clean? If not, protect the data first.
	j, jerr := readJournal(s.journalFile())
	var rec *Recovery
	if dirty, reason := needsRecovery(j, jerr, readLogEvidence(s.LogFile), runEndedCleanly(s.LogFile, j.PID)); dirty {
		rec = &Recovery{Reason: reason}
		s.progress("The last shutdown wasn't clean. " + reason)
		switch {
		case j.Snapshot != "" && dirExists(j.Snapshot):
			// An earlier recovery attempt already saved the data from before
			// any recovery ran. That copy is the one worth keeping.
			rec.Snapshot = j.Snapshot
			s.progress("A copy from before this recovery is already saved: " + j.Snapshot)
		case takeSnapshot:
			s.progress("Saving a copy of the data folder before MariaDB touches it…")
			path, err := s.snapshot()
			if err != nil {
				return fmt.Errorf("mariadb: %w (%v). MariaDB wasn't started, so nothing has changed", ErrSnapshotFailed, err)
			}
			rec.Snapshot = path
			s.progress("Saved a copy to " + path)
		default:
			s.progress("Starting without a copy, as asked.")
		}
	}

	// Record the start before launching, so a crash during startup also
	// counts as unclean. Without the record, Keel couldn't notice a crash,
	// so it doesn't start MariaDB if the record can't be written.
	next := journal{State: stateRunning}
	if rec != nil {
		next.Snapshot = rec.Snapshot
	}
	if err := writeJournal(s.journalFile(), next); err != nil {
		return fmt.Errorf("mariadb: can't record the start in %s (%v), so MariaDB wasn't started", s.journalFile(), err)
	}

	if err := s.launch(ctx); err != nil {
		if rec != nil && rec.Snapshot != "" {
			err = fmt.Errorf("%w\nYour data from before this start is saved in %s", err, rec.Snapshot)
		}
		return err
	}
	// MariaDB is up, so any recovery has finished. Forget the copy: if the
	// server goes away again, the data has changed and needs a fresh one.
	next.PID, next.Snapshot = s.PID(), ""
	if err := writeJournal(s.journalFile(), next); err != nil {
		s.progress("Couldn't update Keel's journal: " + err.Error())
	}

	if rec != nil {
		s.lastRecovery = rec
		s.progress("Checking every table…")
		rec.Checked, rec.Problems, rec.CheckErr = s.CheckTables(ctx)
		switch {
		case rec.CheckErr != nil:
			s.progress("Couldn't check the tables: " + rec.CheckErr.Error())
		case len(rec.Problems) == 0:
			s.progress(fmt.Sprintf("Recovered. All %d tables passed their check.", rec.Checked))
		default:
			s.progress(fmt.Sprintf("%d of %d tables have problems:", len(rec.Problems), rec.Checked))
			for _, p := range rec.Problems {
				s.progress("  " + p.Table + ": " + p.Message)
			}
		}
		s.pruneSnapshots(rec.Snapshot)
	}
	return nil
}

// launch starts mariadbd and waits until it's ready. extra adds options
// for this run only, such as --skip-grant-tables for repair mode.
func (s *Server) launch(ctx context.Context, extra ...string) error {
	logf, err := os.OpenFile(s.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("mariadb: open log: %w", err)
	}
	defer logf.Close()              // the server keeps its own copy of the file handle
	logStart := fileSize(s.LogFile) // so errors only show lines from this start

	// exec.Command, not exec.CommandContext: CommandContext kills the process
	// when the context ends.
	cmd := exec.Command(filepath.Join(s.BinDir, binaryName), append(s.args(), extra...)...)
	// Write straight to the log file instead of through a pipe, so the server
	// never depends on Keel staying alive to read its output.
	cmd.Stdout = logf
	cmd.Stderr = logf
	detach(cmd)

	s.progress(fmt.Sprintf("Starting MariaDB on port %d.", s.Port))
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("mariadb: start %s: %w", cmd.Path, err)
	}
	c := &child{cmd: cmd, exited: make(chan struct{})}
	go func() {
		c.err = cmd.Wait()
		close(c.exited)
	}()
	s.child = c

	return s.waitReady(ctx, logStart)
}

// PID returns the process ID of the server this Server started, or 0.
// RunningPID also finds a server started elsewhere.
func (s *Server) PID() int {
	if s.child == nil {
		return 0
	}
	return s.child.cmd.Process.Pid
}

// Exited is closed when the server this Server started exits. It is nil if
// this Server hasn't started one.
func (s *Server) Exited() <-chan struct{} {
	if s.child == nil {
		return nil
	}
	return s.child.exited
}

// validate catches setup mistakes before mariadbd starts, so they show up
// as a plain message instead of a half-started server.
func (s *Server) validate() error {
	info, err := os.Stat(s.DataDir)
	if errors.Is(err, fs.ErrNotExist) {
		if old := s.interruptedRestore(); old != "" {
			return s.interruptedRestoreError(old)
		}
		return fmt.Errorf("mariadb: data folder %s doesn't exist; create it with mariadb-install-db first", s.DataDir)
	}
	if err != nil {
		return fmt.Errorf("mariadb: data folder: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("mariadb: data folder %s is a file, not a folder", s.DataDir)
	}
	if runtime.GOOS != "windows" && len(s.Socket) > maxSocketPath {
		return fmt.Errorf("mariadb: socket path is %d characters, over the %d MariaDB allows: %s", len(s.Socket), maxSocketPath, s.Socket)
	}
	return nil
}

// args builds the same command line as running mariadbd by hand.
func (s *Server) args() []string {
	return []string{
		"--no-defaults", // must come first: ignore every my.cnf so only Keel's settings apply
		"--datadir=" + s.DataDir,
		"--port=" + strconv.Itoa(s.Port),
		"--bind-address=127.0.0.1",
		"--socket=" + s.Socket,
		"--log-error=" + s.LogFile,
		"--pid-file=" + s.pidFile(),
	}
}

func (s *Server) pidFile() string {
	if s.PIDFile != "" {
		return s.PIDFile
	}
	return filepath.Join(s.DataDir, "mariadbd.pid")
}

// dsn is the connection string for the root account over TCP.
func (s *Server) dsn() string { return s.dsnFor(s.Port) }

func (s *Server) dsnFor(port int) string {
	cfg := mysql.NewConfig()
	cfg.User = "root"
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	cfg.Timeout = time.Second
	cfg.ReadTimeout = 10 * time.Second
	cfg.WriteTimeout = 10 * time.Second
	return cfg.FormatDSN()
}

// waitReady pings the server until it answers, it exits, or the wait ends.
// A successful ping is the real proof of readiness; the wording of MariaDB's
// "ready for connections" log line changes between versions.
func (s *Server) waitReady(ctx context.Context, logStart int64) error {
	ctx, cancel := context.WithTimeout(ctx, orDefault(s.ReadyTimeout, defaultReadyTimeout))
	defer cancel()

	db, err := sql.Open("mysql", s.dsn())
	if err != nil {
		return fmt.Errorf("mariadb: %w", err)
	}
	defer db.Close()

	c := s.child
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-c.exited:
			s.child = nil
			return fmt.Errorf("mariadb: %w (%v)\n%s", ErrExitedDuringStartup, c.err, s.logTail(logStart, 15))
		case <-ctx.Done():
			return fmt.Errorf("mariadb: stopped waiting for the server to be ready; it is still running as PID %d: %w", s.PID(), ctx.Err())
		case <-tick.C:
			pingCtx, cancelPing := context.WithTimeout(ctx, time.Second)
			err := db.PingContext(pingCtx)
			cancelPing()
			if err != nil {
				continue
			}
			if err := s.checkDataDir(ctx, db); err != nil {
				return err
			}
			s.progress(fmt.Sprintf("MariaDB is ready (PID %d).", s.PID()))
			return nil
		}
	}
}

// queryer is satisfied by *sql.DB and *sql.Conn.
type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// checkDataDir makes sure the server answering on our port is the one using
// our data folder, not another MariaDB that ended up on the same port.
func (s *Server) checkDataDir(ctx context.Context, q queryer) error {
	var dir string
	if err := q.QueryRowContext(ctx, "SELECT @@datadir").Scan(&dir); err != nil {
		return fmt.Errorf("mariadb: ask the server for its data folder: %w", err)
	}
	if !sameDir(dir, s.DataDir) {
		return fmt.Errorf("mariadb: port %d is answered by a server using %s, not %s", s.Port, dir, s.DataDir)
	}
	return nil
}

func (s *Server) progress(msg string) {
	if s.Progress != nil {
		s.Progress(msg)
	}
}

// sameDir compares two folder paths after resolving symlinks. macOS and
// Windows file systems ignore case, so the comparison does too.
func sameDir(a, b string) bool {
	clean := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			p = r
		}
		return filepath.Clean(p)
	}
	a, b = clean(a), clean(b)
	if runtime.GOOS == "linux" {
		return a == b
	}
	return strings.EqualFold(a, b)
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func orDefault(d, def time.Duration) time.Duration {
	if d <= 0 {
		return def
	}
	return d
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// readLinesFrom returns the lines of a file after byte offset from.
func readLinesFrom(path string, from int64) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return nil
	}
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20) // allow long lines
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}

// logTail returns the last n lines written to the log since offset from.
func (s *Server) logTail(from int64, n int) string {
	lines := readLinesFrom(s.LogFile, from)
	if len(lines) == 0 {
		return ""
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return "last lines of " + s.LogFile + ":\n  " + strings.Join(lines, "\n  ")
}
