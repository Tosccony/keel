//go:build !windows

package mariadb

import (
	"context"
	"os/exec"
	"syscall"
)

const binaryName = "mariadbd"

// detach puts the server in its own process group, so signals sent to
// Keel's group (Ctrl+C in a terminal, for example) never reach the database.
// The server also keeps running if Keel exits.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// nudge sends SIGTERM, which MariaDB treats as a normal shutdown request.
// It re-checks the process first, in case its number was reused.
func (s *Server) nudge(_ context.Context, pid int) error {
	if !isServerFor(pid, s.DataDir) {
		return nil // already gone
	}
	return syscall.Kill(pid, syscall.SIGTERM)
}
