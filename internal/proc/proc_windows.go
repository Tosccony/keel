package proc

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

const createNoWindow = 0x08000000

// detach gives the process a group of its own and no console window.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | createNoWindow}
}

// ask has no gentle form on Windows for a process without a console, so
// it's the same as force. That's safe for Apache and PHP, which keep no
// data; MariaDB never comes through here.
func ask(pid int) error { return force(pid) }

// force ends the process and its children: Apache on Windows is a parent
// and a worker, and killing only the parent can leave the worker holding
// the port.
func force(pid int) error {
	cmd := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	if out, err := cmd.CombinedOutput(); err != nil {
		if p, ferr := os.FindProcess(pid); ferr == nil && p.Kill() == nil {
			return nil
		}
		return fmt.Errorf("taskkill: %v: %s", err, out)
	}
	return nil
}
