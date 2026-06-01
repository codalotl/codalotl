package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/codalotl/codalotl/internal/q/termformat"
	"golang.org/x/term"
)

// Message is any event or user-defined message sent to a Model's Update method.
type Message any

// ControlKey represents a control key pressed by the user. Values - other than ControlKeyNone - correspond either to ASCII control bytes (0x00-0x1f, 0x7f) or to
// higher-valued identifiers for CSI-based key sequences.
type ControlKey int

const (
	ControlKeyNone ControlKey = -1 // ControlKeyNone indicates no control key was pressed.
)

// These ControlKey constants represent ASCII control-byte key events.
//
// Their numeric values are the corresponding ASCII control bytes.
const (
	ControlKeyCtrlAt           ControlKey = 0x00 // ctrl+@
	ControlKeyCtrlA            ControlKey = 0x01 // ControlKeyCtrlA represents Ctrl+A.
	ControlKeyCtrlB            ControlKey = 0x02 // ControlKeyCtrlB represents Ctrl+B.
	ControlKeyCtrlC            ControlKey = 0x03 // ControlKeyCtrlC represents Ctrl+C.
	ControlKeyCtrlD            ControlKey = 0x04 // ControlKeyCtrlD represents Ctrl+D.
	ControlKeyCtrlE            ControlKey = 0x05 // ControlKeyCtrlE represents Ctrl+E.
	ControlKeyCtrlF            ControlKey = 0x06 // ControlKeyCtrlF represents Ctrl+F.
	ControlKeyCtrlG            ControlKey = 0x07 // ControlKeyCtrlG represents Ctrl+G.
	ControlKeyCtrlH            ControlKey = 0x08 // ControlKeyCtrlH represents Ctrl+H.
	ControlKeyCtrlI            ControlKey = 0x09 // ControlKeyCtrlI represents Ctrl+I and Tab.
	ControlKeyCtrlJ            ControlKey = 0x0a // ControlKeyCtrlJ represents Ctrl+J and line feed.
	ControlKeyCtrlK            ControlKey = 0x0b // ControlKeyCtrlK represents Ctrl+K.
	ControlKeyCtrlL            ControlKey = 0x0c // ControlKeyCtrlL represents Ctrl+L.
	ControlKeyCtrlM            ControlKey = 0x0d // ControlKeyCtrlM represents Ctrl+M and carriage return.
	ControlKeyCtrlN            ControlKey = 0x0e // ControlKeyCtrlN represents Ctrl+N.
	ControlKeyCtrlO            ControlKey = 0x0f // ControlKeyCtrlO represents Ctrl+O.
	ControlKeyCtrlP            ControlKey = 0x10 // ControlKeyCtrlP represents Ctrl+P.
	ControlKeyCtrlQ            ControlKey = 0x11 // ControlKeyCtrlQ represents Ctrl+Q.
	ControlKeyCtrlR            ControlKey = 0x12 // ControlKeyCtrlR represents Ctrl+R.
	ControlKeyCtrlS            ControlKey = 0x13 // ControlKeyCtrlS represents Ctrl+S.
	ControlKeyCtrlT            ControlKey = 0x14 // ControlKeyCtrlT represents Ctrl+T.
	ControlKeyCtrlU            ControlKey = 0x15 // ControlKeyCtrlU represents Ctrl+U.
	ControlKeyCtrlV            ControlKey = 0x16 // ControlKeyCtrlV represents Ctrl+V.
	ControlKeyCtrlW            ControlKey = 0x17 // ControlKeyCtrlW represents Ctrl+W.
	ControlKeyCtrlX            ControlKey = 0x18 // ControlKeyCtrlX represents Ctrl+X.
	ControlKeyCtrlY            ControlKey = 0x19 // ControlKeyCtrlY represents Ctrl+Y.
	ControlKeyCtrlZ            ControlKey = 0x1a // ControlKeyCtrlZ represents Ctrl+Z.
	ControlKeyCtrlOpenBracket  ControlKey = 0x1b // ControlKeyCtrlOpenBracket represents Ctrl+[ and Escape.
	ControlKeyCtrlBackslash    ControlKey = 0x1c // ControlKeyCtrlBackslash represents Ctrl+Backslash.
	ControlKeyCtrlCloseBracket ControlKey = 0x1d // ControlKeyCtrlCloseBracket represents Ctrl+].
	ControlKeyCtrlCaret        ControlKey = 0x1e // ControlKeyCtrlCaret represents Ctrl+^.
	ControlKeyCtrlUnderscore   ControlKey = 0x1f // ControlKeyCtrlUnderscore represents Ctrl+_.
	ControlKeyCtrlQuestionMark ControlKey = 0x7f // ControlKeyCtrlQuestionMark represents Ctrl+? and the DEL control byte.
)

// These ControlKey constants provide named aliases for common ASCII control-byte key events.
const (
	ControlKeyBreak     = ControlKeyCtrlC            // ControlKeyBreak represents Break, encoded as Ctrl+C.
	ControlKeyEnter     = ControlKeyCtrlM            // ControlKeyEnter represents Enter, encoded as carriage return.
	ControlKeyBackspace = ControlKeyCtrlQuestionMark // ControlKeyBackspace represents Backspace, encoded as DEL.
	ControlKeyTab       = ControlKeyCtrlI            // ControlKeyTab represents Tab.
	ControlKeyEsc       = ControlKeyCtrlOpenBracket  // ControlKeyEsc represents Escape, encoded as Ctrl+[.
	ControlKeyEscape    = ControlKeyEsc              // ControlKeyEscape is an alias for ControlKeyEsc.
)

const (
	controlKeySequenceStart ControlKey = 0x100
)

// These ControlKey constants identify recognized terminal escape-sequence keys.
const (
	ControlKeyUp             ControlKey = controlKeySequenceStart + iota // ControlKeyUp is the Up Arrow key.
	ControlKeyDown                                                       // ControlKeyDown is the Down Arrow key.
	ControlKeyRight                                                      // ControlKeyRight is the Right Arrow key.
	ControlKeyLeft                                                       // ControlKeyLeft is the Left Arrow key.
	ControlKeyShiftUp                                                    // ControlKeyShiftUp is the Shift-Up Arrow key.
	ControlKeyShiftDown                                                  // ControlKeyShiftDown is the Shift-Down Arrow key.
	ControlKeyShiftRight                                                 // ControlKeyShiftRight is the Shift-Right Arrow key.
	ControlKeyShiftLeft                                                  // ControlKeyShiftLeft is the Shift-Left Arrow key.
	ControlKeyCtrlUp                                                     // ControlKeyCtrlUp is the Ctrl-Up Arrow key.
	ControlKeyCtrlDown                                                   // ControlKeyCtrlDown is the Ctrl-Down Arrow key.
	ControlKeyCtrlRight                                                  // ControlKeyCtrlRight is the Ctrl-Right Arrow key.
	ControlKeyCtrlLeft                                                   // ControlKeyCtrlLeft is the Ctrl-Left Arrow key.
	ControlKeyCtrlShiftUp                                                // ControlKeyCtrlShiftUp is the Ctrl-Shift-Up Arrow key.
	ControlKeyCtrlShiftDown                                              // ControlKeyCtrlShiftDown is the Ctrl-Shift-Down Arrow key.
	ControlKeyCtrlShiftRight                                             // ControlKeyCtrlShiftRight is the Ctrl-Shift-Right Arrow key.
	ControlKeyCtrlShiftLeft                                              // ControlKeyCtrlShiftLeft is the Ctrl-Shift-Left Arrow key.
	ControlKeyHome                                                       // ControlKeyHome is the Home key.
	ControlKeyEnd                                                        // ControlKeyEnd is the End key.
	ControlKeyCtrlHome                                                   // ControlKeyCtrlHome is the Ctrl-Home key.
	ControlKeyCtrlEnd                                                    // ControlKeyCtrlEnd is the Ctrl-End key.
	ControlKeyShiftHome                                                  // ControlKeyShiftHome is the Shift-Home key.
	ControlKeyShiftEnd                                                   // ControlKeyShiftEnd is the Shift-End key.
	ControlKeyCtrlShiftHome                                              // ControlKeyCtrlShiftHome is the Ctrl-Shift-Home key.
	ControlKeyCtrlShiftEnd                                               // ControlKeyCtrlShiftEnd is the Ctrl-Shift-End key.
	ControlKeyPgUp                                                       // ControlKeyPgUp is the Page Up key.
	ControlKeyPgDown                                                     // ControlKeyPgDown is the Page Down key.
	ControlKeyCtrlPgUp                                                   // ControlKeyCtrlPgUp is the Ctrl-Page Up key.
	ControlKeyCtrlPgDown                                                 // ControlKeyCtrlPgDown is the Ctrl-Page Down key.
	ControlKeyInsert                                                     // ControlKeyInsert is the Insert key.
	ControlKeyDelete                                                     // ControlKeyDelete is the Delete key.
	ControlKeyShiftTab                                                   // ControlKeyShiftTab is the Shift-Tab key.
	ControlKeyF1                                                         // ControlKeyF1 is the F1 function key.
	ControlKeyF2                                                         // ControlKeyF2 is the F2 function key.
	ControlKeyF3                                                         // ControlKeyF3 is the F3 function key.
	ControlKeyF4                                                         // ControlKeyF4 is the F4 function key.
	ControlKeyF5                                                         // ControlKeyF5 is the F5 function key.
	ControlKeyF6                                                         // ControlKeyF6 is the F6 function key.
	ControlKeyF7                                                         // ControlKeyF7 is the F7 function key.
	ControlKeyF8                                                         // ControlKeyF8 is the F8 function key.
	ControlKeyF9                                                         // ControlKeyF9 is the F9 function key.
	ControlKeyF10                                                        // ControlKeyF10 is the F10 function key.
	ControlKeyF11                                                        // ControlKeyF11 is the F11 function key.
	ControlKeyF12                                                        // ControlKeyF12 is the F12 function key.
	ControlKeyF13                                                        // ControlKeyF13 is the F13 function key.
	ControlKeyF14                                                        // ControlKeyF14 is the F14 function key.
	ControlKeyF15                                                        // ControlKeyF15 is the F15 function key.
	ControlKeyF16                                                        // ControlKeyF16 is the F16 function key.
	ControlKeyF17                                                        // ControlKeyF17 is the F17 function key.
	ControlKeyF18                                                        // ControlKeyF18 is the F18 function key.
	ControlKeyF19                                                        // ControlKeyF19 is the F19 function key.
	ControlKeyF20                                                        // ControlKeyF20 is the F20 function key.
)

// These ControlKey aliases provide full-word names for page keys.
const (
	ControlKeyPageUp       = ControlKeyPgUp       // ControlKeyPageUp is an alias for ControlKeyPgUp.
	ControlKeyPageDown     = ControlKeyPgDown     // ControlKeyPageDown is an alias for ControlKeyPgDown.
	ControlKeyCtrlPageUp   = ControlKeyCtrlPgUp   // ControlKeyCtrlPageUp is an alias for ControlKeyCtrlPgUp.
	ControlKeyCtrlPageDown = ControlKeyCtrlPgDown // ControlKeyCtrlPageDown is an alias for ControlKeyCtrlPgDown.
)

// KeyEvent is sent when the user presses keys or pastes text.
type KeyEvent struct {
	ControlKey ControlKey // ControlKey is the control or special key pressed, or ControlKeyNone for rune input.
	Runes      []rune     // Runes contains non-control text input. It is usually one rune, but may contain multiple runes, especially for pasted text.
	Alt        bool       // Alt reports whether Alt was held for the key or rune input.
	Paste      bool       // Paste reports whether Runes came from bracketed paste mode. Paste implies ControlKeyNone, !Alt, and at least one rune.
}

// IsRunes reports whether the key event consists of non-control runes.
func (k KeyEvent) IsRunes() bool {
	return k.ControlKey == ControlKeyNone && len(k.Runes) > 0
}

// Rune returns the first rune in k.Runes, or 0 if there are none.
func (k KeyEvent) Rune() rune {
	if len(k.Runes) > 0 {
		return k.Runes[0]
	}
	return 0
}

// ResizeEvent is sent during startup and when the terminal window is resized.
type ResizeEvent struct {
	Width  int // Width is the terminal width in cells.
	Height int // Height is the terminal height in cells.
}

// SigResumeEvent will be sent when a program resumes from being suspended.
type SigResumeEvent struct{}

// CancelFunc cancels signal events (ex: SigTermEvent) and periodic send. It can be called idempotently and is always safe to call, even after the TUI has finished
// running.
type CancelFunc func()

// SigTermEvent will be sent when Quit is requested. It can be canceled with Cancel. An uncanceled event causes RunTUI to return with a nil error.
type SigTermEvent struct {
	Cancel CancelFunc // Cancel prevents this termination request from stopping the TUI. It is safe to call more than once.
}

// SigIntEvent will be sent when Interrupt is requested. It can be canceled with Cancel. An uncanceled event causes RunTUI to return with an ErrInterrupted error.
type SigIntEvent struct {
	Cancel CancelFunc // Cancel prevents this interrupt request from stopping the TUI. It is safe to call more than once.
}

// Model represents a user program.
//   - Init is called first, after Raw mode is entered.
//   - Update is called when events occur or when Send sends a user-defined message.
//   - View returns a string representing the TUI.
type Model interface {
	// Init is called first, after raw mode is entered. Programs may call things like t.SendPeriodically(myEvent, time.Second).
	Init(t *TUI)

	// Update is called when events occur or when Send sends a user-defined message.
	Update(t *TUI, m Message)

	// View returns a string of the full screen TUI. View is called shortly after Update (multiple Update calls may occur [ex: due to batching] before a single View
	// call).
	//
	// View may return ANY string, and no validation is done on it by this package.
	//
	// That being said, well-behaved TUI applications typically have the the mental model of a TUI that it is a rectangular block of rows separated by newlines. There
	// should be at most ResizeEvent.Height lines. Each line should be at most Width wide in printable, non-ANSI characters. This may include various Unicode characters
	// (uni.TextWidth can be used to determine width). The string may include ANSI control characters for colorizing the text or background. Again, these are just recommendations
	// and not enforced by this package.
	View() string
}

// A terminalController prepares and restores a terminal for a TUI session.
type terminalController interface {
	// Enter switches the terminal into the mode needed for full-screen rendering.
	Enter() error

	// Exit restores the terminal state changed by Enter.
	Exit() error
}

// terminalFactory creates a terminalController for the resolved TUI input and output streams.
//
// A nil controller is treated as a noop terminal.
type terminalFactory func(input io.Reader, output io.Writer) (terminalController, error)

// Options configure RunTUI.
type Options struct {
	// Input overrides os.Stdin when non-nil. Primarily used for testing. A non-tty input will still cause tui to open the controlling TTY for real input.
	Input io.Reader

	Output            io.Writer                // Output overrides os.Stdout when non-nil. Primarily used for testing.
	Framerate         int                      // If Framerate is between 60-120 inclusive, the terminal will be refreshed at this framerate (otherwise, it uses 60 FPS).
	EnableMouse       bool                     // EnableMouse enables mouse tracking and delivery of MouseEvent messages. Disabled by default.
	skipTTYValidation bool                     // skipTTYValidation bypasses TTY checks and falls back to a noop terminal when terminal creation fails.
	terminalFactory   terminalFactory          // terminalFactory overrides terminal controller creation for tests and custom terminals.
	sizeProvider      func() (int, int, error) // sizeProvider overrides terminal size detection and returns width, height, and any detection error.
}

// ErrNoTTY is returned when no usable terminal is available.
var ErrNoTTY = errors.New("tui: no tty available")

// ErrInterrupted is returned when the TUI is interrupted (ex: via Interrupt).
var ErrInterrupted = errors.New("tui: interrupted")

// RunTUI makes a new TUI and runs it. Alt/raw mode is entered (non-TTYs return ErrNoTTY). RunTUI doesn't return until the TUI stops (Quit or Interrupt is called
// without canceling their events).
func RunTUI(m Model, opts Options) error {
	if m == nil {
		return errors.New("tui: model is nil")
	}

	t := newTUI(m, opts)
	if err := t.prepareIO(); err != nil {
		return err
	}
	return t.run()
}

// A signalKind identifies the shutdown behavior requested by a signalRequest.
type signalKind int

const (
	_ signalKind = iota
	signalKindQuit
	signalKindInterrupt
)

// The signalRequest type tracks a cancellable quit or interrupt request while its event is being processed.
type signalRequest struct {
	kind     signalKind  // kind selects the shutdown behavior to apply if the request is not canceled.
	canceled atomic.Bool // canceled reports whether the event's CancelFunc has been called.
	once     sync.Once   // once ensures CancelFunc records cancellation at most once.
}

func newSignalRequest(kind signalKind) *signalRequest {
	return &signalRequest{kind: kind}
}

// The cancelFunc method returns a CancelFunc that marks s as canceled exactly once.
//
// The receiver must be non-nil; the returned function is safe to call repeatedly.
func (s *signalRequest) cancelFunc() CancelFunc {
	return func() {
		s.once.Do(func() {
			s.canceled.Store(true)
		})
	}
}

// The isCanceled method reports whether s is non-nil and its CancelFunc has been called.
func (s *signalRequest) isCanceled() bool {
	return s != nil && s.canceled.Load()
}

// The messageEnvelope type carries a message and any signal request metadata needed after Update returns.
type messageEnvelope struct {
	msg    Message        // msg is the value delivered to Model.Update.
	signal *signalRequest // signal is the optional quit or interrupt request associated with msg.
}

// A suspendRequest asks the TUI loop to suspend the process.
type suspendRequest struct{}

// TUI controls a running full-screen terminal UI session.
//
// RunTUI creates a TUI and passes it to Model methods. Once RunTUI returns, the TUI is inert; its public methods are safe to call but have no effect, and the value
// cannot be reused.
type TUI struct {
	model          Model                    // Model is the user program driven by the session.
	opts           Options                  // Opts is the configuration used to create the session.
	frameDuration  time.Duration            // FrameDuration is the minimum time between terminal renders.
	term           terminalController       // Term prepares and restores the terminal for full-screen rendering.
	termFactory    terminalFactory          // TermFactory creates terminal controllers for resolved streams.
	sizeProvider   func() (int, int, error) // SizeProvider returns terminal width and height when configured.
	input          io.Reader                // Input is the resolved input stream used for key, paste, and mouse input.
	output         io.Writer                // Output is the resolved output stream used for rendering and terminal escape sequences.
	ctx            context.Context          // Ctx is canceled when session shutdown begins.
	cancel         context.CancelFunc       // Cancel cancels ctx to notify managed work that shutdown has begun.
	panicWriter    io.Writer                // PanicWriter receives panic reports before captured panics are re-raised.
	messages       chan messageEnvelope     // Messages queues events and user messages for the main loop.
	mu             sync.Mutex               // Mu protects stopping, err, stopClosers, and cleanupClosers.
	stopping       bool                     // Stopping reports whether shutdown has begun.
	err            error                    // Err is the error RunTUI should return after shutdown.
	stopClosers    []func()                 // StopClosers run as soon as shutdown begins to unblock active work.
	cleanupClosers []func()                 // CleanupClosers run after tracked goroutines finish.
	sizeMu         sync.Mutex               // SizeMu protects the last known terminal dimensions.
	lastWidth      int                      // LastWidth is the most recently observed terminal width in cells.
	lastHeight     int                      // LastHeight is the most recently observed terminal height in cells.
	sizeKnown      bool                     // SizeKnown reports whether a terminal size has been observed.
	suspendMu      sync.Mutex               // SuspendMu protects suspended.
	suspended      bool                     // Suspended reports whether the process is currently suspended by the TUI.
	wg             sync.WaitGroup           // WG tracks goroutines that must finish before final cleanup completes.
	panicOnce      sync.Once                // PanicOnce ensures only the first recovered panic is recorded.
	panicMu        sync.Mutex               // PanicMu protects panicValue and panicStack.
	panicValue     any                      // PanicValue is the first recovered panic value.
	panicStack     []byte                   // PanicStack is the stack trace captured with panicValue.
	renderMu       sync.Mutex               // RenderMu serializes output writes and protects render cache state.
	prevLines      []string                 // PrevLines is the most recent rendered view split into lines.
	fullRedraw     bool                     // FullRedraw makes the next render clear the screen and repaint all lines.
	lastRender     time.Time                // LastRender is the time of the most recent terminal write.
}

// The newTUI function constructs a not-yet-running TUI for m and opts.
//
// It normalizes the framerate, wraps the terminal factory to apply mouse settings, and does not touch the terminal.
func newTUI(m Model, opts Options) *TUI {
	ctx, cancel := context.WithCancel(context.Background())

	baseFactory := opts.terminalFactory
	if baseFactory == nil {
		baseFactory = defaultTerminalFactory
	}
	factory := func(input io.Reader, output io.Writer) (terminalController, error) {
		term, err := baseFactory(input, output)
		if err != nil {
			return term, err
		}
		if term != nil {
			if setter, ok := term.(mouseModeSetter); ok {
				setter.setMouseEnabled(opts.EnableMouse)
			}
		}
		return term, nil
	}

	framerate := opts.Framerate
	if framerate < 60 || framerate > 120 {
		framerate = 60
	}
	frameDuration := time.Second / time.Duration(framerate)

	return &TUI{
		model:         m,
		opts:          opts,
		termFactory:   factory,
		sizeProvider:  opts.sizeProvider,
		ctx:           ctx,
		cancel:        cancel,
		panicWriter:   os.Stderr,
		messages:      make(chan messageEnvelope, 64),
		frameDuration: frameDuration,
		fullRedraw:    true,
	}
}

// The prepareIO method resolves input and output streams, ensures a usable terminal is available, creates the terminal controller, and registers cleanup.
//
// It returns ErrNoTTY when TTY validation is required and no controlling terminal can be opened.
func (t *TUI) prepareIO() error {
	input := t.opts.Input
	if input == nil {
		input = os.Stdin
	}
	output := t.opts.Output
	if output == nil {
		output = os.Stdout
	}
	t.input = input
	t.output = output

	var stopClosers []func()
	var cleanupClosers []func()
	var inputReplaced bool

	if !t.opts.skipTTYValidation {
		inputTTY := isTTY(input)
		outputTTY := isTTY(output)
		if !inputTTY || !outputTTY {
			pair, err := openControllingTTY()
			if err != nil {
				return ErrNoTTY
			}
			if !inputTTY {
				t.input = pair.reader
				inputReplaced = true
			}
			if !outputTTY {
				t.output = pair.writer
			}
			closer := onceFunc(pair.close)
			stopClosers = append(stopClosers, closer)
			cleanupClosers = append(cleanupClosers, closer)
		}
	}

	if err := t.setupInputHandle(&stopClosers, &cleanupClosers, inputReplaced); err != nil {
		for _, fn := range cleanupClosers {
			fn()
		}
		return err
	}

	term, err := t.termFactory(t.input, t.output)
	if err != nil {
		if t.opts.skipTTYValidation {
			term = &noopTerminal{}
		} else {
			for _, fn := range cleanupClosers {
				fn()
			}
			return err
		}
	}
	if term == nil {
		term = &noopTerminal{}
	}
	t.term = term

	termExit := onceFunc(func() { _ = t.term.Exit() })
	t.registerStopCloser(termExit)
	t.registerCleanupCloser(termExit)
	for _, fn := range stopClosers {
		t.registerStopCloser(fn)
	}
	for _, fn := range cleanupClosers {
		t.registerCleanupCloser(fn)
	}

	return nil
}

// The setupInputHandle method prepares t.input for reader shutdown and cleanup.
//
// It appends any required close functions to stopClosers and cleanupClosers, duplicating shared file descriptors when needed. If inputReplaced is true, it leaves
// t.input unchanged.
func (t *TUI) setupInputHandle(stopClosers *[]func(), cleanupClosers *[]func(), inputReplaced bool) error {
	if inputReplaced {
		return nil
	}

	switch r := t.input.(type) {
	case *os.File:
		if isSameFile(r, os.Stdin) {
			dup, err := duplicateFile(r)
			if err != nil {
				return err
			}
			t.input = dup
			closer := onceFunc(func() { _ = dup.Close() })
			*stopClosers = append(*stopClosers, closer)
			*cleanupClosers = append(*cleanupClosers, closer)
			return nil
		}
		if isSameFile(r, t.opts.inputFile()) {
			dup, err := duplicateFile(r)
			if err != nil {
				return err
			}
			t.input = dup
			closer := onceFunc(func() { _ = dup.Close() })
			*stopClosers = append(*stopClosers, closer)
			*cleanupClosers = append(*cleanupClosers, closer)
			return nil
		}
		if t.opts.Input != nil {
			closer := onceFunc(func() { _ = r.Close() })
			*stopClosers = append(*stopClosers, closer)
			*cleanupClosers = append(*cleanupClosers, closer)
		}
	case io.Closer:
		if t.opts.Input != nil {
			closer := onceFunc(func() { _ = r.Close() })
			*stopClosers = append(*stopClosers, closer)
			*cleanupClosers = append(*cleanupClosers, closer)
		}
	}
	return nil
}

// The inputFile method returns Input as an *os.File, or nil when Input is nil or a different reader type.
func (opts Options) inputFile() *os.File {
	if f, ok := opts.Input.(*os.File); ok {
		return f
	}
	return nil
}

func isSameFile(a, b *os.File) bool {
	if a == nil || b == nil {
		return false
	}
	return a.Fd() == b.Fd()
}

func onceFunc(fn func()) func() {
	var once sync.Once
	return func() {
		once.Do(fn)
	}
}

// The capturePanic method records the first non-nil panic and begins shutdown.
//
// If stack is nil, capturePanic records the current goroutine stack.
func (t *TUI) capturePanic(value any, stack []byte) {
	if value == nil {
		return
	}
	if stack == nil {
		stack = debug.Stack()
	}
	t.panicOnce.Do(func() {
		t.panicMu.Lock()
		t.panicValue = value
		t.panicStack = append([]byte(nil), stack...)
		t.panicMu.Unlock()
		t.stop(nil)
	})
}

// The panicInfo method returns the captured panic value and stack trace, if any.
func (t *TUI) panicInfo() (any, []byte) {
	t.panicMu.Lock()
	defer t.panicMu.Unlock()
	return t.panicValue, t.panicStack
}

// The run method executes the TUI loop and performs final cleanup before returning.
//
// If a panic was captured, run writes the panic report to panicWriter and re-panics after cleanup restores terminal state.
func (t *TUI) run() (err error) {
	defer func() {
		t.cleanup()
		if value, stack := t.panicInfo(); value != nil {
			fmt.Fprintf(t.panicWriter, "panic: %v\n%s", value, stack)
			panic(value)
		}
	}()

	err = t.loop()
	return err
}

// The loop method runs the main TUI lifecycle and message loop.
//
// It initializes terminal processing, dispatches messages to the model, renders after updates, and applies uncanceled quit or interrupt requests.
func (t *TUI) loop() (err error) {
	defer func() {
		if r := recover(); r != nil {
			t.capturePanic(r, debug.Stack())
		}
	}()

	t.startSignalProcessor()
	if err := t.enterTerminal(); err != nil {
		return err
	}
	t.startInputReader()
	t.startResizeWatcher()
	t.triggerResizeEvent()
	t.model.Init(t)
	t.render()

	for {
		select {
		case <-t.ctx.Done():
			return t.err
		case env := <-t.messages:
			if _, ok := env.msg.(suspendRequest); ok {
				if err := t.performSuspend(); err != nil {
					t.stop(err)
					return err
				}
				continue
			}
			t.model.Update(t, env.msg)
			t.render()

			if env.signal != nil && !env.signal.isCanceled() {
				switch env.signal.kind {
				case signalKindQuit:
					t.stop(nil)
					return nil
				case signalKindInterrupt:
					t.stop(ErrInterrupted)
					return ErrInterrupted
				}
			}
		}
	}
}

// The render method paints the current model view to the terminal output.
//
// It diffs the new view against the previous rendered lines, honors the configured frame duration, and ignores write errors.
func (t *TUI) render() {
	if t.output == nil {
		return
	}
	lines := splitViewLines(t.model.View())

	for {
		t.renderMu.Lock()

		if t.frameDuration > 0 && !t.lastRender.IsZero() {
			if remaining := t.frameDuration - time.Since(t.lastRender); remaining > 0 {
				t.renderMu.Unlock()
				time.Sleep(remaining)
				continue
			}
		}

		output, changed := t.buildRenderOutputLocked(lines)
		if len(lines) == 0 {
			t.prevLines = nil
		} else {
			t.prevLines = append(t.prevLines[:0], lines...)
		}

		if !changed {
			t.renderMu.Unlock()
			return
		}

		_, _ = io.WriteString(t.output, output)
		t.lastRender = time.Now()
		t.renderMu.Unlock()
		return
	}
}

func splitViewLines(view string) []string {
	if view == "" {
		return nil
	}
	return strings.Split(view, "\n")
}

// The buildRenderOutputLocked method builds the ANSI output needed to render lines.
//
// The caller must hold t.renderMu. The method consumes t.fullRedraw and reports whether any terminal output needs to be written.
func (t *TUI) buildRenderOutputLocked(lines []string) (string, bool) {
	prevLen := len(t.prevLines)
	full := t.fullRedraw
	t.fullRedraw = false

	var b strings.Builder
	if full {
		b.WriteString(clearScreen)
	}

	maxLen := len(lines)
	if !full && prevLen > maxLen {
		maxLen = prevLen
	}
	for i := 0; i < maxLen; i++ {
		var newLine string
		if i < len(lines) {
			newLine = lines[i]
		}

		if full {
			if newLine == "" {
				continue
			}
			appendMoveToLine(&b, i+1)
			b.WriteString(newLine)
			continue
		}

		var prevLine string
		if i < prevLen {
			prevLine = t.prevLines[i]
		}

		if newLine == prevLine && !(i >= len(lines) && i < prevLen) {
			continue
		}

		appendMoveToLine(&b, i+1)
		// If the terminal cell width didn't change, we can overwrite in-place
		// without clearing the whole line first.
		if newLine == "" || termformat.TextWidthWithANSICodes(newLine) != termformat.TextWidthWithANSICodes(prevLine) {
			b.WriteString(clearLine)
		}
		b.WriteString(newLine)
	}

	if b.Len() == 0 {
		return "", false
	}
	return b.String(), true
}

func appendMoveToLine(b *strings.Builder, row int) {
	b.WriteString("\x1b[")
	b.WriteString(strconv.Itoa(row))
	b.WriteString(";1H")
}

// The invalidateRenderCache method clears cached render state.
//
// If forceFull is true, the next render clears the screen and repaints the full view.
func (t *TUI) invalidateRenderCache(forceFull bool) {
	t.renderMu.Lock()
	if forceFull {
		t.fullRedraw = true
	}
	t.prevLines = nil
	t.renderMu.Unlock()
}

// Calling Quit sends the SigTermEvent. Unless the event is canceled, it causes RunTUI to return with a nil error.
//   - This can be called inside a Model's Update method, or elsewhere.
//   - SIGTERM calls Quit().
func (t *TUI) Quit() {
	t.enqueueSignal(signalKindQuit)
}

// Calling Interrupt sends the SigIntEvent. Unless the event is canceled, it causes RunTUI to return with an ErrInterrupted error.
//   - This can be called inside a Model's Update method, or elsewhere.
//   - To handle Ctrl-C, user programs need to detect Ctrl-C keystroke and call Interrupt.
//   - SIGINT calls Interrupt().
func (t *TUI) Interrupt() {
	t.enqueueSignal(signalKindInterrupt)
}

// Calling Suspend causes the program to suspend.
//   - This can be called inside a Model's Update method, or elsewhere.
//   - To handle Ctrl-Z, user programs need to detect Ctrl-Z keystroke and call Suspend.
//   - Any outstanding Update/View call will finish before we actually suspend the program, and won't be called any more until the program is resumed.
//   - SIGTSTP calls Suspend().
//   - When the program is resumed, a SigResumeEvent will be sent to Update.
func (t *TUI) Suspend() {
	if !suspendSupported() {
		return
	}
	t.enqueueSuspend()
}

// Send enqueues m to be sent to the Model's Update function. Can be called from any goroutine.
func (t *TUI) Send(m Message) {
	t.enqueue(messageEnvelope{msg: m})
}

// Periodically will cause m to be sent to the Model's Update function every d duration. The caller can stop this by calling the returned cancel function. The first
// time m is sent must be <= d from now, but is otherwise undefined. Good for consistent animations (ex: blinking cursor).
func (t *TUI) SendPeriodically(m Message, d time.Duration) CancelFunc {
	if d <= 0 {
		d = time.Millisecond
	}

	ctx, cancel := context.WithCancel(t.ctx)
	if !t.registerStopCloser(cancel) {
		cancel()
		return func() {}
	}

	var once sync.Once
	cancelFn := func() { once.Do(cancel) }

	t.wg.Add(1)
	go func() {
		defer t.wg.Done()

		ticker := time.NewTicker(d)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				t.Send(m)
			}
		}
	}()

	return cancelFn
}

// SendOnceAfter will cause m to be sent to the Model's Update function after d time from now. It can be used to produce ad-hoc or variable-timing animations.
func (t *TUI) SendOnceAfter(m Message, d time.Duration) {
	if d < 0 {
		d = 0
	}
	timer := time.AfterFunc(d, func() {
		t.Send(m)
	})
	if !t.registerStopCloser(func() { timer.Stop() }) {
		timer.Stop()
	}
}

// Go runs f in a new goroutine. ctx should be checked for cancellation. If f returns a non-nil value, it is enqueued for sending via Send.
//
// Go is a great place to do I/O like HTTP requests.
func (t *TUI) Go(f func(ctx context.Context) Message) {
	ctx, cancel := context.WithCancel(t.ctx)
	if !t.registerStopCloser(cancel) {
		cancel()
		return
	}
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		defer cancel()
		defer t.handleGoPanic()

		msg := f(ctx)
		if msg != nil {
			t.Send(msg)
		}
	}()
}

// The handleGoPanic method captures a panic from a goroutine started by Go.
func (t *TUI) handleGoPanic() {
	if r := recover(); r != nil {
		t.capturePanic(r, debug.Stack())
	}
}

// The enqueueSignal method creates and enqueues the cancellable signal event for kind.
//
// Unknown signal kinds are ignored.
func (t *TUI) enqueueSignal(kind signalKind) {
	req := newSignalRequest(kind)
	var msg Message

	switch kind {
	case signalKindQuit:
		msg = SigTermEvent{Cancel: req.cancelFunc()}
	case signalKindInterrupt:
		msg = SigIntEvent{Cancel: req.cancelFunc()}
	default:
		return
	}

	t.enqueue(messageEnvelope{
		msg:    msg,
		signal: req,
	})
}

// The enqueueSuspend method queues a suspend request for the TUI loop.
//
// The request is dropped if the TUI is already stopping.
func (t *TUI) enqueueSuspend() {
	t.enqueue(messageEnvelope{msg: suspendRequest{}})
}

// The enqueue method sends env to the TUI loop unless the TUI is stopping.
//
// It blocks until the message is queued or the TUI context is canceled.
func (t *TUI) enqueue(env messageEnvelope) {
	t.mu.Lock()
	if t.stopping {
		t.mu.Unlock()
		return
	}
	ch := t.messages
	t.mu.Unlock()

	select {
	case ch <- env:
	case <-t.ctx.Done():
	}
}

// The stop method begins TUI shutdown with err as the return error.
//
// It is idempotent, cancels the session context, and runs registered stop closers at most once.
func (t *TUI) stop(err error) {
	var closers []func()

	t.mu.Lock()
	if t.stopping {
		if t.err == nil {
			t.err = err
		}
		t.mu.Unlock()
		t.cancel()
		return
	}
	t.stopping = true
	t.err = err
	closers = append(closers, t.stopClosers...)
	t.stopClosers = nil
	t.mu.Unlock()

	t.cancel()
	for _, fn := range closers {
		fn()
	}
}

// The cleanup method performs final TUI cleanup after the main loop exits.
//
// It begins shutdown, waits for tracked goroutines to finish, and then runs final cleanup closers.
func (t *TUI) cleanup() {
	t.stop(t.err)
	t.wg.Wait()

	t.mu.Lock()
	closers := append([]func(){}, t.cleanupClosers...)
	t.cleanupClosers = nil
	t.mu.Unlock()

	for _, fn := range closers {
		fn()
	}
}

// The registerStopCloser method records fn to run when shutdown begins.
//
// It returns false if the TUI is already stopping; in that case fn is not called.
func (t *TUI) registerStopCloser(fn func()) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopping {
		return false
	}
	t.stopClosers = append(t.stopClosers, fn)
	return true
}

// The registerCleanupCloser method records fn to run during final cleanup.
//
// The function must be non-nil.
func (t *TUI) registerCleanupCloser(fn func()) {
	t.mu.Lock()
	t.cleanupClosers = append(t.cleanupClosers, fn)
	t.mu.Unlock()
}

// The enterTerminal method switches the configured terminal controller into TUI mode.
//
// A nil terminal controller is treated as a no-op.
func (t *TUI) enterTerminal() error {
	if t.term == nil {
		return nil
	}
	return t.term.Enter()
}

// The startInputReader method starts processing terminal input when an input stream is configured.
func (t *TUI) startInputReader() {
	if t.input == nil {
		return
	}
	newInputProcessor(t, t.input).start()
}

// The triggerResizeEvent method sends a ResizeEvent when the terminal size changes.
func (t *TUI) triggerResizeEvent() {
	width, height, err := t.terminalSize()
	if err != nil {
		return
	}
	if !t.storeSize(width, height) {
		return
	}
	t.invalidateRenderCache(true)
	t.Send(ResizeEvent{Width: width, Height: height})
}

// The storeSize method records the terminal size and reports whether it changed.
func (t *TUI) storeSize(width, height int) bool {
	t.sizeMu.Lock()
	changed := !t.sizeKnown || t.lastWidth != width || t.lastHeight != height
	if changed {
		t.lastWidth = width
		t.lastHeight = height
		t.sizeKnown = true
	}
	t.sizeMu.Unlock()
	return changed
}

// The terminalSize method returns the current terminal width and height in cells.
//
// It uses the configured size provider when present; otherwise it queries the output file first and the input file second.
func (t *TUI) terminalSize() (int, int, error) {
	if t.sizeProvider != nil {
		return t.sizeProvider()
	}

	var lastErr error
	if f, ok := t.output.(*os.File); ok && f != nil {
		if w, h, err := term.GetSize(int(f.Fd())); err == nil {
			return w, h, nil
		} else {
			lastErr = err
		}
	}
	if f, ok := t.input.(*os.File); ok && f != nil {
		if w, h, err := term.GetSize(int(f.Fd())); err == nil {
			return w, h, nil
		} else {
			lastErr = err
		}
	}
	if lastErr != nil {
		return 0, 0, lastErr
	}
	return 0, 0, errors.New("tui: terminal size unavailable")
}

// The handleResizeSignal method handles a terminal resize notification.
func (t *TUI) handleResizeSignal() {
	t.triggerResizeEvent()
}

// The signalBinding type associates an operating-system signal with the TUI action that handles it.
type signalBinding struct {
	sig    os.Signal  // Sig is the signal received from the operating system.
	action func(*TUI) // Action handles sig for the active TUI.
}

// The startSignalProcessor method starts platform signal handling for the TUI session.
//
// It registers the configured signal bindings, dispatches received signals to their actions, and unregisters the signal channel when shutdown begins.
func (t *TUI) startSignalProcessor() {
	bindings := signalBindings()
	if len(bindings) == 0 {
		return
	}

	signals := make([]os.Signal, len(bindings))
	for i, b := range bindings {
		signals[i] = b.sig
	}

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, signals...)
	if !t.registerStopCloser(func() {
		signal.Stop(ch)
		close(ch)
	}) {
		signal.Stop(ch)
		close(ch)
		return
	}

	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		for {
			select {
			case <-t.ctx.Done():
				return
			case sig, ok := <-ch:
				if !ok {
					return
				}
				for _, b := range bindings {
					if sig == b.sig {
						b.action(t)
						break
					}
				}
			}
		}
	}()
}

// ttyResources contains the input, output, and cleanup function for an opened controlling terminal.
type ttyResources struct {
	reader io.Reader // reader reads from the controlling terminal.
	writer io.Writer // writer writes to the controlling terminal.
	close  func()    // close releases the controlling terminal resources.
}

// openControllingTTY opens the process controlling terminal for input and output.
func openControllingTTY() (*ttyResources, error) {
	switch runtime.GOOS {
	case "windows":
		in, err := os.OpenFile("CONIN$", os.O_RDONLY, 0)
		if err != nil {
			return nil, err
		}
		out, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
		if err != nil {
			_ = in.Close()
			return nil, err
		}
		var once sync.Once
		return &ttyResources{
			reader: in,
			writer: out,
			close: func() {
				once.Do(func() {
					_ = in.Close()
					_ = out.Close()
				})
			},
		}, nil
	default:
		f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
		if err != nil {
			return nil, err
		}
		var once sync.Once
		return &ttyResources{
			reader: f,
			writer: f,
			close: func() {
				once.Do(func() {
					_ = f.Close()
				})
			},
		}, nil
	}
}
