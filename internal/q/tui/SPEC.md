# tui

tui is a package designed to build cross-platform TUIs taking up the full screen. Non-fullscreen CLIs are not supported (only alt/raw mode is supported).

## Usage

```go
type model struct {
	message       string
	width, height int
}

func (m *model) Init(t *TUI) {}
func (m *model) Update(t *TUI, msg Message) {
	switch ev := msg.(type) {
	case KeyEvent:
		if ev.ControlKey == ControlKeyCtrlC {
			t.Quit() // Quit quits without error, which is what we want here.
		} else if ev.IsRunes() { // Non-control character
			m.message += string(ev.Runes)
		}
	case ResizeEvent:
		m.width, m.height = ev.Width, ev.Height
	}
}

func (m *model) View() string {
	// NOTE: real code would ensure this text fits within m.width and m.height.
	return "You typed: '" + m.message + "'"
}

func main() {
	m := model{}
	if err := RunTUI(&m, Options{}); err != nil {
		fmt.Println("Error:", err)
	}
}
```

## Non-features

- Raw/Alt mode is required. There is no graceful fallback to non-TTY modes. Calling packages can still implement fallbacks themselves, but this package doesn't do it.
- ReleaseTerminal/RestoreTerminal (ex: spawning `vi` in the terminal temporarily; running/printing `git` commands).

## Supported Platforms

Only:
- Linux
- OSX (darwin)
- Windows

Any Unix-style OS (ex: freebsd, solaris, etc) will probably work, but is untested and not explicitly designed to work.

## Repainting

When View() returns a string, only lines that are changed are updated in the terminal. Certain events like resizes and resuming from suspension clear this line-level cache, forcing a complete redraw.

The terminal is rendered with a default framerate of 60 fps, configurable up to 120.

## Signals

Since we're in raw mode, keystrokes like Ctrl-C and Ctrl-Z are just sent as `KeyEvent`s and sent to Update. User programs can detect these keystrokes and call `t.Interrupt()` or `t.Suspend()`, for instance.

Signals are still handled (ex: if received via `kill` with or without various options). These call, e.g., `Quit` or `Interrupt`.

Windows doesn't support suspend/resume semantics. `Suspend` on windows is a no-op.

## Panic Recovery

If the program panics (including inside goroutines started by user code via *TUI.Go), tui guarantees that raw mode is exited and the terminal state is restored before the panic propagates. tui prints the panic information and stack trace to stderr before returning.

## Input/Output handling

`Options` allow swapping out stdin/stdout (mostly to make tests deterministic). When either Input or Output is nil, tui uses os.Stdin/os.Stdout.

Regardless of the configured reader/writer, tui requires an actual terminal for raw-mode rendering. If the provided streams are not ttys (ex: piped stdin/stdout), tui attempts to open the controlling terminal (ex: `/dev/tty` or `CONIN$`) for real input/output. If no terminal can be opened, RunTUI returns `ErrNoTTY`; tui does not attempt a degraded "plain print" mode.

## Shutdown / Cleanup

When `RunTUI` returns, the underlying `TUI` is inert and cannot be re-used. All public methods on it are safe to call and do nothing.

Before `RunTUI` returns, cleanup happens:
- Outstanding calls to `Init`, `Update`, and `View` are allowed to return.
- Timers are canceled and cleaned up.
- Goroutines spawned by `*TUI.Go` are allowed to complete (their Context is canceled right away).

## Messages and Events

Messages are sent to a Model's Update method. Some are events (ex: key events). Others are user defined data that can be sent with methods like `t.Send`.

```go
// Message is any event or user-defined message sent to a Model's Update method.
type Message any

// ControlKey is a control key character (ASCII 0-31, and 127), a CSI sequence representing keyboard input (ex: the up arrow key is "\x1b[A") or the value ControlKeyNone.
//
// When cast to an int:
//   - ControlKeyNone is -1.
//   - Control keys represented by ASCII characters are the ASCII value. Ex: Ctrl-J is 0x0A; Enter is 0x0D.
//   - The numeric value of input related to CSI sequences is undefined, except that it cannot occupy the ASCII control character space or -1.
type ControlKey int

// These ControlKey's exist ('Foo' listed below implies the actual constant is 'ControlKeyFoo') and map to ASCII characters (duplicate values are okay):
//   - Break, Enter, Backspace, Tab, Esc
//   - CtrlA to CtrlZ (26 controls)
//   - CtrlAt, CtrlOpenBracket, CtrlBackslash, CtrlCloseBracket, CtrlCaret, CtrlUnderscore, CtrlQuestionMark
//
// These ControlKey's exist and are CSI sequences (again, with 'ControlKey' prefix):
//   - Up, Down, Right, Left, ShiftTab, Home, End, PgUp, PgDown, CtrlPgUp, CtrlPgDown
//   - Delete, Insert, CtrlUp, CtrlDown, CtrlRight, CtrlLeft, CtrlHome, CtrlEnd
//   - ShiftUp, ShiftDown, ShiftRight, ShiftLeft, ShiftHome, ShiftEnd, CtrlShiftUp, CtrlShiftDown, CtrlShiftLeft, CtrlShiftRight, CtrlShiftHome, CtrlShiftEnd
//   - F1 to F20

// KeyEvent is sent when the user presses keys or pastes text. Each event will either be a ControlKey, or Runes.
type KeyEvent struct {
	// ControlKey is the control key pressed (or ControlKeyNone if the KeyEvent is not a control key event). NOTE: "control key" does NOT refer to the physical control
	// key on a keyboard, but rather, control characters in general (ex: ENTER, ESC, NEWLINE, Left Arrow, etc).
	ControlKey ControlKey

	// Runes pressed. Cannot contain control characters. Runes is usually a single rune, but may be mulitple (if multiple runes are available to read, or paste was used).
	Runes []rune

	// A control key or rune was pressed when Alt was held.
	Alt bool

	// Paste is true if the user pasted the runes (via bracketed paste mode). Paste implies ControlKey == ControlKeyNone and !Alt and len(Runes) > 0
	Paste bool
}

// Rune returns the first rune in ke.Runes, or 0 if there are none.
func (ke KeyEvent) Rune() rune

// Sent during startup and when the terminal window is resized.
type ResizeEvent struct {
	Width  int
	Height int
}

// SigResumeEvent will be sent when a program resumes from being suspended.
type SigResumeEvent struct{}

// SigTermEvent will be sent when a program receives SIGTERM or when the program itself requests termination via Quit. It can be canceled with Cancel. An uncanceled
// event causes RunTUI to return with a nil error.
type SigTermEvent struct {
	Cancel CancelFunc
}

// SigIntEvent will be sent when a program receives SIGINT or when the program itself requests termination via Interrupt. It can be canceled with Cancel. An uncanceled
// event causes RunTUI to return with an ErrInterrupted error.
type SigIntEvent struct {
	Cancel CancelFunc
}
```

Mouse events are supported when enabled via `Options.EnableMouse`. When enabled,
mouse input is delivered to `Update` as `MouseEvent` values (for example, wheel
events can be used for scrolling).

## Clipboard (OSC52)

tui provides a way for applications to copy text into the user's clipboard by emitting an OSC52 sequence to the terminal output. This is best-effort: some terminals disable or limit OSC52.

## Public API

This is the minimally required public interface. There may be other public types/methods as well.

```go
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

type TUI struct {
	// ...
}

type Options struct {
	// Input overrides os.Stdin when non-nil. Primarily used for testing. A non-tty input will still cause tui to open the controlling TTY for real input.
	Input io.Reader

	Output      io.Writer // Output overrides os.Stdout when non-nil. Primarily used for testing.
	Framerate   int       // If Framerate is between 60-120 inclusive, the terminal will be refreshed at this framerate (otherwise, it uses 60 FPS).
	EnableMouse bool      // EnableMouse enables mouse tracking and delivery of MouseEvent messages. Disabled by default.
}

// CancelFunc cancels signal events (ex: SigTermEvent) and periodic send. It can be called idempotently and is always safe to call, even after the TUI has finished
// running.
type CancelFunc func()

// RunTUI makes a new TUI and runs it. Alt/raw mode is entered (non-TTYs return ErrNoTTY). RunTUI doesn't return until the TUI stops (Quit or Interrupt is called
// without canceling their events).
func RunTUI(m Model, opts Options) error

// Calling Quit sends the SigTermEvent. Unless the event is canceled, it causes RunTUI to return with a nil error.
//   - This can be called inside a Model's Update method, or elsewhere.
//   - SIGTERM calls Quit().
func (t *TUI) Quit()

// Calling Interrupt sends the SigIntEvent. Unless the event is canceled, it causes RunTUI to return with an ErrInterrupted error.
//   - This can be called inside a Model's Update method, or elsewhere.
//   - To handle Ctrl-C, user programs need to detect Ctrl-C keystroke and call Interrupt.
//   - SIGINT calls Interrupt().
func (t *TUI) Interrupt()

// Calling Suspend causes the program to suspend.
//   - This can be called inside a Model's Update method, or elsewhere.
//   - To handle Ctrl-Z, user programs need to detect Ctrl-Z keystroke and call Suspend.
//   - Any outstanding Update/View call will finish before we actually suspend the program, and won't be called any more until the program is resumed.
//   - SIGTSTP calls Suspend().
//   - When the program is resumed, a SigResumeEvent will be sent to Update.
func (t *TUI) Suspend()

// Send enqueues m to be sent to the Model's Update function. Can be called from any goroutine.
func (t *TUI) Send(m Message)

// Periodically will cause m to be sent to the Model's Update function every d duration. The caller can stop this by calling the returned cancel function. The first
// time m is sent must be <= d from now, but is otherwise undefined. Good for consistent animations (ex: blinking cursor).
func (t *TUI) SendPeriodically(m Message, d time.Duration) CancelFunc

// SendOnceAfter will cause m to be sent to the Model's Update function after d time from now. It can be used to produce ad-hoc or variable-timing animations.
func (t *TUI) SendOnceAfter(m Message, d time.Duration)

// Go runs f in a new goroutine. ctx should be checked for cancellation. If f returns a non-nil value, it is enqueued for sending via Send.
//
// Go is a great place to do I/O like HTTP requests.
func (t *TUI) Go(f func(ctx context.Context) Message)

// SetClipboard requests that the terminal set the clipboard contents to text (copy). Implementations use OSC52 (ESC ] 52 ; c ; <base64(text)> BEL).
func (t *TUI) SetClipboard(text string)
```
