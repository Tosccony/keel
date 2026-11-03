package mariadb

import (
	"path/filepath"
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
