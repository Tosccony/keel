package main

import (
	"context"
	"errors"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"github.com/Tosccony/keel/internal/ftp"
	"github.com/Tosccony/keel/internal/setup"
)

// ftpService runs the FTP server inside Keel, serving the websites folder.
type ftpService struct {
	opts     setup.Options
	settings *setup.Settings
	server   *ftp.Server
}

func newFTPService(opts setup.Options) *ftpService {
	st := opts.Settings()
	user, pass := st.FTPLogin()
	return &ftpService{opts: opts, settings: st, server: &ftp.Server{
		Port:     st.Port(setup.PartFTP, setup.DefaultPorts[setup.PartFTP]),
		User:     user,
		Password: pass,
	}}
}

func (f *ftpService) ID() string          { return setup.PartFTP }
func (f *ftpService) Name() string        { return "FTP server" }
func (f *ftpService) Icon() fyne.Resource { return theme.UploadIcon() }
func (f *ftpService) Running() bool       { return f.server.Running() }
func (f *ftpService) OpenURL() string     { return "" }

func (f *ftpService) Detail() string {
	return fmt.Sprintf("ftp://localhost:%d  ·  User %s  ·  Password %s", f.server.Port, f.server.User, f.server.Password)
}

func (f *ftpService) Start(ctx context.Context) (string, error) {
	sites := f.settings.Sites()
	if sites == "" {
		return "", errors.New("choose a websites folder first")
	}
	port, note, err := f.opts.MovePort(ctx, setup.PartFTP, "the FTP server", f.server.Port)
	if err != nil {
		return "", err
	}
	f.server.Root, f.server.Port = sites, port
	f.server.User, f.server.Password = f.settings.FTPLogin()
	return note, f.server.Start()
}

func (f *ftpService) Stop(context.Context) error { return f.server.Stop() }

func (f *ftpService) Menu() []*fyne.MenuItem {
	copyPass := fyne.NewMenuItem("Copy password", func() {
		fyne.CurrentApp().Clipboard().SetContent(f.server.Password)
	})
	copyPass.Icon = theme.ContentCopyIcon()
	return []*fyne.MenuItem{copyPass}
}
