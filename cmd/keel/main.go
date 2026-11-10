// Command keel opens the Keel control window.
//
// An installed Keel runs the MariaDB that came with it; a development build
// runs the lab setup (see internal/setup). Closing the window leaves
// MariaDB running.
package main

import (
	"flag"
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	"github.com/Tosccony/keel/internal/setup"
)

// version is set at build time: go build -ldflags "-X main.version=1.2.3".
var version = "dev"

func main() {
	opts := setup.Defaults()
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
	u.prepare = opts.Prepare
	settings := opts.Settings()
	u.settings = settings
	u.parts = append(u.parts, newPart(u.window, newMailService(opts), settings))
	w.SetContent(u.content())
	w.Resize(fyne.NewSize(820, 720))

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

	// First run: ask where websites live. Then start the parts that are set
	// to start with Keel.
	autostart := func() {
		if settings.Autostart(setup.PartMariaDB) {
			u.start(false)
		}
		for _, p := range u.parts {
			p.autostart()
		}
	}
	if dir := settings.Sites(); dir != "" {
		u.setSites(dir)
		autostart()
	} else {
		fyne.Do(func() { u.askSites(settings, setup.DefaultSites(), autostart) })
	}

	w.ShowAndRun()
}
