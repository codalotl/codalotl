package tuicontrols

import (
	"testing"

	"github.com/codalotl/codalotl/internal/q/tui"
	"github.com/stretchr/testify/require"
)

func TestKeyMapProcess_NonKeyEvent(t *testing.T) {
	km := NewKeyMap()
	require.Equal(t, "", km.Process(tui.ResizeEvent{Width: 1, Height: 2}))
}

func TestKeyMap_LastMappingWins(t *testing.T) {
	km := NewKeyMap()

	key := tui.KeyEvent{ControlKey: tui.ControlKeyUp}
	km.Add(key, "up")
	km.Add(key, "scrollUp")

	require.Equal(t, "scrollUp", km.Process(key))
}

func TestKeyMap_DistinguishesAltAndRunes(t *testing.T) {
	km := NewKeyMap()

	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyUp}, "up")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyUp, Alt: true}, "altUp")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyNone, Runes: []rune{'a'}}, "insertA")

	require.Equal(t, "up", km.Process(tui.KeyEvent{ControlKey: tui.ControlKeyUp}))
	require.Equal(t, "altUp", km.Process(tui.KeyEvent{ControlKey: tui.ControlKeyUp, Alt: true}))
	require.Equal(t, "insertA", km.Process(tui.KeyEvent{ControlKey: tui.ControlKeyNone, Runes: []rune{'a'}}))
	require.Equal(t, "", km.Process(tui.KeyEvent{ControlKey: tui.ControlKeyNone, Runes: []rune{'b'}}))
}
