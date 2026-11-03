package mariadb

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v4/process"
)

// isServerFor reports whether pid is a running mariadbd using dataDir.
//
// Keel checks this before trusting a PID from a file and before every signal
// it sends, so a reused process number can never point it at another program.
func isServerFor(pid int, dataDir string) bool {
	if pid <= 0 {
		return false
	}
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return false
	}
	args, err := p.CmdlineSlice()
	if err != nil || len(args) == 0 {
		return false
	}
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(args[0])), ".exe")
	if name != "mariadbd" && name != "mysqld" {
		return false
	}
	for _, a := range args[1:] {
		if dir, ok := strings.CutPrefix(a, "--datadir="); ok && sameDir(dir, dataDir) {
			return true
		}
	}
	return false
}

// serverArgs returns a running process's command line, or nil.
func serverArgs(pid int) []string {
	if pid <= 0 {
		return nil
	}
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return nil
	}
	args, _ := p.CmdlineSlice()
	return args
}

// InRepairMode reports whether the MariaDB running as pid was started by
// Repair, without account checks. That only lasts past a repair if Keel
// was killed during it; stopping MariaDB ends it.
func (s *Server) InRepairMode(pid int) bool {
	for _, a := range serverArgs(pid) {
		for _, r := range repairModeArgs {
			if a == r {
				return true
			}
		}
	}
	return false
}

// serverPort returns the --port a running mariadbd was started with, or 0
// if it can't be read.
func serverPort(pid int) int {
	for _, a := range serverArgs(pid) {
		if v, ok := strings.CutPrefix(a, "--port="); ok {
			if port, err := strconv.Atoi(v); err == nil {
				return port
			}
		}
	}
	return 0
}
