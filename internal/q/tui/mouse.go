package tui

import "strings"

// MouseEvent represents a mouse event emitted by the terminal when mouse tracking is enabled (cell motion / button-event tracking).
//
// Coordinates are 0-based where (0,0) is the upper-left cell.
type MouseEvent struct {
	X      int         // X is the zero-based horizontal cell coordinate.
	Y      int         // Y is the zero-based vertical cell coordinate.
	Shift  bool        // Shift reports whether Shift was held during the event.
	Alt    bool        // Alt reports whether Alt was held during the event.
	Ctrl   bool        // Ctrl reports whether Ctrl was held during the event.
	Action MouseAction // Action is the reported mouse interaction.
	Button MouseButton // Button is the reported button or wheel direction.
}

// IsWheel reports whether m is a wheel event in any direction.
func (m MouseEvent) IsWheel() bool {
	switch m.Button {
	case MouseButtonWheelUp, MouseButtonWheelDown, MouseButtonWheelLeft, MouseButtonWheelRight:
		return true
	default:
		return false
	}
}

// String returns a compact human-readable description of m, such as "ctrl+wheel up" or "left press".
//
// The result is intended for display and debugging, not parsing.
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

// MouseAction identifies the kind of mouse interaction reported by a MouseEvent.
type MouseAction int

// MouseAction constants describe the interaction phase of a MouseEvent.
const (
	MouseActionNone    MouseAction = iota // MouseActionNone indicates that no mouse action is specified.
	MouseActionPress                      // MouseActionPress identifies a mouse button press.
	MouseActionRelease                    // MouseActionRelease identifies a mouse button release.
	MouseActionMotion                     // MouseActionMotion identifies mouse motion.
)

// String returns the lower-case label for a.
//
// It returns an empty string for MouseActionNone and unrecognized values.
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

// MouseButton identifies the mouse button or wheel direction involved in a MouseEvent.
type MouseButton int

// MouseButton constants identify mouse buttons and wheel directions in a MouseEvent.
const (
	MouseButtonNone       MouseButton = iota // MouseButtonNone indicates that no specific button is known.
	MouseButtonLeft                          // MouseButtonLeft identifies the left mouse button.
	MouseButtonMiddle                        // MouseButtonMiddle identifies the middle mouse button.
	MouseButtonRight                         // MouseButtonRight identifies the right mouse button.
	MouseButtonWheelUp                       // MouseButtonWheelUp identifies an upward wheel event.
	MouseButtonWheelDown                     // MouseButtonWheelDown identifies a downward wheel event.
	MouseButtonWheelLeft                     // MouseButtonWheelLeft identifies a leftward horizontal wheel event.
	MouseButtonWheelRight                    // MouseButtonWheelRight identifies a rightward horizontal wheel event.
	MouseButtonBackward                      // MouseButtonBackward identifies the backward auxiliary mouse button.
	MouseButtonForward                       // MouseButtonForward identifies the forward auxiliary mouse button.
	MouseButton10                            // MouseButton10 identifies extended mouse button 10.
	MouseButton11                            // MouseButton11 identifies extended mouse button 11.
)

// String returns the lower-case label for b.
//
// It returns "none" for MouseButtonNone and an empty string for unrecognized values.
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

// The parseDecIntUntil function parses a non-empty decimal integer from buf starting at *i and ending at term.
//
// On success it returns the value, advances *i past term, and reports true. On failure it returns false, and callers should treat *i as unspecified.
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
