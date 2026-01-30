package main

import (
	"context"
	"fmt"

	"github.com/codalotl/codalotl/internal/q/tui"
)

type model struct {
	width, height int

	runes string
}

func (m *model) Init(t *tui.TUI) {

}

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
