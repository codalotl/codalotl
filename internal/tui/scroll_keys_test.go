package tui

import (
	"fmt"
	"testing"

	"github.com/codalotl/codalotl/internal/q/termformat"
	qtui "github.com/codalotl/codalotl/internal/q/tui"
	"github.com/stretchr/testify/require"
)

func TestScrollKeysScrollMessagesAreaNotTextArea(t *testing.T) {
	palette := colorPalette{
		primaryBackground: termformat.ANSIColor(0),
		accentBackground:  termformat.ANSIColor(1),
		primaryForeground: termformat.ANSIColor(7),
		accentForeground:  termformat.ANSIColor(7),
	}
	m := newModel(palette, noopFormatter{}, nil, sessionConfig{}, nil, nil)
	m.Update(nil, qtui.ResizeEvent{Width: 80, Height: 20})

	// Create enough content to make the viewport scrollable.
	for i := 0; i < 120; i++ {
		m.messages = append(m.messages, chatMessage{
			kind:        messageKindSystem,
			userMessage: fmt.Sprintf("line %d", i),
		})
	}
	m.refreshViewport(true)
	require.True(t, m.viewport.AtBottom())

	// Populate the text area and place the caret somewhere predictable. The scroll keys
	// must not be routed to the text area (per spec).
	m.textarea.SetContents("hello world")
	m.textarea.MoveToEndOfText()
	startCaret := m.textarea.CaretPositionByteOffset()

	startOffset := m.viewport.Offset()

	// PageUp scrolls up.
	m.Update(nil, qtui.KeyEvent{ControlKey: qtui.ControlKeyPageUp})
	require.Less(t, m.viewport.Offset(), startOffset)
	require.Equal(t, startCaret, m.textarea.CaretPositionByteOffset())

	scrolledUpOffset := m.viewport.Offset()

	// PageDown scrolls down.
	m.Update(nil, qtui.KeyEvent{ControlKey: qtui.ControlKeyPageDown})
	require.Greater(t, m.viewport.Offset(), scrolledUpOffset)
	require.Equal(t, startCaret, m.textarea.CaretPositionByteOffset())

	// Home jumps to the top.
	m.Update(nil, qtui.KeyEvent{ControlKey: qtui.ControlKeyHome})
	require.True(t, m.viewport.AtTop())
	require.Equal(t, startCaret, m.textarea.CaretPositionByteOffset())

	// End jumps to the bottom.
	m.Update(nil, qtui.KeyEvent{ControlKey: qtui.ControlKeyEnd})
	require.True(t, m.viewport.AtBottom())
	require.Equal(t, startCaret, m.textarea.CaretPositionByteOffset())
}
