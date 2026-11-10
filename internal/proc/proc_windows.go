package proc

import (
	"os"
	"os/exec"
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

func force(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
