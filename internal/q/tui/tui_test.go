package tui

import (
	"context"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func runTUITest(t *testing.T, m Model, configure ...func(*Options)) error {
	t.Helper()

	done := make(chan error, 1)
	opts := Options{
		Output:            io.Discard,
		skipTTYValidation: true,
		terminalFactory: func(io.Reader, io.Writer) (terminalController, error) {
			return &noopTerminal{}, nil
		},
	}

	for _, cfg := range configure {
		cfg(&opts)
	}

	go func() {
		done <- RunTUI(m, opts)
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(200 * time.Millisecond):
		t.Fatal("RunTUI timed out")
		return nil
	}
}

type recordingWriter struct {
	mu     sync.Mutex
	writes []string
	times  []time.Time
}

func (w *recordingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes = append(w.writes, string(p))
	w.times = append(w.times, time.Now())
	return len(p), nil
}

func (w *recordingWriter) snapshot() ([]string, []time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	copiedWrites := append([]string(nil), w.writes...)
	copiedTimes := append([]time.Time(nil), w.times...)
	return copiedWrites, copiedTimes
}

type repaintModel struct {
	mu    sync.Mutex
	views []string
	idx   int
}

func newRepaintModel(views []string) *repaintModel {
	return &repaintModel{views: append([]string(nil), views...)}
}

func (m *repaintModel) Init(t *TUI) {
	t.Send("advance")
}

func (m *repaintModel) Update(t *TUI, msg Message) {
	switch v := msg.(type) {
	case string:
		switch v {
		case "advance":
			m.mu.Lock()
			length := len(m.views)
			if length == 0 {
				m.mu.Unlock()
				t.Send("quit")
				return
			}
			if m.idx < length-1 {
				m.idx++
				hasMore := m.idx < length-1
				m.mu.Unlock()
				if hasMore {
					t.Send("advance")
				} else {
					t.Send("quit")
				}
			} else {
				m.mu.Unlock()
				t.Send("quit")
			}
		case "quit":
			t.Quit()
		}
	case ResizeEvent:
		// ignore
	case SigTermEvent:
		// ignore
	}
}

func (m *repaintModel) View() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.views) == 0 {
		return ""
	}
	if m.idx >= len(m.views) {
		return m.views[len(m.views)-1]
	}
	return m.views[m.idx]
}

func runRenderSequence(t *testing.T, views []string, configure ...func(*Options)) ([]string, []time.Time) {
	t.Helper()

	writer := &recordingWriter{}
	model := newRepaintModel(views)

	configs := append([]func(*Options){
		func(opts *Options) {
			opts.Output = writer
			opts.sizeProvider = func() (int, int, error) { return 80, 24, nil }
		},
	}, configure...)

	err := runTUITest(t, model, configs...)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return writer.snapshot()
}

type resizeInvalidationModel struct{}

func (m *resizeInvalidationModel) Init(t *TUI) {
	t.Send("resize")
}

func (m *resizeInvalidationModel) Update(t *TUI, msg Message) {
	switch v := msg.(type) {
	case string:
		switch v {
		case "resize":
			t.handleResizeSignal()
			t.Send("quit")
		case "quit":
			t.Quit()
		}
	case ResizeEvent:
		// ignore
	case SigTermEvent:
		// ignore
	}
}

func (m *resizeInvalidationModel) View() string {
	return "resize\nmodel"
}

type quitModel struct {
	mu              sync.Mutex
	messages        []string
	viewCount       int
	cancelWasNil    bool
	sawSigTermEvent bool
}

func (m *quitModel) Init(t *TUI) {
	t.Send("hello")
}

func (m *quitModel) Update(t *TUI, msg Message) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch v := msg.(type) {
	case string:
		m.messages = append(m.messages, v)
		t.Quit()
	case SigTermEvent:
		m.sawSigTermEvent = true
		if v.Cancel == nil {
			m.cancelWasNil = true
		}
		m.messages = append(m.messages, "sigterm")
	}
}

func (m *quitModel) View() string {
	m.mu.Lock()
	m.viewCount++
	m.mu.Unlock()
	return "view"
}

func TestRunTUIQuitFlow(t *testing.T) {
	model := &quitModel{}

	err := runTUITest(t, model)
	if err != nil {
		t.Fatalf("RunTUI returned error: %v", err)
	}

	if !model.sawSigTermEvent {
		t.Fatal("expected SigTermEvent during quit flow")
	}
	if model.cancelWasNil {
		t.Fatal("expected SigTermEvent cancel function to be non-nil")
	}

	model.mu.Lock()
	defer model.mu.Unlock()

	if got, want := model.messages, []string{"hello", "sigterm"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("messages mismatch: got %v want %v", got, want)
	}
	if model.viewCount < 2 {
		t.Fatalf("expected at least two View calls, got %d", model.viewCount)
	}
}

type interruptModel struct {
	sawSigInt bool
}

func (m *interruptModel) Init(t *TUI) {
	t.Send("start")
}

func (m *interruptModel) Update(t *TUI, msg Message) {
	switch msg.(type) {
	case string:
		t.Interrupt()
	case SigIntEvent:
		m.sawSigInt = true
	}
}

func (m *interruptModel) View() string { return "" }

func TestRunTUIInterruptReturnsError(t *testing.T) {
	model := &interruptModel{}

	err := runTUITest(t, model)
	if err != ErrInterrupted {
		t.Fatalf("expected ErrInterrupted, got %v", err)
	}
	if !model.sawSigInt {
		t.Fatal("expected SigIntEvent to be delivered")
	}
}

type interruptCancelModel struct {
	canceled bool
}

func (m *interruptCancelModel) Init(t *TUI) {
	t.Send("start")
}

func (m *interruptCancelModel) Update(t *TUI, msg Message) {
	switch v := msg.(type) {
	case string:
		t.Interrupt()
	case SigIntEvent:
		v.Cancel()
		m.canceled = true
		t.Quit()
	}
}

func (m *interruptCancelModel) View() string { return "" }

func TestInterruptCanBeCanceled(t *testing.T) {
	model := &interruptCancelModel{}

	err := runTUITest(t, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !model.canceled {
		t.Fatal("expected SigIntEvent cancel to be called")
	}
}

type onceModel struct {
	mu     sync.Mutex
	events []string
}

func (m *onceModel) Init(t *TUI) {
	t.SendOnceAfter("later", 10*time.Millisecond)
}

func (m *onceModel) Update(t *TUI, msg Message) {
	if v, ok := msg.(string); ok {
		m.mu.Lock()
		m.events = append(m.events, v)
		m.mu.Unlock()
		t.Quit()
	}
}

func (m *onceModel) View() string { return "" }

func TestSendOnceAfterDeliversMessage(t *testing.T) {
	model := &onceModel{}

	err := runTUITest(t, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	model.mu.Lock()
	defer model.mu.Unlock()

	if got := model.events; !reflect.DeepEqual(got, []string{"later"}) {
		t.Fatalf("unexpected events: %v", got)
	}
}

type periodicModel struct {
	mu        sync.Mutex
	tickCount int
	cancel    CancelFunc
}

func (m *periodicModel) Init(t *TUI) {
	cancel := t.SendPeriodically("tick", 5*time.Millisecond)
	m.mu.Lock()
	m.cancel = cancel
	m.mu.Unlock()
}

func (m *periodicModel) Update(t *TUI, msg Message) {
	switch msg := msg.(type) {
	case string:
		if msg == "tick" {
			m.mu.Lock()
			m.tickCount++
			cancel := m.cancel
			m.mu.Unlock()
			if cancel != nil {
				cancel()
			}
			t.Quit()
		}
	case SigTermEvent:
		// no-op
	}
}

func (m *periodicModel) View() string { return "" }

func TestSendPeriodicallyDeliversTicks(t *testing.T) {
	model := &periodicModel{}

	err := runTUITest(t, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	model.mu.Lock()
	defer model.mu.Unlock()

	if model.tickCount == 0 {
		t.Fatal("expected at least one tick from SendPeriodically")
	}
}

type goContextModel struct {
	done chan struct{}
}

func (m *goContextModel) Init(t *TUI) {
	m.done = make(chan struct{})
	t.Go(func(ctx context.Context) Message {
		<-ctx.Done()
		close(m.done)
		return nil
	})
	t.Send("quit")
}

func (m *goContextModel) Update(t *TUI, msg Message) {
	if msg == "quit" {
		t.Quit()
	}
}

func (m *goContextModel) View() string { return "" }

func TestGoContextCanceledOnShutdown(t *testing.T) {
	model := &goContextModel{}

	err := runTUITest(t, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-model.done:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("expected Go context to be canceled")
	}
}

type inputCaptureModel struct {
	mu        sync.Mutex
	resizes   []ResizeEvent
	keyEvents []KeyEvent
	quitOnce  sync.Once
	expected  int
}

func (m *inputCaptureModel) Init(*TUI) {}

func (m *inputCaptureModel) Update(t *TUI, msg Message) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch ev := msg.(type) {
	case ResizeEvent:
		m.resizes = append(m.resizes, ev)
	case KeyEvent:
		m.keyEvents = append(m.keyEvents, ev)
		expected := m.expected
		if expected == 0 {
			expected = 6
		}
		if len(m.keyEvents) >= expected {
			m.quitOnce.Do(func() { t.Quit() })
		}
	case SigTermEvent:
		// no-op
	}
}

func (m *inputCaptureModel) View() string { return "" }

func TestInputKeyEventsAndPaste(t *testing.T) {
	wantKeys := []KeyEvent{
		{ControlKey: ControlKeyNone, Runes: []rune{'a'}},
		{ControlKey: ControlKeyEnter},
		{ControlKey: ControlKeyBackspace},
		{ControlKey: ControlKeyCtrlC},
		{ControlKey: ControlKeyNone, Runes: []rune{'b'}, Alt: true},
		{ControlKey: ControlKeyNone, Runes: []rune{'p', 'a', 's', 't', 'e', '\n', '\n', '!'}, Paste: true},
		{ControlKey: ControlKeyUp},
		{ControlKey: ControlKeyDown},
		{ControlKey: ControlKeyRight},
		{ControlKey: ControlKeyLeft},
		{ControlKey: ControlKeyPageUp},
		{ControlKey: ControlKeyPageDown},
		{ControlKey: ControlKeyInsert},
		{ControlKey: ControlKeyDelete},
		{ControlKey: ControlKeyHome},
		{ControlKey: ControlKeyEnd},
		{ControlKey: ControlKeyUp},
		{ControlKey: ControlKeyEnd},
	}

	model := &inputCaptureModel{expected: len(wantKeys)}
	reader, writer := io.Pipe()

	go func() {
		defer writer.Close()
		_, _ = writer.Write([]byte("a"))
		_, _ = writer.Write([]byte{'\r'})
		_, _ = writer.Write([]byte{0x7f})
		_, _ = writer.Write([]byte{0x03})
		_, _ = writer.Write([]byte{0x1b, 'b'})
		_, _ = writer.Write([]byte{0x1b, '[', '2', '0', '0', '~'})
		_, _ = writer.Write([]byte("paste\r\n!"))
		_, _ = writer.Write([]byte{0x1b, '[', '2', '0', '1', '~'})
		_, _ = writer.Write([]byte{0x1b, '[', 'A'})
		_, _ = writer.Write([]byte{0x1b, '[', 'B'})
		_, _ = writer.Write([]byte{0x1b, '[', 'C'})
		_, _ = writer.Write([]byte{0x1b, '[', 'D'})
		_, _ = writer.Write([]byte{0x1b, '[', '5', '~'})
		_, _ = writer.Write([]byte{0x1b, '[', '6', '~'})
		_, _ = writer.Write([]byte{0x1b, '[', '2', '~'})
		_, _ = writer.Write([]byte{0x1b, '[', '3', '~'})
		_, _ = writer.Write([]byte{0x1b, '[', 'H'})
		_, _ = writer.Write([]byte{0x1b, '[', 'F'})
		_, _ = writer.Write([]byte{0x1b, 'O', 'A'})
		_, _ = writer.Write([]byte{0x1b, 'O', 'F'})
	}()

	err := runTUITest(t, model, func(opts *Options) {
		opts.Input = reader
		opts.sizeProvider = func() (int, int, error) { return 80, 24, nil }
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	model.mu.Lock()
	defer model.mu.Unlock()

	if len(model.resizes) == 0 {
		t.Fatal("expected at least one resize event")
	}
	if model.resizes[0].Width != 80 || model.resizes[0].Height != 24 {
		t.Fatalf("unexpected resize event: %+v", model.resizes[0])
	}

	if len(model.keyEvents) != len(wantKeys) {
		t.Fatalf("unexpected key event count: got %d want %d", len(model.keyEvents), len(wantKeys))
	}
	for i := range wantKeys {
		if !keyEventsEqual(model.keyEvents[i], wantKeys[i]) {
			t.Fatalf("key event %d mismatch: got %+v want %+v", i, model.keyEvents[i], wantKeys[i])
		}
	}
}

func keyEventsEqual(a, b KeyEvent) bool {
	if a.ControlKey != b.ControlKey || a.Alt != b.Alt || a.Paste != b.Paste {
		return false
	}
	if len(a.Runes) != len(b.Runes) {
		return false
	}
	for i := range a.Runes {
		if a.Runes[i] != b.Runes[i] {
			return false
		}
	}
	return true
}

type recordingTerminal struct {
	mu         sync.Mutex
	enterCount int
	exitCount  int
}

func (rt *recordingTerminal) Enter() error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.enterCount++
	return nil
}

func (rt *recordingTerminal) Exit() error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.exitCount++
	return nil
}

func TestTerminalEnterExitLifecycle(t *testing.T) {
	term := &recordingTerminal{}
	model := &quitModel{}

	err := runTUITest(t, model, func(opts *Options) {
		opts.terminalFactory = func(io.Reader, io.Writer) (terminalController, error) {
			return term, nil
		}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	term.mu.Lock()
	defer term.mu.Unlock()
	if term.enterCount != 1 {
		t.Fatalf("expected terminal Enter once, got %d", term.enterCount)
	}
	if term.exitCount != 1 {
		t.Fatalf("expected terminal Exit once, got %d", term.exitCount)
	}
}

type panicModel struct{}

func (p *panicModel) Init(*TUI) {
	panic("boom")
}

func (p *panicModel) Update(*TUI, Message) {}

func (p *panicModel) View() string { return "" }

func TestTerminalExitOnPanic(t *testing.T) {
	term := &recordingTerminal{}
	opts := Options{
		Output:            io.Discard,
		skipTTYValidation: true,
		terminalFactory: func(io.Reader, io.Writer) (terminalController, error) {
			return term, nil
		},
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic to propagate")
		}
		term.mu.Lock()
		defer term.mu.Unlock()
		if term.enterCount != 1 {
			t.Fatalf("expected Enter once, got %d", term.enterCount)
		}
		if term.exitCount != 1 {
			t.Fatalf("expected Exit once, got %d", term.exitCount)
		}
	}()

	RunTUI(&panicModel{}, opts)
}

func TestRenderLineDiffing(t *testing.T) {
	writes, _ := runRenderSequence(t,
		[]string{
			"line 1\nline 2",
			"line 1\nline 2 updated",
		},
		func(opts *Options) {
			opts.Framerate = 120
		},
	)

	if len(writes) != 2 {
		t.Fatalf("expected 2 render writes, got %d", len(writes))
	}

	first, second := writes[0], writes[1]
	if !strings.Contains(first, clearScreen) {
		t.Fatalf("expected first write to contain clearScreen sequence, got %q", first)
	}
	if strings.Contains(second, "\x1b[1;1H") {
		t.Fatalf("expected second frame to skip line 1 update, got %q", second)
	}
	if !strings.Contains(second, "\x1b[2;1H") || !strings.Contains(second, clearLine) {
		t.Fatalf("expected second frame to position and clear line 2, got %q", second)
	}
	if !strings.Contains(second, "line 2 updated") {
		t.Fatalf("expected updated content in second frame, got %q", second)
	}
	if strings.Contains(second, "line 1") {
		t.Fatalf("unexpected line 1 contents in diffed frame: %q", second)
	}
}

func TestRenderClearsRemovedLines(t *testing.T) {
	writes, _ := runRenderSequence(t,
		[]string{
			"line 1\nline to clear",
			"line 1",
		},
		func(opts *Options) {
			opts.Framerate = 120
		},
	)

	if len(writes) != 2 {
		t.Fatalf("expected 2 render writes, got %d", len(writes))
	}

	second := writes[1]
	if !strings.Contains(second, "\x1b[2;1H"+clearLine) {
		t.Fatalf("expected second frame to clear removed line, got %q", second)
	}
	if strings.Contains(second, "line to clear") {
		t.Fatalf("expected removed content to be cleared, got %q", second)
	}
}

func TestRenderFullRedrawAfterResize(t *testing.T) {
	writer := &recordingWriter{}
	model := &resizeInvalidationModel{}

	var mu sync.Mutex
	widths := []int{80, 81}
	var idx int

	err := runTUITest(t, model, func(opts *Options) {
		opts.Output = writer
		opts.Framerate = 120
		opts.sizeProvider = func() (int, int, error) {
			mu.Lock()
			defer mu.Unlock()
			if idx >= len(widths) {
				return widths[len(widths)-1], 24, nil
			}
			w := widths[idx]
			idx++
			return w, 24, nil
		}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	writes, _ := writer.snapshot()
	if len(writes) != 2 {
		t.Fatalf("expected 2 render writes, got %d", len(writes))
	}
	second := writes[1]
	if !strings.Contains(second, clearScreen) {
		t.Fatalf("expected full redraw after resize, got %q", second)
	}
}

func TestRenderThrottlesFramerate(t *testing.T) {
	writes, times := runRenderSequence(t,
		[]string{
			"frame one",
			"frame two",
		},
		func(opts *Options) {
			opts.Framerate = 120
		},
	)

	if len(writes) != 2 || len(times) != 2 {
		t.Fatalf("expected 2 render writes and timestamps, got %d writes and %d times", len(writes), len(times))
	}

	frameInterval := times[1].Sub(times[0])
	minFrame := time.Second / 120
	if frameInterval < minFrame-time.Millisecond {
		t.Fatalf("expected frame interval >= %v, got %v", minFrame, frameInterval)
	}
}
