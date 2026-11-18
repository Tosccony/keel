package main

import (
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"

	"github.com/Tosccony/keel/internal/setup"
)

// withAllParts lays the window out as Keel opens it: all five services.
func withAllParts(t *testing.T, size fyne.Size) (*ui, fyne.Window) {
	t.Helper()
	u, w := newTestUI(t)
	home := t.TempDir()
	opts := setup.Options{Home: home}
	st := setup.OpenSettings(filepath.Join(home, "keel-settings.json"))
	u.web = newPart(u.window, newWebService(opts), st)
	u.parts = []*part{u.web,
		newPart(u.window, newMailService(opts), st),
		newPart(u.window, newFTPService(opts), st),
		newPart(u.window, newTomcatService(opts), st)}
	w.SetContent(u.content())
	w.Resize(size)
	w.Content().Refresh()
	w.Content().Refresh()
	return u, w
}

// The activity log keeps room for several lines at the size Keel opens
// at, even with every service shown; it used to get about one line until
// the window was dragged bigger.
func TestActivityShowsAtOpeningSize(t *testing.T) {
	u, _ := withAllParts(t, fyne.NewSize(820, 720))
	if h := u.logList.Size().Height; h < 100 {
		t.Errorf("the activity log is %.0f high at the opening size; it needs room for several lines", h)
	}
}

// In a tall window every service shows without scrolling, and the extra
// height goes to the activity log.
func TestTallWindowShowsEveryServiceAndMoreActivity(t *testing.T) {
	short, _ := withAllParts(t, fyne.NewSize(820, 720))
	tall, _ := withAllParts(t, fyne.NewSize(820, 1100))
	if s := tall.services; s.Size().Height < s.Content.MinSize().Height {
		t.Errorf("services need %.0f but get %.0f in a tall window", s.Content.MinSize().Height, s.Size().Height)
	}
	if tall.logList.Size().Height <= short.logList.Size().Height {
		t.Errorf("the activity log didn't grow with the window: %.0f then %.0f", short.logList.Size().Height, tall.logList.Size().Height)
	}
}

// Messages about the tray name only this system's: never "the menu bar
// on a Mac" on Windows.
func TestTrayNameFitsTheSystem(t *testing.T) {
	for goos, want := range map[string]string{"darwin": "the menu bar", "windows": "the system tray (by the clock)", "linux": "the system tray"} {
		if got := trayName(goos); got != want {
			t.Errorf("%s: %q, want %q", goos, got, want)
		}
	}
}
