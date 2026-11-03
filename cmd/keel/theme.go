package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// Colours Keel adds to Fyne's own.
const (
	colorBar        fyne.ThemeColorName = "keelBar"        // header and status bar
	colorCard       fyne.ThemeColorName = "keelCard"       // cards and the log
	colorCardBorder fyne.ThemeColorName = "keelCardBorder" // card outlines
	colorWarnFill   fyne.ThemeColorName = "keelWarnFill"   // warning banner
	colorWarnEdge   fyne.ThemeColorName = "keelWarnEdge"   // warning banner outline
)

// keelTheme is Fyne's default theme with Keel's colours and rounder
// corners. It follows the system's light or dark mode.
type keelTheme struct{}

var _ fyne.Theme = keelTheme{}

var darkColors = map[fyne.ThemeColorName]color.Color{
	theme.ColorNameBackground:          rgb(0x0F1216),
	colorBar:                           rgb(0x14181E),
	colorCard:                          rgb(0x171C23),
	colorCardBorder:                    rgb(0x252C35),
	colorWarnFill:                      rgba(0xFBBF24, 0x1A),
	colorWarnEdge:                      rgba(0xFBBF24, 0x59),
	theme.ColorNameHeaderBackground:    rgb(0x14181E),
	theme.ColorNameOverlayBackground:   rgb(0x171C23),
	theme.ColorNameMenuBackground:      rgb(0x1A2028),
	theme.ColorNameInputBackground:     rgb(0x1A2028),
	theme.ColorNameInputBorder:         rgb(0x2C343E),
	theme.ColorNameSeparator:           rgb(0x232A33),
	theme.ColorNameForeground:          rgb(0xE6EAF0),
	theme.ColorNameDisabled:            rgb(0x7D8794), // also Keel's muted text
	theme.ColorNamePlaceHolder:         rgb(0x6E7886),
	theme.ColorNameButton:              rgb(0x232A33),
	theme.ColorNameDisabledButton:      rgb(0x1D232B),
	theme.ColorNameHover:               rgba(0xFFFFFF, 0x14),
	theme.ColorNamePressed:             rgba(0xFFFFFF, 0x24),
	theme.ColorNameFocus:               rgba(0x2DD4BF, 0x66),
	theme.ColorNameSelection:           rgba(0x2DD4BF, 0x40),
	theme.ColorNamePrimary:             rgb(0x2DD4BF),
	theme.ColorNameForegroundOnPrimary: rgb(0x042F2B),
	theme.ColorNameHyperlink:           rgb(0x5EEAD4),
	theme.ColorNameSuccess:             rgb(0x34D399),
	theme.ColorNameForegroundOnSuccess: rgb(0x052E1C),
	theme.ColorNameWarning:             rgb(0xFBBF24),
	theme.ColorNameForegroundOnWarning: rgb(0x2B1D02),
	theme.ColorNameError:               rgb(0xF87171),
	theme.ColorNameForegroundOnError:   rgb(0x2C0707),
	theme.ColorNameScrollBar:           rgba(0xFFFFFF, 0x33),
	theme.ColorNameShadow:              rgba(0x000000, 0x80),
}

var lightColors = map[fyne.ThemeColorName]color.Color{
	theme.ColorNameBackground:          rgb(0xF3F5F8),
	colorBar:                           rgb(0xFFFFFF),
	colorCard:                          rgb(0xFFFFFF),
	colorCardBorder:                    rgb(0xE0E4EA),
	colorWarnFill:                      rgb(0xFEF6E4),
	colorWarnEdge:                      rgb(0xF5CF7A),
	theme.ColorNameHeaderBackground:    rgb(0xFFFFFF),
	theme.ColorNameOverlayBackground:   rgb(0xFFFFFF),
	theme.ColorNameMenuBackground:      rgb(0xFFFFFF),
	theme.ColorNameInputBackground:     rgb(0xFFFFFF),
	theme.ColorNameInputBorder:         rgb(0xD5DAE1),
	theme.ColorNameSeparator:           rgb(0xE3E7EC),
	theme.ColorNameForeground:          rgb(0x161B22),
	theme.ColorNameDisabled:            rgb(0x6B7480),
	theme.ColorNamePlaceHolder:         rgb(0x8A93A0),
	theme.ColorNameButton:              rgb(0xE9EDF2),
	theme.ColorNameDisabledButton:      rgb(0xEEF0F3),
	theme.ColorNameHover:               rgba(0x000000, 0x0C),
	theme.ColorNamePressed:             rgba(0x000000, 0x18),
	theme.ColorNameFocus:               rgba(0x0F766E, 0x55),
	theme.ColorNameSelection:           rgba(0x0F766E, 0x30),
	theme.ColorNamePrimary:             rgb(0x0F766E),
	theme.ColorNameForegroundOnPrimary: rgb(0xFFFFFF),
	theme.ColorNameHyperlink:           rgb(0x0F766E),
	theme.ColorNameSuccess:             rgb(0x047857),
	theme.ColorNameForegroundOnSuccess: rgb(0xFFFFFF),
	theme.ColorNameWarning:             rgb(0xB45309),
	theme.ColorNameForegroundOnWarning: rgb(0xFFFFFF),
	theme.ColorNameError:               rgb(0xDC2626),
	theme.ColorNameForegroundOnError:   rgb(0xFFFFFF),
	theme.ColorNameScrollBar:           rgba(0x000000, 0x33),
	theme.ColorNameShadow:              rgba(0x000000, 0x26),
}

func (keelTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	colors := darkColors
	if v == theme.VariantLight {
		colors = lightColors
	}
	if c, ok := colors[name]; ok {
		return c
	}
	return theme.DefaultTheme().Color(name, v)
}

func (keelTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNameInputRadius, theme.SizeNameSelectionRadius, theme.SizeNameButtonRadius:
		return 6
	case theme.SizeNameCardRadius, theme.SizeNamePopupRadius, theme.SizeNameMenuRadius:
		return 10
	case theme.SizeNameDialogRadius:
		return 12
	}
	return theme.DefaultTheme().Size(name)
}

func (keelTheme) Font(s fyne.TextStyle) fyne.Resource     { return theme.DefaultTheme().Font(s) }
func (keelTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(n) }

// compactTheme packs the activity log's rows closer together. It wraps the
// app's theme rather than keelTheme, so it keeps any override of it.
type compactTheme struct{ fyne.Theme }

func (t compactTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == theme.SizeNameInnerPadding {
		return 4
	}
	return t.Theme.Size(name)
}

func rgb(hex uint32) color.NRGBA { return rgba(hex, 0xFF) }

func rgba(hex uint32, alpha uint8) color.NRGBA {
	return color.NRGBA{R: uint8(hex >> 16), G: uint8(hex >> 8), B: uint8(hex), A: alpha}
}

// withAlpha returns c with a new opacity, for tinted backgrounds.
func withAlpha(c color.Color, alpha uint8) color.NRGBA {
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	n.A = alpha
	return n
}
