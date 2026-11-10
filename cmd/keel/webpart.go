package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"

	"github.com/Tosccony/keel/internal/extras"
	"github.com/Tosccony/keel/internal/fetch"
	"github.com/Tosccony/keel/internal/setup"
	"github.com/Tosccony/keel/internal/web"
)

// webService runs Apache with PHP. Like MariaDB, it keeps running after
// Keel's window closes, and Keel finds it again when it opens.
type webService struct {
	opts     setup.Options
	settings *setup.Settings
	server   *web.Server // nil when no Apache and PHP were found
	err      error       // why server is nil

	mu          sync.Mutex
	apache, php string // versions, read in the background
	progress    func(string)
	runTask     func(label string, work func(context.Context) (string, error))
}

func (w *webService) SetProgress(f func(string)) { w.mu.Lock(); w.progress = f; w.mu.Unlock() }
func (w *webService) SetTaskRunner(f func(string, func(context.Context) (string, error))) {
	w.runTask = f
}

func (w *webService) say(msg string) {
	w.mu.Lock()
	f := w.progress
	w.mu.Unlock()
	if f != nil {
		f(msg)
	}
}

// setPerl turns Perl scripts on or off. Turning them on for the first time
// on Windows downloads Strawberry Perl.
func (w *webService) setPerl(ctx context.Context, on bool) (string, error) {
	if on && w.opts.Perl() == "" {
		if runtime.GOOS != "windows" {
			return "", errors.New("Perl isn't installed on this computer")
		}
		d := extras.Perl
		w.say(fmt.Sprintf("Downloading Strawberry Perl %s (%d MB)…", d.Version, d.SizeMB))
		last := -1
		err := fetch.Install(ctx, fetch.Item{Name: "Strawberry Perl", URLs: d.URLs, Checksum: d.Checksum}, w.opts.PerlHome(),
			func(done, total int64) {
				if total > 0 {
					if pct := int(done * 100 / total); pct/10 != last/10 {
						last = pct
						w.say(fmt.Sprintf("Downloading Strawberry Perl: %d%%", pct))
					}
				}
			})
		if err != nil {
			return "", err
		}
	}
	if err := w.settings.SetPerl(on); err != nil {
		return "", err
	}
	switch {
	case !on:
		return "Perl scripts are off.", nil
	case runtime.GOOS == "windows":
		return "Perl scripts are on: .pl and .cgi files in the websites folder run with Strawberry Perl.", nil
	default:
		return "Perl scripts are on: .pl and .cgi files in the websites folder run as CGI. Start each with #!/usr/bin/perl and make it executable (chmod +x).", nil
	}
}

func newWebService(opts setup.Options) *webService {
	w := &webService{opts: opts, settings: opts.Settings()}
	w.server, w.err = opts.Web()
	if w.err != nil {
		return w
	}
	go func() {
		apache, php := w.server.Versions()
		w.mu.Lock()
		w.apache, w.php = apache, php
		w.mu.Unlock()
	}()
	return w
}

func (w *webService) ID() string          { return setup.PartWeb }
func (w *webService) Name() string        { return "Web server" }
func (w *webService) Icon() fyne.Resource { return theme.ComputerIcon() }

func (w *webService) Running() bool { return w.server != nil && w.server.Running() }

func (w *webService) OpenURL() string {
	if w.server == nil {
		return ""
	}
	return fmt.Sprintf("http://localhost:%d/", w.server.Port)
}

func (w *webService) Detail() string {
	if w.server == nil {
		return "Apache and PHP weren't found"
	}
	w.mu.Lock()
	versions := ""
	if w.apache != "" {
		versions += "  ·  Apache " + w.apache
	}
	if w.php != "" {
		versions += "  ·  PHP " + w.php
	}
	w.mu.Unlock()
	return fmt.Sprintf("http://localhost:%d%s", w.server.Port, versions)
}

func (w *webService) Start(ctx context.Context) (string, error) {
	if w.server == nil {
		return "", w.err
	}
	mailPort := w.settings.Port(setup.PartMail, setup.DefaultPorts[setup.PartMail])
	note, err := w.opts.PrepareWeb(ctx, w.server, sendmailCommand(mailPort))
	if err != nil {
		return note, err
	}
	return note, w.server.Start(ctx)
}

func (w *webService) Stop(ctx context.Context) error {
	if w.server == nil {
		return nil
	}
	return w.server.Stop(ctx)
}

func (w *webService) Menu() []*fyne.MenuItem {
	if w.server == nil {
		return nil
	}
	item := func(label string, icon fyne.Resource, path string, isUserFile bool) *fyne.MenuItem {
		it := fyne.NewMenuItem(label, func() {
			if isUserFile {
				w.server.WriteUserFiles()
			}
			openFile(path)
		})
		it.Icon = icon
		return it
	}
	pma := fyne.NewMenuItem("phpMyAdmin", func() {
		if link, err := url.Parse(fmt.Sprintf("http://localhost:%d/phpmyadmin/", w.server.Port)); err == nil {
			fyne.CurrentApp().OpenURL(link)
		}
	})
	pma.Icon = theme.StorageIcon()
	pma.Disabled = w.opts.PHPMyAdmin() == ""
	on := w.settings.Perl()
	perl := fyne.NewMenuItem("Run Perl scripts (.pl, .cgi)", func() {
		if w.runTask != nil {
			w.runTask(map[bool]string{true: "Turning Perl off", false: "Turning Perl on"}[on],
				func(ctx context.Context) (string, error) { return w.setPerl(ctx, !on) })
		}
	})
	perl.Checked = on
	return []*fyne.MenuItem{
		pma,
		fyne.NewMenuItemSeparator(),
		item("Apache settings", theme.SettingsIcon(), w.server.CustomConf(), true),
		item("PHP settings", theme.SettingsIcon(), w.server.CustomIni(), true),
		fyne.NewMenuItemSeparator(),
		item("Apache error log", theme.DocumentIcon(), w.server.ErrorLog(), false),
		item("PHP error log", theme.DocumentIcon(), w.server.PHPErrorLog(), false),
		fyne.NewMenuItemSeparator(),
		perl,
	}
}

// sendmailCommand is what PHP runs to send mail on macOS and Linux: this
// program in its --sendmail mode, which hands the message to the mail
// catcher. Windows' PHP talks SMTP itself, so it's "" there.
func sendmailCommand(mailPort int) string {
	if filepath.Separator == '\\' {
		return ""
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	// PHP runs it through sh, so the path is quoted.
	return fmt.Sprintf("'%s' --sendmail --port %d -t -i", exe, mailPort)
}

// openFile opens a file in the program the system uses for it. A missing
// file is created empty first, so a log that hasn't been written yet still
// opens.
func openFile(path string) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, nil, 0o644)
	}
	if link, err := url.Parse(storage.NewFileURI(path).String()); err == nil {
		fyne.CurrentApp().OpenURL(link)
	}
}

// openFolder opens a folder in Finder, Explorer or the file manager,
// creating it first if needed.
func openFolder(dir string) {
	os.MkdirAll(dir, 0o755)
	if link, err := url.Parse(storage.NewFileURI(dir).String()); err == nil {
		fyne.CurrentApp().OpenURL(link)
	}
}

func joinNotes(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + " " + b
}
