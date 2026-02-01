package tui

import (
	"testing"

	"github.com/codalotl/codalotl/internal/q/termformat"
	"github.com/codalotl/codalotl/internal/tools/authdomain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPermissionView_UsesPaletteColorsWhenColorized(t *testing.T) {
	palette := newColorPalette(Config{
		Palette:      PaletteDark,
		ColorProfile: termformat.ColorProfileANSI,
	})
	require.True(t, palette.colorized)
	require.NotNil(t, palette.primaryForeground)
	require.NotNil(t, palette.accentForeground)
	require.NotNil(t, palette.accentBackground)

	m := &model{
		palette:      palette,
		windowWidth:  80,
		windowHeight: 24,
	}
	m.updateSizes()

	m.activePermission = &permissionPrompt{
		request: authdomain.UserRequest{
			ToolName: "test-permission",
			Prompt:   "Allow the test request?",
		},
	}

	m.refreshPermissionView()
	require.NotEmpty(t, m.permissionViewText)

	// These sequences are derived from the palette (not hard-coded) so the test
	// ensures the view is actually using the palette colors.
	assert.Contains(t, m.permissionViewText, palette.primaryForeground.ANSISequence(false))
	assert.Contains(t, m.permissionViewText, palette.accentForeground.ANSISequence(false))
	assert.Contains(t, m.permissionViewText, palette.accentBackground.ANSISequence(true))

	plain := stripAnsi(m.permissionViewText)
	assert.Contains(t, plain, "Allow the test request?")
	assert.Contains(t, plain, "Y    allow")
	assert.Contains(t, plain, "N    deny")
	assert.Contains(t, plain, "ESC  deny + stop agent")
}

func TestPermissionView_PlainPalette_HasNoANSICodes(t *testing.T) {
	palette := newColorPalette(Config{Palette: PalettePlain})
	require.False(t, palette.colorized)

	m := &model{
		palette:      palette,
		windowWidth:  80,
		windowHeight: 24,
	}
	m.updateSizes()

	m.activePermission = &permissionPrompt{
		request: authdomain.UserRequest{
			ToolName: "test-permission",
			Prompt:   "Allow the test request?",
		},
	}
	m.refreshPermissionView()

	assert.NotContains(t, m.permissionViewText, "\x1b[")
	assert.Contains(t, m.permissionViewText, "Allow the test request?")
}

func TestPermissionView_NarrowTerminal_FallsBackWithoutPanic(t *testing.T) {
	m := &model{
		palette:      newColorPalette(Config{Palette: PaletteDark, ColorProfile: termformat.ColorProfileANSI}),
		windowWidth:  4,
		windowHeight: 24,
	}
	m.updateSizes()

	m.activePermission = &permissionPrompt{
		request: authdomain.UserRequest{
			ToolName: "test-permission",
			Prompt:   "Allow the test request?",
		},
	}

	require.NotPanics(t, func() {
		m.refreshPermissionView()
	})
	assert.Equal(t, termformat.Sanitize("Allow the test request?", 4), m.permissionViewText)
}
