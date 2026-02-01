package tuicontrols

import (
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/q/uni"
	"github.com/stretchr/testify/require"
)

func TestWrapPromptedText_LongSingleLogicalLineWraps(t *testing.T) {
	got := WrapPromptedText(">>", 10, "hello world")
	require.Equal(t, []string{">>hello ", "  world"}, got)

	for _, line := range got {
		require.LessOrEqual(t, uni.TextWidth(line, nil), 10)
	}
}

func TestWrapPromptedText_MultipleLogicalLines_PreservesEmptyLogicalLine(t *testing.T) {
	got := WrapPromptedText(">>", 10, "a\n\nB")
	require.Equal(t, []string{">>a", "  ", "  B"}, got)
}

func TestWrapPromptedText_HangingIndentAlignsToPromptWidth(t *testing.T) {
	prompt := ">>"
	width := 6

	got := WrapPromptedText(prompt, width, "abcde")
	require.Equal(t, []string{">>abc", "  de"}, got)

	promptWidthCells := uni.TextWidth(prompt, nil)
	indent := strings.Repeat(" ", minInt(promptWidthCells, width))
	require.True(t, strings.HasPrefix(got[0], prompt))
	require.True(t, strings.HasPrefix(got[1], indent))
	require.False(t, strings.HasPrefix(got[1], prompt))
}

func TestTextArea_View_MatchesWrapPromptedText_NoStyling(t *testing.T) {
	ta := NewTextArea(10, 10)
	ta.Prompt = ">>"
	ta.SetContents("hello world")
	ta.CaretColor = nil
	ta.BackgroundColor = nil
	ta.ForegroundColor = nil
	ta.PlaceholderColor = nil

	want := WrapPromptedText(ta.Prompt, ta.Width(), ta.Contents())

	// Prevent the fixed-height view from adding extra blank rows that WrapPromptedText doesn't return.
	ta.SetSize(ta.Width(), len(want))
	rows := strings.Split(ta.View(), "\n")
	require.Equal(t, want, rows)
}
