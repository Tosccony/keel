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
	"sync"
	"testing"
	"time"
)

// Most of these tests start real MariaDB servers. They need mariadbd and
// mariadb-install-db on PATH (or KEEL_MARIADB_BIN set to their folder) and
// are skipped otherwise.

func TestMain(m *testing.M) {
	code := m.Run()
	if template.dir != "" {
		os.RemoveAll(template.dir)
	}
	os.Exit(code)
}

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
	if s.LastRecovery() != nil {
		t.Error("a clean start ran recovery")
	}
	if dirExists(s.backupDir()) {
		t.Error("a clean start created a backups folder")
	}
	if j, _ := readJournal(s.journalFile()); j.State != stateRunning || j.PID != s.PID() {
		t.Errorf("journal = %+v, want running with PID %d", j, s.PID())
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
	if j, _ := readJournal(s.journalFile()); j.State != stateStopped {
		t.Errorf("a refused start changed the journal to %q", j.State)
	}
}

func TestStartRefusesWhenAlreadyRunning(t *testing.T) {
	s := startTestServer(t)
	other := sameServer(s)
	other.Port = freePort(t) // even on a different port, it's the same data folder

	err := other.Start(context.Background())
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second Start: got %v, want ErrAlreadyRunning", err)
	}
	if other.PID() != 0 {
		t.Error("a second mariadbd was launched on the same data folder")
	}
	if j, _ := readJournal(s.journalFile()); j.PID != s.PID() {
		t.Errorf("journal PID = %d, want %d", j.PID, s.PID())
	}
}

func TestStartReportsEarlyExit(t *testing.T) {
	// An empty data folder passes validation, but mariadbd aborts because
	// its system tables are missing.
	s := newTestServer(t)
	if err := os.RemoveAll(s.DataDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(s.DataDir, 0o755); err != nil {
		t.Fatal(err)
	}

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
	if clean, why := s.LastShutdown(); !clean {
		t.Errorf("LastShutdown after a clean stop: unclean (%s)", why)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start after Stop: %v", err)
	}
	if s.LastRecovery() != nil {
		t.Error("a start after a clean stop ran recovery")
	}
}

func TestStopFindsServerStartedElsewhere(t *testing.T) {
	s := startTestServer(t)
	exited := s.Exited()

	// A second Server with the same settings, like a new keel-lab run.
	other := sameServer(s)
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
	if clean, _ := s.LastShutdown(); !clean {
		t.Error("the first Server doesn't see the clean stop the second one recorded")
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

// --- helpers ---

// template is a data folder made once with Init; each test gets a copy,
// which is much faster than running mariadb-install-db every time.
var template struct {
	once sync.Once
	dir  string // holds data/
	err  error
}

// newTestServer returns an unstarted Server with a fresh data folder and a
// journal that says it stopped cleanly.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	bin := binDir(t)
	template.once.Do(func() {
		template.dir, template.err = os.MkdirTemp("", "keel-template-")
		if template.err == nil {
			tpl := &Server{BinDir: bin, DataDir: filepath.Join(template.dir, "data")}
			template.err = tpl.Init(context.Background())
		}
	})
	if template.err != nil {
		t.Fatalf("make the template data folder: %v", template.err)
	}

	dir := t.TempDir()
	s := &Server{
		BinDir:  bin,
		DataDir: filepath.Join(dir, "data"),
		Port:    freePort(t),
		Socket:  shortSocketPath(t, dir),
		LogFile: filepath.Join(dir, "error.log"),
	}
	if err := copyTree(filepath.Join(template.dir, "data"), s.DataDir); err != nil {
		t.Fatal(err)
	}
	if err := writeJournal(s.journalFile(), journal{State: stateStopped}); err != nil {
		t.Fatal(err)
	}
	return s
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

// sameServer returns a new Server with the same settings, like a second
// Keel window or a keel-lab run.
func sameServer(s *Server) *Server {
	return &Server{BinDir: s.BinDir, DataDir: s.DataDir, Port: s.Port, Socket: s.Socket, LogFile: s.LogFile}
}

// shortSocketPath keeps socket paths under macOS's limit even when the
// temporary folder's path is long.
func shortSocketPath(t *testing.T, dir string) string {
	t.Helper()
	socket := filepath.Join(dir, "s")
	if len(socket) <= maxSocketPath-3 {
		return socket
	}
	short, err := os.MkdirTemp("/tmp", "keel-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(short) })
	return filepath.Join(short, "s")
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

// killOutside force-stops the server the way something outside Keel would
// (kill -9, Task Manager), without telling the Server or the journal.
func killOutside(t *testing.T, s *Server) {
	t.Helper()
	pid := s.RunningPID()
	if pid == 0 {
		t.Fatal("killOutside: server isn't running")
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Kill(); err != nil {
		t.Fatal(err)
	}
	if ch := s.Exited(); ch != nil {
		<-ch
	} else {
		for isServerFor(pid, s.DataDir) {
			time.Sleep(50 * time.Millisecond)
		}
	}
}
