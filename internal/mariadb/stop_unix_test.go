//go:build !windows

package mariadb

import (
	"context"
	"errors"
	"syscall"
	"testing"
	"time"
)

// A root password (XAMPP courses often have students set one) means Keel
// can't log in to ask for SHUTDOWN. On Linux and macOS, Stop falls back
// to SIGTERM, which MariaDB also treats as a clean shutdown.
func TestStopNudgesWhenSQLIsRefused(t *testing.T) {
	s := startTestServer(t)
	exited := s.Exited()
	db := openDB(t, s)
	rows, err := db.Query("SELECT user, host FROM mysql.global_priv WHERE user = 'root'")
	if err != nil {
		t.Fatal(err)
	}
	var accounts []string
	for rows.Next() {
		var user, host string
		rows.Scan(&user, &host)
		accounts = append(accounts, quoteIdent(user)+"@"+quoteIdent(host))
	}
	rows.Close()
	for _, a := range accounts {
		mustExec(t, db, "ALTER USER "+a+" IDENTIFIED BY 'class-password'")
	}
	db.Close()

	res, err := s.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !res.Nudged {
		t.Error("expected Stop to fall back to SIGTERM")
	}
	if !res.Confirmed {
		t.Error(`SIGTERM should still give a clean "Shutdown complete"`)
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Error("the server didn't exit")
	}
}

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
