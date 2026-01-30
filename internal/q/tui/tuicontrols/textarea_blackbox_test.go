package tuicontrols_test

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/codalotl/codalotl/internal/q/termformat"
	"github.com/codalotl/codalotl/internal/q/tui"
	"github.com/codalotl/codalotl/internal/q/tui/tuicontrols"
	"github.com/codalotl/codalotl/internal/q/uni"
	"github.com/stretchr/testify/require"
)

func TestTextArea_Size(t *testing.T) {
	ta := tuicontrols.NewTextArea(10, 3)
	require.Equal(t, 10, ta.Width())
	require.Equal(t, 3, ta.Height())

	ta.SetSize(4, 1)
	require.Equal(t, 4, ta.Width())
	require.Equal(t, 1, ta.Height())
}

func TestTextArea_SetContents_SanitizesTabsCRAndASCIIControls(t *testing.T) {
	ta := tuicontrols.NewTextArea(20, 2)

	ta.SetContents("a\tb\rc" + string([]byte{0x1b}) + "d" + string([]byte{0x7f}))
	got := ta.Contents()

	require.Contains(t, got, "a    b")
	require.NotContains(t, got, "\t")
	require.NotContains(t, got, "\r")
	require.NotContains(t, got, string([]byte{0x1b}))
	require.NotContains(t, got, string([]byte{0x7f}))

	require.Regexp(t, regexp.MustCompile(`\\x(?i:1b)`), got)
	require.Regexp(t, regexp.MustCompile(`\\x(?i:7f)`), got)
	requireNoDisallowedASCIIControlsExceptNewline(t, got)
	require.True(t, utf8.ValidString(got))
}

func TestTextArea_SetContents_InvalidUTF8_StoredContentsIsValidUTF8(t *testing.T) {
	ta := tuicontrols.NewTextArea(20, 2)

	ta.SetContents(string([]byte{0xff, 'a'}))
	got := ta.Contents()

	require.True(t, utf8.ValidString(got))
	require.False(t, bytes.Contains([]byte(got), []byte{0xff}))
	requireNoDisallowedASCIIControlsExceptNewline(t, got)
}

func TestTextArea_InsertString_AdvancesCaret_AndSanitizes(t *testing.T) {
	ta := tuicontrols.NewTextArea(20, 2)

	ta.InsertString("hi")
	require.Equal(t, "hi", ta.Contents())
	require.Equal(t, len("hi"), ta.CaretPositionByteOffset())
	requireCaretOnGraphemeBoundary(t, ta)

	ta.SetContents("")
	ta.InsertString("\t")
	require.Equal(t, "    ", ta.Contents())
	requireNoDisallowedASCIIControlsExceptNewline(t, ta.Contents())
}

func TestTextArea_InsertString_AllowsNewlines(t *testing.T) {
	ta := tuicontrols.NewTextArea(20, 3)

	ta.InsertString("a\nb")
	require.Equal(t, "a\nb", ta.Contents())
	requireNoDisallowedASCIIControlsExceptNewline(t, ta.Contents())
}

func TestTextArea_MoveLeftRight_ByGraphemeCluster(t *testing.T) {
	ta := tuicontrols.NewTextArea(20, 2)
	ta.SetContents("a\u0301b") // "a" + combining acute accent + "b"

	ta.MoveToEndOfText()
	require.Equal(t, len(ta.Contents()), ta.CaretPositionByteOffset())

	ta.MoveLeft() // over "b"
	require.Equal(t, len(ta.Contents())-len("b"), ta.CaretPositionByteOffset())
	requireCaretOnGraphemeBoundary(t, ta)

	ta.MoveLeft() // over "á" (grapheme cluster)
	require.Equal(t, 0, ta.CaretPositionByteOffset())
	requireCaretOnGraphemeBoundary(t, ta)

	ta.MoveRight()
	require.Equal(t, len("a\u0301"), ta.CaretPositionByteOffset())
	requireCaretOnGraphemeBoundary(t, ta)

	ta.MoveRight()
	require.Equal(t, len(ta.Contents()), ta.CaretPositionByteOffset())
	requireCaretOnGraphemeBoundary(t, ta)
}

func TestTextArea_SetCaretPosition_ClampsToValidLogicalRowCol(t *testing.T) {
	ta := tuicontrols.NewTextArea(20, 2)
	ta.SetContents("hi\nworld")

	ta.SetCaretPosition(999, 999)
	row, col := ta.CaretPositionRowCol()
	require.Equal(t, 1, row)
	require.Equal(t, 5, col)
	require.Equal(t, len(ta.Contents()), ta.CaretPositionByteOffset())

	ta.SetCaretPosition(-1, -1)
	row, col = ta.CaretPositionRowCol()
	require.Equal(t, 0, row)
	require.Equal(t, 0, col)
	require.Equal(t, 0, ta.CaretPositionByteOffset())
}

func TestTextArea_CaretPositionCurrentLineByteOffset(t *testing.T) {
	ta := tuicontrols.NewTextArea(20, 2)
	ta.SetContents("ab\ncde")

	ta.SetCaretPosition(1, 2) // "cd|e"
	require.Equal(t, 2, ta.CaretPositionCurrentLineByteOffset())
	require.Equal(t, 3+2, ta.CaretPositionByteOffset()) // len("ab\n") + 2

	row, col := ta.CaretPositionRowCol()
	require.Equal(t, 1, row)
	require.Equal(t, 2, col)
}

func TestTextArea_MoveToBeginningEndOfLine_IsLogicalLine_NotWrappedLine(t *testing.T) {
	ta := tuicontrols.NewTextArea(4, 4)
	ta.SetContents("012345") // wraps visually, but is one logical line

	ta.SetCaretPosition(0, 5)
	ta.MoveToBeginningOfLine()
	require.Equal(t, 0, ta.CaretPositionByteOffset())

	ta.MoveToEndOfLine()
	require.Equal(t, len("012345"), ta.CaretPositionByteOffset())
}

func TestTextArea_MoveToBeginningEndOfText(t *testing.T) {
	ta := tuicontrols.NewTextArea(20, 2)
	ta.SetContents("ab\ncd")
	ta.SetCaretPosition(0, 1)

	ta.MoveToEndOfText()
	row, col := ta.CaretPositionRowCol()
	require.Equal(t, 1, row)
	require.Equal(t, 2, col)
	require.Equal(t, len(ta.Contents()), ta.CaretPositionByteOffset())

	ta.MoveToBeginningOfText()
	row, col = ta.CaretPositionRowCol()
	require.Equal(t, 0, row)
	require.Equal(t, 0, col)
	require.Equal(t, 0, ta.CaretPositionByteOffset())
}

func TestTextArea_MoveWordRight_Semantics(t *testing.T) {
	ta := tuicontrols.NewTextArea(80, 5)
	ta.SetContents("hi  there world")

	ta.SetCaretPosition(0, 0) // "|hi  there world"
	ta.MoveWordRight()        // end of current word
	require.Equal(t, 2, ta.CaretPositionByteOffset())

	ta.MoveWordRight() // in whitespace: skip ws, then skip next word
	require.Equal(t, 9, ta.CaretPositionByteOffset())

	ta.MoveWordRight()
	require.Equal(t, len(ta.Contents()), ta.CaretPositionByteOffset())

	ta.SetContents("hi\nthere")
	ta.SetCaretPosition(0, 2) // "hi|\nthere"
	ta.MoveWordRight()
	row, col := ta.CaretPositionRowCol()
	require.Equal(t, 1, row)
	require.Equal(t, 5, col)
}

func TestTextArea_MoveWordLeft_BeginsPreviousWord(t *testing.T) {
	ta := tuicontrols.NewTextArea(80, 5)
	ta.SetContents("hi  there world")
	ta.MoveToEndOfText()

	ta.MoveWordLeft()
	require.Equal(t, 10, ta.CaretPositionByteOffset()) // "|world" begins at byte 10

	ta.MoveWordLeft()
	require.Equal(t, 4, ta.CaretPositionByteOffset()) // "|there" begins at byte 4

	ta.MoveWordLeft()
	require.Equal(t, 0, ta.CaretPositionByteOffset()) // "|hi"
}

func TestTextArea_MoveWord_PunctuationRunsAreWordUnits(t *testing.T) {
	ta := tuicontrols.NewTextArea(80, 5)
	ta.SetContents("foo..bar baz")

	ta.SetCaretPosition(0, 0)
	ta.MoveWordRight()
	require.Equal(t, len("foo"), ta.CaretPositionByteOffset())

	ta.MoveWordRight()
	require.Equal(t, len("foo.."), ta.CaretPositionByteOffset())

	ta.MoveWordRight()
	require.Equal(t, len("foo..bar"), ta.CaretPositionByteOffset())

	ta.MoveWordRight()
	require.Equal(t, len("foo..bar baz"), ta.CaretPositionByteOffset())

	ta.MoveToEndOfText()
	ta.MoveWordLeft()
	require.Equal(t, len("foo..bar "), ta.CaretPositionByteOffset())

	ta.MoveWordLeft()
	require.Equal(t, len("foo.."), ta.CaretPositionByteOffset()) // start of "bar"

	ta.MoveWordLeft()
	require.Equal(t, len("foo"), ta.CaretPositionByteOffset()) // start of ".."

	ta.MoveWordLeft()
	require.Equal(t, 0, ta.CaretPositionByteOffset()) // start of "foo"
}

func TestTextArea_DeleteLeftRight_DeletesByGraphemeCluster(t *testing.T) {
	ta := tuicontrols.NewTextArea(20, 2)
	ta.SetContents("a\u0301b")
	ta.MoveToEndOfText()

	ta.DeleteLeft()
	require.Equal(t, "a\u0301", ta.Contents())
	require.Equal(t, len("a\u0301"), ta.CaretPositionByteOffset())
	requireCaretOnGraphemeBoundary(t, ta)

	ta.DeleteLeft()
	require.Equal(t, "", ta.Contents())
	require.Equal(t, 0, ta.CaretPositionByteOffset())

	ta.SetContents("a\u0301b")
	ta.MoveToBeginningOfText()
	ta.DeleteRight()
	require.Equal(t, "b", ta.Contents())
	require.Equal(t, 0, ta.CaretPositionByteOffset())
}

func TestTextArea_DeleteLeftRight_DeletesNewlinesAndJoinsLogicalLines(t *testing.T) {
	ta := tuicontrols.NewTextArea(20, 2)
	ta.SetContents("ab\ncd")

	ta.SetCaretPosition(1, 0) // at start of second line
	ta.DeleteLeft()
	require.Equal(t, "abcd", ta.Contents())
	row, col := ta.CaretPositionRowCol()
	require.Equal(t, 0, row)
	require.Equal(t, 2, col)

	ta.SetContents("ab\ncd")
	ta.SetCaretPosition(0, 2) // end of first line
	ta.DeleteRight()
	require.Equal(t, "abcd", ta.Contents())
	row, col = ta.CaretPositionRowCol()
	require.Equal(t, 0, row)
	require.Equal(t, 2, col)
}

func TestTextArea_DeleteToEndOfLine_DoesNotDeleteNewline(t *testing.T) {
	ta := tuicontrols.NewTextArea(20, 2)
	ta.SetContents("hello\nworld")
	ta.SetCaretPosition(0, 2) // "he|llo"

	ta.DeleteToEndOfLine()
	require.Equal(t, "he\nworld", ta.Contents())
	require.Contains(t, ta.Contents(), "\n")
	require.Equal(t, 2, ta.CaretPositionByteOffset())
}

func TestTextArea_DeleteToBeginningOfLine_DoesNotDeleteNewline(t *testing.T) {
	ta := tuicontrols.NewTextArea(20, 2)
	ta.SetContents("hello\nworld")
	ta.SetCaretPosition(1, 3) // "wor|ld"

	ta.DeleteToBeginningOfLine()
	require.Equal(t, "hello\nld", ta.Contents())
	require.Contains(t, ta.Contents(), "\n")
	row, col := ta.CaretPositionRowCol()
	require.Equal(t, 1, row)
	require.Equal(t, 0, col)
}

func TestTextArea_MoveUpDown_ByDisplayLines_PreservesVisualColumn(t *testing.T) {
	ta := tuicontrols.NewTextArea(5, 10)
	ta.SetContents("0123456789") // display lines: "0123", "4567", "89"

	ta.SetCaretPosition(0, 3)
	drow, dcol := ta.CaretDisplayPositionRowCol()
	require.Equal(t, 0, drow)
	require.Equal(t, 3, dcol)

	ta.MoveDown()
	drow, dcol = ta.CaretDisplayPositionRowCol()
	require.Equal(t, 1, drow)
	require.Equal(t, 3, dcol)

	ta.MoveDown() // last line is shorter: should clamp to end
	drow, dcol = ta.CaretDisplayPositionRowCol()
	require.Equal(t, 2, drow)
	require.Equal(t, 2, dcol)

	ta.MoveUp()
	drow, dcol = ta.CaretDisplayPositionRowCol()
	require.Equal(t, 1, drow)
	require.Equal(t, 3, dcol)
}

func TestTextArea_Wrapping_WordBoundaryAndGraphemeFallback(t *testing.T) {
	ta := tuicontrols.NewTextArea(6, 10)
	ta.SetContents("hi there")

	lines := ta.ClippedDisplayContents()
	require.Equal(t, "hi there", strings.Join(lines, ""))
	require.GreaterOrEqual(t, ta.DisplayLines(), 2)
	hasThereInSingleSegment := false
	for _, line := range lines {
		require.LessOrEqual(t, uni.TextWidth(line, nil), 6)
		if strings.Contains(line, "there") {
			hasThereInSingleSegment = true
		}
	}
	require.True(t, hasThereInSingleSegment, "expected word wrap to avoid splitting a whole word that fits on a new line")

	ta.SetContents("abcdef")
	require.Equal(t, 2, ta.DisplayLines())
	require.Equal(t, []string{"abcde", "f"}, ta.ClippedDisplayContents())
}

func TestTextArea_Wrapping_UsesTerminalCellWidths(t *testing.T) {
	ta := tuicontrols.NewTextArea(3, 10)
	ta.SetContents("世a") // "世" is width 2 (default locale)

	require.Equal(t, 2, ta.DisplayLines())
	require.Equal(t, []string{"世", "a"}, ta.ClippedDisplayContents())
}

func TestTextArea_Wrapping_BreaksAfterSlashPipeAndHyphenBetweenAlnum(t *testing.T) {
	ta := tuicontrols.NewTextArea(4, 10)

	ta.SetContents("aa/bb")
	require.Equal(t, []string{"aa/", "bb"}, ta.ClippedDisplayContents())

	ta.SetContents("aa|bb")
	require.Equal(t, []string{"aa|", "bb"}, ta.ClippedDisplayContents())

	ta.SetContents("aa-bb")
	require.Equal(t, []string{"aa-", "bb"}, ta.ClippedDisplayContents())
}

func TestTextArea_Wrapping_DoesNotBreakAfterDotCommaBetweenAlnum(t *testing.T) {
	ta := tuicontrols.NewTextArea(4, 10)

	ta.SetContents("aa.bb")
	require.Equal(t, []string{"aa", ".bb"}, ta.ClippedDisplayContents())

	ta.SetContents("aa,bb")
	require.Equal(t, []string{"aa", ",bb"}, ta.ClippedDisplayContents())
}

func TestTextArea_Wrapping_WordJoinerPreventsBreaksAroundIt(t *testing.T) {
	ta := tuicontrols.NewTextArea(5, 10)
	ta.SetContents("aa/\u2060bb")

	require.Equal(t, []string{"aa", "/\u2060bb"}, ta.ClippedDisplayContents())
}

func TestTextArea_Prompt_ReducesAvailableWidth_AndAlignsSubsequentLines(t *testing.T) {
	ta := tuicontrols.NewTextArea(6, 3)
	ta.Prompt = ">>"
	ta.CaretColor = termformat.ANSIRed
	ta.SetContents("abcde") // effective user width is 4 (but max graphic width is 3)

	require.Equal(t, 2, ta.DisplayLines())
	require.Equal(t, []string{"abc", "de"}, ta.ClippedDisplayContents())

	rows := splitRows(t, ta.View(), ta.Height())
	r0 := stripANSICodes(rows[0])
	r1 := stripANSICodes(rows[1])
	require.True(t, strings.HasPrefix(r0, ">>"))
	require.Contains(t, r0, "abc")
	require.True(t, strings.HasPrefix(r1, "  "))
	require.Contains(t, r1, "de")
	require.NotContains(t, r1, ">>")
}

func TestTextArea_Placeholder_ShownWhenContentsEmpty(t *testing.T) {
	ta := tuicontrols.NewTextArea(10, 2)
	ta.Prompt = ">>"
	ta.Placeholder = "type"
	ta.CaretColor = termformat.ANSIRed

	ta.SetContents("")
	viewPlain := stripANSICodes(ta.View())
	require.Contains(t, viewPlain, ">>")
	require.Contains(t, viewPlain, "type")
}

func TestTextArea_Caret_IsRenderedAsBackgroundColor_EvenWhenEmpty(t *testing.T) {
	ta := tuicontrols.NewTextArea(4, 1)
	ta.CaretColor = termformat.ANSIBlue
	ta.SetContents("")

	rows := splitRows(t, ta.View(), 1)
	require.Contains(t, rows[0], termformat.ANSIBlue.ANSISequence(true))
	require.GreaterOrEqual(t, termformat.TextWidthWithANSICodes(rows[0]), 1)
}

func TestTextArea_BackgroundColor_PadsEachRowToWidth(t *testing.T) {
	ta := tuicontrols.NewTextArea(4, 3)
	ta.BackgroundColor = termformat.ANSIRed
	ta.CaretColor = termformat.ANSIBlue
	ta.SetContents("hi")

	rows := splitRows(t, ta.View(), 3)
	for _, row := range rows {
		require.Equal(t, 4, termformat.TextWidthWithANSICodes(row))
	}
	require.Contains(t, ta.View(), termformat.ANSIRed.ANSISequence(true))
}

func TestTextArea_VerticalClipping_WhenClipped_NoBlankRowsInClippedDisplayContents(t *testing.T) {
	ta := tuicontrols.NewTextArea(4, 2)
	ta.SetContents("abcdefg") // display lines: "abc", "def", "g" => clipped

	require.Equal(t, 3, ta.DisplayLines())
	got := ta.ClippedDisplayContents()
	require.Len(t, got, 2)
	for _, line := range got {
		require.NotEqual(t, "", line)
		require.False(t, strings.Contains(line, "\n"))
		require.LessOrEqual(t, uni.TextWidth(line, nil), 4)
	}
}

func TestTextArea_VerticalClipping_CaretRemainsVisibleInView(t *testing.T) {
	ta := tuicontrols.NewTextArea(4, 2)
	ta.CaretColor = termformat.ANSIBlue
	ta.SetContents("abcdefg") // clipped

	ta.MoveToEndOfText()
	view := ta.View()
	require.Contains(t, view, termformat.ANSIBlue.ANSISequence(true))
	splitRows(t, view, ta.Height())
}

func TestTextArea_Update_DefaultKeyBindings_Smoke(t *testing.T) {
	ta := tuicontrols.NewTextArea(20, 5)
	ta.Update(nil, tui.KeyEvent{ControlKey: tui.ControlKeyNone, Runes: []rune{'a'}})
	ta.Update(nil, tui.KeyEvent{ControlKey: tui.ControlKeyNone, Runes: []rune{'b'}})
	ta.Update(nil, tui.KeyEvent{ControlKey: tui.ControlKeyNone, Paste: true, Runes: []rune("\tc")})
	require.Equal(t, "ab    c", ta.Contents())

	ta.Update(nil, tui.KeyEvent{ControlKey: tui.ControlKeyLeft})
	require.Equal(t, len("ab    "), ta.CaretPositionByteOffset())
	ta.Update(nil, tui.KeyEvent{ControlKey: tui.ControlKeyCtrlB})
	require.Equal(t, len("ab   "), ta.CaretPositionByteOffset())

	ta.Update(nil, tui.KeyEvent{ControlKey: tui.ControlKeyCtrlF})
	require.Equal(t, len("ab    "), ta.CaretPositionByteOffset())
	ta.Update(nil, tui.KeyEvent{ControlKey: tui.ControlKeyRight})
	require.Equal(t, len("ab    c"), ta.CaretPositionByteOffset())

	ta.Update(nil, tui.KeyEvent{ControlKey: tui.ControlKeyHome})
	require.Equal(t, 0, ta.CaretPositionByteOffset())
	ta.Update(nil, tui.KeyEvent{ControlKey: tui.ControlKeyEnd})
	require.Equal(t, len(ta.Contents()), ta.CaretPositionByteOffset())

	ta.Update(nil, tui.KeyEvent{ControlKey: tui.ControlKeyEnter})
	require.Equal(t, "ab    c\n", ta.Contents())
	ta.Update(nil, tui.KeyEvent{ControlKey: tui.ControlKeyCtrlJ})
	require.Equal(t, "ab    c\n\n", ta.Contents())

	ta.Update(nil, tui.KeyEvent{ControlKey: tui.ControlKeyTab})
	require.Equal(t, "ab    c\n\n    ", ta.Contents())

	ta.Update(nil, tui.KeyEvent{ControlKey: tui.ControlKeyBackspace})
	require.Equal(t, "ab    c\n\n   ", ta.Contents())

	ta.MoveToBeginningOfText()
	ta.Update(nil, tui.KeyEvent{ControlKey: tui.ControlKeyDelete})
	require.Equal(t, "b    c\n\n   ", ta.Contents())
}

func TestTextArea_Update_WordMotionAndWordDelete(t *testing.T) {
	ta := tuicontrols.NewTextArea(80, 5)
	ta.SetContents("hello world")
	ta.MoveToEndOfText()

	ta.Update(nil, tui.KeyEvent{ControlKey: tui.ControlKeyBackspace, Alt: true}) // Alt-Backspace
	require.NotContains(t, ta.Contents(), "world")
	require.Contains(t, ta.Contents(), "hello")

	ta.SetContents("hello world")
	ta.MoveToBeginningOfText()
	ta.Update(nil, tui.KeyEvent{ControlKey: tui.ControlKeyNone, Runes: []rune{'d'}, Alt: true}) // Alt-D
	require.NotContains(t, ta.Contents(), "hello")
}

func splitRows(t *testing.T, view string, height int) []string {
	t.Helper()
	if height == 0 {
		require.Equal(t, "", view)
		return nil
	}
	rows := strings.Split(view, "\n")
	require.Len(t, rows, height)
	return rows
}

func requireNoDisallowedASCIIControlsExceptNewline(t *testing.T, s string) {
	t.Helper()
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b == '\n' {
			continue
		}
		if b <= 0x1F || b == 0x7F {
			t.Fatalf("found disallowed ASCII control byte 0x%02X in %q", b, s)
		}
	}
}

func requireCaretOnGraphemeBoundary(t *testing.T, ta *tuicontrols.TextArea) {
	t.Helper()
	contents := ta.Contents()
	caret := ta.CaretPositionByteOffset()
	require.GreaterOrEqual(t, caret, 0)
	require.LessOrEqual(t, caret, len(contents))

	boundaries := graphemeBoundaries(contents)
	_, ok := boundaries[caret]
	require.Truef(t, ok, "caret offset %d is not on a grapheme boundary in %q", caret, contents)
}

func graphemeBoundaries(s string) map[int]struct{} {
	b := map[int]struct{}{0: {}, len(s): {}}
	iter := uni.NewGraphemeIterator(s, nil)
	for iter.Next() {
		b[iter.Start()] = struct{}{}
		b[iter.End()] = struct{}{}
	}
	return b
}

func stripANSICodes(s string) string {
	if s == "" {
		return ""
	}
	var out strings.Builder
	out.Grow(len(s))

	for i := 0; i < len(s); {
		if s[i] != '\x1b' {
			out.WriteByte(s[i])
			i++
			continue
		}
		seqLen := ansiSequenceLength(s[i:])
		if seqLen <= 0 {
			i++
			continue
		}
		i += seqLen
	}
	return out.String()
}

func ansiSequenceLength(s string) int {
	if len(s) == 0 || s[0] != '\x1b' {
		return 0
	}
	if len(s) == 1 {
		return 1
	}

	switch s[1] {
	case '[':
		for i := 2; i < len(s); i++ {
			final := s[i]
			if final >= 0x40 && final <= 0x7e {
				return i + 1
			}
		}
		return 0
	case ']':
		for i := 2; i < len(s); i++ {
			if s[i] == '\a' {
				return i + 1
			}
			if s[i] == '\\' && s[i-1] == '\x1b' {
				return i + 1
			}
		}
		return 0
	case 'P', '^', '_':
		for i := 2; i < len(s); i++ {
			if s[i] == '\\' && s[i-1] == '\x1b' {
				return i + 1
			}
		}
		return 0
	default:
		return 2
	}
}
