// Package lab is the development setup that keel and keel-lab share until
// Keel ships its own MariaDB: the mariadbd on this computer (Homebrew's,
// for example), a data folder in ~/personal/keel-lab, and port 3307.
package lab

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/Tosccony/keel/internal/mariadb"
)

// Options says which MariaDB to run and where its files live.
type Options struct {
	BinDir string // folder that holds mariadbd
	Folder string // holds data/, the socket and the log
	Port   int
}

// Defaults returns the standard lab setup.
func Defaults() Options {
	home, _ := os.UserHomeDir()
	return Options{
		BinDir: defaultBinDir(),
		Folder: filepath.Join(home, "personal", "keel-lab"),
		Port:   3307,
	}
}

// AddFlags lets -bin, -lab and -port override the defaults.
func (o *Options) AddFlags(fs *flag.FlagSet) {
	fs.StringVar(&o.BinDir, "bin", o.BinDir, "folder that holds mariadbd")
	fs.StringVar(&o.Folder, "lab", o.Folder, "lab folder (holds data/, the socket and the log)")
	fs.IntVar(&o.Port, "port", o.Port, "MariaDB port")
}

// Server returns the MariaDB server these options describe.
func (o Options) Server() *mariadb.Server {
	return &mariadb.Server{
		BinDir:  o.BinDir,
		DataDir: filepath.Join(o.Folder, "data"),
		Port:    o.Port,
		Socket:  filepath.Join(o.Folder, "mysql.sock"),
		LogFile: filepath.Join(o.Folder, "mariadb.log"),
	}
}

// defaultBinDir finds mariadbd. Apps opened from the Finder don't get the
// shell's PATH, so it also checks Homebrew's usual folders.
func defaultBinDir() string {
	if p, err := exec.LookPath("mariadbd"); err == nil {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			p = real
		}
		return filepath.Dir(p)
	}
	for _, dir := range []string{"/opt/homebrew/opt/mariadb/bin", "/usr/local/opt/mariadb/bin"} {
		if _, err := os.Stat(filepath.Join(dir, "mariadbd")); err == nil {
			return dir
		}
	}
	return "/opt/homebrew/opt/mariadb/bin"
}
