package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/Tosccony/keel/internal/mariadb"
)

// maxLogLines keeps the activity log from growing forever. Old lines are
// dropped logTrim at a time, so most new lines are a cheap append.
const (
	maxLogLines = 500
	logTrim     = 50
)

// ui is Keel's control window: a row for MariaDB and an activity log.
//
// Widgets are only touched on Fyne's UI thread. Start, Stop and the status
// check run in goroutines, take mu (mariadb.Server isn't safe for concurrent
// use), and hand their results back with fyne.Do.
type ui struct {
	win    fyne.Window
	server *mariadb.Server
	mu     sync.Mutex

	busy   bool // a start or stop is in progress
	pidNow int  // the PID last shown; 0 means stopped

	status  *widget.Label
	module  *widget.Label
	port    *widget.Label
	pid     *widget.Label
	action  *widget.Button
	logData binding.StringList
	logList *widget.List
	footer  *widget.Label
}

func newUI(win fyne.Window, server *mariadb.Server) *ui {
	u := &ui{
		win:     win,
		server:  server,
		status:  widget.NewLabel(""),
		module:  widget.NewLabel("MariaDB"),
		port:    widget.NewLabel(strconv.Itoa(server.Port)),
		pid:     widget.NewLabel("—"),
		logData: binding.NewStringList(),
		footer:  widget.NewLabel("Data folder: " + server.DataDir),
	}
	u.status.TextStyle.Bold = true
	u.module.TextStyle.Bold = true
	u.port.TextStyle.Monospace = true
	u.pid.TextStyle.Monospace = true
	u.footer.Importance = widget.LowImportance
	u.footer.Truncation = fyne.TextTruncateEllipsis
	u.action = widget.NewButton("Start", u.onAction)

	u.logList = widget.NewListWithData(u.logData,
		func() fyne.CanvasObject {
			l := widget.NewLabel("")
			l.TextStyle.Monospace = true
			l.Truncation = fyne.TextTruncateEllipsis
			return l
		},
		func(item binding.DataItem, o fyne.CanvasObject) {
			o.(*widget.Label).Bind(item.(binding.String))
		})
	u.logList.HideSeparators = true

	// Progress comes from the goroutine running Start or Stop.
	server.Progress = func(msg string) {
		fyne.Do(func() { u.log("MariaDB", msg) })
	}
	u.show(u.pidNow)
	return u
}

// content lays out the window.
func (u *ui) content() fyne.CanvasObject {
	heading := func(text string) fyne.CanvasObject {
		l := widget.NewLabel(text)
		l.Importance = widget.LowImportance
		return l
	}
	modules := container.NewGridWithColumns(5,
		heading("Status"), heading("Module"), heading("Port"), heading("PID"), heading(""),
		u.status, u.module, u.port, u.pid, u.action,
	)
	top := container.NewVBox(modules, widget.NewSeparator(), heading("Activity"))
	return container.NewBorder(top, u.footer, nil, nil, u.logList)
}

// show updates the MariaDB row for a running server (pid > 0) or a
// stopped one (pid == 0).
func (u *ui) show(pid int) {
	u.pidNow = pid
	if pid != 0 {
		u.status.SetText("Running")
		u.status.Importance = widget.SuccessImportance
		u.pid.SetText(strconv.Itoa(pid))
		u.action.SetText("Stop")
		u.action.Importance = widget.MediumImportance
	} else {
		u.status.SetText("Stopped")
		u.status.Importance = widget.LowImportance
		u.pid.SetText("—")
		u.action.SetText("Start")
		u.action.Importance = widget.HighImportance
	}
	u.action.Enable()
	u.status.Refresh()
	u.action.Refresh()
}

// showBusy shows "Starting…" or "Stopping…" and disables the button.
func (u *ui) showBusy(text string) {
	u.status.SetText(text)
	u.status.Importance = widget.WarningImportance
	u.status.Refresh()
	u.action.SetText(text)
	u.action.Disable()
}

func (u *ui) onAction() {
	if u.busy {
		return
	}
	if u.pidNow != 0 {
		u.stop()
	} else {
		u.start()
	}
}

func (u *ui) start() {
	u.busy = true
	u.showBusy("Starting…")
	go func() {
		u.mu.Lock()
		err := u.server.Start(context.Background())
		pid := u.server.RunningPID()
		u.mu.Unlock()

		fyne.Do(func() {
			u.busy = false
			u.show(pid)
			if err == nil {
				return
			}
			msg := err.Error()
			if errors.Is(err, mariadb.ErrPortInUse) {
				msg = fmt.Sprintf("Port %d is already in use by another program.", u.server.Port)
			}
			first, _, _ := strings.Cut(msg, "\n")
			u.log("MariaDB", "Couldn't start: "+strings.TrimPrefix(first, "mariadb: "))
			dialog.ShowError(errors.New(msg), u.win)
		})
	}()
}

func (u *ui) stop() {
	u.busy = true
	u.showBusy("Stopping…")
	go func() {
		u.mu.Lock()
		res, err := u.server.Stop(context.Background())
		u.mu.Unlock()

		fyne.Do(func() {
			u.busy = false
			switch {
			case errors.Is(err, mariadb.ErrStillRunning):
				u.show(res.PID)
				u.confirmKill()
				return
			case errors.Is(err, mariadb.ErrNotRunning):
				u.log("MariaDB", "It had already stopped.")
			case err != nil:
				u.log("MariaDB", "Couldn't stop: "+err.Error())
			default:
				note := ` MariaDB's log confirms "Shutdown complete".`
				if !res.Confirmed {
					note = ` ("Shutdown complete" isn't in the log.)`
				}
				u.log("MariaDB", fmt.Sprintf("Stopped cleanly in %.1fs.%s", res.Took.Seconds(), note))
			}
			u.show(0)
			u.checkNow()
		})
	}()
}

// confirmKill asks before the one hard kill Keel allows.
func (u *ui) confirmKill() {
	d := dialog.NewConfirm("Force-stop MariaDB?",
		"MariaDB is still running after two requests to shut down.\n\n"+
			"Force-stopping it is the hard kill that corrupts XAMPP databases.\n"+
			"The next start will run crash recovery.",
		func(ok bool) {
			if !ok {
				u.log("MariaDB", "Left it running.")
				return
			}
			u.busy = true
			u.showBusy("Stopping…")
			go func() {
				u.mu.Lock()
				err := u.server.Kill()
				u.mu.Unlock()
				fyne.Do(func() {
					u.busy = false
					if err != nil {
						u.log("MariaDB", "Couldn't force-stop: "+err.Error())
					}
					u.checkNow()
				})
			}()
		}, u.win)
	d.SetConfirmText("Force-stop")
	d.SetDismissText("Keep running")
	d.SetConfirmImportance(widget.DangerImportance)
	d.Show()
}

// checkNow looks up whether MariaDB is running, in the background, and
// notes changes Keel didn't make, like a stop from the command line.
func (u *ui) checkNow() {
	go func() {
		if !u.mu.TryLock() {
			return // a start or stop is running; it will report the result
		}
		pid := u.server.RunningPID()
		u.mu.Unlock()
		fyne.Do(func() { u.applyCheck(pid) })
	}()
}

func (u *ui) applyCheck(pid int) {
	if u.busy || pid == u.pidNow {
		return
	}
	switch {
	case pid != 0 && u.pidNow == 0:
		u.log("MariaDB", fmt.Sprintf("Running (PID %d), started outside this window.", pid))
	case pid == 0 && u.pidNow != 0:
		u.log("MariaDB", "Stopped without Keel asking: another program, a crash, or a kill.")
	}
	u.show(pid)
}

// watch re-checks the server every interval until the app quits.
func (u *ui) watch(interval time.Duration) {
	go func() {
		for range time.Tick(interval) {
			u.checkNow()
		}
	}()
}

// log adds a timestamped line to the activity log.
func (u *ui) log(who, msg string) {
	u.logData.Append(fmt.Sprintf("%s  %-8s %s", time.Now().Format("15:04:05"), who, msg))
	if n := u.logData.Length(); n > maxLogLines+logTrim {
		lines, _ := u.logData.Get()
		u.logData.Set(lines[n-maxLogLines:])
	}
	u.logList.ScrollToBottom()
}
