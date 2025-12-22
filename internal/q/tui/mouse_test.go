package tui

import (
	"fmt"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"
)

type mouseCaptureModel struct {
	mu       sync.Mutex
	events   []MouseEvent
	expect   int
	quitOnce sync.Once
}

func (m *mouseCaptureModel) Init(*TUI) {}

func (m *mouseCaptureModel) Update(t *TUI, msg Message) {
	switch v := msg.(type) {
	case MouseEvent:
		m.mu.Lock()
		m.events = append(m.events, v)
		done := m.expect > 0 && len(m.events) >= m.expect
		m.mu.Unlock()
		if done {
			m.quitOnce.Do(t.Quit)
		}
	case SigTermEvent:
		// allow clean shutdown
	}
}

func (m *mouseCaptureModel) View() string { return "" }

func (m *mouseCaptureModel) Events() []MouseEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MouseEvent, len(m.events))
	copy(out, m.events)
	return out
}

func TestParseX10MouseEvent(t *testing.T) {
	encode := func(b byte, x, y int) []byte {
		return []byte{
			0x1b,
			'[',
			'M',
			byte(32) + b,
			byte(x + 32 + 1),
			byte(y + 32 + 1),
		}
	}

	tests := []struct {
		name string
		buf  []byte
		want MouseEvent
	}{
		{
			name: "left press",
			buf:  encode(0b0000_0000, 10, 12),
			want: MouseEvent{X: 10, Y: 12, Action: MouseActionPress, Button: MouseButtonLeft},
		},
		{
			name: "left motion (drag)",
			buf:  encode(0b0010_0000, 10, 12),
			want: MouseEvent{X: 10, Y: 12, Action: MouseActionMotion, Button: MouseButtonLeft},
		},
		{
			name: "release",
			buf:  encode(0b0000_0011, 10, 12),
			want: MouseEvent{X: 10, Y: 12, Action: MouseActionRelease, Button: MouseButtonNone},
		},
		{
			name: "wheel up",
			buf:  encode(0b0100_0000, 0, 0),
			want: MouseEvent{X: 0, Y: 0, Action: MouseActionPress, Button: MouseButtonWheelUp},
		},
		{
			name: "ctrl+alt+shift+right",
			buf:  encode(0b0001_1110, 1, 2),
			want: MouseEvent{X: 1, Y: 2, Shift: true, Alt: true, Ctrl: true, Action: MouseActionPress, Button: MouseButtonRight},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseX10MouseEvent(tc.buf)
			if !ok {
				t.Fatalf("expected ok")
			}
			if got != tc.want {
				t.Fatalf("got %#v want %#v", got, tc.want)
			}
		})
	}
}

func TestParseSGRMouseEvent(t *testing.T) {
	encode := func(b, x, y int, release bool) []byte {
		end := 'M'
		if release {
			end = 'm'
		}
		return []byte(fmt.Sprintf("\x1b[<%d;%d;%d%c", b, x+1, y+1, end))
	}

	tests := []struct {
		name string
		buf  []byte
		want MouseEvent
	}{
		{
			name: "left press",
			buf:  encode(0, 10, 12, false),
			want: MouseEvent{X: 10, Y: 12, Action: MouseActionPress, Button: MouseButtonLeft},
		},
		{
			name: "left release",
			buf:  encode(0, 10, 12, true),
			want: MouseEvent{X: 10, Y: 12, Action: MouseActionRelease, Button: MouseButtonLeft},
		},
		{
			name: "left motion (drag)",
			buf:  encode(32, 10, 12, false),
			want: MouseEvent{X: 10, Y: 12, Action: MouseActionMotion, Button: MouseButtonLeft},
		},
		{
			name: "motion (no button)",
			buf:  encode(35, 10, 12, false),
			want: MouseEvent{X: 10, Y: 12, Action: MouseActionMotion, Button: MouseButtonNone},
		},
		{
			name: "wheel down",
			buf:  encode(65, 0, 0, false),
			want: MouseEvent{X: 0, Y: 0, Action: MouseActionPress, Button: MouseButtonWheelDown},
		},
		{
			name: "ctrl+alt+shift+wheel down",
			buf:  encode(93, 1, 2, false),
			want: MouseEvent{X: 1, Y: 2, Shift: true, Alt: true, Ctrl: true, Action: MouseActionPress, Button: MouseButtonWheelDown},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok, needMore := parseSGRMouseEvent(tc.buf)
			if needMore {
				t.Fatalf("unexpected needMore")
			}
			if !ok {
				t.Fatalf("expected ok")
			}
			if got != tc.want {
				t.Fatalf("got %#v want %#v", got, tc.want)
			}
		})
	}
}

func TestInputDeliversMouseEvents(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()

	model := &mouseCaptureModel{expect: 4}

	go func() {
		defer writer.Close()

		// SGR: left press at (3,4).
		_, _ = writer.Write([]byte("\x1b[<0;4;5M"))
		// SGR: left release at (3,4).
		_, _ = writer.Write([]byte("\x1b[<0;4;5m"))
		// SGR: motion with left pressed at (5,6).
		_, _ = writer.Write([]byte("\x1b[<32;6;7M"))

		// X10: wheel up at (1,2).
		_, _ = writer.Write([]byte{0x1b, '[', 'M', byte(32) + 64, byte(1 + 32 + 1), byte(2 + 32 + 1)})
	}()

	err := runTUITest(t, model, func(opts *Options) {
		opts.Input = reader
		opts.Output = io.Discard
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []MouseEvent{
		{X: 3, Y: 4, Action: MouseActionPress, Button: MouseButtonLeft},
		{X: 3, Y: 4, Action: MouseActionRelease, Button: MouseButtonLeft},
		{X: 5, Y: 6, Action: MouseActionMotion, Button: MouseButtonLeft},
		{X: 1, Y: 2, Action: MouseActionPress, Button: MouseButtonWheelUp},
	}

	got := model.Events()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mouse events mismatch:\n got: %#v\nwant: %#v", got, want)
	}

	// Ensure we didn't hang waiting for the input reader to stop.
	time.Sleep(1 * time.Millisecond)
}
