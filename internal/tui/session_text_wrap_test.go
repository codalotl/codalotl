package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWrapParagraphText_WrapsAndPreservesBlankLines(t *testing.T) {
	lines := wrapParagraphText(10, "hello world this is a test\n\nsecond paragraph")

	require.Equal(t, []string{
		"hello",
		"world this",
		"is a test",
		"",
		"second",
		"paragraph",
	}, lines)
}

func TestNewSessionBlock_WrapsLongParagraphs(t *testing.T) {
	// Force a small content width to ensure wrapping triggers.
	contentWidth := 20
	width := contentWidth + bannerMarginLeft + bannerMarginRight

	pal := newColorPalette(Config{Palette: PaletteDark})
	out := stripANSI(newSessionBlock(width, pal, sessionConfig{}))

	// The hint line should wrap (or at least be constrained) at the content width.
	require.Contains(t, out, "To enter package")
	require.Contains(t, out, "mode, use `/package")
	require.Contains(t, out, "sandbox root).")

	// Ensure we didn't just render one huge unbroken line containing the whole hint.
	require.NotContains(t, out, "To enter package mode, use `/package path/to/pkg` (path is relative to the sandbox root).")

	// Sanity: output should contain line breaks.
	require.Greater(t, strings.Count(out, "\n"), 5)
}
