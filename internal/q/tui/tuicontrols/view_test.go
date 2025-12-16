package tuicontrols

import (
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/q/termformat"
	"github.com/stretchr/testify/require"
)

func TestView_RendersExactlyHeightRows_NoWidthPadding(t *testing.T) {
	v := NewView(10, 3)
	v.SetContent("hi")

	got := strings.Split(v.View(), "\n")
	require.Equal(t, []string{"hi", "", ""}, got)
}

func TestView_ClipsWithANSIAwareCut(t *testing.T) {
	red := termformat.ANSIRed.ANSISequence(false)
	line := red + "hello" + termformat.ANSIReset

	v := NewView(2, 1)
	v.SetContent(line)

	want := termformat.Cut(line, 0, termformat.TextWidthWithANSICodes(line)-2)
	require.Equal(t, want, v.View())
}

func TestView_ScrollAndPercent(t *testing.T) {
	v := NewView(10, 2)
	v.SetContent("a\nb\nc")

	require.Equal(t, "a\nb", v.View())
	require.True(t, v.AtTop())
	require.False(t, v.AtBottom())
	require.Equal(t, 0, v.ScrollPercent())

	v.ScrollDown(1)
	require.Equal(t, "b\nc", v.View())
	require.False(t, v.AtTop())
	require.True(t, v.AtBottom())
	require.Equal(t, 100, v.ScrollPercent())

	v.ScrollUp(1)
	require.Equal(t, "a\nb", v.View())
	require.True(t, v.AtTop())
	require.False(t, v.AtBottom())
	require.Equal(t, 0, v.ScrollPercent())
}

func TestView_ScrollToBottom_Normalizes(t *testing.T) {
	v := NewView(10, 5)
	v.SetContent("a\nb\nc")

	v.ScrollToBottom()
	require.Equal(t, 0, v.Offset())
	require.True(t, v.AtTop())
	require.True(t, v.AtBottom())
	require.Equal(t, 0, v.ScrollPercent())

	v.SetSize(10, 2)
	v.ScrollToBottom()
	require.Equal(t, 1, v.Offset())
	require.Equal(t, "b\nc", v.View())
	require.True(t, v.AtBottom())
	require.Equal(t, 100, v.ScrollPercent())
}

func TestView_SetContent_ClampsOffsetToInvariant(t *testing.T) {
	v := NewView(10, 2)
	v.SetContent("a\nb\nc")
	v.ScrollToBottom()
	require.Equal(t, 1, v.Offset())

	v.SetContent("x")
	require.Equal(t, 0, v.Offset())
	require.Equal(t, "x\n", v.View())
}

func TestView_EmptyLineBackgroundColor(t *testing.T) {
	bg := termformat.ANSIRed.ANSISequence(true)

	v := NewView(3, 3)
	v.SetEmptyLineBackgroundColor(termformat.ANSIRed)
	v.SetContent("x")

	require.Equal(t, []string{"x", bg + "   " + termformat.ANSIReset, bg + "   " + termformat.ANSIReset}, strings.Split(v.View(), "\n"))
}

func TestView_EmptyLineBackgroundColor_AppliesToAllRowsWhenContentIsEmptyString(t *testing.T) {
	bg := termformat.ANSIRed.ANSISequence(true)

	v := NewView(3, 2)
	v.SetEmptyLineBackgroundColor(termformat.ANSIRed)
	v.SetContent("")

	require.Equal(t, []string{bg + "   " + termformat.ANSIReset, bg + "   " + termformat.ANSIReset}, strings.Split(v.View(), "\n"))
}

func TestView_EmptyLineBackgroundColor_NotAppliedToExistingEmptyLines(t *testing.T) {
	v := NewView(3, 3)
	v.SetEmptyLineBackgroundColor(termformat.ANSIRed)
	v.SetContent("x\n\nz")

	require.Equal(t, []string{"x", "", "z"}, strings.Split(v.View(), "\n"))
}

func TestView_ScrollPercent_ContentFitsButOffsetNotTop(t *testing.T) {
	v := NewView(10, 1)
	v.SetContent("a\nb")
	v.ScrollToBottom()
	require.Equal(t, 1, v.Offset())

	v.SetSize(10, 5)
	require.True(t, v.AtBottom())
	require.False(t, v.AtTop())
	require.Equal(t, 100, v.ScrollPercent())
}
