package tuicontrols

import (
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/q/termformat"
	"github.com/codalotl/codalotl/internal/q/tui"
	"github.com/stretchr/testify/require"
)

func TestTextArea_SetContents_Sanitizes(t *testing.T) {
	ta := NewTextArea(10, 1)
	ta.SetContents("a\tb\r\nc" + string([]byte{0x01}))

	require.Equal(t, "a    b\nc\\x01", ta.Contents())
	require.Equal(t, len(ta.Contents()), ta.CaretPositionByteOffset())
}

func TestTextArea_View_RendersExactlyHeightRows(t *testing.T) {
	ta := NewTextArea(10, 3)
	ta.CaretColor = termformat.ANSIRed

	rows := strings.Split(ta.View(), "\n")
	require.Len(t, rows, 3)
	require.NotEmpty(t, rows[0])
}

func TestTextArea_WrapsAtWordBoundaries(t *testing.T) {
	ta := NewTextArea(8, 2)
	ta.SetContents("hello world")

	require.Equal(t, 2, ta.DisplayLines())
	require.Equal(t, []string{"hello ", "world"}, ta.ClippedDisplayContents())

	rows := strings.Split(ta.View(), "\n")
	require.Len(t, rows, 2)
	require.Equal(t, "hello", strings.TrimRight(rows[0], " "))
	require.Equal(t, "world", strings.TrimSpace(rows[1]))
}

func TestTextArea_Update_InsertsAndDeletes(t *testing.T) {
	ta := NewTextArea(20, 1)
	ta.Update(nil, tui.KeyEvent{ControlKey: tui.ControlKeyNone, Runes: []rune("hi")})
	require.Equal(t, "hi", ta.Contents())

	ta.Update(nil, tui.KeyEvent{ControlKey: tui.ControlKeyBackspace})
	require.Equal(t, "h", ta.Contents())

	ta.Update(nil, tui.KeyEvent{ControlKey: tui.ControlKeyEnter})
	require.Equal(t, "h\n", ta.Contents())
}

func TestTextArea_Update_AltBF_WordNavigation(t *testing.T) {
	ta := NewTextArea(20, 1)
	ta.SetContents("hello world")

	ta.Update(nil, tui.KeyEvent{Alt: true, ControlKey: tui.ControlKeyNone, Runes: []rune{'b'}})
	require.Equal(t, "hello world", ta.Contents())
	require.Equal(t, 6, ta.CaretPositionByteOffset())

	ta.Update(nil, tui.KeyEvent{Alt: true, ControlKey: tui.ControlKeyNone, Runes: []rune{'f'}})
	require.Equal(t, "hello world", ta.Contents())
	require.Equal(t, len("hello world"), ta.CaretPositionByteOffset())
}

func TestTextArea_Update_AltArrows_WordNavigation(t *testing.T) {
	ta := NewTextArea(20, 1)
	ta.SetContents("hello world again")

	ta.Update(nil, tui.KeyEvent{Alt: true, ControlKey: tui.ControlKeyLeft})
	require.Equal(t, "hello world again", ta.Contents())
	require.Equal(t, len("hello world "), ta.CaretPositionByteOffset()) // start of "again"

	ta.Update(nil, tui.KeyEvent{Alt: true, ControlKey: tui.ControlKeyRight})
	require.Equal(t, "hello world again", ta.Contents())
	require.Equal(t, len("hello world again"), ta.CaretPositionByteOffset()) // end of "again"
}

func TestTextArea_Update_AltF_ReadlineMovesToEndOfWord(t *testing.T) {
	ta := NewTextArea(40, 1)
	ta.SetContents("hello world again")

	ta.SetCaretPosition(0, 6) // start of "world"
	ta.Update(nil, tui.KeyEvent{Alt: true, ControlKey: tui.ControlKeyNone, Runes: []rune{'f'}})
	require.Equal(t, 11, ta.CaretPositionByteOffset()) // end of "world"

	ta.Update(nil, tui.KeyEvent{Alt: true, ControlKey: tui.ControlKeyNone, Runes: []rune{'f'}})
	require.Equal(t, 17, ta.CaretPositionByteOffset()) // end of "again"
}

func TestTextArea_Update_AltF_CrossesNewlines(t *testing.T) {
	ta := NewTextArea(40, 1)
	ta.SetContents("hello\nworld")

	ta.SetCaretPosition(0, 5) // end of "hello"
	ta.Update(nil, tui.KeyEvent{Alt: true, ControlKey: tui.ControlKeyNone, Runes: []rune{'f'}})
	require.Equal(t, len("hello\nworld"), ta.CaretPositionByteOffset()) // end of "world"
}

func TestTextArea_Update_AltB_CrossesNewlines(t *testing.T) {
	ta := NewTextArea(40, 1)
	ta.SetContents("hello\nworld")

	ta.SetCaretPosition(1, 0) // beginning of "world"
	ta.Update(nil, tui.KeyEvent{Alt: true, ControlKey: tui.ControlKeyNone, Runes: []rune{'b'}})
	require.Equal(t, 0, ta.CaretPositionByteOffset()) // beginning of "hello"
}

func TestTextArea_Update_AltBackspace_DeletesWordLeft(t *testing.T) {
	ta := NewTextArea(40, 1)
	ta.SetContents("hello world")

	ta.Update(nil, tui.KeyEvent{Alt: true, ControlKey: tui.ControlKeyBackspace})
	require.Equal(t, "hello ", ta.Contents())
	require.Equal(t, len("hello "), ta.CaretPositionByteOffset())
}

func TestTextArea_Update_AltD_DeletesWordRight(t *testing.T) {
	ta := NewTextArea(40, 1)
	ta.SetContents("hello world")
	ta.SetCaretPosition(0, 0)

	ta.Update(nil, tui.KeyEvent{Alt: true, ControlKey: tui.ControlKeyNone, Runes: []rune{'d'}})
	require.Equal(t, " world", ta.Contents())
	require.Equal(t, 0, ta.CaretPositionByteOffset())
}

func TestTextArea_Update_AltDelete_DeletesWordRight(t *testing.T) {
	ta := NewTextArea(40, 1)
	ta.SetContents("hello world")
	ta.SetCaretPosition(0, 0)

	ta.Update(nil, tui.KeyEvent{Alt: true, ControlKey: tui.ControlKeyDelete})
	require.Equal(t, " world", ta.Contents())
	require.Equal(t, 0, ta.CaretPositionByteOffset())
}

func TestTextArea_Update_ReadlineCtrlAliases(t *testing.T) {
	ta := NewTextArea(40, 2)
	ta.SetContents("ab\ncd")

	ta.Update(nil, tui.KeyEvent{ControlKey: tui.ControlKeyCtrlB})
	require.Equal(t, len("ab\nc"), ta.CaretPositionByteOffset())

	ta.Update(nil, tui.KeyEvent{ControlKey: tui.ControlKeyCtrlF})
	require.Equal(t, len("ab\ncd"), ta.CaretPositionByteOffset())

	ta.Update(nil, tui.KeyEvent{ControlKey: tui.ControlKeyCtrlP})
	row, _ := ta.CaretPositionRowCol()
	require.Equal(t, 0, row)

	ta.Update(nil, tui.KeyEvent{ControlKey: tui.ControlKeyCtrlN})
	row, _ = ta.CaretPositionRowCol()
	require.Equal(t, 1, row)

	ta.Update(nil, tui.KeyEvent{ControlKey: tui.ControlKeyCtrlH})
	require.Equal(t, "ab\nc", ta.Contents())

	ta.SetCaretPosition(0, 0)
	ta.Update(nil, tui.KeyEvent{ControlKey: tui.ControlKeyCtrlD})
	require.Equal(t, "b\nc", ta.Contents())
}

func TestTextArea_CaretPositionRowCol(t *testing.T) {
	ta := NewTextArea(20, 2)
	ta.SetContents("a\nb")
	row, col := ta.CaretPositionRowCol()
	require.Equal(t, 1, row)
	require.Equal(t, 1, col)
	require.Equal(t, 1, ta.CaretPositionCurrentLineByteOffset())
}

func TestTextArea_View_PadsToFullWidthWhenBackgroundSet(t *testing.T) {
	ta := NewTextArea(6, 3)
	ta.BackgroundColor = termformat.ANSIRed
	ta.ForegroundColor = termformat.ANSIBlack
	ta.CaretColor = termformat.ANSIBrightBlue
	ta.SetContents("a")

	rows := strings.Split(ta.View(), "\n")
	require.Len(t, rows, 3)
	for _, row := range rows {
		require.Equal(t, 6, termformat.TextWidthWithANSICodes(row))
	}
}
