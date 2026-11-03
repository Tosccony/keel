package mariadb

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Version returns the MariaDB version in BinDir, such as "13.0.2".
func (s *Server) Version() (string, error) {
	out, err := exec.Command(filepath.Join(s.BinDir, binaryName), "--version").Output()
	if err != nil {
		return "", fmt.Errorf("mariadb: run %s --version: %w", binaryName, err)
	}
	return parseVersion(string(out))
}

// parseVersion reads output like
// "mariadbd  Ver 13.0.2-MariaDB for osx10.23 on arm64 (Homebrew)".
func parseVersion(out string) (string, error) {
	fields := strings.Fields(out)
	for i, f := range fields {
		if f == "Ver" && i+1 < len(fields) {
			v, _, _ := strings.Cut(fields[i+1], "-")
			return v, nil
		}
	}
	return "", fmt.Errorf("mariadb: can't find a version in %q", strings.TrimSpace(out))
}
