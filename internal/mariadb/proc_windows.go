//go:build windows

package mariadb

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
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

// nudge sets the shutdown event mariadbd makes on Windows,
// "MySQLShutdown<PID>", which starts the same clean shutdown as SQL's
// SHUTDOWN but needs no account, so it works after a student sets a root
// password. (Windows has no SIGTERM, and its console-control events would
// also reach Keel.) If the event can't be set, it asks over SQL again.
// It re-checks the process first, in case its number was reused.
func (s *Server) nudge(ctx context.Context, pid int) error {
	if !isServerFor(pid, s.DataDir) {
		return nil // already gone
	}
	if err := setShutdownEvent(pid); err != nil {
		s.progress("Couldn't use MariaDB's shutdown event (" + err.Error() + "); asking over SQL again.")
		return s.askShutdown(ctx, pid)
	}
	return nil
}

func setShutdownEvent(pid int) error {
	name, err := windows.UTF16PtrFromString(fmt.Sprintf("MySQLShutdown%d", pid))
	if err != nil {
		return err
	}
	h, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, name)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.SetEvent(h)
}
