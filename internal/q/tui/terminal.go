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

type noopTerminal struct{}

func (n *noopTerminal) Enter() error { return nil }

func (n *noopTerminal) Exit() error { return nil }

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

type realTerminal struct {
	in        *os.File
	out       io.Writer
	state     *term.State
	vtRestore func() error

	mu      sync.Mutex
	entered bool
}

func newRealTerminal(in *os.File, out io.Writer) *realTerminal {
	return &realTerminal{
		in:  in,
		out: out,
	}
}

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

	if err := rt.writeString(altScreenEnter + clearScreen + hideCursor + enableBracketedPaste + enableMouseCellMotion + enableMouseSGRMode); err != nil {
		_ = term.Restore(fd, state)
		if restoreVT != nil {
			_ = restoreVT()
		}
		return err
	}

	rt.state = state
	rt.vtRestore = restoreVT
	rt.entered = true
	return nil
}

func (rt *realTerminal) Exit() error {
	rt.mu.Lock()
	if !rt.entered {
		rt.mu.Unlock()
		return nil
	}
	fd := int(rt.in.Fd())
	state := rt.state
	restoreVT := rt.vtRestore
	rt.state = nil
	rt.vtRestore = nil
	rt.entered = false
	rt.mu.Unlock()

	var firstErr error

	if state != nil {
		if err := term.Restore(fd, state); err != nil {
			firstErr = err
		}
	}
	if err := rt.writeString(disableMouseSGRMode + disableMouseCellMotion + disableBracketedPaste + showCursor + altScreenExit); err != nil && firstErr == nil {
		firstErr = err
	}
	if restoreVT != nil {
		if err := restoreVT(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (rt *realTerminal) writeString(s string) error {
	if rt.out == nil || len(s) == 0 {
		return nil
	}
	_, err := io.WriteString(rt.out, s)
	return err
}
