package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// dialogWidth is how wide Keel's message dialogs are. Text wraps inside
// that width, so a dialog is exactly as tall as its message. (Fyne's own
// message dialogs measure wrapped text at a narrower width than they draw
// it, which leaves a gap above the buttons.)
const dialogWidth = 480

// fixedWidth lays out wrapped text at one width and reports the height
// the text needs at that width.
type fixedWidth float32

func (w fixedWidth) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var h float32
	for _, o := range objs {
		o.Resize(fyne.NewSize(float32(w), o.Size().Height)) // wrapped text measures itself at its current width
		h = max(h, o.MinSize().Height)
	}
	return fyne.NewSize(float32(w), h)
}

func (w fixedWidth) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Move(fyne.NewPos(0, 0))
		o.Resize(size)
	}
}

// message is a dialog's text, wrapped at dialogWidth.
func message(text string) fyne.CanvasObject {
	l := widget.NewLabel(text)
	l.Wrapping = fyne.TextWrapWord
	return container.New(fixedWidth(dialogWidth), l)
}

// inform shows a message with an OK button.
func (u *ui) inform(title, text string) {
	dialog.NewCustom(title, "OK", message(text), u.win).Show()
}

// ask shows a question with two buttons and reports which was pressed.
// importance colours the first button: High for the normal choice, Danger
// for one that risks data.
func (u *ui) ask(title, text, yes, no string, importance widget.Importance, answer func(ok bool)) {
	d := dialog.NewCustomConfirm(title, yes, no, message(text), answer, u.win)
	d.SetConfirmImportance(importance)
	d.Show()
}
