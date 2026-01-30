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
