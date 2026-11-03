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
)

// ErrPortInUse means another program already accepts connections on the
// server's port.
var ErrPortInUse = errors.New("port already in use")

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

	ReadyTimeout time.Duration // how long Start waits for connections; default 30s
	StopTimeout  time.Duration // how long Stop waits after asking; default 2 minutes
	NudgeTimeout time.Duration // how long Stop waits after asking again; default 1 minute

	// Progress, if set, receives plain-language updates during Start and
	// Stop, ready for the window's activity log.
	Progress func(msg string)

	child *child // the mariadbd this Server launched, while it runs
}

// child is a mariadbd process this Server launched.
type child struct {
	cmd    *exec.Cmd
	exited chan struct{} // closed when the process exits
	err    error         // exit status; read only after exited is closed
}

// Start launches mariadbd and returns once the server accepts connections.
//
// If ctx is cancelled or the wait times out, Start returns an error but leaves
// the server running: a slow start, such as crash recovery on a big database,
// is still a healthy start, and killing it would cause the damage Keel exists
// to prevent.
func (s *Server) Start(ctx context.Context) error {
	if s.child != nil {
		return errors.New("mariadb: server already started")
	}
	if err := s.validate(); err != nil {
		return err
	}
	if portInUse(s.Port) {
		return fmt.Errorf("mariadb: port %d: %w", s.Port, ErrPortInUse)
	}

	logf, err := os.OpenFile(s.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("mariadb: open log: %w", err)
	}
	defer logf.Close()              // the server keeps its own copy of the file handle
	logStart := fileSize(s.LogFile) // so errors only show lines from this start

	// exec.Command, not exec.CommandContext: CommandContext kills the process
	// when the context ends.
	cmd := exec.Command(filepath.Join(s.BinDir, binaryName), s.args()...)
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
func (s *Server) dsn() string {
	cfg := mysql.NewConfig()
	cfg.User = "root"
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort("127.0.0.1", strconv.Itoa(s.Port))
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
			return fmt.Errorf("mariadb: server exited during startup (%v)\n%s", c.err, s.logTail(logStart, 15))
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
