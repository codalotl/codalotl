package tui

import (
	"math"
	"strings"

	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/q/remotemonitor"
	"github.com/codalotl/codalotl/internal/q/termformat"
)

// Config controls runtime options for the TUI.
type Config struct {
	// Palette selects the color palette. Valid values:
	//   - "" or "auto": derive colors from the terminal defaults (default).
	//   - "dark": force the built-in dark palette.
	//   - "light": force the built-in light palette.
	//   - "plain" / "mono" / "none": disable colorization.
	Palette PaletteName

	// ColorProfile overrides the detected color profile when non-empty.
	ColorProfile termformat.ColorProfile

	// ModelID selects the LLM model to use. If empty, the TUI uses the same
	// default model as it does today (llmmodel.DefaultModel).
	ModelID llmmodel.ModelID

	// PersistModelID, when non-nil, is called by the TUI when the user changes
	// the active model via UI commands (ex: the planned `/model` command).
	//
	// This lets the caller (who owns whatever backing config the TUI was created
	// from) persist the selected model to disk (or elsewhere). If it returns a
	// non-nil error, the TUI will display that error in the message area.
	//
	// NOTE: The TUI does not call this yet.
	PersistModelID func(newModelID llmmodel.ModelID) error

	// Monitor is provided by the CLI (when available) so the TUI can later report
	// panics/errors and display version upgrade notices.
	//
	// NOTE: The TUI does not use this yet.
	Monitor *remotemonitor.Monitor
}

// PaletteName is a symbolic name for a color palette.
type PaletteName string

const (
	paletteAutoName  PaletteName = "auto"
	paletteDarkName  PaletteName = "dark"
	paletteLightName PaletteName = "light"
	palettePlainName PaletteName = "plain"
)

const (
	PaletteAuto  PaletteName = paletteAutoName
	PaletteDark  PaletteName = paletteDarkName
	PaletteLight PaletteName = paletteLightName
	PalettePlain PaletteName = palettePlainName
)

type colorPalette struct {
	name               PaletteName
	colorized          bool
	isLight            bool
	primaryBackground  termformat.Color
	accentBackground   termformat.Color
	borderColor        termformat.Color
	primaryForeground  termformat.Color
	accentForeground   termformat.Color
	redForeground      termformat.Color
	greenForeground    termformat.Color
	colorfulForeground termformat.Color
	workingSeq         [3]string
}

func newColorPalette(cfg Config) colorPalette {
	var p colorPalette
	switch normalizePaletteName(cfg.Palette) {
	case palettePlainName:
		p = colorPalette{name: palettePlainName, colorized: false}
	case paletteDarkName:
		p = darkPalette()
	case paletteLightName:
		p = lightPalette()
	default:
		p = derivedPaletteFromTerminal()
	}
	return convertPaletteToProfile(cfg.ColorProfile, p)
}

func normalizePaletteName(name PaletteName) PaletteName {
	if name == "" {
		return paletteAutoName
	}
	switch PaletteName(strings.ToLower(strings.TrimSpace(string(name)))) {
	case "", paletteAutoName, "default":
		return paletteAutoName
	case paletteDarkName:
		return paletteDarkName
	case paletteLightName:
		return paletteLightName
	case palettePlainName:
		return palettePlainName
	default:
		return paletteAutoName
	}
}

func darkPalette() colorPalette {
	p := colorPalette{
		name:               paletteDarkName,
		colorized:          true,
		isLight:            false,
		primaryBackground:  termformat.NewRGBColor(0x24, 0x27, 0x3a), // 24273a
		accentBackground:   termformat.NewRGBColor(0x3B, 0x3C, 0x52), // 313244
		primaryForeground:  termformat.NewRGBColor(0xca, 0xd3, 0xf5), // cad3f5
		accentForeground:   termformat.NewRGBColor(0x80, 0x87, 0xa2), // 8087a2
		redForeground:      termformat.NewRGBColor(0xf0, 0x66, 0x66),
		greenForeground:    termformat.NewRGBColor(0x57, 0xc9, 0x92),
		colorfulForeground: termformat.NewRGBColor(0x89, 0xb4, 0xfa), // 89b4fa
		borderColor:        termformat.NewRGBColor(0xcb, 0xa6, 0xf7), // cba6f7
	}
	p.workingSeq = workingIndicatorSequences(p)
	return p
}

func disablePaletteColors(p colorPalette) colorPalette {
	p.colorized = false
	// TODO: convert these to NoColor.
	p.primaryBackground = nil
	p.accentBackground = nil
	p.primaryForeground = nil
	p.accentForeground = nil
	p.borderColor = nil
	p.redForeground = nil
	p.greenForeground = nil
	p.colorfulForeground = nil
	p.workingSeq = [3]string{}
	return p
}

func convertPaletteToProfile(profile termformat.ColorProfile, p colorPalette) colorPalette {
	if !p.colorized {
		return disablePaletteColors(p)
	}

	if profile == "" {
		var err error
		profile, err = termformat.GetColorProfile()
		if err != nil {
			return disablePaletteColors(p)
		}
	}

	if profile == termformat.ColorProfileUncolored {
		return disablePaletteColors(p)
	}

	convert := func(c termformat.Color) termformat.Color {
		if c == nil {
			return nil
		}
		if _, ok := c.(termformat.NoColor); ok {
			return nil
		}
		return profile.Convert(c)
	}

	p.primaryBackground = convert(p.primaryBackground)
	p.accentBackground = convert(p.accentBackground)
	p.borderColor = convert(p.borderColor)
	p.primaryForeground = convert(p.primaryForeground)
	p.accentForeground = convert(p.accentForeground)
	p.redForeground = convert(p.redForeground)
	p.greenForeground = convert(p.greenForeground)
	p.colorfulForeground = convert(p.colorfulForeground)
	p.workingSeq = workingIndicatorSequences(p)

	return p
}

func lightPalette() colorPalette {
	p := colorPalette{
		name:               paletteLightName,
		colorized:          true,
		isLight:            true,
		primaryBackground:  termformat.NewRGBColor(0xf8, 0xf9, 0xff),
		accentBackground:   termformat.NewRGBColor(0xe9, 0xed, 0xfa),
		primaryForeground:  termformat.NewRGBColor(0x1c, 0x1f, 0x2b),
		accentForeground:   termformat.NewRGBColor(0x4a, 0x51, 0x6c),
		redForeground:      termformat.NewRGBColor(0xca, 0x3d, 0x3d),
		greenForeground:    termformat.NewRGBColor(0x29, 0x8f, 0x58),
		colorfulForeground: termformat.NewRGBColor(0x1f, 0x63, 0xe0),
	}
	p.borderColor = blendColors(p.primaryForeground, p.primaryBackground, 0.2)
	p.workingSeq = workingIndicatorSequences(p)
	return p
}

func derivedPaletteFromTerminal() colorPalette {
	fg, bg := termformat.DefaultFBBGColor()
	// TODO: get profile. If profile = ColorProfileUncolored, then use plain
	if isNoColor(fg) || isNoColor(bg) {
		return lightPalette()
	}

	accentBG := blendColors(fg, bg, 0.12)
	accentFG := blendColors(fg, bg, 0.6)
	p := colorPalette{
		name:               paletteAutoName,
		colorized:          true,
		isLight:            colorIsLight(bg),
		primaryBackground:  bg,
		accentBackground:   accentBG,
		borderColor:        blendColors(fg, bg, 0.2),
		primaryForeground:  fg,
		accentForeground:   accentFG,
		redForeground:      termformat.NewRGBColor(0xdc, 0x52, 0x52),
		greenForeground:    termformat.NewRGBColor(0x2e, 0x8b, 0x57),
		colorfulForeground: colorfulFromBackground(bg),
	}
	p.workingSeq = workingIndicatorSequences(p)
	return p
}

func isNoColor(c termformat.Color) bool {
	if c == nil {
		return true
	}
	if _, ok := c.(termformat.NoColor); ok {
		return true
	}
	return false
}

func colorIsLight(c termformat.Color) bool {
	if c == nil {
		return false
	}
	r, g, b := c.RGB8()
	brightness := 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
	return brightness >= 160
}

func colorfulFromBackground(bg termformat.Color) termformat.Color {
	if bg == nil {
		return termformat.NewRGBColor(0x30, 0x90, 0xff)
	}
	r, g, b := bg.RGB8()
	brightness := 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
	if brightness >= 180 {
		return termformat.NewRGBColor(0x18, 0x88, 0xff)
	}
	return termformat.NewRGBColor(0x62, 0xc2, 0xff)
}

func blendColors(focus, base termformat.Color, weight float64) termformat.Color {
	if base == nil {
		base = focus
	}
	if focus == nil {
		focus = base
	}
	if base == nil && focus == nil {
		return termformat.NewRGBColor(0, 0, 0)
	}
	if weight <= 0 {
		return cloneColor(base)
	}
	if weight >= 1 {
		return cloneColor(focus)
	}
	fr, fg, fb := rgbChannels(focus)
	br, bg, bb := rgbChannels(base)
	r := weight*fr + (1-weight)*br
	g := weight*fg + (1-weight)*bg
	b := weight*fb + (1-weight)*bb
	return termformat.NewRGBColor(uint8(math.Round(r)), uint8(math.Round(g)), uint8(math.Round(b)))
}

func cloneColor(c termformat.Color) termformat.Color {
	if c == nil {
		return termformat.NewRGBColor(0, 0, 0)
	}
	r, g, b := rgbChannels(c)
	return termformat.NewRGBColor(uint8(math.Round(r)), uint8(math.Round(g)), uint8(math.Round(b)))
}

func rgbChannels(c termformat.Color) (float64, float64, float64) {
	if c == nil {
		return 0, 0, 0
	}
	if _, ok := c.(termformat.NoColor); ok {
		return 0, 0, 0
	}
	r, g, b := c.RGB8()
	return float64(r), float64(g), float64(b)
}

func workingIndicatorSequences(p colorPalette) [3]string {
	var seq [3]string
	if !p.colorized {
		return seq
	}
	colors := workingIndicatorColors(p)
	for i, color := range colors {
		if color == nil {
			continue
		}
		seq[i] = color.ANSISequence(false)
	}
	return seq
}

func workingIndicatorColors(p colorPalette) [3]termformat.Color {
	var colors [3]termformat.Color
	if !p.colorized {
		return colors
	}

	base := p.accentForeground
	if base == nil {
		base = p.primaryForeground
	}
	if base == nil {
		return colors
	}

	if p.isLight {
		colors[0] = blendColors(termformat.NewRGBColor(0x00, 0x00, 0x00), base, 0.65)
		colors[1] = blendColors(termformat.NewRGBColor(0x00, 0x00, 0x00), base, 0.45)
		colors[2] = blendColors(termformat.NewRGBColor(0x00, 0x00, 0x00), base, 0.25)
		return colors
	}

	colors[0] = blendColors(termformat.NewRGBColor(0xff, 0xff, 0xff), base, 0.12)
	colors[1] = blendColors(termformat.NewRGBColor(0xff, 0xff, 0xff), base, 0.25)
	colors[2] = blendColors(termformat.NewRGBColor(0xff, 0xff, 0xff), base, 0.4)
	return colors
}
