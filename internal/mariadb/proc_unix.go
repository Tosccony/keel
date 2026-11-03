//go:build !windows

package mariadb

import (
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
