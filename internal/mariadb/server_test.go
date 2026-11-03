package mariadb

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests start real MariaDB servers. They need mariadbd and
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
	s := newTestServer(t)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { shutdown(t, s) })

	if s.PID() == 0 {
		t.Error("PID() = 0 after a successful start")
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
		shutdown(t, s)
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
	t.Cleanup(func() { shutdown(t, s) })

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
	if len(socket) > 100 { // macOS socket paths max out around 104 characters
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

// shutdown stops a test server with SQL SHUTDOWN and waits for it to exit.
// Stop() will replace this in the next step.
func shutdown(t *testing.T, s *Server) {
	t.Helper()
	if s.Exited() == nil {
		return
	}
	db, err := sql.Open("mysql", s.dsn())
	if err == nil {
		db.Exec("SHUTDOWN")
		db.Close()
	}
	select {
	case <-s.Exited():
	case <-time.After(60 * time.Second):
		// Tests must not leave servers behind, so they may kill as a last
		// resort. Keel itself never does this without asking.
		s.cmd.Process.Kill()
		t.Error("test server didn't shut down within 60s and was killed")
	}
}
