//go:build !windows

package proc

import (
	"os/exec"
	"syscall"
)

// detach puts the process in a group of its own, so a Ctrl+C or a signal
// to Keel's group doesn't reach it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// ask sends SIGTERM: Apache and Tomcat stop at once, PHP-FPM finishes its
// requests first.
func ask(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }

// force kills the whole process group: Apache's and PHP-FPM's children
// with their parent.
func force(pid int) error {
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		return syscall.Kill(pid, syscall.SIGKILL)
	}
	return nil
}
