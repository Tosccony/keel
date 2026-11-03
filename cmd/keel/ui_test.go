package main

import (
	"errors"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"github.com/Tosccony/keel/internal/mariadb"
)

func newTestUI(t *testing.T) (*ui, fyne.Window) {
	t.Helper()
	return newThemedTestUI(t, keelTheme{})
}

func newThemedTestUI(t *testing.T, th fyne.Theme) (*ui, fyne.Window) {
	t.Helper()
	a := test.NewTempApp(t)
	a.Settings().SetTheme(th)
	dir := t.TempDir()
	server := &mariadb.Server{DataDir: filepath.Join(dir, "data"), Port: 3307, LogFile: filepath.Join(dir, "log")}
	w := test.NewTempWindow(t, nil)
	u := newUI(w, server)
	w.SetContent(u.content())
	w.Resize(fyne.NewSize(780, 540))
	return u, w
}

func TestRowFollowsServerState(t *testing.T) {
	u, _ := newTestUI(t)
	if u.status.Text != "Stopped" || u.action.Text != "Start" {
		t.Fatalf("new window shows %q / %q, want Stopped / Start", u.status.Text, u.action.Text)
	}

	u.applyCheck(4242, false, "running")
	if u.status.Text != "Running" || u.action.Text != "Stop" || !strings.Contains(u.detail.Text, "PID 4242") {
		t.Errorf("after finding PID 4242: %q / %q / %q", u.status.Text, u.action.Text, u.detail.Text)
	}

	u.showBusy("Stopping…")
	if !u.action.Disabled() || !u.spinner.Visible() {
		t.Error("the button should be disabled and the spinner shown while stopping")
	}

	u.applyCheck(0, true, "")
	if u.status.Text != "Stopped" || u.action.Disabled() || u.spinner.Visible() {
		t.Errorf("after the server disappeared: %q, disabled=%v, spinner=%v",
			u.status.Text, u.action.Disabled(), u.spinner.Visible())
	}
	if lines := u.logLines(); len(lines) != 2 {
		t.Errorf("expected 2 log lines (outside start, outside stop), got %d: %q", len(lines), lines)
	}
}

func TestBusyIgnoresBackgroundChecks(t *testing.T) {
	u, _ := newTestUI(t)
	u.busy = true
	u.showBusy("Starting…")
	u.applyCheck(4242, false, "")
	if u.status.Text != "Starting…" {
		t.Errorf("a background check overwrote the busy state: %q", u.status.Text)
	}
}

func TestShutdownStatusIsShown(t *testing.T) {
	u, _ := newTestUI(t)

	u.applyCheck(0, true, "")
	if !u.shutdown.Visible() || !strings.Contains(u.shutdown.Text, "clean") || u.banner.Visible() {
		t.Errorf("clean: status bar %q (shown %v), banner shown %v", u.shutdown.Text, u.shutdown.Visible(), u.banner.Visible())
	}

	u.applyCheck(0, false, "MariaDB was force-stopped.")
	if !u.banner.Visible() || !strings.Contains(u.bannerText.Text, "force-stopped") || u.shutdown.Visible() {
		t.Errorf("unclean: banner %q (shown %v), status bar shown %v", u.bannerText.Text, u.banner.Visible(), u.shutdown.Visible())
	}
	u.applyCheck(0, false, "MariaDB was force-stopped.") // same reason again
	if n := countContaining(u.logLines(), "wasn't clean"); n != 1 {
		t.Errorf("the unclean reason was logged %d times, want once: %q", n, u.logLines())
	}

	u.applyCheck(99, false, "running")
	if u.banner.Visible() || u.shutdown.Visible() {
		t.Error("while running, neither the banner nor the last-shutdown line should show")
	}
}

func TestOutsideStopIsLoggedByKind(t *testing.T) {
	u, _ := newTestUI(t)
	u.applyCheck(10, false, "")
	u.applyCheck(0, false, "MariaDB was still running when it went away.")
	if countContaining(u.logLines(), "stopped without Keel asking") != 1 {
		t.Errorf("a crash should be logged as an unexpected stop: %q", u.logLines())
	}
	if u.entries[1].lvl != warn {
		t.Errorf("an unexpected stop should be marked as a warning, got level %d", u.entries[1].lvl)
	}

	u.applyCheck(11, false, "")
	u.applyCheck(0, true, "")
	if countContaining(u.logLines(), "stopped outside this window") != 1 {
		t.Errorf("a clean stop from elsewhere should be logged plainly: %q", u.logLines())
	}
}

func TestRecoveryDialogsDontPanic(t *testing.T) {
	u, _ := newTestUI(t)
	var problems []mariadb.TableProblem
	for i := 0; i < 12; i++ {
		problems = append(problems, mariadb.TableProblem{Table: "shop.t", Message: "crashed"})
	}
	for _, rec := range []*mariadb.Recovery{
		{Reason: "MariaDB was force-stopped.", Snapshot: "/backups/pre-recovery-x", Checked: 31},
		{Reason: "No record.", Checked: 31, Problems: problems},
		{Reason: "No record.", CheckErr: errors.New("connection lost")},
	} {
		u.showRecovery(rec)
	}
	u.confirmStartWithoutCopy(errors.New("mariadb: couldn't save a copy of the data folder (not enough free space)"))
}

func TestLogIsCapped(t *testing.T) {
	u, _ := newTestUI(t)
	for i := 0; i < maxLogLines+logTrim+1; i++ {
		u.log(info, "line")
	}
	if n := len(u.entries); n != maxLogLines {
		t.Errorf("log has %d lines, want %d", n, maxLogLines)
	}
}

func TestCopyActivity(t *testing.T) {
	u, _ := newTestUI(t)
	u.log(info, "first")
	u.log(bad, "second")
	u.copyActivity()
	got := fyne.CurrentApp().Clipboard().Content()
	if !strings.Contains(got, "first") || !strings.Contains(got, "second") || strings.Count(got, "\n") != 1 {
		t.Errorf("clipboard = %q", got)
	}
	if u.copyLog.Text != "Copied" {
		t.Errorf("button says %q after copying", u.copyLog.Text)
	}
}

func TestProgressLevel(t *testing.T) {
	for msg, want := range map[string]level{
		"Starting MariaDB on port 3307.":                              info,
		"Saving a copy of the data folder before MariaDB touches it…": info,
		"Saved a copy to /backups/pre-recovery-x":                     good,
		"MariaDB is ready (PID 14371).":                               good,
		"Recovered. All 31 tables passed their check.":                good,
		"The last shutdown wasn't clean. MariaDB was force-stopped.":  warn,
		"Couldn't check the tables: connection lost":                  warn,
		"2 of 31 tables have problems:":                               warn,
		"  shop.orders: marked as crashed":                            bad,
	} {
		if got := progressLevel(msg); got != want {
			t.Errorf("progressLevel(%q) = %d, want %d", msg, got, want)
		}
	}
}

// TestThemeHasBothModes checks Keel's own colours exist in light and dark
// mode, and that the two modes really differ.
func TestThemeHasBothModes(t *testing.T) {
	th := keelTheme{}
	for _, name := range []fyne.ThemeColorName{
		colorBar, colorCard, colorCardBorder, colorWarnFill, colorWarnEdge,
		theme.ColorNameBackground, theme.ColorNameForeground, theme.ColorNamePrimary,
	} {
		dark, light := th.Color(name, theme.VariantDark), th.Color(name, theme.VariantLight)
		if _, ok := darkColors[name]; !ok {
			t.Errorf("%s has no dark colour", name)
		}
		if _, ok := lightColors[name]; !ok {
			t.Errorf("%s has no light colour", name)
		}
		if sameColor(dark, light) {
			t.Errorf("%s is the same in light and dark mode", name)
		}
	}
}

// fixedVariant shows Keel's theme in one mode regardless of the system,
// for screenshots.
type fixedVariant struct {
	keelTheme
	v fyne.ThemeVariant
}

func (f fixedVariant) Color(n fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	return f.keelTheme.Color(n, f.v)
}

// TestScreenshot saves pictures of the window when KEEL_SCREENSHOT is set
// to a folder. It's for checking the look without opening the app.
func TestScreenshot(t *testing.T) {
	dir := os.Getenv("KEEL_SCREENSHOT")
	if dir == "" {
		t.Skip("set KEEL_SCREENSHOT=/path/to/folder to save screenshots")
	}
	for _, mode := range []struct {
		name string
		v    fyne.ThemeVariant
	}{{"dark", theme.VariantDark}, {"light", theme.VariantLight}} {
		u, w := newThemedTestUI(t, fixedVariant{v: mode.v})
		u.server.DataDir = "/Users/kody/personal/keel-lab/data"
		u.folder.SetText(u.server.DataDir)
		u.setVersion("13.0.2")
		u.log(info, "Watching the MariaDB in /Users/kody/personal/keel-lab/data.")
		u.applyCheck(0, false, "MariaDB was still running when it went away (a crash, a power cut, or a kill).")
		save(t, w, filepath.Join(dir, mode.name+"-stopped.png"))

		for _, msg := range []string{
			"The last shutdown wasn't clean. MariaDB was still running when it went away.",
			"Saving a copy of the data folder before MariaDB touches it…",
			"Saved a copy to /Users/kody/personal/keel-lab/backups/pre-recovery-2026-11-03-121500",
			"Starting MariaDB on port 3307.",
			"MariaDB is ready (PID 14371).",
			"Checking every table…",
			"Recovered. All 31 tables passed their check.",
		} {
			u.log(progressLevel(msg), msg)
		}
		u.applyCheck(14371, true, "")
		save(t, w, filepath.Join(dir, mode.name+"-running.png"))
	}
}

func save(t *testing.T, w fyne.Window, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	// The real window re-lays out every frame. The test canvas doesn't, so
	// do it by hand: twice, because wrapped text only knows its height
	// after the first pass gives it a width.
	w.Content().Refresh()
	w.Content().Refresh()
	if err := png.Encode(f, w.Canvas().Capture()); err != nil {
		t.Fatal(err)
	}
}

func sameColor(a, b color.Color) bool {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

func countContaining(lines []string, s string) int {
	n := 0
	for _, l := range lines {
		if strings.Contains(l, s) {
			n++
		}
	}
	return n
}
