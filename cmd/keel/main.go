// Command keel opens the Keel control window.
//
// An installed Keel runs the MariaDB that came with it; a development build
// runs the lab setup (see internal/setup). Closing the window leaves
// MariaDB running.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	"github.com/Tosccony/keel/internal/mail"
	"github.com/Tosccony/keel/internal/setup"
)

// version is set at build time: go build -ldflags "-X main.version=1.2.3".
var version = "dev"

func main() {
	// PHP runs this program as its sendmail on macOS and Linux.
	if len(os.Args) > 1 && os.Args[1] == "--sendmail" {
		os.Exit(runSendmail(os.Args[2:]))
	}
	opts := setup.Defaults()
	opts.AddFlags(flag.CommandLine)
	showVersion := flag.Bool("version", false, "print the version and exit")
	hidden := flag.Bool(strings.TrimPrefix(setup.HiddenFlag, "--"), false, "start in the tray, without the window (used at sign-in)")
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
	webSvc := newWebService(opts)
	u.web = newPart(u.window, webSvc, settings)
	u.toolPaths = func() []string {
		paths := []string{opts.BinDir}
		if webSvc.server != nil && webSvc.server.PHP != "" {
			paths = append(paths, filepath.Dir(webSvc.server.PHP))
		}
		if perl := opts.Perl(); perl != "" {
			paths = append(paths, filepath.Dir(perl))
		}
		return paths
	}
	u.parts = append(u.parts,
		u.web,
		newPart(u.window, newMailService(opts), settings),
		newPart(u.window, newFTPService(opts), settings),
		newPart(u.window, newTomcatService(opts), settings))
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

	u.setUpTray(a)
	if *hidden {
		a.Run() // started at sign-in: just the tray icon, until the user opens the window
		return
	}
	w.ShowAndRun()
}

// runSendmail hands a message from PHP to the mail catcher. The web server
// passes the catcher's port first ("--port 1025"); without it, the port
// comes from Keel's settings.
func runSendmail(args []string) int {
	port := 0
	if len(args) >= 2 && args[0] == "--port" {
		port, _ = strconv.Atoi(args[1])
		args = args[2:]
	}
	if port == 0 {
		port = setup.Defaults().Settings().Port(setup.PartMail, setup.DefaultPorts[setup.PartMail])
	}
	if err := mail.Sendmail(os.Stdin, args, port); err != nil {
		fmt.Fprintln(os.Stderr, "keel sendmail:", err)
		return 1
	}
	return 0
}
