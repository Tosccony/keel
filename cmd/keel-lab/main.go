// Command keel-lab is a developer tool for trying Keel's server code against
// a local MariaDB install. It isn't shipped in releases.
//
//	go run ./cmd/keel-lab init     # create the data folder in ~/personal/keel-lab
//	go run ./cmd/keel-lab start    # start MariaDB (recovering first if needed)
//	go run ./cmd/keel-lab status   # is it running, and did it last stop cleanly?
//	go run ./cmd/keel-lab check    # run CHECK TABLE on every table
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
		fmt.Fprintln(os.Stderr, "usage: keel-lab [flags] init|start|status|check|stop")
		flag.PrintDefaults()
	}
	flag.Parse()

	s := opts.Server()
	s.Progress = func(msg string) { fmt.Println("  " + msg) }
	ctx := context.Background()

	switch flag.Arg(0) {
	case "init":
		if err := s.Init(ctx); err != nil {
			fail(err)
		}
		fmt.Println("Data folder:", s.DataDir)
	case "start":
		start(ctx, s)
	case "stop":
		stop(ctx, s)
	case "status":
		if pid := s.RunningPID(); pid != 0 {
			fmt.Printf("Running, PID %d.\n", pid)
		} else if clean, why := s.LastShutdown(); clean {
			fmt.Println("Not running. Last shutdown: clean.")
		} else {
			fmt.Println("Not running. Last shutdown: not clean.", why)
			fmt.Println("The next start will copy the data folder and check every table.")
		}
	case "check":
		checked, problems, err := s.CheckTables(ctx)
		if err != nil {
			fail(err)
		}
		if len(problems) == 0 {
			fmt.Printf("All %d tables passed.\n", checked)
			return
		}
		fmt.Printf("%d of %d tables have problems:\n", len(problems), checked)
		for _, p := range problems {
			fmt.Printf("  %s: %s\n", p.Table, p.Message)
		}
		os.Exit(1)
	default:
		flag.Usage()
		os.Exit(2)
	}
}

func start(ctx context.Context, s *mariadb.Server) {
	began := time.Now()
	err := s.Start(ctx)
	switch {
	case errors.Is(err, mariadb.ErrAlreadyRunning):
		fmt.Printf("MariaDB is already running (PID %d).\n", s.RunningPID())
		return
	case errors.Is(err, mariadb.ErrSnapshotFailed):
		fmt.Println(err)
		if !ask("Start anyway, with no copy to go back to if recovery goes wrong?") {
			fmt.Println("Didn't start.")
			return
		}
		began = time.Now()
		err = s.StartWithoutSnapshot(ctx)
	}
	if err != nil {
		fail(err)
	}
	fmt.Printf("Ready in %.1fs. Log: %s\n", time.Since(began).Seconds(), s.LogFile)
}

func stop(ctx context.Context, s *mariadb.Server) {
	res, err := s.Stop(ctx)
	switch {
	case errors.Is(err, mariadb.ErrNotRunning):
		fmt.Println("MariaDB isn't running.")
	case errors.Is(err, mariadb.ErrStillRunning):
		fmt.Printf("MariaDB (PID %d) is still running after two requests to shut down.\n", res.PID)
		if !ask("Force-stop it? This is the hard kill that corrupts XAMPP databases.") {
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
			fmt.Println(` ("Shutdown complete" isn't in the log, so the next start will check the tables.)`)
		}
	}
}

// ask asks a yes/no question; anything but "y" means no.
func ask(question string) bool {
	fmt.Print(question + " [y/N] ")
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.EqualFold(strings.TrimSpace(answer), "y")
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
