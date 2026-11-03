package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/Tosccony/keel/internal/mariadb"
)

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

// ui is Keel's control window: a header, a card per service, a warning
// banner after an unclean shutdown, the activity log and a status bar.
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

	// MariaDB's card.
	name    *widget.Label
	version *widget.Label
	detail  *widget.Label
	status  *pill
	spinner *widget.Activity
	action  *widget.Button

	// Shown while stopped after an unclean shutdown.
	banner     *box
	bannerText *widget.Label

	// Status bar.
	folder       *widget.Label
	shutdown     *widget.Label
	shutdownIcon *widget.Icon

	entries []logEntry
	logList *widget.List
	copyLog *widget.Button

	lastWhy string // last reported reason for an unclean shutdown, so it's logged once
}

func newUI(win fyne.Window, server *mariadb.Server) *ui {
	u := &ui{
		win:          win,
		server:       server,
		name:         widget.NewLabel("MariaDB"),
		version:      muted(""),
		detail:       muted(""),
		status:       newPill(),
		spinner:      widget.NewActivity(),
		folder:       muted(server.DataDir),
		shutdown:     muted(""),
		shutdownIcon: widget.NewIcon(good.icon()),
	}
	u.name.TextStyle.Bold = true
	u.folder.Truncation = fyne.TextTruncateEllipsis
	u.spinner.Hide()
	u.action = widget.NewButtonWithIcon("Start", theme.MediaPlayIcon(), u.onAction)
	u.copyLog = widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), u.copyActivity)
	u.copyLog.Importance = widget.LowImportance

	u.bannerText = widget.NewLabel("")
	u.bannerText.Wrapping = fyne.TextWrapWord
	bannerTitle := widget.NewLabel("The last shutdown wasn't clean")
	bannerTitle.TextStyle.Bold = true
	bannerIcon := widget.NewIcon(warn.icon())
	u.banner = newBanner(container.NewBorder(nil, nil,
		container.NewVBox(container.NewPadded(bannerIcon)), nil,
		vstack(-14, bannerTitle, u.bannerText)), colorWarnFill, colorWarnEdge)
	u.banner.Hide()
	u.shutdown.Hide()
	u.shutdownIcon.Hide()

	u.logList = widget.NewList(
		func() int { return len(u.entries) },
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
			e := u.entries[id]
			row := o.(*fyne.Container).Objects
			left := row[0].(*fyne.Container).Objects
			left[0].(*widget.Label).SetText(e.at.Format("15:04:05"))
			left[1].(*widget.Icon).SetResource(e.lvl.icon())
			row[1].(*widget.Label).SetText(e.msg)
		})
	u.logList.HideSeparators = true

	// Progress comes from the goroutine running Start or Stop.
	server.Progress = func(msg string) {
		fyne.Do(func() { u.log(progressLevel(msg), msg) })
	}
	u.show(u.pidNow)
	return u
}

// content lays out the window.
func (u *ui) content() fyne.CanvasObject {
	activityHead := container.NewBorder(nil, nil, container.NewCenter(sectionTitle("Activity")), u.copyLog)
	top := vstack(10,
		vstack(0, sectionTitle("Services"), u.mariadbCard()),
		u.banner,
		activityHead,
	)
	logPanel := newCard(container.NewThemeOverride(u.logList, compactTheme{fyne.CurrentApp().Settings().Theme()}), 4)
	body := container.New(layout.NewCustomPaddedLayout(12, 16, 18, 18),
		container.NewBorder(top, nil, nil, nil, logPanel))
	return container.NewBorder(u.header(), u.statusBar(), nil, nil, body)
}

func (u *ui) header() fyne.CanvasObject {
	logo := canvas.NewImageFromResource(appIcon)
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(fyne.NewSquareSize(34))

	title := widget.NewLabel("Keel")
	title.TextStyle.Bold = true
	title.SizeName = theme.SizeNameSubHeadingText
	tagline := muted("Local web stack that shuts down safely")
	tagline.SizeName = theme.SizeNameCaptionText
	words := vstack(-16, title, tagline)

	v := "Version " + strings.TrimPrefix(version, "v")
	if version == "dev" {
		v = "Development build"
	}
	ver := muted(v)
	ver.SizeName = theme.SizeNameCaptionText

	row := container.NewBorder(nil, nil,
		hstack(6, container.NewCenter(logo), container.NewCenter(words)),
		container.NewCenter(ver))
	return vstack(0, newBar(row, fyne.NewSize(16, 10)), widget.NewSeparator())
}

func (u *ui) mariadbCard() fyne.CanvasObject {
	words := vstack(-14, hstack(-10, u.name, u.version), u.detail)
	right := hstack(8,
		container.NewCenter(u.spinner),
		container.NewCenter(u.status),
		container.NewCenter(u.action))
	return newCard(container.NewBorder(nil, nil, newTile(theme.StorageIcon()), right, words), 12)
}

func (u *ui) statusBar() fyne.CanvasObject {
	open := widget.NewButtonWithIcon("Open folder", theme.FolderOpenIcon(), u.openDataFolder)
	open.Importance = widget.LowImportance
	right := container.NewHBox(
		container.NewCenter(u.shutdownIcon), u.shutdown, container.NewCenter(open))
	row := container.NewBorder(nil, nil,
		container.NewCenter(widget.NewIcon(theme.NewColoredResource(theme.FolderIcon(), theme.ColorNameDisabled))),
		right, u.folder)
	return vstack(0, widget.NewSeparator(), newBar(row, fyne.NewSize(14, 2)))
}

// setVersion shows the MariaDB version found on disk.
func (u *ui) setVersion(v string) { u.version.SetText(v) }

// show updates MariaDB's card for a running server (pid > 0) or a stopped
// one (pid == 0).
func (u *ui) show(pid int) {
	u.pidNow = pid
	u.spinner.Stop()
	u.spinner.Hide()
	if pid != 0 {
		u.status.set("Running", theme.ColorNameSuccess)
		u.detail.SetText(fmt.Sprintf("Port %d  ·  PID %d", u.server.Port, pid))
		u.action.SetText("Stop")
		u.action.SetIcon(theme.MediaStopIcon())
		u.action.Importance = widget.MediumImportance
	} else {
		u.status.set("Stopped", "")
		u.detail.SetText(fmt.Sprintf("Port %d", u.server.Port))
		u.action.SetText("Start")
		u.action.SetIcon(theme.MediaPlayIcon())
		u.action.Importance = widget.HighImportance
	}
	u.action.Enable()
	u.action.Refresh()
}

// showBusy shows "Starting…" or "Stopping…" and disables the button.
func (u *ui) showBusy(text string) {
	u.status.set(text, theme.ColorNameWarning)
	u.spinner.Show()
	u.spinner.Start()
	u.action.SetText(text)
	u.action.SetIcon(nil)
	u.action.Disable()
}

func (u *ui) onAction() {
	if u.busy {
		return
	}
	if u.pidNow != 0 {
		u.stop()
	} else {
		u.start(false)
	}
}

// start starts MariaDB. withoutCopy skips the safety copy before recovery,
// and is only used after the user agreed to that.
func (u *ui) start(withoutCopy bool) {
	u.busy = true
	u.showBusy("Starting…")
	go func() {
		u.mu.Lock()
		var err error
		if withoutCopy {
			err = u.server.StartWithoutSnapshot(context.Background())
		} else {
			err = u.server.Start(context.Background())
		}
		pid := u.server.RunningPID()
		rec := u.server.LastRecovery()
		u.mu.Unlock()

		fyne.Do(func() {
			u.busy = false
			u.show(pid)
			u.checkNow()
			switch {
			case err == nil && rec != nil:
				u.showRecovery(rec)
			case err == nil:
			case errors.Is(err, mariadb.ErrAlreadyRunning):
				u.log(info, "MariaDB is already running.")
			case errors.Is(err, mariadb.ErrSnapshotFailed):
				u.confirmStartWithoutCopy(err)
			default:
				msg := err.Error()
				if errors.Is(err, mariadb.ErrPortInUse) {
					msg = fmt.Sprintf("Port %d is already in use by another program.", u.server.Port)
				}
				first, _, _ := strings.Cut(msg, "\n")
				u.log(bad, "Couldn't start: "+strings.TrimPrefix(first, "mariadb: "))
				dialog.ShowError(errors.New(msg), u.win)
			}
		})
	}()
}

// showRecovery tells the user what happened after an unclean shutdown.
func (u *ui) showRecovery(rec *mariadb.Recovery) {
	var b strings.Builder
	b.WriteString(rec.Reason + "\n\n")
	title := "Recovered from an unclean shutdown"
	switch {
	case rec.CheckErr != nil:
		title = "Recovered, but the tables weren't checked"
		fmt.Fprintf(&b, "MariaDB started, but Keel couldn't check the tables:\n%v", rec.CheckErr)
	case len(rec.Problems) == 0:
		fmt.Fprintf(&b, "MariaDB recovered, and all %d tables passed their check.", rec.Checked)
	default:
		title = "Some tables need attention"
		fmt.Fprintf(&b, "MariaDB started, but %d of %d tables have problems:\n", len(rec.Problems), rec.Checked)
		for i, p := range rec.Problems {
			if i == 10 {
				fmt.Fprintf(&b, "…and %d more (see the activity log)\n", len(rec.Problems)-10)
				break
			}
			fmt.Fprintf(&b, "• %s: %s\n", p.Table, p.Message)
		}
	}
	if rec.Snapshot != "" {
		b.WriteString("\n\nA copy of the data from before recovery is in:\n" + rec.Snapshot)
	}
	dialog.ShowInformation(title, b.String(), u.win)
}

// confirmStartWithoutCopy asks before letting MariaDB recover with no copy
// to fall back on.
func (u *ui) confirmStartWithoutCopy(err error) {
	u.log(warn, "Didn't start: Keel couldn't save a safety copy first.")
	d := dialog.NewConfirm("Start without a safety copy?",
		strings.TrimPrefix(err.Error(), "mariadb: ")+"\n\n"+
			"The last shutdown wasn't clean, so MariaDB will run crash recovery.\n"+
			"Starting anyway means there's no copy to go back to if recovery goes wrong.",
		func(ok bool) {
			if ok {
				u.start(true)
			}
		}, u.win)
	d.SetConfirmText("Start anyway")
	d.SetDismissText("Don't start")
	d.SetConfirmImportance(widget.DangerImportance)
	d.Show()
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
				u.log(info, "MariaDB had already stopped.")
			case err != nil:
				u.log(bad, "Couldn't stop: "+err.Error())
			case res.Confirmed:
				u.log(good, fmt.Sprintf(`Stopped cleanly in %.1fs. MariaDB's log confirms "Shutdown complete".`, res.Took.Seconds()))
			default:
				u.log(warn, fmt.Sprintf(`Stopped in %.1fs, but "Shutdown complete" isn't in MariaDB's log.`, res.Took.Seconds()))
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
				u.log(info, "Left MariaDB running.")
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
						u.log(bad, "Couldn't force-stop: "+err.Error())
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
		clean, why := u.server.LastShutdown() // reads files only; safe without the lock
		fyne.Do(func() { u.applyCheck(pid, clean, why) })
	}()
}

// applyCheck shows the result of a background check: whether MariaDB is
// running, and while it's stopped, whether it last shut down cleanly.
func (u *ui) applyCheck(pid int, clean bool, why string) {
	if u.busy {
		return
	}
	if pid != u.pidNow {
		switch {
		case pid != 0 && u.pidNow == 0:
			u.log(info, fmt.Sprintf("MariaDB is running (PID %d), started outside this window.", pid))
		case pid == 0 && u.pidNow != 0 && !clean:
			u.log(warn, "MariaDB stopped without Keel asking: another program, a crash, or a kill. Keel will check it on the next start.")
		case pid == 0 && u.pidNow != 0:
			u.log(info, "MariaDB was stopped outside this window.")
		}
		u.show(pid)
	}
	if !clean && pid == 0 && why != u.lastWhy {
		u.log(warn, "The last shutdown wasn't clean: "+why)
	}
	u.lastWhy = why
	u.showShutdown(pid, clean, why)
}

// showShutdown shows how MariaDB last shut down, while it's stopped: a
// line in the status bar when it was clean, a banner when it wasn't.
func (u *ui) showShutdown(pid int, clean bool, why string) {
	stopped := pid == 0
	if stopped && !clean {
		u.bannerText.SetText(why + " Keel will save a copy of the data folder and check every table the next time MariaDB starts.")
		u.banner.Show()
	} else {
		u.banner.Hide()
	}
	if stopped && clean {
		u.shutdown.SetText("Last shutdown was clean")
		u.shutdown.Show()
		u.shutdownIcon.Show()
	} else {
		u.shutdown.Hide()
		u.shutdownIcon.Hide()
	}
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
func (u *ui) log(lvl level, msg string) {
	u.entries = append(u.entries, logEntry{at: time.Now(), lvl: lvl, msg: msg})
	if n := len(u.entries); n > maxLogLines+logTrim {
		u.entries = append(u.entries[:0:0], u.entries[n-maxLogLines:]...)
	}
	u.logList.Refresh()
	u.logList.ScrollToBottom()
}

// logLines returns the activity log as plain text lines.
func (u *ui) logLines() []string {
	lines := make([]string, len(u.entries))
	for i, e := range u.entries {
		lines[i] = e.String()
	}
	return lines
}

// copyActivity puts the activity log on the clipboard, for pasting into a
// message when asking for help.
func (u *ui) copyActivity() {
	fyne.CurrentApp().Clipboard().SetContent(strings.Join(u.logLines(), "\n"))
	u.copyLog.SetText("Copied")
	u.copyLog.SetIcon(theme.ConfirmIcon())
	time.AfterFunc(2*time.Second, func() {
		fyne.Do(func() {
			u.copyLog.SetText("Copy")
			u.copyLog.SetIcon(theme.ContentCopyIcon())
		})
	})
}

// openDataFolder shows the data folder in Finder, Explorer or the file
// manager.
func (u *ui) openDataFolder() {
	link, err := url.Parse(storage.NewFileURI(u.server.DataDir).String())
	if err == nil {
		err = fyne.CurrentApp().OpenURL(link)
	}
	if err != nil {
		u.log(bad, "Couldn't open the data folder: "+err.Error())
	}
}

// progressLevel picks the mark for a progress message from the server.
// It only changes the icon beside the line.
func progressLevel(msg string) level {
	switch {
	case strings.HasPrefix(msg, "  "): // a table under "N of M tables have problems:"
		return bad
	case strings.HasPrefix(msg, "Couldn't"),
		strings.HasPrefix(msg, "The second request failed"),
		strings.HasPrefix(msg, "The last shutdown wasn't clean"),
		strings.HasPrefix(msg, "Force-stopped"),
		strings.HasSuffix(msg, "tables have problems:"):
		return warn
	case strings.HasPrefix(msg, "MariaDB is ready"),
		strings.HasPrefix(msg, "Recovered."),
		strings.HasPrefix(msg, "Saved a copy"):
		return good
	}
	return info
}
