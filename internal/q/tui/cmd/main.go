package main

import (
	"context"
	"fmt"

	"github.com/codalotl/codalotl/internal/q/tui"
)

// model stores the state rendered by the terminal UI.
type model struct {
	width, height int    // Width and height are the latest terminal dimensions in cells.
	runes         string // Runes is the key input or control-key description currently shown by View.
}

// Init intentionally leaves the model unchanged.
func (m *model) Init(t *tui.TUI) {

}

// Update applies TUI messages to the model state.
func (m *model) Update(t *tui.TUI, msg tui.Message) {

	switch evt := msg.(type) {
	case tui.ResizeEvent:
		m.width, m.height = evt.Width, evt.Height
	case tui.KeyEvent:
		switch evt.ControlKey {
		case tui.ControlKeyNone:
			if r := evt.Rune(); evt.Alt && r == 'p' {
				t.Go(func(ctx context.Context) tui.Message {
					panic("boom")
				})
			}
			m.runes = string(evt.Runes)
			if evt.Alt {
				m.runes += " (alt)"
			}
		case tui.ControlKeyCtrlC:
			m.runes = "ctrl-c"
			t.Quit()
		case tui.ControlKeyCtrlD:
			t.Suspend()
		default:
			m.runes = fmt.Sprintf("control: %02x", int(evt.ControlKey))
		}
	}
}

// View returns a single-line rendering of the current terminal size and displayed input.
func (m *model) View() string {
	return fmt.Sprintf("hello world. width=%d height=%d runes='%s'", m.width, m.height, m.runes)
}

func main() {
	m := &model{}
	if err := tui.RunTUI(m, tui.Options{}); err != nil {
		fmt.Printf("TUI exited with err: %v\n", err)
	}
	fmt.Printf("TUI exited with no error\n")

}
