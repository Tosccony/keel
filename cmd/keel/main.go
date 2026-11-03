// Command keel opens the Keel control window.
//
// This is a placeholder so the release pipeline has something to build and
// package. The real window, background service and server management replace it.
package main

import (
	"flag"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// version is set at build time: go build -ldflags "-X main.version=1.2.3".
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("keel", version)
		return
	}

	a := app.NewWithID("io.github.tosccony.keel")
	w := a.NewWindow("Keel")
	w.SetContent(container.NewVBox(
		widget.NewLabelWithStyle("Keel "+version, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Placeholder build. The control window is on its way."),
	))
	w.Resize(fyne.NewSize(420, 140))
	w.ShowAndRun()
}
