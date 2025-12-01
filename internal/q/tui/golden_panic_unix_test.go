//go:build !windows

package tui_test

import (
	"context"
	"testing"
	"time"

	"github.com/codalotl/codalotl/internal/q/tui"
	"github.com/stretchr/testify/require"
	"golang.org/x/term"
)

func TestUpdatePanicRestoresTerminalState(t *testing.T) {
	const panicMessage = "integration test panic sentinel"
	runPanicScenario(t, panicMessage, func(m *flexibleModel) {
		m.update = func(t *tui.TUI, msg tui.Message) {
			panic(panicMessage)
		}
		m.view = func() string {
			return ""
		}
	})
}

func TestViewPanicRestoresTerminalState(t *testing.T) {
	const panicMessage = "integration test view panic sentinel"
	runPanicScenario(t, panicMessage, func(m *flexibleModel) {
		m.view = func() string {
			panic(panicMessage)
		}
	})
}
func TestGoPanicIsRecoveredByRuntime(t *testing.T) {
	const panicMessage = "integration test go panic sentinel"

	runPanicScenario(t, panicMessage, func(m *flexibleModel) {
		m.init = func(tuiApp *tui.TUI) {
			tuiApp.Go(func(context.Context) tui.Message {
				panic(panicMessage)
			})
		}
	})
}

func runPanicScenario(t *testing.T, panicMessage string, configure func(*flexibleModel)) {
	input, output := requireTestTTY(t)
	initialStatePtr, err := term.GetState(int(input.Fd()))
	require.NoError(t, err)
	initialState := *initialStatePtr

	model := &flexibleModel{}
	if configure != nil {
		configure(model)
	}

	var failSafe *time.Timer
	origInit := model.init
	model.init = func(tuiApp *tui.TUI) {
		failSafe = time.AfterFunc(500*time.Millisecond, func() {
			tuiApp.Quit()
		})
		if origInit != nil {
			origInit(tuiApp)
		}
	}

	panicValue := func() (recovered any) {
		defer func() {
			if failSafe != nil {
				failSafe.Stop()
			}
			recovered = recover()
		}()
		_ = tui.RunTUI(model, tui.Options{
			Input:  input,
			Output: output,
		})
		return nil
	}()
	require.NotNil(t, panicValue)
	require.Equal(t, panicMessage, panicValue)

	finalStatePtr, err := term.GetState(int(input.Fd()))
	require.NoError(t, err)
	finalState := *finalStatePtr
	require.Equal(t, initialState, finalState)
}
