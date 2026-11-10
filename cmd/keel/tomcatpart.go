package main

import (
	"context"
	"fmt"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"github.com/Tosccony/keel/internal/extras"
	"github.com/Tosccony/keel/internal/fetch"
	"github.com/Tosccony/keel/internal/setup"
	"github.com/Tosccony/keel/internal/tomcat"
)

// progressReporter is a service that tells the user how a long step is
// going, such as a download. The part sets the function.
type progressReporter interface {
	SetProgress(func(msg string))
}

// tomcatService runs Tomcat. The first start downloads it, and a Java
// runtime if the computer has none.
type tomcatService struct {
	opts     setup.Options
	settings *setup.Settings
	server   *tomcat.Server

	mu       sync.Mutex
	progress func(string)
	java     int // the Java version in use, once known
}

func newTomcatService(opts setup.Options) *tomcatService {
	st := opts.Settings()
	return &tomcatService{opts: opts, settings: st, server: &tomcat.Server{
		Home: opts.TomcatHome(),
		Base: opts.TomcatBase(),
		Port: st.Port(setup.PartTomcat, setup.DefaultPorts[setup.PartTomcat]),
	}}
}

func (t *tomcatService) ID() string          { return setup.PartTomcat }
func (t *tomcatService) Name() string        { return "Tomcat" }
func (t *tomcatService) Icon() fyne.Resource { return theme.GridIcon() }
func (t *tomcatService) Running() bool       { return t.server.Running() }

func (t *tomcatService) SetProgress(f func(string)) {
	t.mu.Lock()
	t.progress = f
	t.mu.Unlock()
}

func (t *tomcatService) say(msg string) {
	t.mu.Lock()
	f := t.progress
	t.mu.Unlock()
	if f != nil {
		f(msg)
	}
}

func (t *tomcatService) OpenURL() string { return fmt.Sprintf("http://localhost:%d/", t.server.Port) }

func (t *tomcatService) Detail() string {
	if !t.server.Installed() {
		return fmt.Sprintf("Not installed: the first start downloads Tomcat %s (%d MB)", extras.Tomcat.Version, extras.Tomcat.SizeMB)
	}
	d := fmt.Sprintf("http://localhost:%d  ·  Tomcat %s", t.server.Port, extras.Tomcat.Version)
	t.mu.Lock()
	if t.java > 0 {
		d += fmt.Sprintf("  ·  Java %d", t.java)
	}
	t.mu.Unlock()
	return d
}

func (t *tomcatService) Start(ctx context.Context) (string, error) {
	if !t.server.Installed() {
		if err := t.download(ctx, "Tomcat "+extras.Tomcat.Version, extras.Tomcat, t.opts.TomcatHome()); err != nil {
			return "", err
		}
	}
	java := t.opts.Java()
	if java == "" {
		d, ok := extras.Java()
		if !ok {
			return "", fmt.Errorf("Tomcat needs Java 11 or later, and Keel has no Java runtime for this computer; install one and try again")
		}
		if err := t.download(ctx, "Java "+d.Version+" for Tomcat", d, t.opts.JavaHome()); err != nil {
			return "", err
		}
		java = t.opts.KeelJava()
	}
	port, note, err := t.opts.MovePort(ctx, setup.PartTomcat, "Tomcat", t.server.Port)
	if err != nil {
		return "", err
	}
	t.server.Java, t.server.Port = java, port
	t.mu.Lock()
	t.java = tomcat.JavaVersion(java)
	t.mu.Unlock()
	return note, t.server.Start(ctx)
}

// download installs a pinned download, telling the user how it's going.
func (t *tomcatService) download(ctx context.Context, name string, d extras.Download, dest string) error {
	t.say(fmt.Sprintf("Downloading %s (%d MB)…", name, d.SizeMB))
	last := -1
	err := fetch.Install(ctx, fetch.Item{Name: name, URLs: d.URLs, Checksum: d.Checksum, Strip: 1}, dest,
		func(done, total int64) {
			if total <= 0 {
				return
			}
			if pct := int(done * 100 / total); pct/25 != last/25 { // every quarter
				last = pct
				t.say(fmt.Sprintf("Downloading %s: %d%%", name, pct))
			}
		})
	if err != nil {
		return err
	}
	t.say(name + " is installed.")
	return nil
}

func (t *tomcatService) Stop(ctx context.Context) error { return t.server.Stop(ctx) }

func (t *tomcatService) Menu() []*fyne.MenuItem {
	webapps := fyne.NewMenuItem("Open webapps folder", func() { openFolder(t.server.Webapps()) })
	webapps.Icon = theme.FolderOpenIcon()
	log := fyne.NewMenuItem("Tomcat log", func() { openFile(t.server.Log()) })
	log.Icon = theme.DocumentIcon()
	return []*fyne.MenuItem{webapps, log}
}
