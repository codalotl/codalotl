package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/q/termformat"
	"github.com/stretchr/testify/require"
)

func TestBannerUsesWordArtWhenWidthAllows(t *testing.T) {
	iconWidth := termformat.BlockWidth(bannerIcon)
	nameWidth := termformat.BlockWidth(bannerName)
	contentWidth := iconWidth + bannerIconNameGap + nameWidth
	width := contentWidth + bannerMarginLeft + bannerMarginRight

	pal := newColorPalette(Config{ColorProfile: termformat.ColorProfileANSI256, Palette: PaletteDark})
	result := bannerBlock(width, pal, "gpt-5-max")

	require.Contains(t, result, "▄▀▀▀▀")
	require.Contains(t, result, "Model: gpt-5-max")
	require.Contains(t, result, "Start by describing a task")
}

func TestBannerFallsBackToPlainNameWhenTight(t *testing.T) {
	iconWidth := termformat.BlockWidth(bannerIcon)
	contentWidth := iconWidth + bannerIconNameGap + termformat.BlockWidth(productNameLine)
	width := contentWidth + bannerMarginLeft + bannerMarginRight

	pal := newColorPalette(Config{ColorProfile: termformat.ColorProfileANSI256, Palette: PaletteDark})
	result := bannerBlock(width, pal, "gpt-4.1-mini")

	require.Contains(t, result, productNameLine)
	require.NotContains(t, result, "▄▀▀▀▀")
}

func TestBannerStacksNameWhenExtremelyNarrow(t *testing.T) {
	iconWidth := termformat.BlockWidth(bannerIcon)
	contentWidth := iconWidth + bannerIconNameGap + termformat.BlockWidth(productNameLine) - 1
	width := contentWidth + bannerMarginLeft + bannerMarginRight

	pal := newColorPalette(Config{ColorProfile: termformat.ColorProfileANSI256, Palette: PaletteDark})
	result := bannerBlock(width, pal, "gpt-4.1-mini")

	expectedLine := "\n" + strings.Repeat(" ", bannerMarginLeft) + productNameLine
	require.Contains(t, stripANSI(result), expectedLine)
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}
