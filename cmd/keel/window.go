package main

import (
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// window is what every part of Keel's window shares: the Fyne window, the
// activity log and its Copy button, dialogs (dialogs.go), and timers.
// Its fields are only touched on Fyne's UI thread.
type window struct {
	win     fyne.Window
	entries []logEntry
	logList *widget.List
	copyLog *widget.Button

	// later runs f on the UI thread after d. Tests replace it: Fyne's
	// test driver would run f on the timer's goroutine instead.
	later func(d time.Duration, f func())
}

func newWindow(win fyne.Window) *window {
	w := &window{win: win}
	w.copyLog = widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), w.copyActivity)
	w.copyLog.Importance = widget.LowImportance
	w.logList = widget.NewList(
		func() int { return len(w.entries) },
		func() fyne.CanvasObject {
			when := widget.NewLabel("00:00:00")
			when.Importance = widget.LowImportance
			icon := widget.NewIcon(nil)
			msg := widget.NewLabel("")
			msg.Truncation = fyne.TextTruncateEllipsis
			left := container.NewHBox(when, icon)
			return container.New(layout.NewBorderLayout(nil, nil, left, nil), left, msg)
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			e := w.entries[id]
			row := o.(*fyne.Container).Objects
			left := row[0].(*fyne.Container).Objects
			left[0].(*widget.Label).SetText(e.at.Format("15:04:05"))
			left[1].(*widget.Icon).SetResource(e.lvl.icon())
			row[1].(*widget.Label).SetText(e.msg)
		})
	w.logList.HideSeparators = true
	w.later = func(d time.Duration, f func()) { time.AfterFunc(d, func() { fyne.Do(f) }) }
	return w
}

// maxLogLines keeps the activity log from growing forever. Old lines are
// dropped logTrim at a time, so most new lines are a cheap append.
const (
	maxLogLines = 500
	logTrim     = 50
)

// level is how an activity log line is marked.
type level int

const (
	info level = iota
	good
	warn
	bad
)

type logEntry struct {
	at  time.Time
	lvl level
	msg string
}

func (e logEntry) String() string { return e.at.Format("15:04:05") + "  " + e.msg }

// icon is the mark shown beside a log line; info lines have none. It's
// looked up when needed, because theme icons need the app to exist.
func (l level) icon() fyne.Resource {
	switch l {
	case good:
		return theme.NewSuccessThemedResource(theme.ConfirmIcon())
	case warn:
		return theme.NewWarningThemedResource(theme.WarningIcon())
	case bad:
		return theme.NewErrorThemedResource(theme.ErrorIcon())
	}
	return nil
}

// log adds a timestamped line to the activity log.
func (w *window) log(lvl level, msg string) {
	w.entries = append(w.entries, logEntry{at: time.Now(), lvl: lvl, msg: msg})
	if n := len(w.entries); n > maxLogLines+logTrim {
		w.entries = append(w.entries[:0:0], w.entries[n-maxLogLines:]...)
	}
	w.logList.Refresh()
	w.logList.ScrollToBottom()
}

// logLines returns the activity log as plain text lines.
func (w *window) logLines() []string {
	lines := make([]string, len(w.entries))
	for i, e := range w.entries {
		lines[i] = e.String()
	}
	return lines
}

// copyActivity puts the activity log on the clipboard, for pasting into a
// message when asking for help.
func (w *window) copyActivity() {
	fyne.CurrentApp().Clipboard().SetContent(strings.Join(w.logLines(), "\n"))
	w.copyLog.SetText("Copied")
	w.copyLog.SetIcon(theme.ConfirmIcon())
	w.later(2*time.Second, func() {
		w.copyLog.SetText("Copy")
		w.copyLog.SetIcon(theme.ContentCopyIcon())
	})
}
