package main

import (
	_ "embed"
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

//go:embed icon.svg
var iconSVG []byte

// appIcon is Keel's logo, also used as the window and app icon.
var appIcon = fyne.NewStaticResource("keel.svg", iconSVG)

// currentVariant is the system's light or dark mode.
func currentVariant() fyne.ThemeVariant {
	return fyne.CurrentApp().Settings().ThemeVariant()
}

// box draws a rounded rectangle behind its content. Its colours come from
// the theme each time it refreshes, so it follows light and dark mode.
type box struct {
	widget.BaseWidget
	content fyne.CanvasObject
	inset   fyne.Size // space between the edge and the content
	paint   func(th fyne.Theme, v fyne.ThemeVariant, bg *canvas.Rectangle)
}

func newBox(content fyne.CanvasObject, inset fyne.Size, paint func(fyne.Theme, fyne.ThemeVariant, *canvas.Rectangle)) *box {
	b := &box{content: content, inset: inset, paint: paint}
	b.ExtendBaseWidget(b)
	return b
}

// newCard is a raised panel with an outline.
func newCard(content fyne.CanvasObject, inset float32) *box {
	return newBox(content, fyne.NewSquareSize(inset), func(th fyne.Theme, v fyne.ThemeVariant, bg *canvas.Rectangle) {
		bg.FillColor = th.Color(colorCard, v)
		bg.StrokeColor = th.Color(colorCardBorder, v)
		bg.StrokeWidth = 1
		bg.CornerRadius = th.Size(theme.SizeNameCardRadius)
	})
}

// newBanner is a tinted card, for warnings.
func newBanner(content fyne.CanvasObject, fill, edge fyne.ThemeColorName) *box {
	return newBox(content, fyne.NewSize(14, 10), func(th fyne.Theme, v fyne.ThemeVariant, bg *canvas.Rectangle) {
		bg.FillColor = th.Color(fill, v)
		bg.StrokeColor = th.Color(edge, v)
		bg.StrokeWidth = 1
		bg.CornerRadius = th.Size(theme.SizeNameCardRadius)
	})
}

// newBar is a full-width strip for the header and status bar.
func newBar(content fyne.CanvasObject, inset fyne.Size) *box {
	return newBox(content, inset, func(th fyne.Theme, v fyne.ThemeVariant, bg *canvas.Rectangle) {
		bg.FillColor = th.Color(colorBar, v)
	})
}

// newTile is a small rounded square tinted with the primary colour, for
// a service's icon.
func newTile(icon fyne.Resource) fyne.CanvasObject {
	tile := newBox(widget.NewIcon(theme.NewPrimaryThemedResource(icon)), fyne.NewSquareSize(8),
		func(th fyne.Theme, v fyne.ThemeVariant, bg *canvas.Rectangle) {
			bg.FillColor = withAlpha(th.Color(theme.ColorNamePrimary, v), 0x26)
			bg.CornerRadius = 8
		})
	return container.NewCenter(container.NewGridWrap(fyne.NewSquareSize(40), tile))
}

func (b *box) CreateRenderer() fyne.WidgetRenderer {
	r := &boxRenderer{b: b, bg: canvas.NewRectangle(color.Transparent)}
	r.Refresh()
	return r
}

type boxRenderer struct {
	b  *box
	bg *canvas.Rectangle
}

func (r *boxRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	in := r.b.inset
	r.b.content.Move(fyne.NewPos(in.Width, in.Height))
	r.b.content.Resize(fyne.NewSize(size.Width-2*in.Width, size.Height-2*in.Height))
}

func (r *boxRenderer) MinSize() fyne.Size {
	in := r.b.inset
	return r.b.content.MinSize().Add(fyne.NewSize(2*in.Width, 2*in.Height))
}

func (r *boxRenderer) Refresh() {
	r.b.paint(r.b.Theme(), currentVariant(), r.bg)
	r.bg.Refresh()
	r.Layout(r.b.Size()) // the content may have grown or shrunk
	r.b.content.Refresh()
}

func (r *boxRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.bg, r.b.content} }
func (r *boxRenderer) Destroy()                     {}

// pill is a rounded status badge: a coloured dot and a word on a tint of
// the same colour.
type pill struct {
	widget.BaseWidget
	Text string
	tone fyne.ThemeColorName
}

func newPill() *pill {
	p := &pill{}
	p.ExtendBaseWidget(p)
	return p
}

func (p *pill) set(text string, tone fyne.ThemeColorName) {
	p.Text, p.tone = text, tone
	p.Refresh()
}

func (p *pill) CreateRenderer() fyne.WidgetRenderer {
	r := &pillRenderer{
		p:    p,
		bg:   canvas.NewRectangle(color.Transparent),
		dot:  canvas.NewCircle(color.Transparent),
		text: canvas.NewText("", color.Transparent),
	}
	r.text.TextStyle.Bold = true
	r.Refresh()
	return r
}

type pillRenderer struct {
	p    *pill
	bg   *canvas.Rectangle
	dot  *canvas.Circle
	text *canvas.Text
}

const (
	pillPadX = 10
	pillPadY = 4
	pillDot  = 7
	pillGap  = 6
)

func (r *pillRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	r.bg.CornerRadius = size.Height / 2
	r.dot.Resize(fyne.NewSquareSize(pillDot))
	r.dot.Move(fyne.NewPos(pillPadX, (size.Height-pillDot)/2))
	t := r.text.MinSize()
	r.text.Resize(t)
	r.text.Move(fyne.NewPos(pillPadX+pillDot+pillGap, (size.Height-t.Height)/2))
}

func (r *pillRenderer) MinSize() fyne.Size {
	t := r.text.MinSize()
	return fyne.NewSize(2*pillPadX+pillDot+pillGap+t.Width, t.Height+2*pillPadY)
}

func (r *pillRenderer) Refresh() {
	th, v := r.p.Theme(), currentVariant()
	tone := r.p.tone
	if tone == "" {
		tone = theme.ColorNameDisabled
	}
	c := th.Color(tone, v)
	r.bg.FillColor = withAlpha(c, 0x24)
	r.dot.FillColor = c
	r.text.Color = c
	r.text.Text = r.p.Text
	r.text.TextSize = th.Size(theme.SizeNameText) - 1
	r.Layout(r.p.Size())
	canvas.Refresh(r.p)
}

func (r *pillRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bg, r.dot, r.text}
}
func (r *pillRenderer) Destroy() {}

// vstack and hstack are box layouts with a chosen gap. A negative gap
// pulls labels' built-in padding together.
func vstack(gap float32, objs ...fyne.CanvasObject) *fyne.Container {
	return container.New(layout.NewCustomPaddedVBoxLayout(gap), objs...)
}

func hstack(gap float32, objs ...fyne.CanvasObject) *fyne.Container {
	return container.New(layout.NewCustomPaddedHBoxLayout(gap), objs...)
}

// sectionTitle is a small, muted, upper-case heading.
func sectionTitle(text string) *widget.Label {
	l := widget.NewLabel(strings.ToUpper(text))
	l.TextStyle.Bold = true
	l.SizeName = theme.SizeNameCaptionText
	l.Importance = widget.LowImportance
	return l
}

// muted is a label in the secondary text colour.
func muted(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.Importance = widget.LowImportance
	return l
}
