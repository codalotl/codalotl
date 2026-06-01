package tui

import (
	"errors"
	"io"
	"os"
	"sync"

	"golang.org/x/term"
)

const (
	cursorHome             = "\x1b[H"
	clearLine              = "\x1b[2K"
	altScreenEnter         = "\x1b[?1049h" + cursorHome
	altScreenExit          = "\x1b[?1049l"
	hideCursor             = "\x1b[?25l"
	showCursor             = "\x1b[?25h"
	clearScreen            = "\x1b[2J" + cursorHome
	enableBracketedPaste   = "\x1b[?2004h"
	disableBracketedPaste  = "\x1b[?2004l"
	enableMouseCellMotion  = "\x1b[?1002h"
	disableMouseCellMotion = "\x1b[?1002l"
	enableMouseSGRMode     = "\x1b[?1006h"
	disableMouseSGRMode    = "\x1b[?1006l"
)

var errNoFileDescriptor = errors.New("tui: raw mode requires *os.File input")

// A noopTerminal is a terminalController that performs no terminal setup or teardown.
type noopTerminal struct{}

// Enter satisfies terminalController without changing terminal state.
func (n *noopTerminal) Enter() error { return nil }

// Exit satisfies terminalController without changing terminal state.
func (n *noopTerminal) Exit() error { return nil }

// mouseModeSetter is an internal hook used by the default terminal controller to optionally enable mouse tracking based on Options.
type mouseModeSetter interface {
	// The setMouseEnabled method records whether the next Enter call should enable terminal mouse tracking.
	setMouseEnabled(enabled bool)
}

func defaultTerminalFactory(input io.Reader, output io.Writer) (terminalController, error) {
	file, ok := input.(*os.File)
	if !ok || file == nil {
		return nil, errNoFileDescriptor
	}
	if output == nil {
		output = file
	}
	return newRealTerminal(file, output), nil
}

// A realTerminal controls raw mode, alternate-screen rendering, and related terminal modes for a real TTY.
type realTerminal struct {
	in          *os.File     // in is the terminal input file used for raw-mode configuration.
	out         io.Writer    // out receives terminal escape sequences.
	state       *term.State  // state is the terminal state saved before entering raw mode.
	vtRestore   func() error // vtRestore restores platform-specific virtual terminal state changed by Enter.
	mouseActive bool         // mouseActive reports whether mouse tracking was enabled by the active Enter call.
	mu          sync.Mutex   // mu protects terminal state changed by Enter, Exit, and setMouseEnabled.
	entered     bool         // entered reports whether the terminal is currently in TUI mode.
	enableMouse bool         // enableMouse reports whether Enter should enable terminal mouse tracking.
}

func newRealTerminal(in *os.File, out io.Writer) *realTerminal {
	return &realTerminal{
		in:  in,
		out: out,
	}
}

// The setMouseEnabled method sets whether Enter enables terminal mouse tracking.
func (rt *realTerminal) setMouseEnabled(enabled bool) {
	rt.mu.Lock()
	rt.enableMouse = enabled
	rt.mu.Unlock()
}

// Enter switches rt into raw, alternate-screen rendering mode.
//
// It is idempotent and enables bracketed paste and configured mouse tracking until Exit.
func (rt *realTerminal) Enter() error {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	if rt.entered {
		return nil
	}

	fd := int(rt.in.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return err
	}

	restoreVT, err := enableVirtualTerminal(rt.out)
	if err != nil {
		_ = term.Restore(fd, state)
		return err
	}

	seq := altScreenEnter + clearScreen + hideCursor + enableBracketedPaste
	if rt.enableMouse {
		seq += enableMouseCellMotion + enableMouseSGRMode
	}
	if err := rt.writeString(seq); err != nil {
		_ = term.Restore(fd, state)
		if restoreVT != nil {
			_ = restoreVT()
		}
		return err
	}

	rt.state = state
	rt.vtRestore = restoreVT
	rt.mouseActive = rt.enableMouse
	rt.entered = true
	return nil
}

// Exit restores terminal state changed by Enter.
//
// It is idempotent and returns the first restore or output error encountered.
func (rt *realTerminal) Exit() error {
	rt.mu.Lock()
	if !rt.entered {
		rt.mu.Unlock()
		return nil
	}
	fd := int(rt.in.Fd())
	state := rt.state
	restoreVT := rt.vtRestore
	mouseActive := rt.mouseActive
	rt.state = nil
	rt.vtRestore = nil
	rt.mouseActive = false
	rt.entered = false
	rt.mu.Unlock()

	var firstErr error

	if state != nil {
		if err := term.Restore(fd, state); err != nil {
			firstErr = err
		}
	}

	seq := disableBracketedPaste + showCursor + altScreenExit
	if mouseActive {
		seq = disableMouseSGRMode + disableMouseCellMotion + seq
	}
	if err := rt.writeString(seq); err != nil && firstErr == nil {
		firstErr = err
	}
	if restoreVT != nil {
		if err := restoreVT(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// The writeString method writes s to the terminal output, if there is output to write.
func (rt *realTerminal) writeString(s string) error {
	if rt.out == nil || len(s) == 0 {
		return nil
	}
	_, err := io.WriteString(rt.out, s)
	return err
}
