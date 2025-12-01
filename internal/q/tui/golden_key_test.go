package tui_test

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codalotl/codalotl/internal/q/tui"
	"github.com/stretchr/testify/require"
)

func TestKeyboardInput(t *testing.T) {
	input, output, ptmx := requireTestTTYWithPTY(t)

	testCases := []struct {
		sequence []byte
		want     tui.KeyEvent
	}{
		// Normal letters/ascii charactes:
		{sequence: []byte("a"), want: tui.KeyEvent{ControlKey: tui.ControlKeyNone, Runes: []rune{'a'}}},
		{sequence: []byte("b"), want: tui.KeyEvent{ControlKey: tui.ControlKeyNone, Runes: []rune{'b'}}},
		{sequence: []byte("c"), want: tui.KeyEvent{ControlKey: tui.ControlKeyNone, Runes: []rune{'c'}}},
		{sequence: []byte("X"), want: tui.KeyEvent{ControlKey: tui.ControlKeyNone, Runes: []rune{'X'}}},
		{sequence: []byte("Y"), want: tui.KeyEvent{ControlKey: tui.ControlKeyNone, Runes: []rune{'Y'}}},
		{sequence: []byte("Z"), want: tui.KeyEvent{ControlKey: tui.ControlKeyNone, Runes: []rune{'Z'}}},
		{sequence: []byte("."), want: tui.KeyEvent{ControlKey: tui.ControlKeyNone, Runes: []rune{'.'}}},
		{sequence: []byte("["), want: tui.KeyEvent{ControlKey: tui.ControlKeyNone, Runes: []rune{'['}}},

		// Common control keys:
		{sequence: []byte{0x00}, want: tui.KeyEvent{ControlKey: tui.ControlKeyCtrlAt}},
		{sequence: []byte{0x01}, want: tui.KeyEvent{ControlKey: tui.ControlKeyCtrlA}},
		{sequence: []byte{0x02}, want: tui.KeyEvent{ControlKey: tui.ControlKeyCtrlB}},
		{sequence: []byte{0x03}, want: tui.KeyEvent{ControlKey: tui.ControlKeyCtrlC}},
		{sequence: []byte{0x04}, want: tui.KeyEvent{ControlKey: tui.ControlKeyCtrlD}},
		{sequence: []byte{0x07}, want: tui.KeyEvent{ControlKey: tui.ControlKeyCtrlG}},
		{sequence: []byte{0x08}, want: tui.KeyEvent{ControlKey: tui.ControlKeyCtrlH}},
		{sequence: []byte{0x09}, want: tui.KeyEvent{ControlKey: tui.ControlKeyTab}},
		{sequence: []byte{0x0a}, want: tui.KeyEvent{ControlKey: tui.ControlKeyCtrlJ}},
		{sequence: []byte{0x0d}, want: tui.KeyEvent{ControlKey: tui.ControlKeyEnter}},
		{sequence: []byte{0x1b}, want: tui.KeyEvent{ControlKey: tui.ControlKeyEsc}},
		{sequence: []byte{0x7f}, want: tui.KeyEvent{ControlKey: tui.ControlKeyBackspace}},

		// Unicode characters:
		{sequence: []byte("λ"), want: tui.KeyEvent{ControlKey: tui.ControlKeyNone, Runes: []rune{'λ'}}},
		{sequence: []byte("é"), want: tui.KeyEvent{ControlKey: tui.ControlKeyNone, Runes: []rune{'é'}}},
		{sequence: []byte("你"), want: tui.KeyEvent{ControlKey: tui.ControlKeyNone, Runes: []rune{'你'}}},
		{sequence: []byte("🙂"), want: tui.KeyEvent{ControlKey: tui.ControlKeyNone, Runes: []rune{'🙂'}}},

		// Alt-modified runes:
		{sequence: []byte{0x1b, 'a'}, want: tui.KeyEvent{ControlKey: tui.ControlKeyNone, Runes: []rune{'a'}, Alt: true}},
		{sequence: []byte{0x1b, 'Z'}, want: tui.KeyEvent{ControlKey: tui.ControlKeyNone, Runes: []rune{'Z'}, Alt: true}},
		{sequence: []byte{0x1b, 'b'}, want: tui.KeyEvent{ControlKey: tui.ControlKeyNone, Runes: []rune{'b'}, Alt: true}}, // on osx: alt + left arrow
		{sequence: []byte{0x1b, 'f'}, want: tui.KeyEvent{ControlKey: tui.ControlKeyNone, Runes: []rune{'f'}, Alt: true}}, // on osx: alt + right arrow

		//
		// CSI sequences:
		//

		// Arrow keys:
		{sequence: []byte("\x1b[A"), want: tui.KeyEvent{ControlKey: tui.ControlKeyUp}},
		{sequence: []byte("\x1b[B"), want: tui.KeyEvent{ControlKey: tui.ControlKeyDown}},
		{sequence: []byte("\x1b[C"), want: tui.KeyEvent{ControlKey: tui.ControlKeyRight}},
		{sequence: []byte("\x1b[D"), want: tui.KeyEvent{ControlKey: tui.ControlKeyLeft}},

		// Home sequences:
		{sequence: []byte("\x1b[H"), want: tui.KeyEvent{ControlKey: tui.ControlKeyHome}},
		{sequence: []byte("\x1b[1~"), want: tui.KeyEvent{ControlKey: tui.ControlKeyHome}}, // xterm, lxterm
		{sequence: []byte("\x1b[7~"), want: tui.KeyEvent{ControlKey: tui.ControlKeyHome}}, // urxvt
		{sequence: []byte("\x1b[1;3H"), want: tui.KeyEvent{ControlKey: tui.ControlKeyHome, Alt: true}},
		{sequence: []byte("\x1b[1;5H"), want: tui.KeyEvent{ControlKey: tui.ControlKeyCtrlHome}},
		{sequence: []byte("\x1b[1;7H"), want: tui.KeyEvent{ControlKey: tui.ControlKeyCtrlHome, Alt: true}},
		{sequence: []byte("\x1b[1;2H"), want: tui.KeyEvent{ControlKey: tui.ControlKeyShiftHome}},
		{sequence: []byte("\x1b[1;4H"), want: tui.KeyEvent{ControlKey: tui.ControlKeyShiftHome, Alt: true}},
		{sequence: []byte("\x1b[1;6H"), want: tui.KeyEvent{ControlKey: tui.ControlKeyCtrlShiftHome}},
		{sequence: []byte("\x1b[1;8H"), want: tui.KeyEvent{ControlKey: tui.ControlKeyCtrlShiftHome, Alt: true}},

		// End sequences:
		{sequence: []byte("\x1b[F"), want: tui.KeyEvent{ControlKey: tui.ControlKeyEnd}},

		// Page Up/Down:
		{sequence: []byte("\x1b[5~"), want: tui.KeyEvent{ControlKey: tui.ControlKeyPgUp}},
		{sequence: []byte("\x1b[6~"), want: tui.KeyEvent{ControlKey: tui.ControlKeyPgDown}},

		// Insert/Delete:
		{sequence: []byte("\x1b[2~"), want: tui.KeyEvent{ControlKey: tui.ControlKeyInsert}},
		{sequence: []byte("\x1b[3~"), want: tui.KeyEvent{ControlKey: tui.ControlKeyDelete}},

		// Function Keys:
		{sequence: []byte("\x1b[[A"), want: tui.KeyEvent{ControlKey: tui.ControlKeyF1}},
		{sequence: []byte("\x1bOP"), want: tui.KeyEvent{ControlKey: tui.ControlKeyF1}},
		{sequence: []byte("\x1b[11~"), want: tui.KeyEvent{ControlKey: tui.ControlKeyF1}},

		// Powershell sequences:
		{sequence: []byte{0x1b, 'O', 'A'}, want: tui.KeyEvent{ControlKey: tui.ControlKeyUp}},
		{sequence: []byte{0x1b, 'O', 'B'}, want: tui.KeyEvent{ControlKey: tui.ControlKeyDown}},
		{sequence: []byte{0x1b, 'O', 'C'}, want: tui.KeyEvent{ControlKey: tui.ControlKeyRight}},
		{sequence: []byte{0x1b, 'O', 'D'}, want: tui.KeyEvent{ControlKey: tui.ControlKeyLeft}},

		// TODO {sequence: []byte("abc"), want: tui.KeyEvent{ControlKey: tui.ControlKeyNone, Runes: []rune{'a', 'b', 'c'}}},
		// TODO: []byte("abc\x0a") gets converted into two key events
	}

	sequences := make([][]byte, len(testCases))
	for i, tc := range testCases {
		sequences[i] = tc.sequence
	}

	m := &keyboardInputModel{
		writer:    ptmx,
		sequences: sequences,
		expect:    len(testCases),
		timeout:   500 * time.Millisecond,
	}

	err := tui.RunTUI(m, tui.Options{
		Input:  input,
		Output: output,
	})
	require.NoError(t, err)

	events := m.Events()
	require.Len(t, events, len(testCases))

	for i, tc := range testCases {
		require.Equalf(t, tc.want, events[i], "for sequence %s, expected %+v but got %+v", formatByteSequence(tc.sequence), tc.want, events[i])
	}
}

func formatByteSequence(seq []byte) string {
	if len(seq) == 0 {
		return "[]byte{}"
	}
	var b strings.Builder
	b.WriteString("[]byte{")
	for i, v := range seq {
		if i > 0 {
			b.WriteString(", ")
		}
		_, _ = fmt.Fprintf(&b, "0x%02x", v)
	}
	b.WriteString("}")
	return b.String()
}

type keyboardInputModel struct {
	writer    io.Writer
	sequences [][]byte
	expect    int
	timeout   time.Duration

	mu     sync.Mutex
	events []tui.KeyEvent
	timer  *time.Timer
}

func (m *keyboardInputModel) Init(t *tui.TUI) {
	if m.writer == nil {
		return
	}
	m.startTimer(t)
	go func() {
		time.Sleep(20 * time.Millisecond)
		for _, seq := range m.sequences {
			if len(seq) == 0 {
				continue
			}
			_, _ = m.writer.Write(seq)
			time.Sleep(5 * time.Millisecond)
		}
	}()
}

func (m *keyboardInputModel) Update(t *tui.TUI, msg tui.Message) {
	switch ev := msg.(type) {
	case tui.KeyEvent:
		m.mu.Lock()
		m.events = append(m.events, ev)
		count := len(m.events)
		m.mu.Unlock()
		if count >= m.expect {
			m.stopTimer()
			t.Quit()
		}
	case tui.SigTermEvent:
		// allow clean shutdown
	}
}

func (m *keyboardInputModel) View() string {
	return ""
}

func (m *keyboardInputModel) Events() []tui.KeyEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]tui.KeyEvent, len(m.events))
	copy(out, m.events)
	return out
}

func (m *keyboardInputModel) startTimer(t *tui.TUI) {
	if m.timeout <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.timer != nil {
		m.timer.Stop()
	}
	m.timer = time.AfterFunc(m.timeout, func() {
		t.Quit()
	})
}

func (m *keyboardInputModel) stopTimer() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
}
