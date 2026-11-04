// Package setup decides which MariaDB Keel runs and where its files live.
//
// An installed Keel carries its own MariaDB (see Bundled) and keeps its
// files in the user's app data folder, with MariaDB on the standard port
// 3306. A development build has no MariaDB of its own and uses the lab:
// the mariadbd on this computer (Homebrew's, for example), a folder in
// ~/personal/keel-lab, and port 3307.
//
// Either way, a port Keel had to move to (see ChoosePort) is remembered in
// keel-settings.json in the folder.
package setup

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/Tosccony/keel/internal/mariadb"
)

// Ports MariaDB starts from. ChoosePort moves on from them if they're taken.
const (
	DefaultPort = 3306 // what PHP code and phpMyAdmin expect
	LabPort     = 3307 // leaves 3306 for another MariaDB on a developer's machine
)

// Options says which MariaDB to run and where its files live.
type Options struct {
	BinDir string // folder that holds mariadbd
	Folder string // holds data/, backups/, the socket and the log
	Port   int    // preferred port; a port saved by ChoosePort wins unless PortSet

	// PortSet means the port was asked for explicitly (-port), so it
	// overrides the one ChoosePort saved.
	PortSet bool

	// Bundled means BinDir is the MariaDB that came with Keel.
	Bundled bool
}

// Defaults returns the installed setup if this Keel carries its own
// MariaDB, and the lab otherwise.
func Defaults() Options {
	if bin := Bundled(); bin != "" {
		return Options{
			BinDir:  bin,
			Folder:  filepath.Join(appDataDir(), appFolderName(), "mariadb"),
			Port:    DefaultPort,
			Bundled: true,
		}
	}
	return Lab()
}

// Lab returns the development setup.
func Lab() Options {
	home, _ := os.UserHomeDir()
	return Options{
		BinDir: labBinDir(),
		Folder: filepath.Join(home, "personal", "keel-lab"),
		Port:   LabPort,
	}
}

// AddFlags lets -bin, -lab and -port override the defaults.
func (o *Options) AddFlags(fs *flag.FlagSet) {
	fs.StringVar(&o.BinDir, "bin", o.BinDir, "folder that holds mariadbd")
	fs.StringVar(&o.Folder, "lab", o.Folder, "folder for MariaDB's files (holds data/, backups/, the socket and the log)")
	fs.Func("port", fmt.Sprintf("MariaDB port (default %d, or the port Keel last moved it to)", o.Port), func(v string) error {
		p, err := strconv.Atoi(v)
		if err != nil || p < 1 || p > 65535 {
			return fmt.Errorf("not a port: %q", v)
		}
		o.Port, o.PortSet = p, true
		return nil
	})
}

// Server returns the MariaDB server these options describe.
func (o Options) Server() *mariadb.Server {
	port := o.Port
	if st, err := loadSettings(o.settingsFile()); err == nil && st.MariaDBPort != 0 && !o.PortSet {
		port = st.MariaDBPort
	}
	return &mariadb.Server{
		BinDir:  o.BinDir,
		DataDir: filepath.Join(o.Folder, "data"),
		Port:    port,
		Socket:  filepath.Join(o.Folder, "mysql.sock"),
		LogFile: filepath.Join(o.Folder, "mariadb.log"),
	}
}

// Prepare gets MariaDB ready to start: it creates the data folder on the
// first run, then makes sure the port is free (see ChoosePort). The note
// is for the user; it's empty when nothing needed saying.
func (o Options) Prepare(ctx context.Context, s *mariadb.Server) (note string, err error) {
	if err := s.Init(ctx); err != nil {
		return "", err
	}
	return o.ChoosePort(ctx, s)
}

// Bundled returns the bin folder of the MariaDB that came with Keel, or ""
// for a development build that has none.
func Bundled() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return findBundled(filepath.Dir(exe))
}

// findBundled looks where each installer puts MariaDB: beside keel(.exe)
// (Windows, and the Linux .tar.gz), in Keel.app/Contents/Resources
// (macOS), and in /usr/lib/keel (the Linux .deb and .rpm).
func findBundled(exeDir string) string {
	for _, dir := range []string{
		filepath.Join(exeDir, "mariadb"),
		filepath.Join(exeDir, "..", "Resources", "mariadb"),
		filepath.Join(exeDir, "..", "lib", "keel", "mariadb"),
	} {
		bin := filepath.Join(dir, "bin")
		if _, err := os.Stat(filepath.Join(bin, exe("mariadbd"))); err == nil {
			return filepath.Clean(bin)
		}
	}
	return ""
}

// appDataDir is where per-user app data goes: ~/Library/Application
// Support on macOS, $XDG_DATA_HOME (or ~/.local/share) on Linux, and
// %LOCALAPPDATA% on Windows. Never the roaming AppData folder: college
// networks copy it to a server at sign-out, and a database must not be
// copied while it's in use.
func appDataDir() string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support")
	case "windows":
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return d
		}
		return filepath.Join(home, "AppData", "Local")
	default:
		if d := os.Getenv("XDG_DATA_HOME"); d != "" {
			return d
		}
		return filepath.Join(home, ".local", "share")
	}
}

// appFolderName follows each system's convention: "keel" on Linux, "Keel"
// elsewhere.
func appFolderName() string {
	if runtime.GOOS == "linux" {
		return "keel"
	}
	return "Keel"
}

// labBinDir finds mariadbd. Apps opened from the Finder don't get the
// shell's PATH, so it also checks Homebrew's usual folders.
func labBinDir() string {
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

// exe adds ".exe" to a program name on Windows.
func exe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}
