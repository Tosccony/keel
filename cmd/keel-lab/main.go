// Command keel-lab is a developer tool for trying Keel's server code against
// a local MariaDB install. It isn't shipped in releases.
//
//	go run ./cmd/keel-lab start
//
// The server keeps running after keel-lab exits, the same way it will keep
// running after Keel's window closes.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/Tosccony/keel/internal/mariadb"
)

func main() {
	home, _ := os.UserHomeDir()
	bin := flag.String("bin", defaultBinDir(), "folder that holds mariadbd")
	lab := flag.String("lab", filepath.Join(home, "personal", "keel-lab"), "lab folder (holds data/, the socket and the log)")
	port := flag.Int("port", 3307, "port to listen on")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: keel-lab [flags] start")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.Arg(0) != "start" {
		flag.Usage()
		os.Exit(2)
	}

	s := &mariadb.Server{
		BinDir:  *bin,
		DataDir: filepath.Join(*lab, "data"),
		Port:    *port,
		Socket:  filepath.Join(*lab, "mysql.sock"),
		LogFile: filepath.Join(*lab, "mariadb.log"),
	}

	fmt.Printf("Starting MariaDB on port %d...\n", *port)
	began := time.Now()
	if err := s.Start(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Printf("ready, PID %d (%.1fs)\n", s.PID(), time.Since(began).Seconds())
	fmt.Printf("log:  %s\n", s.LogFile)
	fmt.Printf("stop: mariadb --no-defaults -h 127.0.0.1 -P %d -u root -e \"SHUTDOWN\"\n", *port)
}

// defaultBinDir finds mariadbd on PATH, following Homebrew's symlinks.
func defaultBinDir() string {
	p, err := exec.LookPath("mariadbd")
	if err != nil {
		return "/opt/homebrew/opt/mariadb/bin"
	}
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	return filepath.Dir(p)
}
