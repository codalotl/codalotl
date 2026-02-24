package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/q/termformat"
	qtui "github.com/codalotl/codalotl/internal/q/tui"
	"github.com/stretchr/testify/require"
)

func TestOverlayModeCtrlOToggles(t *testing.T) {
	m := newModel(colorPalette{}, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil, nil)
	m.Update(nil, qtui.ResizeEvent{Width: 80, Height: 20})

	require.False(t, m.overlayMode)

	m.Update(nil, qtui.KeyEvent{ControlKey: qtui.ControlKeyCtrlO})
	require.True(t, m.overlayMode)

	m.Update(nil, qtui.KeyEvent{ControlKey: qtui.ControlKeyCtrlO})
	require.False(t, m.overlayMode)
}

func TestOverlayModeDoubleClickToggles(t *testing.T) {
	m := newModel(colorPalette{}, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil, nil)
	m.Update(nil, qtui.ResizeEvent{Width: 80, Height: 20})

	base := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return base }

	require.False(t, m.overlayMode)

	m.Update(nil, qtui.MouseEvent{Action: qtui.MouseActionPress, Button: qtui.MouseButtonLeft, X: 3, Y: 3})
	require.False(t, m.overlayMode)

	m.Update(nil, qtui.MouseEvent{Action: qtui.MouseActionPress, Button: qtui.MouseButtonLeft, X: 3, Y: 3})
	require.True(t, m.overlayMode)
}

func TestOverlayModeCopyCopiesRenderedMessageAndShowsFeedback(t *testing.T) {
	palette := colorPalette{
		colorized:          true,
		primaryBackground:  termformat.ANSIColor(0),
		accentBackground:   termformat.ANSIColor(1),
		primaryForeground:  termformat.ANSIColor(7),
		accentForeground:   termformat.ANSIColor(7),
		colorfulForeground: termformat.ANSIColor(6),
	}

	m := newModel(palette, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil, nil)
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

	// Enter overlay mode, which makes copy buttons appear in the separator rows.
	m.Update(nil, qtui.KeyEvent{ControlKey: qtui.ControlKeyCtrlO})
	require.True(t, m.overlayMode)

	var target overlayTarget
	found := false
	for _, t := range m.overlayTargets {
		if t.kind == overlayTargetCopy {
			target = t
			found = true
			break
		}
	}
	require.True(t, found)

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
	require.Contains(t, view, overlayCopyButtonCopiedLabel)

	// Once the feedback duration expires, the label should revert.
	now = base.Add(overlayCopyFeedbackDuration + time.Millisecond)
	m.refreshViewport(false)

	view = stripAnsi(m.viewport.View())
	require.Contains(t, view, overlayCopyButtonLabel)
	require.False(t, strings.Contains(view, overlayCopyButtonCopiedLabel))
}

type stubToolFormatter struct{}

func (stubToolFormatter) FormatEvent(ev agent.Event, _ int) string {
	if ev.ToolCall != nil || ev.ToolResult != nil {
		return "• Read main.go"
	}
	return ""
}

func TestOverlayModeDetailsOpensDialogForToolMessage(t *testing.T) {
	palette := colorPalette{
		colorized:          true,
		primaryBackground:  termformat.ANSIColor(0),
		accentBackground:   termformat.ANSIColor(1),
		primaryForeground:  termformat.ANSIColor(7),
		accentForeground:   termformat.ANSIColor(7),
		colorfulForeground: termformat.ANSIColor(6),
		borderColor:        termformat.ANSIColor(7),
	}

	m := newModel(palette, stubToolFormatter{}, nil, sessionConfig{}, nil, nil, nil, nil)
	m.Update(nil, qtui.ResizeEvent{Width: 80, Height: 30})

	callID := "call-123"
	call := &llmstream.ToolCall{CallID: callID, Name: "read_file", Type: "function", Input: `{"path":"main.go"}`}
	result := &llmstream.ToolResult{CallID: callID, Name: "read_file", Type: "function", Result: `{"content":"hi"}`}
	m.handleAgentEvent(agent.Event{Type: agent.EventTypeToolComplete, Tool: "read_file", ToolCall: call, ToolResult: result})
	m.refreshViewport(true)

	m.Update(nil, qtui.KeyEvent{ControlKey: qtui.ControlKeyCtrlO})
	require.True(t, m.overlayMode)

	var detailsTarget overlayTarget
	found := false
	for _, t := range m.overlayTargets {
		if t.kind == overlayTargetDetails && t.messageIndex == 0 {
			detailsTarget = t
			found = true
			break
		}
	}
	require.True(t, found)

	y := detailsTarget.contentLine - m.viewport.Offset()
	m.Update(nil, qtui.MouseEvent{
		Action: qtui.MouseActionPress,
		Button: qtui.MouseButtonLeft,
		X:      detailsTarget.xStart,
		Y:      y,
	})

	require.NotNil(t, m.detailsDialog)

	view := stripAnsi(m.View())
	require.Contains(t, view, "Read main.go")
	require.Contains(t, view, "Tool: read_file")
	require.Contains(t, view, `"path": "main.go"`)
	require.Contains(t, view, `"content": "hi"`)
	require.Contains(t, view, "ESC to close")

	m.Update(nil, qtui.KeyEvent{ControlKey: qtui.ControlKeyEsc})
	require.Nil(t, m.detailsDialog)
}

func TestOverlayModeDetailsOpensDialogForContextStatusMessage(t *testing.T) {
	palette := colorPalette{
		colorized:          true,
		primaryBackground:  termformat.ANSIColor(0),
		accentBackground:   termformat.ANSIColor(1),
		primaryForeground:  termformat.ANSIColor(7),
		accentForeground:   termformat.ANSIColor(7),
		colorfulForeground: termformat.ANSIColor(6),
		borderColor:        termformat.ANSIColor(7),
	}

	m := newModel(palette, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil, nil)
	m.Update(nil, qtui.ResizeEvent{Width: 80, Height: 30})

	index := m.appendContextStatusMessage("Gathering context for some/pkg", packageContextStatusSuccess)
	m.messages[index].contextDetails = "context payload\nline2"
	m.messages[index].contextError = ""
	m.refreshViewport(true)

	m.Update(nil, qtui.KeyEvent{ControlKey: qtui.ControlKeyCtrlO})
	require.True(t, m.overlayMode)

	var detailsTarget overlayTarget
	found := false
	for _, t := range m.overlayTargets {
		if t.kind == overlayTargetDetails && t.messageIndex == index {
			detailsTarget = t
			found = true
			break
		}
	}
	require.True(t, found)

	y := detailsTarget.contentLine - m.viewport.Offset()
	m.Update(nil, qtui.MouseEvent{
		Action: qtui.MouseActionPress,
		Button: qtui.MouseButtonLeft,
		X:      detailsTarget.xStart,
		Y:      y,
	})

	require.NotNil(t, m.detailsDialog)
	view := stripAnsi(m.View())
	require.Contains(t, view, "Gathering context for some/pkg")
	require.Contains(t, view, "Status: success")
	require.Contains(t, view, "Context:")
	require.Contains(t, view, "context payload")
}

func TestDetailsDialogScrollKeepsForegroundColor(t *testing.T) {
	palette := colorPalette{
		colorized:         true,
		primaryBackground: termformat.ANSIColor(0),
		accentBackground:  termformat.ANSIColor(1),
		primaryForeground: termformat.ANSIColor(3),
		accentForeground:  termformat.ANSIColor(7),
		borderColor:       termformat.ANSIColor(7),
	}

	m := newModel(palette, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil, nil)
	m.Update(nil, qtui.ResizeEvent{Width: 100, Height: 35})

	index := m.appendContextStatusMessage("Gathering context for some/pkg", packageContextStatusSuccess)
	m.messages[index].contextDetails = strings.Repeat("line\n", 200)
	m.refreshViewport(true)

	m.Update(nil, qtui.KeyEvent{ControlKey: qtui.ControlKeyCtrlO})
	require.True(t, m.overlayMode)

	// Open details dialog for the context status message.
	var detailsTarget overlayTarget
	found := false
	for _, t := range m.overlayTargets {
		if t.kind == overlayTargetDetails && t.messageIndex == index {
			detailsTarget = t
			found = true
			break
		}
	}
	require.True(t, found)
	y := detailsTarget.contentLine - m.viewport.Offset()
	m.Update(nil, qtui.MouseEvent{
		Action: qtui.MouseActionPress,
		Button: qtui.MouseButtonLeft,
		X:      detailsTarget.xStart,
		Y:      y,
	})
	require.NotNil(t, m.detailsDialog)

	// Verify initial foreground is set.
	initialView := m.detailsDialog.view.View()
	requireColorEqual(t, palette.primaryForeground, colorAt(initialView, 0, 0, false))

	// Scroll within the dialog and ensure the new top row still has the foreground style.
	m.detailsDialogScrollDown(1)
	scrolledView := m.detailsDialog.view.View()
	require.GreaterOrEqual(t, termformat.BlockHeight(scrolledView), 2)
	requireColorEqual(t, palette.primaryForeground, colorAt(scrolledView, 0, 1, false))
}
