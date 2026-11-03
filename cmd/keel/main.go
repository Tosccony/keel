// Command keel opens the Keel control window.
//
// For now it controls the development MariaDB described in internal/lab:
// Homebrew's mariadbd with a data folder in ~/personal/keel-lab on port 3307.
// Closing the window leaves MariaDB running.
package main

import (
	"flag"
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	"github.com/Tosccony/keel/internal/lab"
)

// version is set at build time: go build -ldflags "-X main.version=1.2.3".
var version = "dev"

func main() {
	opts := lab.Defaults()
	opts.AddFlags(flag.CommandLine)
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("keel", version)
		return
	}

	a := app.NewWithID("io.github.tosccony.keel")
	w := a.NewWindow("Keel")
	server := opts.Server()
	u := newUI(w, server)
	w.SetContent(u.content())
	w.Resize(fyne.NewSize(720, 440))

	u.log("Keel", fmt.Sprintf("Keel %s. Watching the MariaDB in %s.", version, server.DataDir))
	go func() {
		v, err := server.Version()
		fyne.Do(func() {
			if err != nil {
				u.log("MariaDB", fmt.Sprintf("Can't find MariaDB in %s.", server.BinDir))
				return
			}
			u.module.SetText("MariaDB " + v)
		})
	}()
	u.checkNow()
	u.watch(2 * time.Second)

	w.ShowAndRun()
}
