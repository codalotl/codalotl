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

// bannerBlock returns a fully formatted block of proper width/bg that can be directly dropped into a view.
// The function assumes width >= agentformatter.MinTerminalWidth (30), with undefined behavior under.
//
// bannerBlock is intentionally art-only: it renders the logo + word art and nothing else.
func bannerBlock(width int, pal colorPalette) string {
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

	return bs.Apply(termformat.BlockStylePerLine(renderBannerArt(contentWidth, pal)))
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

// newSessionBlock returns the "new session text" described in SPEC.md.
// It includes banner art and a short, mode-specific help text.
func newSessionBlock(width int, pal colorPalette, cfg sessionConfig) string {
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

	var lines []string
	lines = append(lines, renderBannerArt(contentWidth, pal))
	lines = append(lines, "")

	bodyStyle := termformat.Style{Foreground: pal.primaryForeground}
	hintStyle := termformat.Style{Foreground: pal.accentForeground}
	emphTitleStyle := termformat.Style{Foreground: pal.colorfulForeground, Bold: termformat.StyleSetOn}

	appendWrapped := func(style termformat.Style, text string) {
		for _, line := range wrapParagraphText(contentWidth, text) {
			if line == "" {
				lines = append(lines, "")
				continue
			}
			lines = append(lines, style.Apply(line))
		}
	}

	// Mixed styling needs to be built "word by word" so that ANSI spans can't be
	// broken by wrapping, but we keep the call sites readable by chunking.
	type styledChunk struct {
		style termformat.Style
		text  string
	}
	appendWrappedChunks := func(chunks ...styledChunk) {
		var words []string
		for _, ch := range chunks {
			for _, w := range strings.Fields(ch.text) {
				words = append(words, ch.style.Apply(w))
			}
		}
		for _, line := range wrapWords(contentWidth, words) {
			lines = append(lines, line)
		}
	}

	// Bullet-style wrapping with a hanging indent for subsequent lines.
	appendBullet := func(style termformat.Style, text string) {
		const bulletPrefix = "• "
		prefixWidth := termformat.TextWidthWithANSICodes(bulletPrefix)
		avail := contentWidth - prefixWidth
		if avail <= 0 {
			appendWrapped(style, bulletPrefix+text)
			return
		}

		inner := wrapWords(avail, strings.Fields(text))
		for i, line := range inner {
			if i == 0 {
				lines = append(lines, style.Apply(bulletPrefix+line))
				continue
			}
			lines = append(lines, style.Apply(strings.Repeat(" ", prefixWidth)+line))
		}
	}

	if cfg.packageMode() {
		appendWrappedChunks(
			styledChunk{style: bodyStyle, text: "You are in"},
			styledChunk{style: emphTitleStyle, text: "package mode."},
			styledChunk{style: bodyStyle, text: " Package mode is a Go-specific mode that isolates the agent to work on a single package at once; it explores other packages by reading their public godoc-style API. Other notable differences:"},
		)

		appendBullet(hintStyle, "Optimized package context; auto-gofmt and build errors on patch.")
		appendBullet(hintStyle, "It can spawn subagents to update other packages and answer questions about the codebase.")
		appendBullet(hintStyle, "No raw shell access (to keep it in package mode).")

		lines = append(lines, "")
		appendWrappedChunks(
			styledChunk{style: bodyStyle, text: "To share context from other packages directly, use"},
			styledChunk{style: hintStyle, text: "@path/to/context."},
		)
	} else {
		appendWrappedChunks(
			styledChunk{style: bodyStyle, text: "You are in the generic, language-agnostic,"},
			styledChunk{style: emphTitleStyle, text: "non-package mode:"},
			styledChunk{style: bodyStyle, text: "the agent will operate as agents typically do, with a small but useful set of tools:"},
			styledChunk{style: hintStyle, text: "shell, patch, read file, and directory listing."},
		)

		lines = append(lines, "")

		appendWrappedChunks(
			styledChunk{style: bodyStyle, text: "To enter package mode, use"},
			styledChunk{style: hintStyle, text: "`/package path/to/pkg`"},
			styledChunk{style: bodyStyle, text: "(path is relative to the sandbox root). Package mode is a Go-specific mode that isolates the agent to work on a single package at once; it explores other packages by reading their public godoc-style API."},
		)

		lines = append(lines, "")
		appendWrapped(bodyStyle, "Start by describing a task below, or use one of the commands:")
		appendWrapped(hintStyle, "• /package path/to/pkg")
		appendWrapped(hintStyle, "• /model gpt-5.2-high")
		appendWrapped(hintStyle, "• /session abc-some-session")
		appendWrapped(hintStyle, "• /quit")
	}

	return bs.Apply(termformat.BlockStylePerLine(strings.Join(lines, "\n")))
}

// wrapParagraphText word-wraps text to the given width while preserving blank lines as paragraph breaks.
// Newlines inside a paragraph are treated as spaces, so the programmer can format source strings across
// multiple lines without affecting output (unless a blank line is inserted).
func wrapParagraphText(width int, text string) []string {
	if width <= 0 {
		if text == "" {
			return nil
		}
		return []string{text}
	}

	rawLines := strings.Split(text, "\n")
	var out []string

	var words []string
	flush := func() {
		if len(words) == 0 {
			return
		}
		out = append(out, wrapWords(width, words)...)
		words = words[:0]
	}

	for _, l := range rawLines {
		if strings.TrimSpace(l) == "" {
			flush()
			// Preserve a single blank line break in output.
			if len(out) == 0 || out[len(out)-1] != "" {
				out = append(out, "")
			}
			continue
		}
		words = append(words, strings.Fields(l)...)
	}
	flush()

	// Trim trailing blank line introduced by paragraph parsing.
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}

	return out
}

func wrapWords(width int, words []string) []string {
	var out []string

	var line string
	flush := func() {
		if line == "" {
			return
		}
		out = append(out, line)
		line = ""
	}

	for _, word := range words {
		for _, chunk := range breakWord(width, word) {
			if line == "" {
				line = chunk
				continue
			}
			candidate := line + " " + chunk
			if termformat.TextWidthWithANSICodes(candidate) <= width {
				line = candidate
				continue
			}
			flush()
			line = chunk
		}
	}
	flush()

	return out
}

func breakWord(width int, word string) []string {
	if width <= 0 || word == "" {
		return nil
	}

	if termformat.TextWidthWithANSICodes(word) <= width {
		return []string{word}
	}

	var chunks []string
	remaining := word
	for termformat.TextWidthWithANSICodes(remaining) > width {
		total := termformat.TextWidthWithANSICodes(remaining)
		chunk := termformat.Cut(remaining, 0, total-width)
		if chunk == "" {
			break
		}
		chunks = append(chunks, chunk)
		remaining = termformat.Cut(remaining, width, 0)
		if remaining == "" {
			break
		}
	}
	if remaining != "" {
		chunks = append(chunks, remaining)
	}
	return chunks
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
