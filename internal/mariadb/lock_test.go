package mariadb

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLockIsExclusive(t *testing.T) {
	s := &Server{DataDir: filepath.Join(t.TempDir(), "data")}
	unlock, err := s.lock()
	if err != nil {
		t.Fatal(err)
	}
	// A second Server on the same folder stands in for a second window.
	other := &Server{DataDir: s.DataDir}
	if _, err := other.lock(); !errors.Is(err, ErrBusy) {
		t.Fatalf("second lock: err = %v, want ErrBusy", err)
	}
	unlock()
	unlock2, err := other.lock()
	if err != nil {
		t.Fatalf("lock after unlock: %v", err)
	}
	unlock2()
	if _, err := os.Stat(s.lockFile()); err != nil {
		t.Errorf("the lock file should stay in place: %v", err)
	}
}

func TestLockFailsWhenFolderIsMissing(t *testing.T) {
	s := &Server{DataDir: filepath.Join(t.TempDir(), "missing", "data")}
	if _, err := s.lock(); err == nil || errors.Is(err, ErrBusy) {
		t.Errorf("err = %v, want an error that isn't ErrBusy", err)
	}
}

// TestLockAcrossProcesses holds the lock in a second process, the way a
// second Keel window would, then kills that process the way a crash would.
// The lock must be released by the OS, never left stale.
func TestLockAcrossProcesses(t *testing.T) {
	s := &Server{DataDir: filepath.Join(t.TempDir(), "data")}

	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperHoldLock$")
	cmd.Env = append(os.Environ(), "KEEL_HOLD_LOCK="+s.lockFile())
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	defer stdin.Close()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "locked\n" {
		t.Fatalf("helper said %q (%v), want \"locked\"", line, err)
	}

	if _, err := s.lock(); !errors.Is(err, ErrBusy) {
		t.Fatalf("lock while another process holds it: err = %v, want ErrBusy", err)
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	unlock, err := s.lock()
	if err != nil {
		t.Fatalf("lock after the holder was killed: %v", err)
	}
	unlock()
}

// TestHelperHoldLock isn't a test: TestLockAcrossProcesses runs it in a
// separate process to hold a lock until it's killed.
func TestHelperHoldLock(t *testing.T) {
	path := os.Getenv("KEEL_HOLD_LOCK")
	if path == "" {
		t.Skip("helper for TestLockAcrossProcesses")
	}
	unlock, err := (&Server{LockFile: path}).lock()
	if err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
	defer unlock()
	fmt.Println("locked")
	io.Copy(io.Discard, os.Stdin) // hold until killed or stdin closes
}

// While another Keel holds the lock, Start, Stop, Kill and Init must all
// refuse without touching anything. No MariaDB is needed for this.
func TestBusyFolderRefusesEverything(t *testing.T) {
	dir := t.TempDir()
	s := &Server{
		DataDir: filepath.Join(dir, "data"),
		Port:    freePort(t),
		Socket:  filepath.Join(dir, "s.sock"),
		LogFile: filepath.Join(dir, "log"),
	}
	if err := os.Mkdir(s.DataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	unlock, err := (&Server{DataDir: s.DataDir}).lock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	ctx := context.Background()
	if err := s.Start(ctx); !errors.Is(err, ErrBusy) {
		t.Errorf("Start: err = %v, want ErrBusy", err)
	}
	if err := s.StartWithoutSnapshot(ctx); !errors.Is(err, ErrBusy) {
		t.Errorf("StartWithoutSnapshot: err = %v, want ErrBusy", err)
	}
	if _, err := s.Stop(ctx); !errors.Is(err, ErrBusy) {
		t.Errorf("Stop: err = %v, want ErrBusy", err)
	}
	if err := s.Kill(); !errors.Is(err, ErrBusy) {
		t.Errorf("Kill: err = %v, want ErrBusy", err)
	}
	if _, err := os.Stat(s.journalFile()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused Start wrote the journal (stat err = %v)", err)
	}

	fresh := &Server{DataDir: filepath.Join(dir, "data2"), LockFile: s.lockFile()}
	if err := fresh.Init(ctx); !errors.Is(err, ErrBusy) {
		t.Errorf("Init: err = %v, want ErrBusy", err)
	}
	if _, err := os.Stat(fresh.DataDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused Init created the data folder (stat err = %v)", err)
	}
}

// TestTwoStartsAtOnce presses Start in two "windows" at the same moment.
// Exactly one starts MariaDB; the other is told Keel is busy, or that
// MariaDB is already running if it came second.
func TestTwoStartsAtOnce(t *testing.T) {
	a := newTestServer(t)
	b := sameServer(a)
	t.Cleanup(func() { stopForCleanup(t, a) })

	errs := make(chan error, 2)
	for _, s := range []*Server{a, b} {
		go func() { errs <- s.Start(context.Background()) }()
	}
	var started, refused int
	for range 2 {
		select {
		case err := <-errs:
			switch {
			case err == nil:
				started++
			case errors.Is(err, ErrBusy), errors.Is(err, ErrAlreadyRunning):
				refused++
			default:
				t.Errorf("unexpected error: %v", err)
			}
		case <-time.After(time.Minute):
			t.Fatal("a Start never returned")
		}
	}
	if started != 1 || refused != 1 {
		t.Errorf("started %d, refused %d; want exactly one of each", started, refused)
	}
}
