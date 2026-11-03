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
	a.Settings().SetTheme(keelTheme{})
	a.SetIcon(appIcon)
	w := a.NewWindow("Keel")
	server := opts.Server()
	u := newUI(w, server)
	u.choosePort = opts.ChoosePort
	w.SetContent(u.content())
	w.Resize(fyne.NewSize(780, 540))

	u.log(info, "Watching the MariaDB in "+server.DataDir+".")
	go func() {
		v, err := server.Version()
		fyne.Do(func() {
			if err != nil {
				u.log(bad, fmt.Sprintf("Can't find MariaDB in %s.", server.BinDir))
				return
			}
			u.setVersion(v)
		})
	}()
	u.checkNow()
	u.watch(2 * time.Second)

	w.ShowAndRun()
}
