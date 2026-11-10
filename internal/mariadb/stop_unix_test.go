//go:build !windows

package mariadb

import (
	"context"
	"errors"
	"syscall"
	"testing"
	"time"
)

func TestStopNeverKills(t *testing.T) {
	s := startTestServer(t)
	pid := s.PID()

	// Freeze the server so it can't respond to anything.
	if err := syscall.Kill(pid, syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { syscall.Kill(pid, syscall.SIGCONT) }) // runs before the stop cleanup

	s.StopTimeout, s.NudgeTimeout = time.Second, time.Second
	_, err := s.Stop(context.Background())
	if !errors.Is(err, ErrStillRunning) {
		t.Fatalf("Stop on a frozen server: got %v, want ErrStillRunning", err)
	}
	if got := s.RunningPID(); got != pid {
		t.Errorf("RunningPID() = %d, want %d: Stop must leave the server alone", got, pid)
	}
	select {
	case <-s.Exited():
		t.Error("the server exited; Stop must never kill it")
	default:
	}
}
