package tui

import (
	"strings"
	"time"

	"github.com/codalotl/codalotl/internal/agentformatter"
	"github.com/codalotl/codalotl/internal/q/termformat"
	qtui "github.com/codalotl/codalotl/internal/q/tui"
)

const (
	optionCopyFeedbackDuration  = 900 * time.Millisecond
	optionDoubleClickThreshold  = 350 * time.Millisecond
	optionDoubleClickMaxDistXY  = 1
	optionDetailsButtonLabel    = "details"
	optionCopyButtonLabel       = "copy"
	optionCopyButtonCopiedLabel = "copied!"
)

type renderedBlock struct {
	text         string
	messageIndex int
	copyable     bool
	detailable   bool
}

type optionTargetKind int

const (
	optionTargetCopy optionTargetKind = iota
	optionTargetDetails
)

type optionTarget struct {
	kind         optionTargetKind
	contentLine  int
	messageIndex int
	xStart       int
	xEnd         int
}

// optionCopyExpiredMsg is scheduled after a copy action so the UI can clear the transient state.
type optionCopyExpiredMsg struct{}

func (m *model) nowOrTimeNow() time.Time {
	if m != nil && m.now != nil {
		return m.now()
	}
	return time.Now()
}

func (m *model) toggleOptionMode() {
	m.optionMode = !m.optionMode
	// Clear click state so a rapid triple-click doesn't toggle twice.
	m.lastLeftClickAt = time.Time{}
	m.lastLeftClickX = 0
	m.lastLeftClickY = 0
	m.refreshViewport(false)
}

func (m *model) isDoubleClick(ev qtui.MouseEvent) bool {
	if m.lastLeftClickAt.IsZero() {
		return false
	}
	now := m.nowOrTimeNow()
	if now.Sub(m.lastLeftClickAt) > optionDoubleClickThreshold {
		return false
	}
	if abs(ev.X-m.lastLeftClickX) > optionDoubleClickMaxDistXY {
		return false
	}
	if abs(ev.Y-m.lastLeftClickY) > optionDoubleClickMaxDistXY {
		return false
	}
	return true
}

func (m *model) tryHandleOptionClick(ev qtui.MouseEvent) bool {
	if m == nil || m.viewport == nil || !m.optionMode {
		return false
	}

	// Only support click targets in the messages viewport (left side, top area).
	if ev.X < 0 || ev.Y < 0 || ev.X >= m.viewportWidth || ev.Y >= m.viewportHeight {
		return false
	}

	contentLine := ev.Y + m.viewport.Offset()
	for _, t := range m.optionTargets {
		if t.contentLine != contentLine {
			continue
		}
		if ev.X < t.xStart || ev.X > t.xEnd {
			continue
		}
		switch t.kind {
		case optionTargetCopy:
			m.copyMessageToClipboard(t.messageIndex)
			return true
		case optionTargetDetails:
			m.openDetailsDialog(t.messageIndex)
			return true
		default:
			return false
		}
	}
	return false
}

func (m *model) isMessageCopyable(msg *chatMessage) bool {
	if msg == nil {
		return false
	}
	// The welcome/banner message is excluded (spec allows it to be included or excluded).
	return msg.kind != messageKindWelcome
}

func (m *model) isMessageDetailable(msg *chatMessage) bool {
	if msg == nil {
		return false
	}
	switch msg.kind {
	case messageKindContextStatus:
		return true
	case messageKindAgent:
		return msg.toolCallID != ""
	default:
		return false
	}
}

func (m *model) copyMessageToClipboard(messageIndex int) {
	if m == nil {
		return
	}
	text := m.plainMessageTextForCopy(messageIndex)
	if text == "" {
		return
	}

	didCopy := false

	// Prefer trying to write to the OS clipboard as well (best-effort), since OSC52
	// is frequently filtered or disabled depending on terminal/mux/remote setup.
	if m.osClipboardAvailable != nil && m.osClipboardWrite != nil && m.osClipboardAvailable() {
		if err := m.osClipboardWrite(text); err != nil {
			debugLogf("clipboard write error: %v", err)
		} else {
			didCopy = true
		}
	}

	// OSC52 clipboard (best-effort). Keep this even if OS clipboard succeeds, since
	// either mechanism may be disabled depending on environment.
	if m.clipboardSetter != nil {
		m.clipboardSetter(text)
		didCopy = true
	} else if m.tui != nil {
		// Best effort; allows copy to work even if clipboardSetter wasn't injected.
		m.tui.SetClipboard(text)
		didCopy = true
	}

	if !didCopy {
		return
	}

	now := m.nowOrTimeNow()
	if m.optionCopyFeedback == nil {
		m.optionCopyFeedback = make(map[int]time.Time)
	}
	m.optionCopyFeedback[messageIndex] = now.Add(optionCopyFeedbackDuration)

	if m.tui != nil {
		m.tui.SendOnceAfter(optionCopyExpiredMsg{}, optionCopyFeedbackDuration)
	}
	m.refreshViewport(false)
}

func (m *model) plainMessageTextForCopy(messageIndex int) string {
	if m == nil || messageIndex < 0 || messageIndex >= len(m.messages) {
		return ""
	}

	width := agentformatter.MinTerminalWidth
	if m.viewport != nil && m.viewport.Width() > 0 {
		width = m.viewport.Width()
	}

	msg := &m.messages[messageIndex]
	m.ensureMessageFormatted(msg, width)
	plain := stripAnsi(msg.formatted)

	lines := strings.Split(plain, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

func (m *model) clearExpiredOptionCopyFeedback() {
	if m == nil || len(m.optionCopyFeedback) == 0 {
		return
	}
	now := m.nowOrTimeNow()
	for idx, until := range m.optionCopyFeedback {
		if !until.After(now) {
			delete(m.optionCopyFeedback, idx)
		}
	}
}

func (m *model) joinRenderedBlocksWithOptions(blocks []renderedBlock, width int) (string, []optionTarget) {
	if len(blocks) == 0 {
		return "", nil
	}

	m.clearExpiredOptionCopyFeedback()

	var (
		b       strings.Builder
		targets []optionTarget
	)

	curLine := 0
	separatorForPrev := func(prev renderedBlock) string {
		if !m.optionMode || !prev.copyable || prev.messageIndex < 0 {
			return m.blankRow(width, m.palette.primaryBackground)
		}

		label := optionCopyButtonLabel
		if until, ok := m.optionCopyFeedback[prev.messageIndex]; ok && until.After(m.nowOrTimeNow()) {
			label = optionCopyButtonCopiedLabel
		}

		row, detailXStart, detailXEnd, copyXStart, copyXEnd, ok := m.optionButtonsRow(width, label, prev.detailable)
		if !ok {
			return m.blankRow(width, m.palette.primaryBackground)
		}

		if prev.detailable {
			targets = append(targets, optionTarget{
				kind:         optionTargetDetails,
				contentLine:  curLine,
				messageIndex: prev.messageIndex,
				xStart:       detailXStart,
				xEnd:         detailXEnd,
			})
		}

		targets = append(targets, optionTarget{
			kind:         optionTargetCopy,
			contentLine:  curLine,
			messageIndex: prev.messageIndex,
			xStart:       copyXStart,
			xEnd:         copyXEnd,
		})
		return row
	}

	normalizeBlock := func(s string) string {
		return strings.TrimSuffix(s, "\n")
	}

	prev := renderedBlock{messageIndex: -1}
	for i, blk := range blocks {
		blk.text = normalizeBlock(blk.text)
		if i > 0 {
			// newline to start separator line
			b.WriteByte('\n')
			curLine++

			sep := separatorForPrev(prev)
			b.WriteString(sep)

			// newline to start next block line
			b.WriteByte('\n')
			curLine++
		}

		if blk.text != "" {
			b.WriteString(blk.text)
			curLine += termformat.BlockHeight(blk.text) - 1
		} else {
			// empty block still occupies a line
			curLine += 0
		}

		prev = blk
	}

	return b.String(), targets
}

func (m *model) optionButtonsRow(width int, copyLabel string, showDetails bool) (row string, detailsXStart int, detailsXEnd int, copyXStart int, copyXEnd int, ok bool) {
	detailsText := " " + optionDetailsButtonLabel + " "
	copyText := " " + copyLabel + " "

	buttons := copyText
	sep := " "
	if showDetails {
		buttons = detailsText + sep + copyText
	}

	if width <= 0 {
		width = 1
	}
	if len(buttons) > width {
		return "", 0, 0, 0, 0, false
	}

	xStart := width - len(buttons)

	left := termformat.Style{Background: m.palette.primaryBackground}.Wrap(strings.Repeat(" ", xStart))
	btnStyle := termformat.Style{Foreground: m.palette.colorfulForeground, Background: m.palette.accentBackground}

	var b strings.Builder
	b.WriteString(left)

	if showDetails {
		detailsXStart = xStart
		detailsXEnd = detailsXStart + len(detailsText) - 1
		b.WriteString(btnStyle.Wrap(detailsText))
		b.WriteString(termformat.Style{Background: m.palette.primaryBackground}.Wrap(sep))
		copyXStart = detailsXEnd + len(sep) + 1
	} else {
		detailsXStart = 0
		detailsXEnd = -1
		copyXStart = xStart
	}

	copyXEnd = copyXStart + len(copyText) - 1
	b.WriteString(btnStyle.Wrap(copyText))

	rightCount := width - xStart - len(buttons)
	right := ""
	if rightCount > 0 {
		right = termformat.Style{Background: m.palette.primaryBackground}.Wrap(strings.Repeat(" ", rightCount))
	}
	b.WriteString(right)
	return b.String(), detailsXStart, detailsXEnd, copyXStart, copyXEnd, true
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
