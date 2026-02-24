package tui

import (
	"testing"

	"github.com/codalotl/codalotl/internal/q/termformat"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigColorProfileOverridesDetection(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	palette := newColorPalette(Config{
		Palette:      PaletteDark,
		ColorProfile: termformat.ColorProfileANSI,
	})

	require.True(t, palette.colorized)
	require.NotNil(t, palette.primaryForeground)
	assert.IsType(t, termformat.ANSIColor(0), palette.primaryForeground)
}
func TestLightPalettePrimaryBackgroundIsWhiteInTrueColor(t *testing.T) {
	palette := newColorPalette(Config{
		Palette:      PaletteLight,
		ColorProfile: termformat.ColorProfileTrueColor,
	})
	require.True(t, palette.colorized)
	require.NotNil(t, palette.primaryBackground)
	assert.IsType(t, termformat.RGBColor(""), palette.primaryBackground)
	r, g, b := palette.primaryBackground.RGB8()
	assert.Equal(t, uint8(0xff), r)
	assert.Equal(t, uint8(0xff), g)
	assert.Equal(t, uint8(0xff), b)
}
func TestLightPalettePrimaryBackgroundFallsBackToWhiteInANSI256(t *testing.T) {
	palette := newColorPalette(Config{
		Palette:      PaletteLight,
		ColorProfile: termformat.ColorProfileANSI256,
	})
	require.True(t, palette.colorized)
	require.NotNil(t, palette.primaryBackground)
	assert.IsType(t, termformat.ANSI256Color(0), palette.primaryBackground)
	r, g, b := palette.primaryBackground.RGB8()
	assert.Equal(t, uint8(0xff), r)
	assert.Equal(t, uint8(0xff), g)
	assert.Equal(t, uint8(0xff), b)
}
func TestAutoPaletteFromTerminalDefaultsSelectsLightWhenBackgroundIsLight(t *testing.T) {
	palette := autoPaletteFromTerminalDefaults(
		termformat.NewRGBColor(0x00, 0x00, 0x00),
		termformat.NewRGBColor(0xff, 0xff, 0xff),
	)
	require.True(t, palette.colorized)
	assert.Equal(t, paletteLightName, palette.name)
	assert.True(t, palette.isLight)
}
func TestAutoPaletteFromTerminalDefaultsSelectsDarkWhenBackgroundIsDark(t *testing.T) {
	palette := autoPaletteFromTerminalDefaults(
		termformat.NewRGBColor(0xff, 0xff, 0xff),
		termformat.NewRGBColor(0x00, 0x00, 0x00),
	)
	require.True(t, palette.colorized)
	assert.Equal(t, paletteDarkName, palette.name)
	assert.False(t, palette.isLight)
}
func TestAutoPaletteFromTerminalDefaultsFallsBackToLightPaletteWhenTerminalColorsUnknown(t *testing.T) {
	palette := autoPaletteFromTerminalDefaults(termformat.NoColor{}, termformat.NoColor{})
	require.True(t, palette.colorized)
	assert.Equal(t, paletteLightName, palette.name)
	assert.True(t, palette.isLight)
}
