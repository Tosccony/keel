//go:build windows

package mariadb

import (
	"context"
	"os/exec"
	"syscall"
)

const binaryName = "mariadbd.exe"

// createNoWindow stops Windows from opening a console window for the server.
const createNoWindow = 0x08000000

// detach starts the server in a new process group, so Ctrl+C and Ctrl+Break
// sent to Keel's console never reach the database. The server is not put in
// a Job Object, so it keeps running if Keel exits or crashes.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | createNoWindow,
	}
}

// nudge asks again over SQL. Windows has no SIGTERM, and the console-control
// events it does have would also reach Keel.
func (s *Server) nudge(ctx context.Context, _ int) error {
	return s.askShutdown(ctx)
}
