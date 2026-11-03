// Command keel-lab is a developer tool for trying Keel's server code against
// a local MariaDB install. It isn't shipped in releases.
//
//	go run ./cmd/keel-lab start    # start MariaDB from ~/personal/keel-lab
//	go run ./cmd/keel-lab status   # is it running?
//	go run ./cmd/keel-lab stop     # stop it the clean way
//
// The server keeps running after keel-lab exits, the same way it keeps
// running after Keel's window closes.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Tosccony/keel/internal/lab"
	"github.com/Tosccony/keel/internal/mariadb"
)

func main() {
	opts := lab.Defaults()
	opts.AddFlags(flag.CommandLine)
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: keel-lab [flags] start|stop|status")
		flag.PrintDefaults()
	}
	flag.Parse()

	s := opts.Server()
	s.Progress = func(msg string) { fmt.Println("  " + msg) }

	switch flag.Arg(0) {
	case "start":
		start(s)
	case "stop":
		stop(s)
	case "status":
		if pid := s.RunningPID(); pid != 0 {
			fmt.Printf("Running, PID %d.\n", pid)
		} else {
			fmt.Println("Not running.")
		}
	default:
		flag.Usage()
		os.Exit(2)
	}
}

func start(s *mariadb.Server) {
	began := time.Now()
	if err := s.Start(context.Background()); err != nil {
		fail(err)
	}
	fmt.Printf("Ready in %.1fs. Log: %s\n", time.Since(began).Seconds(), s.LogFile)
}

func stop(s *mariadb.Server) {
	res, err := s.Stop(context.Background())
	switch {
	case errors.Is(err, mariadb.ErrNotRunning):
		fmt.Println("MariaDB isn't running.")
	case errors.Is(err, mariadb.ErrStillRunning):
		fmt.Printf("MariaDB (PID %d) is still running after two requests to shut down.\n", res.PID)
		fmt.Print("Force-stop it? This is the hard kill that corrupts XAMPP databases. [y/N] ")
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if !strings.EqualFold(strings.TrimSpace(answer), "y") {
			fmt.Println("Left it running.")
			return
		}
		if err := s.Kill(); err != nil {
			fail(err)
		}
	case err != nil:
		fail(err)
	default:
		how := "cleanly"
		if res.Nudged {
			how = "cleanly, after a second request"
		}
		fmt.Printf("Stopped %s in %.1fs.", how, res.Took.Seconds())
		if res.Confirmed {
			fmt.Println(` MariaDB's log confirms "Shutdown complete".`)
		} else {
			fmt.Println(` ("Shutdown complete" isn't in the log; it may have been started with a different log file.)`)
		}
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
