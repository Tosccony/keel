package main

import (
	_ "embed"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/Tosccony/keel/internal/setup"
)

//go:embed tray.svg
var traySVG []byte

// trayIcon is a one-colour sailboat; themed, so macOS tints it to match
// the menu bar.
var trayIcon = theme.NewThemedResource(fyne.NewStaticResource("keel-tray.svg", traySVG))

// setUpTray puts Keel in the system tray (the menu bar on macOS) and makes
// closing the window hide it, unless the user chose otherwise. Fyne adds
// the tray menu's Quit item itself.
func (u *ui) setUpTray(a fyne.App) {
	desk, ok := a.(desktop.App)
	if !ok {
		return
	}
	open := fyne.NewMenuItem("Open Keel", func() { u.win.Show(); u.win.RequestFocus() })
	startAll := fyne.NewMenuItem("Start everything", u.startAll)
	stopAll := fyne.NewMenuItem("Stop everything", u.stopAll)
	desk.SetSystemTrayMenu(fyne.NewMenu("Keel", open, fyne.NewMenuItemSeparator(), startAll, stopAll))
	desk.SetSystemTrayIcon(trayIcon)

	told := false
	u.win.SetCloseIntercept(func() {
		if u.settings != nil && u.settings.QuitOnClose() {
			a.Quit()
			return
		}
		u.win.Hide()
		if !told {
			told = true
			u.log(info, "Keel is still running in the tray (the menu bar on a Mac). Choose Quit there to close it.")
		}
	})
}

// startAll starts MariaDB and every part that's stopped.
func (u *ui) startAll() {
	if u.pidNow == 0 && !u.busy {
		u.start(false)
	}
	for _, p := range u.parts {
		if !p.running && !p.busy {
			p.start()
		}
	}
}

// stopAll stops every part, then MariaDB (cleanly, as always).
func (u *ui) stopAll() {
	for _, p := range u.parts {
		if p.running && !p.busy {
			p.stop()
		}
	}
	if u.pidNow != 0 && !u.busy {
		u.stop()
	}
}

// showSettings shows Keel's settings: the websites folder, opening at
// sign-in, and what closing the window does.
func (u *ui) showSettings() {
	if u.settings == nil {
		return
	}
	st := u.settings
	folder := widget.NewLabel(u.sites)
	folder.Wrapping = fyne.TextWrapWord
	change := widget.NewButtonWithIcon("Change…", theme.FolderOpenIcon(), nil)
	change.OnTapped = func() {
		dialog.NewFolderOpen(func(dir fyne.ListableURI, err error) {
			if dir == nil || err != nil {
				return
			}
			if err := u.useSites(st, dir.Path()); err != nil {
				u.inform("Couldn't use that folder", err.Error())
				return
			}
			folder.SetText(dir.Path())
			u.log(info, "Restart the web server and the FTP server to use the new websites folder.")
		}, u.win).Show()
	}

	signIn := widget.NewCheck("Open Keel when I sign in", func(on bool) {
		if err := setup.SetStartAtSignIn(on); err != nil {
			u.log(bad, "Couldn't change that: "+err.Error())
		}
	})
	signIn.SetChecked(setup.StartsAtSignIn())
	tray := widget.NewCheck("Keep running in the tray when the window closes", func(on bool) {
		if err := st.SetQuitOnClose(!on); err != nil {
			u.log(bad, "Couldn't save that: "+err.Error())
		}
	})
	tray.SetChecked(!st.QuitOnClose())

	hint := muted("At sign-in, Keel starts in the tray and starts the parts set to \"Start when Keel opens\" (each card's menu).")
	hint.Wrapping = fyne.TextWrapWord
	content := container.New(fixedWidth(dialogWidth), container.NewVBox(
		sectionTitle("Websites folder"),
		container.NewBorder(nil, nil, nil, change, folder),
		widget.NewSeparator(),
		sectionTitle("Starting and closing"),
		signIn,
		tray,
		hint,
	))
	dialog.NewCustom("Settings", "Done", content, u.win).Show()
}
