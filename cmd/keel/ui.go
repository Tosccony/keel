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

	busy       bool // a start or stop is in progress
	pidNow     int  // the PID last shown; 0 means stopped
	repairMode bool // the running MariaDB was left in repair mode by an interrupted repair
	setUp      bool // the data folder exists; false until the first start creates it
	port       int  // the port shown; the server's Port is only touched under mu

	// prepare, if set, runs before each start: it creates the data folder
	// on the first start and moves MariaDB off a port another program
	// holds. It returns a note for the log.
	prepare func(context.Context, *mariadb.Server) (string, error)

	// recheck runs after each start or stop: checkNow, except in tests,
	// where Fyne's test driver would run the check's result on another
	// goroutine instead of the UI thread.
	recheck func()
	// later runs f on the UI thread after d. Tests replace it, for the
	// same reason as recheck.
	later func(d time.Duration, f func())

	// MariaDB's card.
	name    *widget.Label
	version *widget.Label
	detail  *widget.Label
	status  *pill
	spinner *widget.Activity
	action  *widget.Button
	more    *widget.Button // the ⋯ menu: back up, restore, repair, check

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
		port:         server.Port,
		setUp:        true,
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
	u.more = widget.NewButtonWithIcon("", theme.MoreVerticalIcon(), u.showMoreMenu)
	u.more.Importance = widget.LowImportance
	u.copyLog = widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), u.copyActivity)
	u.copyLog.Importance = widget.LowImportance

	u.bannerText = widget.NewLabel("")
	u.bannerText.Wrapping = fyne.TextWrapWord
	bannerTitle := widget.NewLabel("Keel will check MariaDB on the next start")
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
	u.recheck = u.checkNow
	u.later = func(d time.Duration, f func()) { time.AfterFunc(d, func() { fyne.Do(f) }) }
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
		container.NewCenter(u.action),
		container.NewCenter(u.more))
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
	u.detail.SetText(u.detailText())
	switch {
	case pid != 0 && u.repairMode:
		u.status.set("Repair mode", theme.ColorNameWarning)
		u.action.SetText("Stop")
		u.action.SetIcon(theme.MediaStopIcon())
		u.action.Importance = widget.HighImportance
	case pid != 0:
		u.status.set("Running", theme.ColorNameSuccess)
		u.action.SetText("Stop")
		u.action.SetIcon(theme.MediaStopIcon())
		u.action.Importance = widget.MediumImportance
	default:
		u.status.set("Stopped", "")
		u.action.SetText("Start")
		u.action.SetIcon(theme.MediaPlayIcon())
		u.action.Importance = widget.HighImportance
	}
	u.action.Enable()
	u.action.Refresh()
	u.more.Enable()
}

// detailText is the line under MariaDB's name: its port, and its PID while
// running.
func (u *ui) detailText() string {
	if u.pidNow != 0 {
		return fmt.Sprintf("Port %d  ·  PID %d", u.port, u.pidNow)
	}
	return fmt.Sprintf("Port %d", u.port)
}

// setPort shows a new port, after prepare moved MariaDB.
func (u *ui) setPort(port int) {
	u.port = port
	u.detail.SetText(u.detailText())
}

// busyNote explains ErrBusy.
const busyNote = "Another Keel window (or keel-lab) is starting or stopping MariaDB right now. Try again in a moment."

// showBusy shows "Starting…" or "Stopping…" and disables the button.
func (u *ui) showBusy(text string) {
	u.status.set(text, theme.ColorNameWarning)
	u.spinner.Show()
	u.spinner.Start()
	u.action.SetText(text)
	u.action.SetIcon(nil)
	u.action.Disable()
	u.more.Disable()
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
		r := u.runStart(withoutCopy)
		fyne.Do(func() { u.finishStart(r) })
	}()
}

// startResult is what runStart hands back to the UI thread.
type startResult struct {
	port, pid int
	rec       *mariadb.Recovery
	err       error
}

// runStart moves MariaDB off a taken port if it has to, then starts it.
// It runs off the UI thread.
func (u *ui) runStart(withoutCopy bool) startResult {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.startLocked(context.Background(), withoutCopy)
}

// startLocked is runStart for callers that already hold mu.
func (u *ui) startLocked(ctx context.Context, withoutCopy bool) startResult {
	var err error
	if u.prepare != nil {
		var note string
		note, err = u.prepare(ctx, u.server)
		if note != "" {
			port := u.server.Port
			fyne.Do(func() {
				u.setPort(port)
				u.log(warn, note)
			})
		}
	}
	if err == nil && withoutCopy {
		err = u.server.StartWithoutSnapshot(ctx)
	} else if err == nil {
		err = u.server.Start(ctx)
	}
	return startResult{port: u.server.Port, pid: u.server.RunningPID(), rec: u.server.LastRecovery(), err: err}
}

// finishStart shows how a start went. It runs on the UI thread.
func (u *ui) finishStart(r startResult) {
	u.busy = false
	u.port = r.port
	u.show(r.pid)
	u.recheck()
	switch err := r.err; {
	case err == nil && r.rec != nil:
		u.showRecovery(r.rec)
	case err == nil:
	case errors.Is(err, mariadb.ErrBusy):
		u.log(info, busyNote)
	case errors.Is(err, mariadb.ErrAlreadyRunning):
		u.log(info, "MariaDB is already running.")
	case errors.Is(err, mariadb.ErrSnapshotFailed):
		u.confirmStartWithoutCopy(err)
	case errors.Is(err, mariadb.ErrExitedDuringStartup):
		u.log(bad, "MariaDB quit while starting: "+startErrorLine(err))
		u.askRepair("MariaDB couldn't start", "MariaDB quit while starting:\n"+startErrorLine(err))
	default:
		msg := err.Error()
		if errors.Is(err, mariadb.ErrPortInUse) {
			msg = fmt.Sprintf("Port %d is already in use by another program.", r.port)
		}
		first, _, _ := strings.Cut(msg, "\n")
		u.log(bad, "Couldn't start: "+strings.TrimPrefix(first, "mariadb: "))
		u.inform("MariaDB couldn't start", strings.TrimPrefix(msg, "mariadb: "))
	}
}

// showRecovery tells the user what happened after an unclean shutdown.
func (u *ui) showRecovery(rec *mariadb.Recovery) {
	var b strings.Builder
	b.WriteString(rec.Reason + "\n\n")
	title := "Recovery finished"
	switch {
	case rec.CheckErr != nil:
		title = "Recovered, but the tables weren't checked"
		fmt.Fprintf(&b, "MariaDB started, but Keel couldn't check the tables:\n%v", rec.CheckErr)
	case len(rec.Problems) == 0:
		fmt.Fprintf(&b, "MariaDB recovered, and all %d tables passed their check.", rec.Checked)
	default:
		// The server already logged each problem; offer to fix them.
		u.askRepair("Some tables need attention", repairPrompt(rec))
		return
	}
	if rec.Snapshot != "" {
		b.WriteString("\n\nA copy of the data from before recovery is in:\n" + rec.Snapshot)
	}
	u.inform(title, b.String())
}

// confirmStartWithoutCopy asks before letting MariaDB recover with no copy
// to fall back on.
func (u *ui) confirmStartWithoutCopy(err error) {
	u.log(warn, "Didn't start: Keel couldn't save a safety copy first.")
	u.ask("Start without a safety copy?",
		strings.TrimPrefix(err.Error(), "mariadb: ")+"\n\n"+
			"The last shutdown wasn't clean, so MariaDB will run crash recovery. "+
			"Starting anyway means there's no copy to go back to if recovery goes wrong.",
		"Start anyway", "Don't start", widget.DangerImportance, func(ok bool) {
			if ok {
				u.start(true)
			}
		})
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
			case errors.Is(err, mariadb.ErrBusy):
				u.log(info, busyNote)
				u.show(u.pidNow)
				u.recheck()
				return
			case errors.Is(err, mariadb.ErrNotRunning):
				u.log(info, "MariaDB had already stopped.")
			case err != nil:
				u.log(bad, "Couldn't stop: "+err.Error())
				u.show(u.pidNow)
				u.recheck()
				return
			case res.Confirmed:
				u.log(good, fmt.Sprintf(`Stopped cleanly in %.1fs. MariaDB's log confirms "Shutdown complete".`, res.Took.Seconds()))
			default:
				u.log(warn, fmt.Sprintf(`Stopped in %.1fs, but "Shutdown complete" isn't in MariaDB's log.`, res.Took.Seconds()))
			}
			u.show(0)
			u.recheck()
		})
	}()
}

// confirmKill asks before the one hard kill Keel allows.
func (u *ui) confirmKill() {
	u.ask("Force-stop MariaDB?",
		"MariaDB is still running after two requests to shut down.\n\n"+
			"Force-stopping it is the hard kill that corrupts XAMPP databases. "+
			"The next start will run crash recovery.",
		"Force-stop", "Keep running", widget.DangerImportance, func(ok bool) {
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
				fyne.Do(func() { u.finishKill(err) })
			}()
		})
}

// finishKill shows how a force-stop went. On success the server's own
// progress message has already said so.
func (u *ui) finishKill(err error) {
	u.busy = false
	switch {
	case errors.Is(err, mariadb.ErrBusy):
		u.log(info, busyNote)
		u.show(u.pidNow)
	case errors.Is(err, mariadb.ErrNotRunning):
		u.show(0)
	case err != nil:
		u.log(bad, "Couldn't force-stop: "+err.Error())
		u.show(u.pidNow)
	default:
		u.show(0)
	}
	u.recheck()
}

// checkNow looks up whether MariaDB is running, in the background, and
// notes changes Keel didn't make, like a stop from the command line.
func (u *ui) checkNow() {
	go func() {
		if !u.mu.TryLock() {
			return // a start or stop is running; it will report the result
		}
		pid := u.server.RunningPID()
		repairMode := pid != 0 && u.server.InRepairMode(pid)
		setUp := u.server.IsSetUp()
		u.mu.Unlock()
		clean, why := u.server.LastShutdown() // reads files only; safe without the lock
		fyne.Do(func() {
			u.setUp = setUp
			u.noteRepairMode(repairMode)
			u.applyCheck(pid, clean, why)
		})
	}()
}

// noteRepairMode records whether MariaDB is stuck in repair mode, and says
// so once when it's first noticed.
func (u *ui) noteRepairMode(on bool) {
	if u.busy || on == u.repairMode {
		return
	}
	u.repairMode = on
	if on {
		u.log(warn, "MariaDB is still in repair mode, without account checks, because a repair was interrupted. Stop it, then start it again.")
	}
	u.show(u.pidNow)
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
		u.log(warn, "The next start will check MariaDB: "+why)
	}
	u.lastWhy = why
	u.showShutdown(pid, clean, why)
}

// showShutdown shows how MariaDB last shut down, while it's stopped: a
// line in the status bar when it was clean, a banner when it wasn't.
func (u *ui) showShutdown(pid int, clean bool, why string) {
	stopped := pid == 0
	if stopped && !clean {
		u.bannerText.SetText(why + " Before MariaDB opens its data, Keel makes sure there's a copy to fall back on, then checks every table.")
		u.banner.Show()
	} else {
		u.banner.Hide()
	}
	switch {
	case stopped && !u.setUp:
		u.shutdown.SetText("Not set up yet: the first start creates MariaDB's data")
		u.shutdown.Show()
		u.shutdownIcon.Hide()
	case stopped && clean:
		u.shutdown.SetText("Last shutdown was clean")
		u.shutdown.Show()
		u.shutdownIcon.Show()
	default:
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
	u.later(2*time.Second, func() {
		u.copyLog.SetText("Copy")
		u.copyLog.SetIcon(theme.ContentCopyIcon())
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

// repairPrompt describes the damage a recovery found, for the repair offer.
func repairPrompt(rec *mariadb.Recovery) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\nMariaDB started, but %d of %d tables have problems:\n", rec.Reason, len(rec.Problems), rec.Checked)
	for i, p := range rec.Problems {
		if i == 8 {
			fmt.Fprintf(&b, "…and %d more (see the activity log)\n", len(rec.Problems)-8)
			break
		}
		fmt.Fprintf(&b, "• %s: %s\n", p.Table, p.Message)
	}
	if rec.Snapshot != "" {
		fmt.Fprintf(&b, "\nA copy of the data from before recovery is in:\n%s", rec.Snapshot)
	}
	return strings.TrimRight(b.String(), "\n")
}
