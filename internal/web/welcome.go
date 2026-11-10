package web

import (
	_ "embed"
	"os"
	"path/filepath"
	"strings"
)

//go:embed welcome.php
var welcomePage []byte

// WriteWelcome puts Keel's welcome page, index.php, in an empty websites
// folder, the way XAMPP has a dashboard at localhost. It reports whether
// it wrote one. A folder with anything in it is left alone.
func WriteWelcome(sites string) (bool, error) {
	entries, err := os.ReadDir(sites)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") { // .DS_Store and the like don't count
			return false, nil
		}
	}
	return true, os.WriteFile(filepath.Join(sites, "index.php"), welcomePage, 0o644)
}

// WriteUserFiles creates the files the user owns (custom.conf and
// php.d/custom.ini) if they're missing, so they can be opened before the
// first start.
func (s *Server) WriteUserFiles() error {
	if err := os.MkdirAll(filepath.Join(s.conf(), "php.d"), 0o755); err != nil {
		return err
	}
	for path, body := range map[string]string{s.CustomConf(): customConf, s.CustomIni(): customIni} {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}
