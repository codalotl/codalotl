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

// A model holds the state for the terminal message composer. Use newModel to create a model with initialized controls.
type model struct {
	view     *tuicontrols.View     // The view displays submitted lines and handles scrolling.
	ta       *tuicontrols.TextArea // The text area edits the message being composed.
	bg       termformat.Color      // The background color fills rendered view rows.
	rawLines []string              // Raw lines store submitted text before width-dependent formatting.
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

// Init performs startup initialization for the model. The model's controls are initialized before the TUI starts, so Init does not change the model.
func (m *model) Init(t *tui.TUI) {
}

// Update applies a TUI message to the model. It resizes child controls on ResizeEvent, requests interruption on Ctrl+C, submits on Enter, inserts a newline on Alt+Enter,
// and otherwise passes editing input to the text area. Page Up, Page Down, Home, and End scroll the submitted-text view without interfering with text editing.
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

// View renders the submitted-text view above the text area, omitting empty sections.
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

// The rebuildContent method regenerates the scrollable view content from rawLines. It formats each raw line for the current view width and background color before
// replacing the view content.
func (m *model) rebuildContent() {
	width := m.view.Width()
	lines := make([]string, 0, len(m.rawLines))
	for _, raw := range m.rawLines {
		lines = append(lines, formatBGLine(raw, width, m.bg))
	}
	m.view.SetContent(strings.Join(lines, "\n"))
}

// The applySize method resizes child controls to fit a terminal of the given cell dimensions. It clamps negative dimensions to zero, gives the text area up to four
// rows, assigns the remaining rows to the view, and rebuilds width-dependent content.
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

// The submitTextArea method submits the current text area contents as message lines. It returns without change if the text area is nil, clears whitespace-only input,
// rebuilds the rendered view after successful submission, and preserves bottom-following scroll behavior.
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

// The formatBGLine function formats s as a single terminal row with the given background color. It measures width in terminal cells while ignoring ANSI sequences,
// trims overflow from the right, and returns "" for non-positive widths. When bg produces a background ANSI sequence, it pads short rows to width cells and appends
// an ANSI reset; otherwise it returns the clipped text without padding. The input s must not contain newlines, and bg must be non-nil.
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
