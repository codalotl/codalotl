package tui

import (
	"strings"
	"testing"

	qtui "github.com/codalotl/codalotl/internal/q/tui"
	"github.com/stretchr/testify/require"
)

func TestTextareaHeightUsesDisplayLinesForWrappedInput(t *testing.T) {
	m := newModel(colorPalette{}, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil)

	// Keep the viewport/text area narrow so a single long logical line wraps into
	// multiple user-visible lines.
	m.Update(nil, qtui.ResizeEvent{Width: 20, Height: 20})
	require.Equal(t, 4, m.textAreaHeight) // 3 visible lines + 1 margin top

	m.textarea.SetContents(strings.Repeat("a", 70)) // no '\n' => relies on wrapping
	m.updateTextareaHeight()

	require.Greater(t, m.textAreaHeight, 4)
	require.Equal(t, m.textAreaHeight-1, m.textarea.Height())
}

func TestTextareaHeightClampsToMaxVisibleLinesUsingWrappedInput(t *testing.T) {
	m := newModel(colorPalette{}, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil)

	m.Update(nil, qtui.ResizeEvent{Width: 20, Height: 20})

	// A single long line that would wrap well beyond maxInputLines.
	m.textarea.SetContents(strings.Repeat("a", 1000))
	m.updateTextareaHeight()

	require.Equal(t, maxInputLines+1, m.textAreaHeight) // +1 margin top
	require.Equal(t, maxInputLines, m.textarea.Height())
}
