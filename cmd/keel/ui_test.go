package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/Tosccony/keel/internal/mariadb"
)

func newTestUI(t *testing.T) (*ui, fyne.Window) {
	t.Helper()
	test.NewTempApp(t)
	dir := t.TempDir()
	server := &mariadb.Server{DataDir: filepath.Join(dir, "data"), Port: 3307, LogFile: filepath.Join(dir, "log")}
	w := test.NewTempWindow(t, nil)
	u := newUI(w, server)
	w.SetContent(u.content())
	w.Resize(fyne.NewSize(720, 440))
	return u, w
}

func TestRowFollowsServerState(t *testing.T) {
	u, _ := newTestUI(t)
	if u.status.Text != "Stopped" || u.action.Text != "Start" {
		t.Fatalf("new window shows %q / %q, want Stopped / Start", u.status.Text, u.action.Text)
	}

	u.applyCheck(4242)
	if u.status.Text != "Running" || u.action.Text != "Stop" || u.pid.Text != "4242" {
		t.Errorf("after finding PID 4242: %q / %q / %q", u.status.Text, u.action.Text, u.pid.Text)
	}

	u.showBusy("Stopping…")
	if !u.action.Disabled() {
		t.Error("the button should be disabled while stopping")
	}

	u.busy = false
	u.applyCheck(0)
	if u.status.Text != "Stopped" || u.action.Disabled() {
		t.Errorf("after the server disappeared: %q, disabled=%v", u.status.Text, u.action.Disabled())
	}
	lines, _ := u.logData.Get()
	if len(lines) != 2 {
		t.Errorf("expected 2 log lines (outside start, outside stop), got %d: %q", len(lines), lines)
	}
}

func TestLogIsCapped(t *testing.T) {
	u, _ := newTestUI(t)
	for i := 0; i < maxLogLines+logTrim+1; i++ {
		u.log("Keel", "line")
	}
	if lines, _ := u.logData.Get(); len(lines) != maxLogLines {
		t.Errorf("log has %d lines, want %d", len(lines), maxLogLines)
	}
}

// TestScreenshot saves a picture of the window when KEEL_SCREENSHOT is set
// to a file path. It's for checking the layout without opening the app.
func TestScreenshot(t *testing.T) {
	path := os.Getenv("KEEL_SCREENSHOT")
	if path == "" {
		t.Skip("set KEEL_SCREENSHOT=/path/to/file.png to save a screenshot")
	}
	u, w := newTestUI(t)
	u.module.SetText("MariaDB 13.0.2")
	u.log("Keel", "Keel dev. Watching the MariaDB in /Users/kody/personal/keel-lab/data.")
	u.log("MariaDB", "Starting MariaDB on port 3307.")
	u.log("MariaDB", "MariaDB is ready (PID 14371).")
	u.applyCheck(14371)

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, w.Canvas().Capture()); err != nil {
		t.Fatal(err)
	}
}
