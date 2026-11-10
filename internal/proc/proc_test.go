//go:build !windows

package proc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// sleeper is a stand-in server: a shell that sleeps, with a marker in its
// command line.
func sleeper(t *testing.T, marker, trap string) *Process {
	t.Helper()
	dir := t.TempDir()
	return &Process{
		Path:    "/bin/sh",
		Args:    []string{"-c", trap + "sleep 60 & wait", marker},
		LogFile: filepath.Join(dir, "log"),
		PIDFile: filepath.Join(dir, "pid"),
		Marker:  marker,
	}
}

func TestStartFindStop(t *testing.T) {
	p := sleeper(t, "keel-test-"+strconv.Itoa(os.Getpid()), "")
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	pid := p.PID()
	if pid == 0 {
		t.Fatal("no PID after Start")
	}
	// A new Process for the same server, as after Keel restarts.
	again := &Process{PIDFile: p.PIDFile, Marker: p.Marker}
	if got := again.PID(); got != pid {
		t.Errorf("found PID %d through the PID file, want %d", got, pid)
	}
	if err := again.Stop(context.Background(), 5*time.Second); err != nil {
		t.Fatal(err)
	}
	select { // the first Process hears about the exit once its Wait returns
	case <-p.Exited():
	case <-time.After(5 * time.Second):
		t.Fatal("the process didn't exit")
	}
	if p.PID() != 0 || again.PID() != 0 {
		t.Error("still running after Stop")
	}
	if err := again.Stop(context.Background(), time.Second); !errors.Is(err, ErrNotRunning) {
		t.Errorf("second Stop: %v, want ErrNotRunning", err)
	}
}

// A process that ignores SIGTERM is forced after the timeout.
func TestStopForcesAStubbornProcess(t *testing.T) {
	p := sleeper(t, "keel-stubborn-"+strconv.Itoa(os.Getpid()), "trap '' TERM; ")
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond) // let the trap take hold
	began := time.Now()
	if err := p.Stop(context.Background(), 500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(began); took < 500*time.Millisecond {
		t.Errorf("forced after %v, before the timeout", took)
	}
	if p.PID() != 0 {
		t.Error("still running")
	}
}

// A stale PID file pointing at another program is ignored.
func TestStalePIDFileIsIgnored(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o644) // this test binary, not "our" server
	p := &Process{PIDFile: pidFile, Marker: "keel-not-this-one"}
	if pid := p.PID(); pid != 0 {
		t.Errorf("a stale PID file gave PID %d", pid)
	}
	if err := p.Stop(context.Background(), time.Second); !errors.Is(err, ErrNotRunning) {
		t.Errorf("Stop on a stale PID file: %v", err)
	}
}

func TestOutputGoesToTheLog(t *testing.T) {
	dir := t.TempDir()
	p := &Process{Path: "/bin/sh", Args: []string{"-c", "echo hello from the server; echo oops >&2"},
		LogFile: filepath.Join(dir, "log"), PIDFile: filepath.Join(dir, "pid"), Marker: "x"}
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	<-p.Exited()
	b, _ := os.ReadFile(p.LogFile)
	if !strings.Contains(string(b), "hello from the server") || !strings.Contains(string(b), "oops") {
		t.Errorf("log: %q", b)
	}
}
