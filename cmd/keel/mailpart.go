package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"github.com/Tosccony/keel/internal/mail"
	"github.com/Tosccony/keel/internal/setup"
)

// mailService runs the mail catcher inside Keel. It stops when Keel
// closes; nothing else needs it then.
type mailService struct {
	opts    setup.Options
	catcher *mail.Catcher
}

func newMailService(opts setup.Options) *mailService {
	st := opts.Settings()
	return &mailService{opts: opts, catcher: &mail.Catcher{
		Dir:      filepath.Join(opts.Home, "mail"),
		SMTPPort: st.Port(setup.PartMail, setup.DefaultPorts[setup.PartMail]),
		WebPort:  st.Port(setup.PartMailWeb, setup.DefaultPorts[setup.PartMailWeb]),
	}}
}

func (m *mailService) ID() string          { return setup.PartMail }
func (m *mailService) Name() string        { return "Mail catcher" }
func (m *mailService) Icon() fyne.Resource { return theme.MailComposeIcon() }
func (m *mailService) Running() bool       { return m.catcher.Running() }
func (m *mailService) OpenURL() string     { return m.catcher.InboxURL() }

func (m *mailService) Detail() string {
	return fmt.Sprintf("SMTP port %d  ·  Inbox on port %d", m.catcher.SMTPPort, m.catcher.WebPort)
}

func (m *mailService) Start(ctx context.Context) (string, error) {
	var notes []string
	smtp, note, err := m.opts.MovePort(ctx, setup.PartMail, "the mail catcher", m.catcher.SMTPPort)
	if err != nil {
		return "", err
	}
	notes = append(notes, note)
	web, note, err := m.opts.MovePort(ctx, setup.PartMailWeb, "the mail inbox page", m.catcher.WebPort)
	if err != nil {
		return strings.TrimSpace(strings.Join(notes, " ")), err
	}
	notes = append(notes, note)
	m.catcher.SMTPPort, m.catcher.WebPort = smtp, web
	return strings.TrimSpace(strings.Join(notes, " ")), m.catcher.Start()
}

func (m *mailService) Stop(ctx context.Context) error { return m.catcher.Stop(ctx) }

func (m *mailService) Menu() []*fyne.MenuItem {
	clear := fyne.NewMenuItem("Delete all caught mail", func() { m.catcher.DeleteAll() })
	clear.Icon = theme.DeleteIcon()
	return []*fyne.MenuItem{clear}
}
