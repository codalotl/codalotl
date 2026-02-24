package tui

import (
	"fmt"
	"testing"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/q/termformat"
	qtui "github.com/codalotl/codalotl/internal/q/tui"
	"github.com/stretchr/testify/require"
)

func TestMouseWheelScrollsMessagesArea(t *testing.T) {
	palette := colorPalette{
		primaryBackground: termformat.ANSIColor(0),
		accentBackground:  termformat.ANSIColor(1),
		primaryForeground: termformat.ANSIColor(7),
		accentForeground:  termformat.ANSIColor(7),
	}
	m := newModel(palette, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil, nil)

	m.Update(nil, qtui.ResizeEvent{Width: 80, Height: 20})

	// Create enough content to make the viewport scrollable.
	for i := 0; i < 80; i++ {
		m.messages = append(m.messages, chatMessage{
			kind:        messageKindSystem,
			userMessage: fmt.Sprintf("line %d", i),
		})
	}
	m.refreshViewport(true)
	require.True(t, m.viewport.AtBottom())

	startOffset := m.viewport.Offset()

	// Wheel-up scrolls up.
	m.Update(nil, qtui.MouseEvent{Action: qtui.MouseActionPress, Button: qtui.MouseButtonWheelUp})
	require.Less(t, m.viewport.Offset(), startOffset)
	require.False(t, m.viewport.AtBottom())

	afterUp := m.viewport.Offset()

	// Wheel-down scrolls down.
	m.Update(nil, qtui.MouseEvent{Action: qtui.MouseActionPress, Button: qtui.MouseButtonWheelDown})
	require.Greater(t, m.viewport.Offset(), afterUp)
}

func TestMouseScrollDoesNotDisableManualScrollDuringAgentEvents(t *testing.T) {
	palette := colorPalette{
		primaryBackground: termformat.ANSIColor(0),
		accentBackground:  termformat.ANSIColor(1),
		primaryForeground: termformat.ANSIColor(7),
		accentForeground:  termformat.ANSIColor(7),
	}
	m := newModel(palette, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil, nil)
	m.Update(nil, qtui.ResizeEvent{Width: 80, Height: 20})

	for i := 0; i < 80; i++ {
		m.messages = append(m.messages, chatMessage{
			kind:        messageKindSystem,
			userMessage: fmt.Sprintf("line %d", i),
		})
	}
	m.refreshViewport(true)

	// Scroll up a bit.
	m.Update(nil, qtui.MouseEvent{Action: qtui.MouseActionPress, Button: qtui.MouseButtonWheelUp})
	require.False(t, m.viewport.AtBottom())
	offset := m.viewport.Offset()

	// Agent events should not force-scroll back to the bottom if the user is
	// currently scrolled up.
	m.handleAgentEvent(agent.Event{Type: agent.EventTypeAssistantText})
	require.Equal(t, offset, m.viewport.Offset())
}

func TestAgentEventsKeepAutoScrollingWhenAtBottom(t *testing.T) {
	palette := colorPalette{
		primaryBackground: termformat.ANSIColor(0),
		accentBackground:  termformat.ANSIColor(1),
		primaryForeground: termformat.ANSIColor(7),
		accentForeground:  termformat.ANSIColor(7),
	}
	m := newModel(palette, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil, nil)
	m.Update(nil, qtui.ResizeEvent{Width: 80, Height: 20})

	for i := 0; i < 80; i++ {
		m.messages = append(m.messages, chatMessage{
			kind:        messageKindSystem,
			userMessage: fmt.Sprintf("line %d", i),
		})
	}
	m.refreshViewport(true)
	require.True(t, m.viewport.AtBottom())

	m.handleAgentEvent(agent.Event{Type: agent.EventTypeAssistantText})
	require.True(t, m.viewport.AtBottom())
}
