package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// serviceCard is one part's row in the window: an icon, the name and
// version, a detail line (ports, PID), a status badge, a spinner while
// busy, an optional Open button, Start/Stop, and a ⋯ menu.
type serviceCard struct {
	name    *widget.Label
	version *widget.Label
	detail  *widget.Label
	status  *pill
	spinner *widget.Activity
	open    *widget.Button // hidden unless the part has something to open
	action  *widget.Button
	more    *widget.Button
	icon    fyne.Resource
}

func newServiceCard(name string, icon fyne.Resource, onAction, onOpen, onMore func()) *serviceCard {
	c := &serviceCard{
		name:    widget.NewLabel(name),
		version: muted(""),
		detail:  muted(""),
		status:  newPill(),
		spinner: widget.NewActivity(),
		icon:    icon,
	}
	c.name.TextStyle.Bold = true
	c.spinner.Hide()
	c.action = widget.NewButtonWithIcon("Start", theme.MediaPlayIcon(), onAction)
	c.open = widget.NewButtonWithIcon("Open", theme.ComputerIcon(), onOpen)
	c.open.Hide()
	c.more = widget.NewButtonWithIcon("", theme.MoreVerticalIcon(), onMore)
	c.more.Importance = widget.LowImportance
	return c
}

// object lays the card out.
func (c *serviceCard) object() fyne.CanvasObject {
	words := vstack(-14, hstack(-10, c.name, c.version), c.detail)
	right := hstack(8,
		container.NewCenter(c.spinner),
		container.NewCenter(c.status),
		container.NewCenter(c.open),
		container.NewCenter(c.action),
		container.NewCenter(c.more))
	return newCard(container.NewBorder(nil, nil, newTile(c.icon), right, words), 12)
}

// showRunning shows a running part. canOpen shows the Open button.
func (c *serviceCard) showRunning(detail string, canOpen bool) {
	c.idle()
	c.status.set("Running", theme.ColorNameSuccess)
	c.detail.SetText(detail)
	c.action.SetText("Stop")
	c.action.SetIcon(theme.MediaStopIcon())
	c.action.Importance = widget.MediumImportance
	c.action.Refresh()
	if canOpen {
		c.open.Show()
	} else {
		c.open.Hide()
	}
}

// showStopped shows a stopped part.
func (c *serviceCard) showStopped(detail string) {
	c.idle()
	c.status.set("Stopped", "")
	c.detail.SetText(detail)
	c.action.SetText("Start")
	c.action.SetIcon(theme.MediaPlayIcon())
	c.action.Importance = widget.HighImportance
	c.action.Refresh()
	c.open.Hide()
}

// showBusy shows "Starting…" or "Stopping…" and disables the buttons.
func (c *serviceCard) showBusy(text string) {
	c.status.set(text, theme.ColorNameWarning)
	c.spinner.Show()
	c.spinner.Start()
	c.action.SetText(text)
	c.action.SetIcon(nil)
	c.action.Disable()
	c.open.Disable()
	c.more.Disable()
}

// idle ends the busy state.
func (c *serviceCard) idle() {
	c.spinner.Stop()
	c.spinner.Hide()
	c.action.Enable()
	c.open.Enable()
	c.more.Enable()
}
