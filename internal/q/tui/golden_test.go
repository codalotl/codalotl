package tui_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codalotl/codalotl/internal/q/tui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This is a test-only package, that tests internal/q/tui.

type basicModel struct {
	initTUI   *tui.TUI
	updateTUI *tui.TUI

	resizeReceived bool
	viewCalled     bool
	gotSigTerm     bool

	testErrs []error
}

func (m *basicModel) Init(t *tui.TUI) {
	if m.initTUI != nil {
		m.testErrs = append(m.testErrs, errors.New("initTUI already set"))
	} else {
		m.initTUI = t
	}

	// Send quit after 20 milliseconds:
	go func() {
		time.Sleep(20 * time.Millisecond)
		t.Quit()
	}()
}

func (m *basicModel) Update(t *tui.TUI, msg tui.Message) {
	if m.updateTUI == nil {
		m.updateTUI = t
	} else if m.updateTUI != t {
		m.testErrs = append(m.testErrs, errors.New("updateTUI is different"))
	}

	switch evt := msg.(type) {
	case tui.ResizeEvent:
		m.resizeReceived = true
		if evt.Width <= 0 {
			m.testErrs = append(m.testErrs, errors.New("bad width"))
		}
		if evt.Height <= 0 {
			m.testErrs = append(m.testErrs, errors.New("bad height"))
		}
	case tui.SigTermEvent:
		m.gotSigTerm = true
	}
}

func (m *basicModel) View() string {
	m.viewCalled = true
	return "hi"
}

// TestTUIBasic tests:
//   - Init, Update, and View are called. The t *tui.TUI argument is correct.
//   - Update is called with the screen size (which is non-zero)
//   - Quit works, and causes a SigTermEvent. Don't cancel -> nil error.
func TestTUIBasic(t *testing.T) {

	input, output := requireTestTTY(t)

	m := &basicModel{}
	err := tui.RunTUI(m, tui.Options{
		Input:  input,
		Output: output,
	})

	require.NoError(t, err)
	assert.Len(t, m.testErrs, 0)
	assert.NotNil(t, m.initTUI)
	assert.NotNil(t, m.updateTUI)
	assert.True(t, m.initTUI == m.updateTUI)
	assert.True(t, m.resizeReceived)
	assert.True(t, m.viewCalled)
	assert.True(t, m.gotSigTerm)
}

// flexibleModel allows individual test cases to change and dynamically define the model on an ad-hoc basis, without requiring a new top-level model for each test
// case.
type flexibleModel struct {
	init   func(*tui.TUI)
	update func(*tui.TUI, tui.Message)
	view   func() string
}

func (m *flexibleModel) Init(t *tui.TUI) {
	if m.init != nil {
		m.init(t)
	}
}

func (m *flexibleModel) Update(t *tui.TUI, msg tui.Message) {
	if m.update != nil {
		m.update(t, msg)
	}
}

func (m *flexibleModel) View() string {
	if m.view != nil {
		return m.view()
	}
	return ""
}

func TestQuitWithCancel(t *testing.T) {
	input, output := requireTestTTY(t)

	quitCount := 0
	m := &flexibleModel{
		init: func(t *tui.TUI) {
			go func() {
				time.Sleep(20 * time.Millisecond)
				t.Quit()
				time.Sleep(20 * time.Millisecond)
				t.Quit()
			}()
		},
		update: func(t *tui.TUI, msg tui.Message) {
			switch evt := msg.(type) {
			case tui.SigTermEvent:
				if quitCount == 0 {
					evt.Cancel()
				}
				quitCount++
			}
		},
	}

	err := tui.RunTUI(m, tui.Options{
		Input:  input,
		Output: output,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 2, quitCount)
}

func TestTerminateWithCancel(t *testing.T) {
	input, output := requireTestTTY(t)

	intCount := 0
	m := &flexibleModel{
		init: func(t *tui.TUI) {
			go func() {
				time.Sleep(20 * time.Millisecond)
				t.Interrupt()
				time.Sleep(20 * time.Millisecond)
				t.Interrupt()
			}()
		},
		update: func(t *tui.TUI, msg tui.Message) {
			switch evt := msg.(type) {
			case tui.SigIntEvent:
				if intCount == 0 {
					evt.Cancel()
				}
				intCount++
			}
		},
	}

	err := tui.RunTUI(m, tui.Options{
		Input:  input,
		Output: output,
	})
	assert.Error(t, err)
	assert.Equal(t, tui.ErrInterrupted, err)
	assert.EqualValues(t, 2, intCount)
}

func TestSend(t *testing.T) {
	input, output := requireTestTTY(t)

	type customMessage struct{ v int }

	count := 0

	m := &flexibleModel{
		init: func(t *tui.TUI) {
			t.Send(customMessage{v: 1})
			t.Send(customMessage{v: 2})
		},
		update: func(tuiApp *tui.TUI, msg tui.Message) {
			switch evt := msg.(type) {
			case customMessage:
				if count == 0 {
					assert.Equal(t, 1, evt.v)
				}
				if count == 1 {
					assert.Equal(t, 2, evt.v)
				}
				if evt.v == 2 {
					tuiApp.Quit()
				}
				count++
			}
		},
	}

	err := tui.RunTUI(m, tui.Options{
		Input:  input,
		Output: output,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 2, count)
}

func TestSendOnceAfter(t *testing.T) {
	input, output := requireTestTTY(t)

	type customMessage struct{ v int }

	count := 0

	m := &flexibleModel{
		init: func(t *tui.TUI) {
			t.SendOnceAfter(customMessage{v: 1}, time.Millisecond*100) // The first one we send should come in second
			t.SendOnceAfter(customMessage{v: 2}, time.Millisecond*10)
		},
		update: func(tuiApp *tui.TUI, msg tui.Message) {
			switch evt := msg.(type) {
			case customMessage:
				if count == 0 {
					assert.Equal(t, 2, evt.v)
				}
				if count == 1 {
					assert.Equal(t, 1, evt.v)
				}
				if evt.v == 1 {
					tuiApp.Quit()
				}
				count++
			}
		},
	}

	err := tui.RunTUI(m, tui.Options{
		Input:  input,
		Output: output,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 2, count)
}

func TestSendPeriodically(t *testing.T) {
	input, output := requireTestTTY(t)

	type customMessage struct{}
	type customMessage2 struct{}

	count := 0
	count2 := 0
	count2AtCancel := 0
	var cancelFn tui.CancelFunc

	now := time.Now()
	m := &flexibleModel{
		init: func(t *tui.TUI) {
			t.SendPeriodically(customMessage{}, time.Millisecond*5)
			cancelFn = t.SendPeriodically(customMessage2{}, time.Millisecond*10)
		},
		update: func(tuiApp *tui.TUI, msg tui.Message) {
			switch msg.(type) {
			case customMessage:
				if count > 3 {
					cancelFn()
					count2AtCancel = count2
				}
				if count >= 10 {
					tuiApp.Quit()
				}
				count++
			case customMessage2:
				count2++
			}
		},
	}

	err := tui.RunTUI(m, tui.Options{
		Input:  input,
		Output: output,
	})
	elapsed := time.Since(now)
	require.NoError(t, err)
	assert.EqualValues(t, 11, count)
	assert.True(t, elapsed < 200*time.Millisecond) // When I tested this, there was 50ms overhead when quitting on the first message (101ms actual elapsed).
	assert.Equal(t, count2AtCancel, count2)
	assert.True(t, count2 <= 3) // It should be 2, but allowing 3 for some timing slop.
	cancelFn()                  // call again just to make sure we don't panic for some reason
}

func TestGo(t *testing.T) {
	input, output := requireTestTTY(t)

	type customMessage struct{ v int }

	isDone := false
	deliveredV1 := false
	ranGoroutine1 := false
	ranGoroutine2 := false
	ranGoroutine3 := false

	m := &flexibleModel{
		init: func(t *tui.TUI) {
			t.Go(func(ctx context.Context) tui.Message {
				ranGoroutine1 = true
				return customMessage{v: 1}
			})
			t.Go(func(ctx context.Context) tui.Message {
				ranGoroutine2 = true
				return nil // ensure nil is not delivered
			})
			t.Go(func(ctx context.Context) tui.Message {
				ranGoroutine3 = true
				for {
					err := ctx.Err()
					if err != nil {
						isDone = true
						return customMessage{v: 2}
					}
					time.Sleep(10 * time.Millisecond)
				}
			})
			t.SendOnceAfter(customMessage{v: 3}, time.Millisecond*20)
		},
		update: func(tuiApp *tui.TUI, msg tui.Message) {
			if msg == nil {
				assert.Fail(t, "message is nil")
			}
			switch msg := msg.(type) {
			case customMessage:
				if msg.v == 1 {
					deliveredV1 = true
				}
				if msg.v == 2 {
					assert.Fail(t, "msg.v was 2 (this event shouldn't have been delivered)")
				}
				if msg.v == 3 {
					tuiApp.Quit()
				}
			}
		},
	}

	err := tui.RunTUI(m, tui.Options{
		Input:  input,
		Output: output,
	})
	require.NoError(t, err)
	assert.True(t, ranGoroutine1)
	assert.True(t, ranGoroutine2)
	assert.True(t, ranGoroutine3)
	assert.True(t, deliveredV1)
	assert.True(t, isDone)
}

// View refresh rate and diffing

// Maybe: quit while quitting, or terminate while quitting
