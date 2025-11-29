package tui

import (
	_ "embed"
	"strings"

	"github.com/codalotl/codalotl/internal/q/termformat"
)

const (
	bannerIconNameGap = 4
	productNameLine   = "codalotl"
	bannerMarginLeft  = 2
	bannerMarginRight = 0
)

//go:embed banner-icon.txt
var bannerIcon string

//go:embed banner-name.txt
var bannerName string

// bannerBlock returns a fully formatted block of proper width/bag that can be directly dropped into a view.
// The function assumes width >= agentformatter.MinTerminalWidth (30), with undefined behavior under.
func bannerBlock(width int, pal colorPalette, modelName string) string {
	contentWidth := width - bannerMarginLeft - bannerMarginRight
	if contentWidth <= 0 {
		contentWidth = width
	}

	bs := termformat.BlockStyle{
		TotalWidth:         width,
		BlockNormalizeMode: termformat.BlockNormalizeModeExtend,
		MarginTop:          1,
		MarginLeft:         bannerMarginLeft,
		MarginRight:        bannerMarginRight,
		TextBackground:     pal.primaryBackground,
		MarginBackground:   pal.primaryBackground,
	}

	modelStyle := termformat.Style{Foreground: pal.primaryForeground}
	lines := []string{
		renderBannerArt(contentWidth, pal),
		"",
		modelStyle.Apply("Model: " + modelName),
		modelStyle.Apply("Start by describing a task"),
	}

	content := strings.Join(lines, "\n")
	return bs.Apply(termformat.BlockStylePerLine(content))
}

func renderBannerArt(width int, pal colorPalette) string {
	icon := newBannerSection(bannerIcon, pal.primaryForeground)
	name := newBannerSection(bannerName, pal.colorfulForeground)
	fallback := newBannerSection(productNameLine, pal.colorfulForeground)

	if icon.width+bannerIconNameGap+name.width <= width {
		return layoutBannerSections(icon, name, bannerIconNameGap)
	}
	if icon.width+bannerIconNameGap+fallback.width <= width {
		return layoutBannerSections(icon, fallback, bannerIconNameGap)
	}

	if icon.width <= width {
		return stackBannerSections(icon, fallback)
	}

	return fallback.block
}

type bannerSection struct {
	width  int
	height int
	block  string
	lines  []string
}

func newBannerSection(block string, fg termformat.Color) bannerSection {
	normalized := termformat.BlockNormalizeWidth(block, termformat.BlockNormalizeModeNaive)
	width := termformat.BlockWidth(normalized)
	rawLines := strings.Split(normalized, "\n")
	style := termformat.Style{Foreground: fg}

	lines := make([]string, len(rawLines))
	for i, line := range rawLines {
		if line == "" {
			lines[i] = ""
			continue
		}
		lines[i] = style.Apply(line)
	}

	styledBlock := strings.Join(lines, "\n")

	return bannerSection{
		width:  width,
		height: len(lines),
		block:  styledBlock,
		lines:  lines,
	}
}

func layoutBannerSections(left, right bannerSection, gap int) string {
	rightX := left.width + gap
	blocks := []termformat.LayoutBlock{
		{Block: left.block, X: 0, Y: 0},
		{Block: right.block, X: rightX, Y: 0},
	}
	layout, err := termformat.Layout(blocks, nil)
	if err != nil {
		return stackBannerSections(left, right)
	}
	return layout
}

func stackBannerSections(top, bottom bannerSection) string {
	stacked := make([]string, 0, len(top.lines)+len(bottom.lines))
	stacked = append(stacked, top.lines...)
	stacked = append(stacked, bottom.lines...)
	return strings.Join(stacked, "\n")
}
