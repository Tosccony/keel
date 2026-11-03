package mariadb

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Most of these tests start real MariaDB servers. They need mariadbd and
// mariadb-install-db on PATH (or KEEL_MARIADB_BIN set to their folder) and
// are skipped otherwise.

func TestPortInUse(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if !portInUse(port) {
		t.Errorf("portInUse(%d) = false while a listener is open", port)
	}
	l.Close()
	if portInUse(port) {
		t.Errorf("portInUse(%d) = true after the listener closed", port)
	}
}

func TestStartCatchesSetupMistakes(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name string
		s    Server
		want string
	}{
		{"missing data folder", Server{DataDir: filepath.Join(dir, "nope"), Socket: filepath.Join(dir, "s")}, "doesn't exist"},
		{"socket path too long", Server{DataDir: dir, Socket: "/" + strings.Repeat("a", 120)}, "socket path is 121 characters"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.s.Start(context.Background())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want an error containing %q", err, tt.want)
			}
			if tt.s.PID() != 0 {
				t.Error("mariadbd was launched despite the mistake")
			}
		})
	}
}

func TestStartIsReadyAndOurs(t *testing.T) {
	s := startTestServer(t)
	if s.PID() == 0 {
		t.Error("PID() = 0 after a successful start")
	}
	if s.RunningPID() != s.PID() {
		t.Errorf("RunningPID() = %d, want %d", s.RunningPID(), s.PID())
	}
	var port int
	if err := openDB(t, s).QueryRow("SELECT @@port").Scan(&port); err != nil {
		t.Fatal(err)
	}
	if port != s.Port {
		t.Errorf("server reports port %d, want %d", port, s.Port)
	}
}

func TestStartRefusesBusyPort(t *testing.T) {
	s := newTestServer(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	s.Port = l.Addr().(*net.TCPAddr).Port

	err = s.Start(context.Background())
	if !errors.Is(err, ErrPortInUse) {
		t.Fatalf("Start on a busy port: got %v, want ErrPortInUse", err)
	}
	if s.PID() != 0 {
		t.Error("mariadbd was launched even though the port was busy")
	}
}

func TestStartReportsEarlyExit(t *testing.T) {
	// An empty data folder passes validation, but mariadbd aborts because
	// its system tables are missing.
	s := newTestServer(t)
	s.DataDir = t.TempDir()

	err := s.Start(context.Background())
	if err == nil {
		stopForCleanup(t, s)
		t.Fatal("Start succeeded with an empty data folder")
	}
	if !strings.Contains(err.Error(), "exited during startup") || !strings.Contains(err.Error(), "last lines of") {
		t.Errorf("error should say the server exited and quote its log, got:\n%v", err)
	}
}

func TestCancelledStartLeavesServerRunning(t *testing.T) {
	s := newTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()

	err := s.Start(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Start with an expired context: got %v, want DeadlineExceeded", err)
	}
	t.Cleanup(func() { stopForCleanup(t, s) })

	select {
	case <-s.Exited():
		t.Fatal("the server exited after Start gave up waiting; it must keep running")
	case <-time.After(2 * time.Second):
	}
	// It should still finish starting on its own.
	db := openDB(t, s)
	deadline := time.Now().Add(30 * time.Second)
	for db.Ping() != nil {
		if time.Now().After(deadline) {
			t.Fatal("server never became ready after the cancelled start")
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func TestStopIsClean(t *testing.T) {
	s := startTestServer(t)
	exited := s.Exited()
	var msgs []string
	s.Progress = func(m string) { msgs = append(msgs, m) }

	res, err := s.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !res.Confirmed {
		t.Error(`Stop didn't find "Shutdown complete" in the log`)
	}
	if res.Nudged {
		t.Error("a normal stop shouldn't need a second request")
	}
	select {
	case <-exited:
	default:
		t.Error("the process is still running after Stop returned")
	}
	if pid := s.RunningPID(); pid != 0 {
		t.Errorf("RunningPID() = %d after Stop, want 0", pid)
	}
	if len(msgs) == 0 {
		t.Error("Stop sent no progress messages")
	}
	if err := s.Start(context.Background()); err != nil {
		t.Errorf("Start after Stop: %v", err)
	}
}

func TestStopFindsServerStartedElsewhere(t *testing.T) {
	s := startTestServer(t)
	exited := s.Exited()

	// A second Server with the same settings, like a new keel-lab run.
	other := &Server{BinDir: s.BinDir, DataDir: s.DataDir, Port: s.Port, Socket: s.Socket, LogFile: s.LogFile}
	if got := other.RunningPID(); got != s.PID() {
		t.Fatalf("RunningPID() through the PID file = %d, want %d", got, s.PID())
	}
	res, err := other.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !res.Confirmed {
		t.Error(`Stop didn't find "Shutdown complete" in the log`)
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Error("the server didn't exit")
	}
}

func TestStopWhenNotRunning(t *testing.T) {
	s := newTestServer(t)
	if _, err := s.Stop(context.Background()); !errors.Is(err, ErrNotRunning) {
		t.Errorf("Stop with no server: got %v, want ErrNotRunning", err)
	}
}

func TestStalePIDFileIsIgnored(t *testing.T) {
	// After a crash the PID file stays behind, and its number can be reused.
	// Point it at a live process that isn't MariaDB: this test itself.
	s := newTestServer(t)
	if err := os.WriteFile(s.pidFile(), []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}
	if pid := s.RunningPID(); pid != 0 {
		t.Fatalf("RunningPID() trusted a stale PID file: %d", pid)
	}
	if _, err := s.Stop(context.Background()); !errors.Is(err, ErrNotRunning) {
		t.Errorf("Stop: got %v, want ErrNotRunning", err)
	}
	if err := s.Kill(); !errors.Is(err, ErrNotRunning) {
		t.Errorf("Kill: got %v, want ErrNotRunning", err) // if it had killed, we wouldn't be here
	}
}

func TestKillThenStartRecovers(t *testing.T) {
	s := startTestServer(t)
	if _, err := openDB(t, s).Exec("CREATE DATABASE lab"); err != nil {
		t.Fatal(err)
	}
	if err := s.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if pid := s.RunningPID(); pid != 0 {
		t.Fatalf("RunningPID() = %d after Kill, want 0", pid)
	}
	logStart := fileSize(s.LogFile)

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start after Kill: %v", err)
	}
	if !strings.Contains(strings.Join(readLinesFrom(s.LogFile, logStart), "\n"), "crash") {
		t.Error("expected crash-recovery lines in the log after a kill")
	}
	var name string
	if err := openDB(t, s).QueryRow("SHOW DATABASES LIKE 'lab'").Scan(&name); err != nil {
		t.Errorf("database created before the kill is gone: %v", err)
	}
}

// startTestServer starts a fresh server and stops it when the test ends.
func startTestServer(t *testing.T) *Server {
	t.Helper()
	s := newTestServer(t)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { stopForCleanup(t, s) })
	return s
}

// newTestServer creates a fresh data folder and returns an unstarted Server.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	bin := binDir(t)
	dir := t.TempDir()
	data := filepath.Join(dir, "data")

	install := exec.Command(filepath.Join(bin, "mariadb-install-db"),
		"--no-defaults",
		"--basedir="+filepath.Dir(bin),
		"--datadir="+data,
		"--auth-root-authentication-method=normal",
		"--skip-test-db",
	)
	if out, err := install.CombinedOutput(); err != nil {
		t.Fatalf("mariadb-install-db: %v\n%s", err, out)
	}

	socket := filepath.Join(dir, "s")
	if len(socket) > maxSocketPath-3 {
		short, err := os.MkdirTemp("/tmp", "keel-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(short) })
		socket = filepath.Join(short, "s")
	}

	return &Server{
		BinDir:  bin,
		DataDir: data,
		Port:    freePort(t),
		Socket:  socket,
		LogFile: filepath.Join(dir, "error.log"),
	}
}

// stopForCleanup stops a test server. Tests must not leave servers behind,
// so as a last resort it kills; Keel itself never does that without asking.
func stopForCleanup(t *testing.T, s *Server) {
	t.Helper()
	s.StopTimeout, s.NudgeTimeout = 0, 0
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	_, err := s.Stop(ctx)
	if err == nil || errors.Is(err, ErrNotRunning) {
		return
	}
	s.Kill()
	t.Errorf("test server didn't stop cleanly (%v) and was killed", err)
}

func binDir(t *testing.T) string {
	t.Helper()
	if dir := os.Getenv("KEEL_MARIADB_BIN"); dir != "" {
		return dir
	}
	p, err := exec.LookPath(binaryName)
	if err != nil {
		t.Skip("mariadbd not found; install MariaDB or set KEEL_MARIADB_BIN")
	}
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	return filepath.Dir(p)
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func openDB(t *testing.T, s *Server) *sql.DB {
	t.Helper()
	db, err := sql.Open("mysql", s.dsn())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
