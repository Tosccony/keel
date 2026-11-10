package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/Tosccony/keel/internal/mariadb"
	"github.com/Tosccony/keel/internal/setup"
)

// The MariaDB card's ⋯ menu (Back up now, Restore a copy, Repair, Check
// tables) and the dialogs that go with it.

// moreMenu builds the ⋯ menu for the current state. Back up, Restore and
// Repair need MariaDB stopped; if it's running, Keel stops it first and
// starts it again afterwards.
func (u *ui) moreMenu() *fyne.Menu {
	backup := fyne.NewMenuItem("Back up now", u.askBackUp)
	backup.Icon = theme.DocumentSaveIcon()
	restore := fyne.NewMenuItem("Restore a copy…", u.chooseRestore)
	restore.Icon = theme.HistoryIcon()
	repair := fyne.NewMenuItem("Repair…", func() { u.askRepair("", "") })
	repair.Icon = theme.SettingsIcon()
	check := fyne.NewMenuItem("Check tables", u.checkTables)
	check.Icon = theme.ConfirmIcon()
	check.Disabled = u.pidNow == 0
	folder := fyne.NewMenuItem("Open backups folder", u.openBackups)
	folder.Icon = theme.FolderOpenIcon()
	data := fyne.NewMenuItem("Open data folder", u.openDataFolder)
	data.Icon = theme.FolderOpenIcon()
	logItem := fyne.NewMenuItem("MariaDB log", func() { openFile(u.server.LogFile) })
	logItem.Icon = theme.DocumentIcon()
	if u.busy {
		for _, it := range []*fyne.MenuItem{backup, restore, repair, check} {
			it.Disabled = true
		}
	}
	items := []*fyne.MenuItem{backup, restore, repair, fyne.NewMenuItemSeparator(), check, logItem, data, folder}
	if u.settings != nil {
		auto := fyne.NewMenuItem("Start when Keel opens", func() {
			if err := u.settings.SetAutostart(setup.PartMariaDB, !u.settings.Autostart(setup.PartMariaDB)); err != nil {
				u.log(bad, "Couldn't save that: "+err.Error())
			}
		})
		auto.Checked = u.settings.Autostart(setup.PartMariaDB)
		items = append(items, fyne.NewMenuItemSeparator(), auto)
	}
	return fyne.NewMenu("", items...)
}

func (u *ui) showMoreMenu() {
	widget.ShowPopUpMenuAtRelativePosition(u.moreMenu(), u.win.Canvas(),
		fyne.NewPos(0, u.more.Size().Height), u.more)
}

// maintResult is what runMaintenance hands back to the UI thread.
type maintResult struct {
	stopErr error        // MariaDB wouldn't stop, so the work didn't run
	workErr error        // what the work returned
	start   *startResult // set if MariaDB was started afterwards
	port    int
	pid     int
}

// maintain runs work with MariaDB stopped: it stops MariaDB first if it's
// running, and afterwards starts it if startAfter says so. done reports
// the work's result on the UI thread.
func (u *ui) maintain(busyText string, startAfter func(wasRunning bool, workErr error) bool,
	work func(context.Context) error, done func(error)) {
	u.busy = true
	u.showBusy(busyText)
	go func() {
		r := u.runMaintenance(startAfter, work)
		fyne.Do(func() { u.finishMaintenance(r, done) })
	}()
}

// runMaintenance is maintain's work, off the UI thread.
func (u *ui) runMaintenance(startAfter func(bool, error) bool, work func(context.Context) error) maintResult {
	u.mu.Lock()
	defer u.mu.Unlock()
	ctx := context.Background()
	var r maintResult
	wasRunning := u.server.RunningPID() != 0
	if wasRunning {
		if _, err := u.server.Stop(ctx); err != nil && !errors.Is(err, mariadb.ErrNotRunning) {
			r.stopErr = err
		}
	}
	if r.stopErr == nil {
		r.workErr = work(ctx)
		if startAfter(wasRunning, r.workErr) {
			s := u.startLocked(ctx, false)
			r.start = &s
		}
	}
	r.port, r.pid = u.server.Port, u.server.RunningPID()
	return r
}

// finishMaintenance shows how it went, on the UI thread.
func (u *ui) finishMaintenance(r maintResult, done func(error)) {
	u.busy = false
	if r.stopErr != nil {
		u.port = r.port
		u.show(r.pid)
		u.recheck()
		u.log(bad, "MariaDB didn't stop, so Keel changed nothing: "+plain(r.stopErr))
		return
	}
	done(r.workErr)
	if r.start != nil {
		u.finishStart(*r.start)
		return
	}
	u.port = r.port
	u.show(r.pid)
	u.recheck()
}

// plain is an error's message without the package prefix.
func plain(err error) string { return strings.TrimPrefix(err.Error(), "mariadb: ") }

// whileRunningNote is added to confirmations when MariaDB will be stopped.
func (u *ui) whileRunningNote() string {
	if u.pidNow == 0 {
		return ""
	}
	return "\n\nMariaDB stops while this runs and starts again afterwards."
}

func (u *ui) askBackUp() {
	if u.pidNow == 0 {
		u.backUp()
		return
	}
	u.ask("Back up now?", "Keel copies the data folder to the backups folder."+u.whileRunningNote(),
		"Back up", "Cancel", widget.HighImportance, func(ok bool) {
			if ok {
				u.backUp()
			}
		})
}

func (u *ui) backUp() {
	var b mariadb.Backup
	u.maintain("Backing up…",
		func(wasRunning bool, _ error) bool { return wasRunning },
		func(context.Context) (err error) {
			b, err = u.server.BackUp()
			return err
		},
		func(err error) {
			if err != nil {
				u.log(bad, "Couldn't back up: "+plain(err))
				u.inform("Couldn't back up", plain(err))
				return
			}
			u.log(good, "Backed up to "+b.Name()+".")
		})
}

// backupLabels names each copy for the restore list: when it was taken and
// why. Two copies from the same minute get a number, so every label is
// different.
func backupLabels(list []mariadb.Backup) []string {
	labels := make([]string, len(list))
	seen := map[string]int{}
	for i, b := range list {
		l := b.Taken.Format("Mon 2 Jan 2006, 15:04") + "  ·  " + b.Kind.Label()
		seen[l]++
		if n := seen[l]; n > 1 {
			l += fmt.Sprintf(" (%d)", n)
		}
		labels[i] = l
	}
	return labels
}

func (u *ui) chooseRestore() {
	list, err := u.server.Backups()
	if err != nil {
		u.inform("Couldn't list the copies", err.Error())
		return
	}
	if len(list) == 0 {
		u.inform("No copies yet",
			"Keel saves a copy before every recovery, restore and repair. Use Back up now to save one yourself.")
		return
	}
	labels := backupLabels(list)
	choice := widget.NewRadioGroup(labels, nil)
	choice.Required = true
	choice.SetSelected(labels[0])
	intro := widget.NewLabel("Pick the copy to put back. Keel saves your current data first, so you can undo this." + u.whileRunningNote())
	intro.Wrapping = fyne.TextWrapWord
	scroll := container.NewVScroll(choice)
	scroll.SetMinSize(fyne.NewSize(0, 180))
	d := dialog.NewCustomConfirm("Restore a copy", "Restore", "Cancel",
		container.NewBorder(intro, nil, nil, nil, scroll),
		func(ok bool) {
			if !ok {
				return
			}
			for i, l := range labels {
				if l == choice.Selected {
					u.restore(list[i])
				}
			}
		}, u.win)
	d.Resize(fyne.NewSize(500, 380))
	d.Show()
}

func (u *ui) restore(b mariadb.Backup) {
	var saved string
	u.maintain("Restoring…",
		func(wasRunning bool, _ error) bool { return wasRunning },
		func(context.Context) (err error) {
			saved, err = u.server.Restore(b)
			return err
		},
		func(err error) {
			if err != nil {
				u.log(bad, "Couldn't restore: "+plain(err))
				u.inform("Couldn't restore", plain(err))
				return
			}
			u.log(good, "Restored "+b.Name()+".")
			if saved != "" {
				u.log(info, "The data from before the restore is saved as "+saved+". Restore that copy to undo.")
			}
		})
}

// askRepair confirms a repair. title and why, if set, say what went wrong.
func (u *ui) askRepair(title, why string) {
	if title == "" {
		title = "Repair MariaDB?"
	}
	if why != "" {
		why += "\n\n"
	}
	u.ask(title, why+
		"Keel can repair it: it saves a copy of your data first, starts MariaDB in repair mode, "+
		"fixes the damaged tables and log files it finds, and checks every table. It takes a minute or so."+
		u.whileRunningNote(),
		"Repair", "Not now", widget.HighImportance, func(ok bool) {
			if ok {
				u.repair()
			}
		})
}

func (u *ui) repair() {
	var rep *mariadb.RepairReport
	u.maintain("Repairing…",
		// Start MariaDB afterwards whenever the repair finished: that's what
		// the user was trying to do.
		func(_ bool, err error) bool { return err == nil },
		func(ctx context.Context) (err error) {
			rep, err = u.server.Repair(ctx)
			return err
		},
		func(err error) {
			title, body := repairSummary(rep, err)
			if err != nil {
				u.log(bad, "Repair didn't finish: "+firstLine(plain(err)))
			}
			u.inform(title, body)
		})
}

// repairSummary turns a repair's result into a dialog's title and text.
func repairSummary(rep *mariadb.RepairReport, err error) (title, body string) {
	var b strings.Builder
	switch {
	case err != nil:
		title = "The repair didn't finish"
		fmt.Fprintln(&b, firstLine(plain(err)))
	case len(rep.Remaining) > 0:
		title = "Some damage is left"
	case len(rep.Repaired) > 0 || rep.AriaReset:
		title = "Repaired"
	default:
		title = "Nothing needed repairing"
	}
	if rep != nil {
		if rep.AriaReset {
			fmt.Fprintln(&b, "Rebuilt Aria's damaged log files.")
		}
		if n := len(rep.Repaired); n > 0 {
			shown := rep.Repaired[:min(n, 8)]
			fmt.Fprintf(&b, "Repaired %s.\n", strings.Join(shown, ", ")+more(n-len(shown)))
		}
		if len(rep.Remaining) > 0 {
			fmt.Fprintln(&b, "Still damaged:")
			for i, p := range rep.Remaining {
				if i == 8 {
					fmt.Fprintf(&b, "…and %d more (see the activity log)\n", len(rep.Remaining)-8)
					break
				}
				fmt.Fprintf(&b, "• %s: %s\n", p.Table, p.Message)
			}
		} else if err == nil {
			fmt.Fprintf(&b, "All %d tables pass their check.\n", rep.Checked)
		}
		if rep.Copy != "" {
			fmt.Fprintf(&b, "\nA copy from before the repair is in:\n%s", rep.Copy)
		}
	}
	return title, strings.TrimRight(b.String(), "\n")
}

func more(n int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf(" and %d more", n)
}

func firstLine(s string) string {
	first, _, _ := strings.Cut(s, "\n")
	return first
}

// startErrorLine picks the line that explains a failed start from the log
// lines quoted in the error: a "Fatal error" if there is one, otherwise the
// first [ERROR]. It drops the timestamp.
func startErrorLine(err error) string {
	var first string
	for _, l := range strings.Split(err.Error(), "\n") {
		_, msg, ok := strings.Cut(l, "[ERROR] ")
		if !ok {
			continue
		}
		if strings.Contains(msg, "Fatal error") {
			return msg
		}
		if first == "" {
			first = msg
		}
	}
	return first
}

func (u *ui) checkTables() {
	u.busy = true
	u.showBusy("Checking…")
	go func() {
		u.mu.Lock()
		checked, problems, err := u.server.CheckTables(context.Background())
		pid := u.server.RunningPID()
		u.mu.Unlock()
		fyne.Do(func() {
			u.busy = false
			u.show(pid)
			u.recheck()
			switch {
			case err != nil:
				u.log(bad, "Couldn't check the tables: "+plain(err))
			case len(problems) == 0:
				u.log(good, fmt.Sprintf("All %d tables passed their check.", checked))
			default:
				u.reportProblems(checked, problems)
			}
		})
	}()
}

// reportProblems logs damaged tables and offers a repair.
func (u *ui) reportProblems(checked int, problems []mariadb.TableProblem) {
	u.log(warn, fmt.Sprintf("%d of %d tables have problems:", len(problems), checked))
	var b strings.Builder
	for i, p := range problems {
		u.log(bad, "  "+p.Table+": "+p.Message)
		if i < 8 {
			fmt.Fprintf(&b, "• %s: %s\n", p.Table, p.Message)
		}
	}
	if len(problems) > 8 {
		fmt.Fprintf(&b, "…and %d more (see the activity log)\n", len(problems)-8)
	}
	u.askRepair("Some tables need attention",
		fmt.Sprintf("%d of %d tables have problems:\n%s", len(problems), checked, strings.TrimRight(b.String(), "\n")))
}

func (u *ui) openBackups() {
	dir := u.server.BackupFolder()
	os.MkdirAll(dir, 0o755)
	link, err := url.Parse(storage.NewFileURI(dir).String())
	if err == nil {
		err = fyne.CurrentApp().OpenURL(link)
	}
	if err != nil {
		u.log(bad, "Couldn't open the backups folder: "+err.Error())
	}
}
