package main

import (
	"context"
	"errors"
	"net"
	"net/smtp"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"github.com/Tosccony/keel/internal/setup"
)

// fakeService is a part that does what the test says.
type fakeService struct {
	running  bool
	startErr error
	note     string
	starts   int
}

func (f *fakeService) ID() string          { return "fake" }
func (f *fakeService) Name() string        { return "Fake" }
func (f *fakeService) Icon() fyne.Resource { return theme.InfoIcon() }
func (f *fakeService) Running() bool       { return f.running }
func (f *fakeService) Detail() string      { return "Port 1234" }
func (f *fakeService) OpenURL() string     { return "http://localhost:1234/" }
func (f *fakeService) Menu() []*fyne.MenuItem {
	return []*fyne.MenuItem{fyne.NewMenuItem("Do a thing", func() {})}
}
func (f *fakeService) Start(context.Context) (string, error) {
	f.starts++
	if f.startErr == nil {
		f.running = true
	}
	return f.note, f.startErr
}
func (f *fakeService) Stop(context.Context) error { f.running = false; return nil }

func newTestPart(t *testing.T, svc service) (*part, *setup.Settings) {
	t.Helper()
	u, _ := newTestUI(t)
	st := setup.OpenSettings(filepath.Join(t.TempDir(), "keel-settings.json"))
	return newPart(u.window, svc, st), st
}

func TestPartStartsAndStops(t *testing.T) {
	f := &fakeService{note: "Port 1234 is in use by mysqld (PID 9), so Fake will use port 1235 from now on."}
	p, _ := newTestPart(t, f)
	if p.card.status.Text != "Stopped" || p.card.open.Visible() {
		t.Fatalf("new part: %q, open shown %v", p.card.status.Text, p.card.open.Visible())
	}
	p.busy = true
	p.card.showBusy("Starting…")
	if !p.card.action.Disabled() || !p.card.more.Disabled() {
		t.Error("buttons should be disabled while starting")
	}
	p.finishStart(p.runStart())
	if p.card.status.Text != "Running" || p.card.action.Text != "Stop" || !p.card.open.Visible() || p.busy {
		t.Errorf("after start: %q / %q, open %v", p.card.status.Text, p.card.action.Text, p.card.open.Visible())
	}
	lines := p.logLines()
	if countContaining(lines, "Fake will use port 1235") != 1 || countContaining(lines, "Fake is running: Port 1234.") != 1 {
		t.Errorf("log: %q", lines)
	}
	p.finishStop(p.runStop())
	if p.card.status.Text != "Stopped" || p.card.open.Visible() || countContaining(p.logLines(), "Fake stopped.") != 1 {
		t.Errorf("after stop: %q, log %q", p.card.status.Text, p.logLines())
	}
}

func TestPartStartFailureIsShown(t *testing.T) {
	f := &fakeService{startErr: errors.New("port 25 needs admin rights")}
	p, _ := newTestPart(t, f)
	p.finishStart(p.runStart())
	if p.card.status.Text != "Stopped" || p.running {
		t.Errorf("after a failed start: %q", p.card.status.Text)
	}
	if countContaining(p.logLines(), "Couldn't start Fake: port 25 needs admin rights") != 1 ||
		p.win.Canvas().Overlays().Top() == nil {
		t.Errorf("the failure should be logged and shown: %q", p.logLines())
	}
}

func TestPartAutostart(t *testing.T) {
	f := &fakeService{}
	p, st := newTestPart(t, f)
	menu := p.menu()
	last := menu.Items[len(menu.Items)-1]
	if last.Label != "Start when Keel opens" || last.Checked || menu.Items[0].Label != "Do a thing" {
		t.Fatalf("menu: %q checked=%v", last.Label, last.Checked)
	}
	p.autostart()
	if f.starts != 0 {
		t.Error("autostart started a part that isn't set to")
	}
	last.Action()
	if !st.Autostart("fake") || !p.menu().Items[len(menu.Items)-1].Checked {
		t.Error("the menu item didn't turn autostart on")
	}
	p.busy = true // autostart starts in the background; check the decision only
	p.autostart()
	p.busy = false
	if f.starts != 0 {
		t.Error("autostart shouldn't start a part that's busy")
	}
}

// The real mail catcher, started and stopped from its card.
func TestMailPart(t *testing.T) {
	home := t.TempDir()
	opts := setup.Options{Home: home}
	st := opts.Settings()
	st.SetPort(setup.PartMail, freeTCPPort(t))
	st.SetPort(setup.PartMailWeb, freeTCPPort(t))
	svc := newMailService(opts)
	p, _ := newTestPart(t, svc)
	p.finishStart(p.runStart())
	t.Cleanup(func() { svc.Stop(context.Background()) })
	if !p.running || !strings.Contains(p.card.detail.Text, "SMTP port "+strconv.Itoa(svc.catcher.SMTPPort)) {
		t.Fatalf("mail catcher: running %v, detail %q, log %q", p.running, p.card.detail.Text, p.logLines())
	}
	err := smtp.SendMail("127.0.0.1:"+strconv.Itoa(svc.catcher.SMTPPort), nil, "a@example.test",
		[]string{"b@example.test"}, []byte("Subject: hi\r\n\r\nhello\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(filepath.Join(home, "mail")); len(entries) != 1 {
		t.Errorf("caught %d messages in %s, want 1", len(entries), filepath.Join(home, "mail"))
	}
	p.finishStop(p.runStop())
	if p.running || svc.Running() {
		t.Error("still running after Stop")
	}
}

func TestUseSitesCreatesAndRemembersTheFolder(t *testing.T) {
	u, _ := newTestUI(t)
	st := setup.OpenSettings(filepath.Join(t.TempDir(), "keel-settings.json"))
	dir := filepath.Join(t.TempDir(), "Documents", "Keel", "htdocs")
	if err := u.useSites(st, dir); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("folder not created: %v", err)
	}
	if st.Sites() != dir || !strings.Contains(u.folder.Text, dir) {
		t.Errorf("saved %q, status bar %q", st.Sites(), u.folder.Text)
	}
	u.askSites(st, dir, func() {}) // the dialog itself opens without errors
}

func TestMariaDBMenuOffersAutostart(t *testing.T) {
	u, _ := newTestUI(t)
	u.settings = setup.OpenSettings(filepath.Join(t.TempDir(), "keel-settings.json"))
	items := u.moreMenu().Items
	last := items[len(items)-1]
	if last.Label != "Start when Keel opens" {
		t.Fatalf("last item %q", last.Label)
	}
	last.Action()
	if !u.settings.Autostart(setup.PartMariaDB) {
		t.Error("MariaDB's autostart wasn't saved")
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
