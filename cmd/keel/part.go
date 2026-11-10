package main

import (
	"context"
	"net/url"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"

	"github.com/Tosccony/keel/internal/setup"
)

// service is a part of Keel other than MariaDB, which has its own,
// richer controller (ui.go): the mail catcher, the FTP server, Apache, and
// so on. Its methods may be called off the UI thread, one at a time.
type service interface {
	ID() string // key in the settings file (setup.PartMail...)
	Name() string
	Icon() fyne.Resource
	// Start starts it. note, if not empty, tells the user something, such
	// as a port that had to move.
	Start(ctx context.Context) (note string, err error)
	Stop(ctx context.Context) error
	Running() bool
	Detail() string         // the line under the name: ports, addresses
	OpenURL() string        // what Open shows, or "" for nothing
	Menu() []*fyne.MenuItem // the part's own ⋯ items
}

// part shows a service as a card, and runs its start and stop off the UI
// thread. Its fields are only touched on the UI thread.
type part struct {
	*window
	svc      service
	settings *setup.Settings
	card     *serviceCard
	mu       sync.Mutex // one start or stop at a time
	busy     bool
	running  bool
}

func newPart(w *window, svc service, settings *setup.Settings) *part {
	p := &part{window: w, svc: svc, settings: settings}
	p.card = newServiceCard(svc.Name(), svc.Icon(), p.onAction, p.open, p.showMenu)
	p.show()
	return p
}

func (p *part) object() fyne.CanvasObject { return p.card.object() }

func (p *part) show() {
	if p.running {
		p.card.showRunning(p.svc.Detail(), p.svc.OpenURL() != "")
	} else {
		p.card.showStopped(p.svc.Detail())
	}
}

func (p *part) onAction() {
	if p.busy {
		return
	}
	if p.running {
		p.stop()
	} else {
		p.start()
	}
}

// partResult is what a start or stop hands back to the UI thread.
type partResult struct {
	note    string
	err     error
	running bool
}

func (p *part) start() {
	p.busy = true
	p.card.showBusy("Starting…")
	go func() {
		r := p.runStart()
		fyne.Do(func() { p.finishStart(r) })
	}()
}

func (p *part) runStart() partResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	note, err := p.svc.Start(context.Background())
	return partResult{note: note, err: err, running: p.svc.Running()}
}

func (p *part) finishStart(r partResult) {
	p.busy = false
	p.running = r.running
	p.show()
	if r.note != "" {
		p.log(warn, r.note)
	}
	if r.err != nil {
		p.log(bad, "Couldn't start "+p.svc.Name()+": "+plain(r.err))
		p.inform("Couldn't start "+p.svc.Name(), plain(r.err))
		return
	}
	p.log(good, p.svc.Name()+" is running: "+p.svc.Detail()+".")
}

func (p *part) stop() {
	p.busy = true
	p.card.showBusy("Stopping…")
	go func() {
		r := p.runStop()
		fyne.Do(func() { p.finishStop(r) })
	}()
}

func (p *part) runStop() partResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	err := p.svc.Stop(context.Background())
	return partResult{err: err, running: p.svc.Running()}
}

func (p *part) finishStop(r partResult) {
	p.busy = false
	p.running = r.running
	p.show()
	if r.err != nil {
		p.log(bad, "Couldn't stop "+p.svc.Name()+": "+plain(r.err))
		return
	}
	p.log(info, p.svc.Name()+" stopped.")
}

func (p *part) open() {
	if link, err := url.Parse(p.svc.OpenURL()); err == nil {
		fyne.CurrentApp().OpenURL(link)
	}
}

// menu is the part's own items, then "Start when Keel opens".
func (p *part) menu() *fyne.Menu {
	items := p.svc.Menu()
	auto := fyne.NewMenuItem("Start when Keel opens", func() {
		on := !p.settings.Autostart(p.svc.ID())
		if err := p.settings.SetAutostart(p.svc.ID(), on); err != nil {
			p.log(bad, "Couldn't save that: "+err.Error())
		}
	})
	auto.Checked = p.settings.Autostart(p.svc.ID())
	if len(items) > 0 {
		items = append(items, fyne.NewMenuItemSeparator())
	}
	items = append(items, auto)
	if p.busy {
		for _, it := range items {
			it.Disabled = true
		}
	}
	return fyne.NewMenu("", items...)
}

func (p *part) showMenu() {
	widget.ShowPopUpMenuAtRelativePosition(p.menu(), p.win.Canvas(),
		fyne.NewPos(0, p.card.more.Size().Height), p.card.more)
}

// autostart starts the part if it's set to start when Keel opens.
func (p *part) autostart() {
	if p.settings.Autostart(p.svc.ID()) && !p.running && !p.busy {
		p.start()
	}
}
