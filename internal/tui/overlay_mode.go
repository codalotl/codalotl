package tui

import (
	"strings"
	"time"

	"github.com/codalotl/codalotl/internal/agentformatter"
	"github.com/codalotl/codalotl/internal/q/termformat"
	qtui "github.com/codalotl/codalotl/internal/q/tui"
)

const (
	overlayCopyFeedbackDuration  = 900 * time.Millisecond
	overlayDoubleClickThreshold  = 350 * time.Millisecond
	overlayDoubleClickMaxDistXY  = 1
	overlayDetailsButtonLabel    = "details"
	overlayCopyButtonLabel       = "copy"
	overlayCopyButtonCopiedLabel = "copied!"
)

// renderedBlock is a formatted message-viewport block with overlay metadata.
type renderedBlock struct {
	text         string // text is the rendered block content.
	messageIndex int    // messageIndex is the index in model.messages, or -1 for synthetic blocks.
	copyable     bool   // copyable reports whether Overlay Mode should offer a copy action.
	detailable   bool   // detailable reports whether Overlay Mode should offer a details action.
}

// overlayTargetKind classifies a clickable Overlay Mode target.
type overlayTargetKind int

const (
	overlayTargetCopy overlayTargetKind = iota
	overlayTargetDetails
)

// overlayTarget describes a clickable Overlay Mode target in the rendered message viewport.
type overlayTarget struct {
	kind         overlayTargetKind // kind is the action performed when the target is clicked.
	contentLine  int               // contentLine is the target's absolute line in the viewport content.
	messageIndex int               // messageIndex is the index of the associated message in model.messages.
	xStart       int               // xStart is the inclusive left cell of the clickable range.
	xEnd         int               // xEnd is the inclusive right cell of the clickable range.
}

// overlayCopyExpiredMsg is scheduled after a copy action so the UI can clear the transient state.
type overlayCopyExpiredMsg struct{}

// nowOrTimeNow returns the injected clock time, or the current time when no clock is configured.
func (m *model) nowOrTimeNow() time.Time {
	if m != nil && m.now != nil {
		return m.now()
	}
	return time.Now()
}

// toggleOverlayMode enters or exits Overlay Mode and refreshes the viewport without changing the scroll position.
func (m *model) toggleOverlayMode() {
	m.overlayMode = !m.overlayMode
	// Clear click state so a rapid triple-click doesn't toggle twice.
	m.lastLeftClickAt = time.Time{}
	m.lastLeftClickX = 0
	m.lastLeftClickY = 0
	m.refreshViewport(false)
}

// isDoubleClick reports whether ev is close enough to the last left click to count as a double-click.
func (m *model) isDoubleClick(ev qtui.MouseEvent) bool {
	if m.lastLeftClickAt.IsZero() {
		return false
	}
	now := m.nowOrTimeNow()
	if now.Sub(m.lastLeftClickAt) > overlayDoubleClickThreshold {
		return false
	}
	if abs(ev.X-m.lastLeftClickX) > overlayDoubleClickMaxDistXY {
		return false
	}
	if abs(ev.Y-m.lastLeftClickY) > overlayDoubleClickMaxDistXY {
		return false
	}
	return true
}

// TryHandleOverlayClick handles a mouse click on an Overlay Mode target in the messages viewport. It returns true when the click is consumed by a recognized target.
func (m *model) tryHandleOverlayClick(ev qtui.MouseEvent) bool {
	if m == nil || m.viewport == nil || !m.overlayMode {
		return false
	}

	// Only support click targets in the messages viewport (left side, top area).
	if ev.X < 0 || ev.Y < 0 || ev.X >= m.viewportWidth || ev.Y >= m.viewportHeight {
		return false
	}

	contentLine := ev.Y + m.viewport.Offset()
	for _, t := range m.overlayTargets {
		if t.contentLine != contentLine {
			continue
		}
		if ev.X < t.xStart || ev.X > t.xEnd {
			continue
		}
		switch t.kind {
		case overlayTargetCopy:
			m.copyMessageToClipboard(t.messageIndex)
			return true
		case overlayTargetDetails:
			m.openDetailsDialog(t.messageIndex)
			return true
		default:
			return false
		}
	}
	return false
}

// isMessageCopyable reports whether msg should show a copy action in Overlay Mode. It returns false for nil messages and for the welcome banner.
func (m *model) isMessageCopyable(msg *chatMessage) bool {
	if msg == nil {
		return false
	}
	// The welcome/banner message is excluded (spec allows it to be included or excluded).
	return msg.kind != messageKindWelcome
}

// isMessageDetailable reports whether msg can open a Details dialog in Overlay Mode. Context-status messages are detailable, and agent messages are detailable when
// they are associated with a tool call.
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

// copyMessageToClipboard copies the displayed text for the message at messageIndex. It uses both the OS clipboard and OSC52 clipboard best-effort, and shows transient
// copy feedback when either clipboard mechanism succeeds.
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
	if m.overlayCopyFeedback == nil {
		m.overlayCopyFeedback = make(map[int]time.Time)
	}
	m.overlayCopyFeedback[messageIndex] = now.Add(overlayCopyFeedbackDuration)

	if m.tui != nil {
		m.tui.SendOnceAfter(overlayCopyExpiredMsg{}, overlayCopyFeedbackDuration)
	}
	m.refreshViewport(false)
}

// plainMessageTextForCopy returns the unstyled, wrapped text for the message at messageIndex. It formats the message at the current viewport width, strips ANSI
// styling, removes trailing spaces from each line, and removes trailing blank lines. It returns an empty string for nil models, invalid indexes, or messages that
// format to no visible text.
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

// clearExpiredOverlayCopyFeedback removes expired Overlay Mode copy feedback entries.
func (m *model) clearExpiredOverlayCopyFeedback() {
	if m == nil || len(m.overlayCopyFeedback) == 0 {
		return
	}
	now := m.nowOrTimeNow()
	for idx, until := range m.overlayCopyFeedback {
		if !until.After(now) {
			delete(m.overlayCopyFeedback, idx)
		}
	}
}

// joinRenderedBlocksWithOverlay joins rendered message blocks with separator rows and returns any Overlay Mode hit-test targets. When Overlay Mode is active, copyable
// message blocks receive right-aligned copy controls, and detailable blocks also receive details controls, if the controls fit within width.
func (m *model) joinRenderedBlocksWithOverlay(blocks []renderedBlock, width int) (string, []overlayTarget) {
	if len(blocks) == 0 {
		return "", nil
	}

	m.clearExpiredOverlayCopyFeedback()

	var (
		b       strings.Builder
		targets []overlayTarget
	)

	curLine := 0
	separatorForPrev := func(prev renderedBlock) string {
		if !m.overlayMode || !prev.copyable || prev.messageIndex < 0 {
			return m.blankRow(width, m.palette.primaryBackground)
		}

		label := overlayCopyButtonLabel
		if until, ok := m.overlayCopyFeedback[prev.messageIndex]; ok && until.After(m.nowOrTimeNow()) {
			label = overlayCopyButtonCopiedLabel
		}

		row, detailXStart, detailXEnd, copyXStart, copyXEnd, ok := m.overlayButtonsRow(width, label, prev.detailable)
		if !ok {
			return m.blankRow(width, m.palette.primaryBackground)
		}

		if prev.detailable {
			targets = append(targets, overlayTarget{
				kind:         overlayTargetDetails,
				contentLine:  curLine,
				messageIndex: prev.messageIndex,
				xStart:       detailXStart,
				xEnd:         detailXEnd,
			})
		}

		targets = append(targets, overlayTarget{
			kind:         overlayTargetCopy,
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

// overlayButtonsRow renders a right-aligned overlay button row and returns its hit-test ranges. The returned x ranges are zero-based and inclusive; ok is false
// when the buttons do not fit.
func (m *model) overlayButtonsRow(width int, copyLabel string, showDetails bool) (row string, detailsXStart int, detailsXEnd int, copyXStart int, copyXEnd int, ok bool) {
	detailsText := " " + overlayDetailsButtonLabel + " "
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
