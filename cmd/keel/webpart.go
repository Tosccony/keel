package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"

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
}

func newWebService(opts setup.Options) *webService {
	w := &webService{opts: opts, settings: opts.Settings()}
	rt, err := opts.WebRuntime()
	if err != nil {
		w.err = err
		return w
	}
	w.server = &web.Server{
		Runtime: rt,
		Folder:  opts.WebFolder(),
		Port:    w.settings.Port(setup.PartWeb, setup.DefaultPorts[setup.PartWeb]),
		FPMPort: w.settings.Port(setup.PartPHP, setup.DefaultPorts[setup.PartPHP]),
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
	sites := w.settings.Sites()
	if sites == "" {
		return "", errors.New("choose a websites folder first")
	}
	var notes string
	port, note, err := w.opts.MovePort(ctx, setup.PartWeb, "the web server", w.server.Port)
	if err != nil {
		return "", err
	}
	notes += note
	fpm := w.server.FPMPort
	if w.server.PHPFPM != "" {
		if fpm, note, err = w.opts.MovePort(ctx, setup.PartPHP, "PHP", fpm); err != nil {
			return notes, err
		}
		notes = joinNotes(notes, note)
	}
	mailPort := w.settings.Port(setup.PartMail, setup.DefaultPorts[setup.PartMail])
	mailWeb := w.settings.Port(setup.PartMailWeb, setup.DefaultPorts[setup.PartMailWeb])
	s := w.server
	s.Sites, s.Port, s.FPMPort = sites, port, fpm
	s.MariaDBPort = w.opts.MariaDBPort()
	s.MariaDBSocket = w.opts.Server().Socket
	s.MailPort, s.MailInbox = mailPort, fmt.Sprintf("http://localhost:%d/", mailWeb)
	s.Sendmail = sendmailCommand(mailPort)
	s.Timezone = setup.Timezone()
	if wrote, err := web.WriteWelcome(sites); err == nil && wrote {
		notes = joinNotes(notes, "The websites folder was empty, so Keel put a welcome page in it (index.php).")
	}
	return notes, s.Start(ctx)
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
	return []*fyne.MenuItem{
		item("Apache settings", theme.SettingsIcon(), w.server.CustomConf(), true),
		item("PHP settings", theme.SettingsIcon(), w.server.CustomIni(), true),
		fyne.NewMenuItemSeparator(),
		item("Apache error log", theme.DocumentIcon(), w.server.ErrorLog(), false),
		item("PHP error log", theme.DocumentIcon(), w.server.PHPErrorLog(), false),
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

func joinNotes(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + " " + b
}
