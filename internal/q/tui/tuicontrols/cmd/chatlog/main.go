package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/codalotl/codalotl/internal/q/termformat"
	"github.com/codalotl/codalotl/internal/q/tui"
	"github.com/codalotl/codalotl/internal/q/tui/tuicontrols"
)

type model struct {
	view *tuicontrols.View
	ta   *tuicontrols.TextArea

	bg termformat.Color

	rawLines []string
}

func newModel() *model {
	bg := termformat.ANSIWhite
	v := tuicontrols.NewView(40, 15)
	v.SetEmptyLineBackgroundColor(bg)

	ta := tuicontrols.NewTextArea(40, 4)
	ta.Prompt = "› "
	ta.Placeholder = "Type a message… (Enter to send, Alt+Enter for newline)"
	ta.BackgroundColor = bg
	ta.ForegroundColor = termformat.ANSIBlack
	ta.PlaceholderColor = termformat.ANSIBrightBlack
	ta.CaretColor = termformat.ANSIBrightBlue

	return &model{
		view: v,
		ta:   ta,
		bg:   bg,
	}
}

func (m *model) Init(t *tui.TUI) {
}

func (m *model) Update(t *tui.TUI, msg tui.Message) {
	switch v := msg.(type) {
	case tui.ResizeEvent:
		m.applySize(v.Width, v.Height)
		return
	case tui.KeyEvent:
		if v.ControlKey == tui.ControlKeyCtrlC {
			t.Interrupt()
			return
		}
		if v.ControlKey == tui.ControlKeyEnter {
			if v.Alt {
				m.ta.InsertString("\n")
				return
			}
			m.submitTextArea()
			return
		}
	}

	m.ta.Update(t, msg)

	// Allow scrolling the view without interfering with text editing.
	if key, ok := msg.(tui.KeyEvent); ok {
		switch key.ControlKey {
		case tui.ControlKeyPageUp, tui.ControlKeyPageDown, tui.ControlKeyHome, tui.ControlKeyEnd:
			m.view.Update(t, msg)
		}
	}
}

func (m *model) View() string {
	top := m.view.View()
	bottom := m.ta.View()
	if top == "" {
		return bottom
	}
	if bottom == "" {
		return top
	}
	return top + "\n" + bottom
}

func (m *model) rebuildContent() {
	width := m.view.Width()
	lines := make([]string, 0, len(m.rawLines))
	for _, raw := range m.rawLines {
		lines = append(lines, formatBGLine(raw, width, m.bg))
	}
	m.view.SetContent(strings.Join(lines, "\n"))
}

func (m *model) applySize(width, height int) {
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}

	taHeight := 4
	if height < taHeight {
		taHeight = height
	}
	viewHeight := height - taHeight
	if viewHeight < 0 {
		viewHeight = 0
	}

	m.view.SetSize(width, viewHeight)
	m.ta.SetSize(width, taHeight)
	m.rebuildContent()
}

func (m *model) submitTextArea() {
	if m.ta == nil {
		return
	}
	text := m.ta.Contents()
	if strings.TrimSpace(text) == "" {
		m.ta.SetContents("")
		return
	}

	isAtBottom := m.view.AtBottom()

	for _, line := range strings.Split(text, "\n") {
		m.rawLines = append(m.rawLines, line)
	}
	m.ta.SetContents("")

	m.rebuildContent()
	if isAtBottom {
		m.view.ScrollToBottom()
	}
}

func formatBGLine(s string, width int, bg termformat.Color) string {
	if width <= 0 {
		return ""
	}

	text := s
	textWidth := termformat.TextWidthWithANSICodes(text)
	if textWidth > width {
		text = termformat.Cut(text, 0, textWidth-width)
		textWidth = termformat.TextWidthWithANSICodes(text)
	}

	pad := width - textWidth
	if pad < 0 {
		pad = 0
	}

	bgSeq := bg.ANSISequence(true)
	if bgSeq == "" {
		return text
	}
	return bgSeq + text + strings.Repeat(" ", pad) + termformat.ANSIReset
}

func main() {
	if err := tui.RunTUI(newModel(), tui.Options{}); err != nil && !errors.Is(err, tui.ErrInterrupted) {
		_, _ = fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
		os.Exit(1)
	}
}
