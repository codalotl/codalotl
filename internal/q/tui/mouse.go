package tui

import "strings"

// MouseEvent represents a mouse event emitted by the terminal when mouse tracking is enabled (cell motion / button-event tracking).
//
// Coordinates are 0-based where (0,0) is the upper-left cell.
type MouseEvent struct {
	X      int
	Y      int
	Shift  bool
	Alt    bool
	Ctrl   bool
	Action MouseAction
	Button MouseButton
}

func (m MouseEvent) IsWheel() bool {
	switch m.Button {
	case MouseButtonWheelUp, MouseButtonWheelDown, MouseButtonWheelLeft, MouseButtonWheelRight:
		return true
	default:
		return false
	}
}

func (m MouseEvent) String() string {
	var b strings.Builder
	if m.Ctrl {
		b.WriteString("ctrl+")
	}
	if m.Alt {
		b.WriteString("alt+")
	}
	if m.Shift {
		b.WriteString("shift+")
	}

	if m.Button != MouseButtonNone {
		b.WriteString(m.Button.String())
	}
	if m.Action != MouseActionNone {
		if b.Len() > 0 && m.Button != MouseButtonNone {
			b.WriteByte(' ')
		}
		b.WriteString(m.Action.String())
	}
	return b.String()
}

type MouseAction int

const (
	MouseActionNone MouseAction = iota
	MouseActionPress
	MouseActionRelease
	MouseActionMotion
)

func (a MouseAction) String() string {
	switch a {
	case MouseActionPress:
		return "press"
	case MouseActionRelease:
		return "release"
	case MouseActionMotion:
		return "motion"
	default:
		return ""
	}
}

type MouseButton int

const (
	MouseButtonNone MouseButton = iota
	MouseButtonLeft
	MouseButtonMiddle
	MouseButtonRight
	MouseButtonWheelUp
	MouseButtonWheelDown
	MouseButtonWheelLeft
	MouseButtonWheelRight
	MouseButtonBackward
	MouseButtonForward
	MouseButton10
	MouseButton11
)

func (b MouseButton) String() string {
	switch b {
	case MouseButtonNone:
		return "none"
	case MouseButtonLeft:
		return "left"
	case MouseButtonMiddle:
		return "middle"
	case MouseButtonRight:
		return "right"
	case MouseButtonWheelUp:
		return "wheel up"
	case MouseButtonWheelDown:
		return "wheel down"
	case MouseButtonWheelLeft:
		return "wheel left"
	case MouseButtonWheelRight:
		return "wheel right"
	case MouseButtonBackward:
		return "backward"
	case MouseButtonForward:
		return "forward"
	case MouseButton10:
		return "button 10"
	case MouseButton11:
		return "button 11"
	default:
		return ""
	}
}

const x10MouseByteOffset = 32

// parseX10MouseEvent parses X10 encoded mouse events:
//
//	ESC [ M Cb Cx Cy
func parseX10MouseEvent(buf []byte) (MouseEvent, bool) {
	if len(buf) < 6 || buf[0] != 0x1b || buf[1] != '[' || buf[2] != 'M' {
		return MouseEvent{}, false
	}
	m := parseMouseButton(int(buf[3]), false)

	// (1,1) is the upper-left cell. Normalize to (0,0).
	m.X = int(buf[4]) - x10MouseByteOffset - 1
	m.Y = int(buf[5]) - x10MouseByteOffset - 1
	return m, true
}

// parseSGRMouseEvent parses SGR encoded mouse events:
//
//	ESC [ < Cb ; Cx ; Cy (M or m)
func parseSGRMouseEvent(buf []byte) (MouseEvent, bool, bool) {
	// Returns: event, ok, needMore.
	// The caller should pass in a buffer starting at the ESC byte.
	if len(buf) < 4 || buf[0] != 0x1b || buf[1] != '[' || buf[2] != '<' {
		return MouseEvent{}, false, false
	}

	// Find terminator: 'M' (press/motion) or 'm' (release).
	end := -1
	var term byte
	for i := 3; i < len(buf); i++ {
		if buf[i] == 'M' || buf[i] == 'm' {
			end = i
			term = buf[i]
			break
		}
	}
	if end < 0 {
		return MouseEvent{}, false, true
	}

	i := 3
	cb, ok := parseDecIntUntil(buf, &i, ';')
	if !ok {
		return MouseEvent{}, false, false
	}
	x1, ok := parseDecIntUntil(buf, &i, ';')
	if !ok {
		return MouseEvent{}, false, false
	}
	y1, ok := parseDecIntUntil(buf, &i, term)
	if !ok {
		return MouseEvent{}, false, false
	}

	ev := parseMouseButton(cb, true)

	release := term == 'm'
	// Wheel buttons don't have release events. Motion can also be reported as
	// a release event by some terminals (notably Windows Terminal).
	if ev.Action != MouseActionMotion && !ev.IsWheel() && release {
		ev.Action = MouseActionRelease
	}

	// (1,1) is the upper-left cell. Normalize to (0,0).
	ev.X = x1 - 1
	ev.Y = y1 - 1
	return ev, true, false
}

func parseDecIntUntil(buf []byte, i *int, term byte) (int, bool) {
	if i == nil || *i < 0 || *i >= len(buf) {
		return 0, false
	}
	start := *i
	n := 0
	for *i < len(buf) && buf[*i] != term {
		b := buf[*i]
		if b < '0' || b > '9' {
			return 0, false
		}
		n = n*10 + int(b-'0')
		*i++
	}
	if *i >= len(buf) || buf[*i] != term || *i == start {
		return 0, false
	}
	*i++ // consume terminator
	return n, true
}

// parseMouseButton decodes the "button code" (Cb) part of xterm mouse tracking. It returns an event with Action/Button and modifier flags set.
func parseMouseButton(b int, isSGR bool) MouseEvent {
	var m MouseEvent
	e := b
	if !isSGR {
		e -= x10MouseByteOffset
	}

	const (
		bitShift  = 0b0000_0100
		bitAlt    = 0b0000_1000
		bitCtrl   = 0b0001_0000
		bitMotion = 0b0010_0000
		bitWheel  = 0b0100_0000
		bitAdd    = 0b1000_0000 // additional buttons 8-11

		bitsMask = 0b0000_0011
	)

	m.Action = MouseActionPress

	if e&bitAdd != 0 {
		m.Button = MouseButtonBackward + MouseButton(e&bitsMask)
	} else if e&bitWheel != 0 {
		m.Button = MouseButtonWheelUp + MouseButton(e&bitsMask)
	} else {
		m.Button = MouseButtonLeft + MouseButton(e&bitsMask)

		// X10 reports a button release as 0b0000_0011 (3) and doesn't convey
		// which button was released.
		if e&bitsMask == bitsMask {
			m.Action = MouseActionRelease
			m.Button = MouseButtonNone
		}
	}

	// Motion bit doesn't get reported for wheel events.
	if e&bitMotion != 0 && !m.IsWheel() {
		m.Action = MouseActionMotion
	}

	// Modifiers.
	m.Alt = e&bitAlt != 0
	m.Ctrl = e&bitCtrl != 0
	m.Shift = e&bitShift != 0

	return m
}
