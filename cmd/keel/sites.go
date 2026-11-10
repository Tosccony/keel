package main

import (
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/Tosccony/keel/internal/setup"
)

// askSites asks, on the first run, where students' websites should live
// (XAMPP's htdocs). The suggestion is Documents/Keel/htdocs; "Choose
// another" opens a folder picker. Keel creates the folder and remembers it,
// then calls done.
func (u *ui) askSites(st *setup.Settings, suggestion string, done func()) {
	path := widget.NewEntry()
	path.SetText(suggestion)
	intro := widget.NewLabel("Keel serves the websites in this folder at http://localhost. " +
		"It's XAMPP's htdocs folder: put each project in a folder inside it.")
	intro.Wrapping = fyne.TextWrapWord
	content := container.NewVBox(container.New(fixedWidth(dialogWidth), intro), path)

	var d *dialog.ConfirmDialog
	d = dialog.NewCustomConfirm("Where should your websites live?", "Use this folder", "Choose another…", content,
		func(ok bool) {
			if !ok {
				u.pickSites(st, path.Text, done)
				return
			}
			if err := u.useSites(st, path.Text); err != nil {
				u.inform("Couldn't use that folder", err.Error())
				u.askSites(st, path.Text, done)
				return
			}
			done()
		}, u.win)
	d.Show()
}

// pickSites opens a folder picker, then asks again with the chosen folder.
func (u *ui) pickSites(st *setup.Settings, current string, done func()) {
	picker := dialog.NewFolderOpen(func(dir fyne.ListableURI, err error) {
		if dir != nil && err == nil {
			current = dir.Path()
		}
		u.askSites(st, current, done)
	}, u.win)
	picker.Show()
}

// useSites creates the folder and saves it.
func (u *ui) useSites(st *setup.Settings, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := st.SetSites(dir); err != nil {
		return err
	}
	u.setSites(dir)
	u.log(info, "Websites folder: "+dir)
	return nil
}
