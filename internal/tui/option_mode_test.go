package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/codalotl/codalotl/internal/q/termformat"
	qtui "github.com/codalotl/codalotl/internal/q/tui"
	"github.com/stretchr/testify/require"
)

func TestOptionModeCtrlOToggles(t *testing.T) {
	m := newModel(colorPalette{}, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil)
	m.Update(nil, qtui.ResizeEvent{Width: 80, Height: 20})

	require.False(t, m.optionMode)

	m.Update(nil, qtui.KeyEvent{ControlKey: qtui.ControlKeyCtrlO})
	require.True(t, m.optionMode)

	m.Update(nil, qtui.KeyEvent{ControlKey: qtui.ControlKeyCtrlO})
	require.False(t, m.optionMode)
}

func TestOptionModeDoubleClickToggles(t *testing.T) {
	m := newModel(colorPalette{}, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil)
	m.Update(nil, qtui.ResizeEvent{Width: 80, Height: 20})

	base := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return base }

	require.False(t, m.optionMode)

	m.Update(nil, qtui.MouseEvent{Action: qtui.MouseActionPress, Button: qtui.MouseButtonLeft, X: 3, Y: 3})
	require.False(t, m.optionMode)

	m.Update(nil, qtui.MouseEvent{Action: qtui.MouseActionPress, Button: qtui.MouseButtonLeft, X: 3, Y: 3})
	require.True(t, m.optionMode)
}

func TestOptionModeCopyCopiesRenderedMessageAndShowsFeedback(t *testing.T) {
	palette := colorPalette{
		colorized:          true,
		primaryBackground:  termformat.ANSIColor(0),
		accentBackground:   termformat.ANSIColor(1),
		primaryForeground:  termformat.ANSIColor(7),
		accentForeground:   termformat.ANSIColor(7),
		colorfulForeground: termformat.ANSIColor(6),
	}

	m := newModel(palette, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil)
	m.Update(nil, qtui.ResizeEvent{Width: 80, Height: 20})

	base := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	now := base
	m.now = func() time.Time { return now }

	var copied string
	m.clipboardSetter = func(text string) { copied = text }

	var osCopied string
	m.osClipboardAvailable = func() bool { return true }
	m.osClipboardWrite = func(text string) error {
		osCopied = text
		return nil
	}

	m.appendSystemMessage("hello world")
	m.refreshViewport(true)

	// Enter option mode, which makes copy buttons appear in the separator rows.
	m.Update(nil, qtui.KeyEvent{ControlKey: qtui.ControlKeyCtrlO})
	require.True(t, m.optionMode)

	require.NotEmpty(t, m.optionCopyTargets)
	target := m.optionCopyTargets[0]

	want := m.plainMessageTextForCopy(0)

	y := target.contentLine - m.viewport.Offset()
	require.GreaterOrEqual(t, y, 0)
	require.Less(t, y, m.viewportHeight)

	m.Update(nil, qtui.MouseEvent{
		Action: qtui.MouseActionPress,
		Button: qtui.MouseButtonLeft,
		X:      target.xStart,
		Y:      y,
	})

	require.Equal(t, want, copied)
	require.Equal(t, want, osCopied)

	view := stripAnsi(m.viewport.View())
	require.Contains(t, view, optionCopyButtonCopiedLabel)

	// Once the feedback duration expires, the label should revert.
	now = base.Add(optionCopyFeedbackDuration + time.Millisecond)
	m.refreshViewport(false)

	view = stripAnsi(m.viewport.View())
	require.Contains(t, view, optionCopyButtonLabel)
	require.False(t, strings.Contains(view, optionCopyButtonCopiedLabel))
}
