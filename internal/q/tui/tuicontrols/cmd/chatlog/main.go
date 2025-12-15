package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/codalotl/codalotl/internal/q/termformat"
	"github.com/codalotl/codalotl/internal/q/tui"
	"github.com/codalotl/codalotl/internal/q/tui/tuicontrols"
)

type tickMsg struct{}

type model struct {
	view *tuicontrols.View

	bg termformat.Color

	rawLines []string
	tick     int

	cancelTick tui.CancelFunc
}

func newModel() *model {
	bg := termformat.ANSIWhite
	v := tuicontrols.NewView(40, 15)
	v.SetEmptyLineBackgroundColor(bg)
	return &model{
		view: v,
		bg:   bg,
	}
}

func (m *model) Init(t *tui.TUI) {
	m.cancelTick = t.SendPeriodically(tickMsg{}, 2*time.Second)
}

func (m *model) Update(t *tui.TUI, msg tui.Message) {
	switch v := msg.(type) {
	case tui.ResizeEvent:
		// m.view.SetSize(v.Width, v.Height)
		// m.rebuildContent()
		return
	case tui.KeyEvent:
		if v.ControlKey == tui.ControlKeyCtrlC {
			if m.cancelTick != nil {
				m.cancelTick()
			}
			t.Interrupt()
			return
		}
	case tickMsg:
		isAtBottom := m.view.AtBottom()
		m.tick++
		m.rawLines = append(m.rawLines, nextSampleLine(m.tick))
		m.rebuildContent()
		if isAtBottom {
			m.view.ScrollToBottom()
		}
		return
	}

	m.view.Update(t, msg)
}

func (m *model) View() string {
	return m.view.View()
}

func (m *model) rebuildContent() {
	width := m.view.Width()
	lines := make([]string, 0, len(m.rawLines))
	for _, raw := range m.rawLines {
		lines = append(lines, formatBGLine(raw, width, m.bg))
	}
	m.view.SetContent(strings.Join(lines, "\n"))
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

func nextSampleLine(i int) string {
	samples := []string{
		"short",
		"some medium text",
		"wide chars: 世a界",
		"greek: λλ λ",
		"accents: café",
		"numbers: 1234567890",
		"longer line that will likely be clipped by the current terminal width",
	}
	return fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), samples[i%len(samples)])
}

func main() {
	if err := tui.RunTUI(newModel(), tui.Options{}); err != nil && !errors.Is(err, tui.ErrInterrupted) {
		_, _ = fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
		os.Exit(1)
	}
}
