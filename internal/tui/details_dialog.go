package tui

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/agentformatter"
	"github.com/codalotl/codalotl/internal/q/termformat"
	"github.com/codalotl/codalotl/internal/q/tui/tuicontrols"
)

const (
	detailsDialogMargin = 3
	detailsMaxBytes     = 1 << 20  // 1 MiB
	detailsMaxHexBytes  = 64 << 10 // 64 KiB
)

// detailsDialog contains the content and viewport state for the modal Details overlay.
type detailsDialog struct {
	messageIndex    int               // MessageIndex is the source message index in model.messages.
	title           string            // Title is the dialog heading shown above the scrollable details.
	body            string            // Body is the raw detail content displayed in the scrollable view.
	view            *tuicontrols.View // View scrolls the styled body content within the dialog.
	lastInnerWidth  int               // Cached sizing/layout info so we can keep the scroll viewport stable across renders.
	lastInnerHeight int               // LastInnerHeight is the cached inner dialog height used for layout reuse.
	titleLines      []string          // TitleLines is the sanitized, wrapped title cached for the current inner width.
}

// openDetailsDialog opens the Details overlay for the message at messageIndex. It does nothing when messageIndex is invalid or the message has no details payload.
func (m *model) openDetailsDialog(messageIndex int) {
	if m == nil || messageIndex < 0 || messageIndex >= len(m.messages) {
		return
	}
	msg := &m.messages[messageIndex]
	if !m.isMessageDetailable(msg) {
		return
	}

	title := m.detailsTitleForMessage(messageIndex)
	body := m.detailsBodyForMessage(messageIndex)

	dlg := &detailsDialog{
		messageIndex: messageIndex,
		title:        title,
		body:         body,
		view:         tuicontrols.NewView(0, 0),
	}
	dlg.view.SetEmptyLineBackgroundColor(m.palette.accentBackground)

	// The view content itself is unbordered; it sits inside the dialog BlockStyle.
	//
	// Important: a scrollable View may render starting from any line offset. If we only
	// apply the foreground color once at the start of the whole string, scrolling to an
	// offset that doesn't include that opening ANSI sequence would render in the terminal's
	// default foreground. So we apply the style per-line.
	styled := styleEachLine(body, termformat.Style{Foreground: m.palette.primaryForeground})
	dlg.view.SetContent(styled)

	m.detailsDialog = dlg
	m.detailsDialogEnsureSized()
}

func styleEachLine(s string, style termformat.Style) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i := range lines {
		// Wrap("") returns "", which is fine for blank lines.
		lines[i] = style.Wrap(lines[i])
	}
	return strings.Join(lines, "\n")
}

// closeDetailsDialog closes the active Details dialog.
func (m *model) closeDetailsDialog() {
	m.detailsDialog = nil
}

// detailsDialogScrollUp scrolls the open details dialog up by n lines.
func (m *model) detailsDialogScrollUp(n int) {
	if m == nil || m.detailsDialog == nil || m.detailsDialog.view == nil {
		return
	}
	m.detailsDialog.view.ScrollUp(n)
}

// detailsDialogScrollDown scrolls the open details dialog down by n lines.
func (m *model) detailsDialogScrollDown(n int) {
	if m == nil || m.detailsDialog == nil || m.detailsDialog.view == nil {
		return
	}
	m.detailsDialog.view.ScrollDown(n)
}

// detailsDialogPageUp scrolls the open details dialog up by one page.
func (m *model) detailsDialogPageUp() {
	if m == nil || m.detailsDialog == nil || m.detailsDialog.view == nil {
		return
	}
	m.detailsDialog.view.PageUp()
}

// detailsDialogPageDown scrolls the open details dialog down by one page.
func (m *model) detailsDialogPageDown() {
	if m == nil || m.detailsDialog == nil || m.detailsDialog.view == nil {
		return
	}
	m.detailsDialog.view.PageDown()
}

// detailsDialogScrollToTop moves the open details dialog to the top of its content.
func (m *model) detailsDialogScrollToTop() {
	if m == nil || m.detailsDialog == nil || m.detailsDialog.view == nil {
		return
	}
	m.detailsDialog.view.ScrollToTop()
}

// detailsDialogScrollToBottom moves the open details dialog to the bottom of its content.
func (m *model) detailsDialogScrollToBottom() {
	if m == nil || m.detailsDialog == nil || m.detailsDialog.view == nil {
		return
	}
	m.detailsDialog.view.ScrollToBottom()
}

// detailsDialogEnsureSized recalculates the open details dialog layout for the current terminal size.
func (m *model) detailsDialogEnsureSized() {
	if m == nil || m.detailsDialog == nil || m.detailsDialog.view == nil {
		return
	}

	w := m.windowWidth
	h := m.windowHeight
	if w <= 0 || h <= 0 {
		return
	}

	dialogW := w - 2*detailsDialogMargin
	dialogH := h - 2*detailsDialogMargin

	// Border (2 rows/cols) + padding (2 rows/cols).
	innerW := dialogW - 4
	innerH := dialogH - 4
	if innerW < 1 {
		innerW = 1
	}
	if innerH < 1 {
		innerH = 1
	}

	dlg := m.detailsDialog
	if innerW == dlg.lastInnerWidth && innerH == dlg.lastInnerHeight && len(dlg.titleLines) > 0 {
		return
	}
	dlg.lastInnerWidth = innerW
	dlg.lastInnerHeight = innerH

	title := termformat.Sanitize(dlg.title, 4)
	titleLines := wrapParagraphText(innerW, title)
	if len(titleLines) == 0 {
		titleLines = []string{"Details"}
	}

	// Reserve space for:
	//   - title lines
	//   - blank line
	//   - hint line
	//   - blank line
	//   - body (at least 1)
	maxTitleLines := innerH - 4
	if maxTitleLines < 1 {
		maxTitleLines = 1
	}
	if len(titleLines) > maxTitleLines {
		titleLines = titleLines[:maxTitleLines]
		last := titleLines[len(titleLines)-1]
		if innerW >= 4 {
			trim := innerW - 3
			if len(last) > trim {
				last = last[:trim]
			}
			last += "..."
		} else {
			last = strings.Repeat(".", innerW)
		}
		titleLines[len(titleLines)-1] = last
	}
	dlg.titleLines = titleLines

	headerHeight := len(dlg.titleLines) + 3 // blank + hint + blank
	bodyHeight := innerH - headerHeight
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	dlg.view.SetSize(innerW, bodyHeight)
}

// detailsDialogView renders a modal details dialog. The base view is ignored (we keep it only for future overlay improvements); the dialog is drawn over a blank
// background.
func (m *model) detailsDialogView(_ string) string {
	if m == nil || m.detailsDialog == nil {
		return ""
	}
	m.detailsDialogEnsureSized()

	w := m.windowWidth
	h := m.windowHeight
	if w <= 0 || h <= 0 {
		return ""
	}
	if w < 2*detailsDialogMargin+10 || h < 2*detailsDialogMargin+8 {
		// Too small for a proper dialog; fall back to a minimal view.
		return "window too small for details"
	}

	dialogW := w - 2*detailsDialogMargin
	dialogH := h - 2*detailsDialogMargin

	dlg := m.detailsDialog

	title := strings.Join(dlg.titleLines, "\n")
	titleStyled := termformat.Style{Foreground: m.palette.primaryForeground, Bold: termformat.StyleSetOn}.Wrap(title)
	hint := termformat.Style{Foreground: m.palette.accentForeground}.Wrap("ESC to close")

	var b strings.Builder
	b.WriteString(titleStyled)
	b.WriteString("\n\n")
	b.WriteString(hint)
	b.WriteString("\n\n")
	b.WriteString(dlg.view.View())

	dialog := termformat.BlockStyle{
		TotalWidth:         dialogW,
		MinTotalHeight:     dialogH,
		BorderStyle:        termformat.BorderStyleThick,
		Padding:            1,
		TextBackground:     m.palette.accentBackground,
		PaddingBackground:  m.palette.accentBackground,
		BorderForeground:   m.palette.borderColor,
		BorderBackground:   m.palette.primaryBackground,
		BlockNormalizeMode: termformat.BlockNormalizeModeExtend,
	}.Apply(b.String())

	dialogLines := strings.Split(strings.TrimSuffix(dialog, "\n"), "\n")
	if len(dialogLines) < dialogH {
		// Ensure we can index dialogLines[y] below.
		missing := dialogH - len(dialogLines)
		for i := 0; i < missing; i++ {
			dialogLines = append(dialogLines, termformat.Style{Background: m.palette.primaryBackground}.Wrap(strings.Repeat(" ", dialogW)))
		}
	}

	leftMargin := m.blankRow(detailsDialogMargin, m.palette.primaryBackground)
	rightMargin := m.blankRow(detailsDialogMargin, m.palette.primaryBackground)
	topBottomRow := m.blankRow(w, m.palette.primaryBackground)

	var screen strings.Builder
	for y := 0; y < h; y++ {
		if y > 0 {
			screen.WriteByte('\n')
		}
		if y < detailsDialogMargin || y >= h-detailsDialogMargin {
			screen.WriteString(topBottomRow)
			continue
		}
		dy := y - detailsDialogMargin
		line := dialogLines[dy]
		// Guard: if dialog line width isn't what we expect, normalize it.
		if termformat.TextWidthWithANSICodes(line) != dialogW {
			line = termformat.BlockStyle{
				TotalWidth:         dialogW,
				TextBackground:     m.palette.accentBackground,
				BlockNormalizeMode: termformat.BlockNormalizeModeExtend,
			}.Apply(line)
		}
		screen.WriteString(leftMargin)
		screen.WriteString(line)
		screen.WriteString(rightMargin)
	}

	// If something went wrong with the dialog dimensions, ensure at least full width.
	view := screen.String()
	if termformat.BlockWidth(view) != w || termformat.BlockHeight(view) != h {
		// Make a best-effort normalization.
		view = termformat.BlockStyle{
			TotalWidth:         w,
			MinTotalHeight:     h,
			TextBackground:     m.palette.primaryBackground,
			BlockNormalizeMode: termformat.BlockNormalizeModeExtend,
		}.Apply(view)
	}

	return view
}

// detailsTitleForMessage returns the title for the Details dialog for the message at messageIndex. It uses the first rendered line of the message, strips styling
// and a leading bullet, sanitizes it for terminal display, and falls back to "Details" when no title is available.
func (m *model) detailsTitleForMessage(messageIndex int) string {
	if m == nil || messageIndex < 0 || messageIndex >= len(m.messages) {
		return "Details"
	}
	msg := &m.messages[messageIndex]

	width := agentformatter.MinTerminalWidth
	if m.viewport != nil && m.viewport.Width() > 0 {
		width = m.viewport.Width()
	}
	m.ensureMessageFormatted(msg, width)

	plain := stripAnsi(msg.formatted)
	firstLine := plain
	if i := strings.IndexByte(plain, '\n'); i >= 0 {
		firstLine = plain[:i]
	}
	firstLine = strings.TrimSpace(firstLine)
	firstLine = strings.TrimPrefix(firstLine, "• ")
	firstLine = strings.TrimSpace(firstLine)
	if firstLine == "" {
		return "Details"
	}
	return termformat.Sanitize(firstLine, 4)
}

// detailsBodyForMessage builds the plain-text Details dialog body for a message. It returns an empty string when the message has no supported details payload.
func (m *model) detailsBodyForMessage(messageIndex int) string {
	if m == nil || messageIndex < 0 || messageIndex >= len(m.messages) {
		return ""
	}
	msg := &m.messages[messageIndex]

	switch msg.kind {
	case messageKindContextStatus:
		var b strings.Builder
		path := ""
		if m.packageContext != nil && m.packageContext.messageIndex == messageIndex {
			path = m.packageContext.packagePath
		}
		if path != "" {
			fmt.Fprintf(&b, "Package: %s\n", termformat.Sanitize(path, 4))
		}
		if msg.contextStatus != nil {
			fmt.Fprintf(&b, "Status: %s\n", packageContextStatusString(msg.contextStatus.status))
		}
		if msg.contextError != "" {
			fmt.Fprintf(&b, "\nError:\n%s\n", termformat.Sanitize(msg.contextError, 4))
		}
		b.WriteString("\nContext:\n")
		if msg.contextDetails == "" {
			b.WriteString("<empty>\n")
		} else {
			b.WriteString(detailsFormatBlob(msg.contextDetails))
			if !strings.HasSuffix(b.String(), "\n") {
				b.WriteByte('\n')
			}
		}
		return strings.TrimSuffix(b.String(), "\n")

	case messageKindAgent:
		if msg.toolCallID == "" {
			return ""
		}
		switch msg.event.Type {
		case agent.EventTypeToolCall:
			return detailsBodyForToolCall(msg.event)
		case agent.EventTypeToolComplete:
			return detailsBodyForToolResult(msg.event)
		default:
			return ""
		}
	default:
		return ""
	}
}

func detailsBodyForToolCall(ev agent.Event) string {
	if ev.ToolCall == nil {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Tool: %s\n", termformat.Sanitize(toolName(ev), 4))
	fmt.Fprintf(&b, "Call ID: %s\n", termformat.Sanitize(ev.ToolCall.CallID, 4))
	if ev.ToolCall.Type != "" {
		fmt.Fprintf(&b, "Type: %s\n", termformat.Sanitize(ev.ToolCall.Type, 4))
	}
	if ev.ToolCall.ProviderID != "" {
		fmt.Fprintf(&b, "Provider: %s\n", termformat.Sanitize(ev.ToolCall.ProviderID, 4))
	}
	b.WriteString("\nInput:\n")
	b.WriteString(detailsFormatBlob(ev.ToolCall.Input))
	return strings.TrimSuffix(b.String(), "\n")
}

func detailsBodyForToolResult(ev agent.Event) string {
	if ev.ToolResult == nil {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Tool: %s\n", termformat.Sanitize(toolName(ev), 4))
	fmt.Fprintf(&b, "Call ID: %s\n", termformat.Sanitize(ev.ToolResult.CallID, 4))
	if ev.ToolResult.Type != "" {
		fmt.Fprintf(&b, "Type: %s\n", termformat.Sanitize(ev.ToolResult.Type, 4))
	}
	b.WriteString("\nResult:\n")
	if ev.ToolResult.IsError {
		b.WriteString("(is_error=true)\n")
	}
	b.WriteString(detailsFormatBlob(ev.ToolResult.Result))
	return strings.TrimSuffix(b.String(), "\n")
}

func packageContextStatusString(status packageContextStatus) string {
	switch status {
	case packageContextStatusPending:
		return "pending"
	case packageContextStatusSuccess:
		return "success"
	case packageContextStatusFailure:
		return "failure"
	default:
		return "unknown"
	}
}

// detailsFormatBlob formats raw detail data for safe display in the Details dialog. It truncates large data, renders invalid UTF-8 as hex, pretty-prints JSON when
// possible, and sanitizes terminal control characters.
func detailsFormatBlob(s string) string {
	if s == "" {
		return "<empty>"
	}

	if len(s) > detailsMaxBytes {
		prefix := s[:detailsMaxBytes]
		return fmt.Sprintf("<truncated: %d bytes shown of %d>\n%s", detailsMaxBytes, len(s), detailsFormatBlob(prefix))
	}

	// Terminal safety: if it isn't valid UTF-8, render as hex.
	if !utf8.ValidString(s) {
		data := []byte(s)
		if len(data) > detailsMaxHexBytes {
			data = data[:detailsMaxHexBytes]
		}
		return fmt.Sprintf("<binary (hex dump; %d bytes shown)>\n%s", len(data), hex.Dump(data))
	}

	// Best-effort JSON pretty printing.
	leftTrimmed := strings.TrimLeft(s, " \r\n\t")
	if strings.HasPrefix(leftTrimmed, "{") || strings.HasPrefix(leftTrimmed, "[") {
		var out bytes.Buffer
		if err := json.Indent(&out, []byte(s), "", "  "); err == nil {
			return termformat.Sanitize(out.String(), 4)
		}
	}

	return termformat.Sanitize(s, 4)
}
