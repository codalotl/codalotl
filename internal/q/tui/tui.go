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

const (
	ControlKeyCtrlAt           ControlKey = 0x00 // ctrl+@
	ControlKeyCtrlA            ControlKey = 0x01
	ControlKeyCtrlB            ControlKey = 0x02
	ControlKeyCtrlC            ControlKey = 0x03
	ControlKeyCtrlD            ControlKey = 0x04
	ControlKeyCtrlE            ControlKey = 0x05
	ControlKeyCtrlF            ControlKey = 0x06
	ControlKeyCtrlG            ControlKey = 0x07
	ControlKeyCtrlH            ControlKey = 0x08
	ControlKeyCtrlI            ControlKey = 0x09
	ControlKeyCtrlJ            ControlKey = 0x0a
	ControlKeyCtrlK            ControlKey = 0x0b
	ControlKeyCtrlL            ControlKey = 0x0c
	ControlKeyCtrlM            ControlKey = 0x0d
	ControlKeyCtrlN            ControlKey = 0x0e
	ControlKeyCtrlO            ControlKey = 0x0f
	ControlKeyCtrlP            ControlKey = 0x10
	ControlKeyCtrlQ            ControlKey = 0x11
	ControlKeyCtrlR            ControlKey = 0x12
	ControlKeyCtrlS            ControlKey = 0x13
	ControlKeyCtrlT            ControlKey = 0x14
	ControlKeyCtrlU            ControlKey = 0x15
	ControlKeyCtrlV            ControlKey = 0x16
	ControlKeyCtrlW            ControlKey = 0x17
	ControlKeyCtrlX            ControlKey = 0x18
	ControlKeyCtrlY            ControlKey = 0x19
	ControlKeyCtrlZ            ControlKey = 0x1a
	ControlKeyCtrlOpenBracket  ControlKey = 0x1b
	ControlKeyCtrlBackslash    ControlKey = 0x1c
	ControlKeyCtrlCloseBracket ControlKey = 0x1d
	ControlKeyCtrlCaret        ControlKey = 0x1e
	ControlKeyCtrlUnderscore   ControlKey = 0x1f
	ControlKeyCtrlQuestionMark ControlKey = 0x7f
)

const (
	ControlKeyBreak     = ControlKeyCtrlC
	ControlKeyEnter     = ControlKeyCtrlM
	ControlKeyBackspace = ControlKeyCtrlQuestionMark
	ControlKeyTab       = ControlKeyCtrlI
	ControlKeyEsc       = ControlKeyCtrlOpenBracket
	ControlKeyEscape    = ControlKeyEsc
)

const (
	controlKeySequenceStart ControlKey = 0x100
)

const (
	ControlKeyUp ControlKey = controlKeySequenceStart + iota
	ControlKeyDown
	ControlKeyRight
	ControlKeyLeft
	ControlKeyShiftUp
	ControlKeyShiftDown
	ControlKeyShiftRight
	ControlKeyShiftLeft
	ControlKeyCtrlUp
	ControlKeyCtrlDown
	ControlKeyCtrlRight
	ControlKeyCtrlLeft
	ControlKeyCtrlShiftUp
	ControlKeyCtrlShiftDown
	ControlKeyCtrlShiftRight
	ControlKeyCtrlShiftLeft
	ControlKeyHome
	ControlKeyEnd
	ControlKeyCtrlHome
	ControlKeyCtrlEnd
	ControlKeyShiftHome
	ControlKeyShiftEnd
	ControlKeyCtrlShiftHome
	ControlKeyCtrlShiftEnd
	ControlKeyPgUp
	ControlKeyPgDown
	ControlKeyCtrlPgUp
	ControlKeyCtrlPgDown
	ControlKeyInsert
	ControlKeyDelete
	ControlKeyShiftTab
	ControlKeyF1
	ControlKeyF2
	ControlKeyF3
	ControlKeyF4
	ControlKeyF5
	ControlKeyF6
	ControlKeyF7
	ControlKeyF8
	ControlKeyF9
	ControlKeyF10
	ControlKeyF11
	ControlKeyF12
	ControlKeyF13
	ControlKeyF14
	ControlKeyF15
	ControlKeyF16
	ControlKeyF17
	ControlKeyF18
	ControlKeyF19
	ControlKeyF20
)

const (
	ControlKeyPageUp       = ControlKeyPgUp
	ControlKeyPageDown     = ControlKeyPgDown
	ControlKeyCtrlPageUp   = ControlKeyCtrlPgUp
	ControlKeyCtrlPageDown = ControlKeyCtrlPgDown
)

// KeyEvent is sent when the user presses keys or pastes text.
type KeyEvent struct {
	ControlKey ControlKey
	Runes      []rune
	Alt        bool
	Paste      bool
}

// IsRunes reports whether the key event consists of non-control runes.
func (k KeyEvent) IsRunes() bool {
	return k.ControlKey == ControlKeyNone && len(k.Runes) > 0
}

func (k KeyEvent) Rune() rune {
	if len(k.Runes) > 0 {
		return k.Runes[0]
	}
	return 0
}

// ResizeEvent is sent during startup and when the terminal window is resized.
type ResizeEvent struct {
	Width  int
	Height int
}

// SigResumeEvent will be sent when a program resumes from being suspended.
type SigResumeEvent struct{}

// CancelFunc cancels signal events (ex: SigTermEvent) and periodic send. It can be called idempotently and is always safe to call, even after the TUI has finished
// running.
type CancelFunc func()

// SigTermEvent will be sent when Quit is requested. It can be canceled with Cancel. An uncanceled event causes RunTUI to return with a nil error.
type SigTermEvent struct {
	Cancel CancelFunc
}

// SigIntEvent will be sent when Interrupt is requested. It can be canceled with Cancel. An uncanceled event causes RunTUI to return with an ErrInterrupted error.
type SigIntEvent struct {
	Cancel CancelFunc
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

type terminalController interface {
	Enter() error
	Exit() error
}

type terminalFactory func(input io.Reader, output io.Writer) (terminalController, error)

// Options configure RunTUI.
type Options struct {
	// Input overrides os.Stdin when non-nil. Primarily used for testing. A non-tty input will still cause tui to open the controlling TTY for real input.
	Input io.Reader

	Output            io.Writer // Output overrides os.Stdout when non-nil. Primarily used for testing.
	Framerate         int       // If Framerate is between 60-120 inclusive, the terminal will be refreshed at this framerate (otherwise, it uses 60 FPS).
	EnableMouse       bool      // EnableMouse enables mouse tracking and delivery of MouseEvent messages. Disabled by default.
	skipTTYValidation bool
	terminalFactory   terminalFactory
	sizeProvider      func() (int, int, error)
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

type signalKind int

const (
	_ signalKind = iota
	signalKindQuit
	signalKindInterrupt
)

type signalRequest struct {
	kind     signalKind
	canceled atomic.Bool
	once     sync.Once
}

func newSignalRequest(kind signalKind) *signalRequest {
	return &signalRequest{kind: kind}
}

func (s *signalRequest) cancelFunc() CancelFunc {
	return func() {
		s.once.Do(func() {
			s.canceled.Store(true)
		})
	}
}

func (s *signalRequest) isCanceled() bool {
	return s != nil && s.canceled.Load()
}

type messageEnvelope struct {
	msg    Message
	signal *signalRequest
}

type suspendRequest struct{}

type TUI struct {
	model Model

	opts Options

	frameDuration time.Duration

	term         terminalController
	termFactory  terminalFactory
	sizeProvider func() (int, int, error)

	input  io.Reader
	output io.Writer

	ctx    context.Context
	cancel context.CancelFunc

	panicWriter io.Writer

	messages chan messageEnvelope

	mu             sync.Mutex
	stopping       bool
	err            error
	stopClosers    []func()
	cleanupClosers []func()

	sizeMu     sync.Mutex
	lastWidth  int
	lastHeight int
	sizeKnown  bool

	suspendMu sync.Mutex
	suspended bool

	wg sync.WaitGroup

	panicOnce  sync.Once
	panicMu    sync.Mutex
	panicValue any
	panicStack []byte

	renderMu   sync.Mutex
	prevLines  []string
	fullRedraw bool
	lastRender time.Time
}

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

func (t *TUI) panicInfo() (any, []byte) {
	t.panicMu.Lock()
	defer t.panicMu.Unlock()
	return t.panicValue, t.panicStack
}

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

func (t *TUI) handleGoPanic() {
	if r := recover(); r != nil {
		t.capturePanic(r, debug.Stack())
	}
}

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

func (t *TUI) enqueueSuspend() {
	t.enqueue(messageEnvelope{msg: suspendRequest{}})
}

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

func (t *TUI) registerStopCloser(fn func()) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopping {
		return false
	}
	t.stopClosers = append(t.stopClosers, fn)
	return true
}

func (t *TUI) registerCleanupCloser(fn func()) {
	t.mu.Lock()
	t.cleanupClosers = append(t.cleanupClosers, fn)
	t.mu.Unlock()
}

func (t *TUI) enterTerminal() error {
	if t.term == nil {
		return nil
	}
	return t.term.Enter()
}

func (t *TUI) startInputReader() {
	if t.input == nil {
		return
	}
	newInputProcessor(t, t.input).start()
}

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

func (t *TUI) handleResizeSignal() {
	t.triggerResizeEvent()
}

type signalBinding struct {
	sig    os.Signal
	action func(*TUI)
}

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

type ttyResources struct {
	reader io.Reader
	writer io.Writer
	close  func()
}

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
