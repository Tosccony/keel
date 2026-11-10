//go:build !windows

package setup

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// documentsDir is ~/Documents, or on Linux the folder the desktop's
// user-dirs.dirs names (it's translated in some languages: ~/Dokumente).
func documentsDir() string {
	if runtime.GOOS == "linux" {
		if dir := xdgDocuments(); dir != "" {
			return dir
		}
	}
	return homeDocuments()
}

func xdgDocuments() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(home, ".config")
	}
	f, err := os.Open(filepath.Join(config, "user-dirs.dirs"))
	if err != nil {
		return ""
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		value, ok := strings.CutPrefix(strings.TrimSpace(s.Text()), "XDG_DOCUMENTS_DIR=")
		if !ok {
			continue
		}
		value = strings.Trim(value, `"`)
		value = strings.Replace(value, "$HOME", home, 1)
		if filepath.IsAbs(value) && value != home {
			return value
		}
	}
	return ""
}
