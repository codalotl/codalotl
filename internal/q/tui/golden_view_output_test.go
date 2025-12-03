package tui_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codalotl/codalotl/internal/q/tui"
	"github.com/stretchr/testify/require"
)

func TestViewOutputsSingleLine(t *testing.T) {
	input, output, buf := requireTestTTYWithCapture(t)

	const line = "integration view output"

	model := &flexibleModel{
		init: func(tuiApp *tui.TUI) {
			go func() {
				time.Sleep(20 * time.Millisecond)
				tuiApp.Quit()
			}()
		},
		view: func() string {
			return line
		},
	}

	err := tui.RunTUI(model, tui.Options{
		Input:  input,
		Output: output,
	})
	require.NoError(t, err)

	rendered := buf.String()
	require.Truef(t, strings.Contains(rendered, line), "terminal output did not include %q; got:\n%s", line, rendered)
}

func TestViewRequiresLineDiffing(t *testing.T) {
	input, output, buf := requireTestTTYWithCapture(t)

	const (
		stableLine = "line-diff-stable"
		oldLine    = "line-diff-old"
		newLine    = "line-diff-new"
	)

	type changeMessage struct{}
	type quitMessage struct{}

	var mu sync.RWMutex
	currentLine := oldLine

	model := &flexibleModel{
		init: func(tuiApp *tui.TUI) {
			tuiApp.SendOnceAfter(changeMessage{}, 30*time.Millisecond)
			tuiApp.SendOnceAfter(quitMessage{}, 120*time.Millisecond)
		},
		update: func(tuiApp *tui.TUI, msg tui.Message) {
			switch msg := msg.(type) {
			case changeMessage:
				mu.Lock()
				currentLine = newLine
				mu.Unlock()
			case quitMessage:
				tuiApp.Quit()
			case tui.SigTermEvent:
				// allow clean shutdown
			default:
				_ = msg
			}
		},
		view: func() string {
			mu.RLock()
			dynamic := currentLine
			mu.RUnlock()
			return stableLine + "\n" + dynamic
		},
	}

	err := tui.RunTUI(model, tui.Options{
		Input:  input,
		Output: output,
	})
	require.NoError(t, err)

	rendered := buf.String()
	require.Contains(t, rendered, oldLine, "expected initial render to include the old line")
	require.Contains(t, rendered, newLine, "expected updated render to include the new line")

	countStable := strings.Count(rendered, stableLine)
	require.Equalf(t, 1, countStable, "expected stable line to be drawn once with line diffing; got %d occurrences.\nFull output:\n%s", countStable, rendered)
}
