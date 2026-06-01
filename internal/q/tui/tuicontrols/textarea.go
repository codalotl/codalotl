package tuicontrols

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/codalotl/codalotl/internal/q/termformat"
	"github.com/codalotl/codalotl/internal/q/tui"
	"github.com/codalotl/codalotl/internal/q/uni"
)

// TextArea lets user enter multiline text in a terminal area.
//
// Text is wrapped at word boundaries (or grapheme boundaries when needed to prevent overflowing). Wrapping is based on display cells (uni.TextWidth).
//
// The caret is rendered as a background color at the location where the next character would be inserted. It does not blink.
type TextArea struct {
	Placeholder      string           // Placeholder is shown as text (in PlaceholderColor) if the TextArea's contents is "".
	BackgroundColor  termformat.Color // BackgroundColor is applied to the rendered area; when set, View pads each row to Width cells.
	ForegroundColor  termformat.Color // ForegroundColor is applied to user contents and the prompt.
	PlaceholderColor termformat.Color // PlaceholderColor is applied to Placeholder text.
	CaretColor       termformat.Color // CaretColor is the color of the caret/cursor. It should be visible on the background color.

	// Prompt is the first characters to display in the upper-left of the box. The user's first character typed would immediately follow it. Subsequent lines don't have
	// Prompt, but the user's text is aligned to the column of their first character.
	Prompt string

	width           int              // The width is the non-negative rendered width in terminal cells.
	height          int              // The height is the non-negative rendered height in terminal rows.
	contents        string           // The contents are sanitized user text, excluding placeholder, prompt, and styles.
	caretByte       int              // The caret byte is the next insertion offset in contents, clamped to a grapheme boundary.
	displayOffset   int              // The display offset is the first wrapped display-line index rendered after vertical clipping.
	preferredCol    int              // preferredCol is the desired visual column (cells) used for vertical caret navigation.
	hasPreferredCol bool             // The preferred-column flag reports whether preferredCol should be reused for vertical caret navigation.
	keyMap          *KeyMap          // The key map maps TUI input events to editing operations.
	layoutDirty     bool             // The layout dirty flag reports whether cached wrapping data must be rebuilt.
	lastLayoutText  string           // The last layout text is the effective contents or placeholder value used to build the cached layout.
	lastPrompt      string           // The last prompt is the prompt used to build the cached layout.
	lastWidth       int              // The last width is the width used to build the cached layout.
	segments        []displaySegment // The segments are the cached wrapped display lines corresponding to lastLayoutText.

	// promptedLines mirrors segments, but includes the prompt on the first line and the hanging indent on subsequent lines. It is cached alongside segments and is used
	// by View() so other callers can share identical wrapping logic.
	promptedLines []string
}

// A displaySegment describes one display line produced by wrapping source text.
type displaySegment struct {
	text          string // The text field contains the segment contents, excluding any logical newline.
	startByte     int    // The startByte field is the byte offset in the source text where text begins.
	endByte       int    // The endByte field is the byte offset in the source text immediately after text.
	endsLogicalLn bool   // The endsLogicalLn field reports whether this segment is the final display segment of its logical line.
}

// NewTextArea returns a new text area of the given size.
func NewTextArea(width, height int) *TextArea {
	ta := &TextArea{
		width:  max0(width),
		height: max0(height),
	}

	km := NewKeyMap()
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyLeft}, "left")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyRight}, "right")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyUp}, "up")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyDown}, "down")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyHome}, "home")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyEnd}, "end")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyCtrlB}, "left")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyCtrlF}, "right")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyCtrlP}, "up")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyCtrlN}, "down")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyLeft, Alt: true}, "wordleft")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyRight, Alt: true}, "wordright")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyNone, Alt: true, Runes: []rune{'b'}}, "wordleft")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyNone, Alt: true, Runes: []rune{'B'}}, "wordleft")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyNone, Alt: true, Runes: []rune{'f'}}, "wordright")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyNone, Alt: true, Runes: []rune{'F'}}, "wordright")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyCtrlHome}, "textstart")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyCtrlEnd}, "textend")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyCtrlA}, "home")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyCtrlE}, "end")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyBackspace}, "backspace")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyCtrlH}, "backspace")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyBackspace, Alt: true}, "delwordleft")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyDelete}, "delete")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyCtrlD}, "delete")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyDelete, Alt: true}, "delwordright")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyNone, Alt: true, Runes: []rune{'d'}}, "delwordright")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyNone, Alt: true, Runes: []rune{'D'}}, "delwordright")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyCtrlW}, "delwordleft")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyCtrlK}, "deltoeol")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyCtrlU}, "deltobol")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyEnter}, "enter")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyCtrlJ}, "enter")
	km.Add(tui.KeyEvent{ControlKey: tui.ControlKeyTab}, "tab")
	ta.keyMap = km

	ta.layoutDirty = true
	return ta
}

// SetSize sets the width and height of the ta to w, h.
func (ta *TextArea) SetSize(w, h int) {
	if ta == nil {
		return
	}
	ta.width = max0(w)
	ta.height = max0(h)
	ta.layoutDirty = true
}

// Width returns the width.
func (ta *TextArea) Width() int {
	if ta == nil {
		return 0
	}
	return ta.width
}

// Height returns the height.
func (ta *TextArea) Height() int {
	if ta == nil {
		return 0
	}
	return ta.height
}

// Init implements tui.Model's Init.
func (ta *TextArea) Init(t *tui.TUI) {}

// Update implements tui.Model's Update.
func (ta *TextArea) Update(t *tui.TUI, m tui.Message) {
	if ta == nil {
		return
	}

	key, ok := m.(tui.KeyEvent)
	if !ok {
		return
	}

	if key.Paste && len(key.Runes) > 0 {
		ta.InsertString(string(key.Runes))
		return
	}

	semantic := ""
	if ta.keyMap != nil {
		semantic = ta.keyMap.Process(key)
	}

	switch semantic {
	case "left":
		ta.MoveLeft()
	case "right":
		ta.MoveRight()
	case "up":
		ta.MoveUp()
	case "down":
		ta.MoveDown()
	case "home":
		ta.MoveToBeginningOfLine()
	case "end":
		ta.MoveToEndOfLine()
	case "wordleft":
		ta.MoveWordLeft()
	case "wordright":
		ta.MoveWordRight()
	case "textstart":
		ta.MoveToBeginningOfText()
	case "textend":
		ta.MoveToEndOfText()
	case "backspace":
		ta.DeleteLeft()
	case "delete":
		ta.DeleteRight()
	case "delwordleft":
		ta.DeleteWordLeft()
	case "delwordright":
		ta.DeleteWordRight()
	case "deltoeol":
		ta.DeleteToEndOfLine()
	case "deltobol":
		ta.DeleteToBeginningOfLine()
	case "enter":
		ta.InsertString("\n")
	case "tab":
		ta.InsertString("\t")
	case "":
		if key.IsRunes() {
			ta.InsertString(string(key.Runes))
			return
		}
		return
	}

	ta.ensureCaretVisible()
}

// View implements tui.Model's View. The rendered output always contains exactly Height() rows.
func (ta *TextArea) View() string {
	if ta == nil || ta.height == 0 {
		return ""
	}

	ta.rebuildLayoutIfNeeded()
	ta.ensureCaretVisible()

	prefixWidthCells := ta.promptWidth()
	if prefixWidthCells < 0 {
		prefixWidthCells = 0
	}

	promptPrefix := cutPlainStringToWidth(ta.Prompt, max0(ta.width))
	indentPrefix := strings.Repeat(" ", minInt(prefixWidthCells, ta.width))
	promptedLines := ta.promptedLines

	rows := make([]string, 0, ta.height)

	caretLine, caretCol := ta.effectiveCaretDisplayPos()

	for row := 0; row < ta.height; row++ {
		displayIdx := ta.displayOffset + row

		// Match the TextArea's longstanding behavior: prompt is always shown on the first row of the box,
		// not only on the first display line of the content.
		prefix := indentPrefix
		if row == 0 {
			prefix = promptPrefix
		}

		lineText := ""
		hasSeg := false
		if displayIdx >= 0 && displayIdx < len(promptedLines) {
			src := promptedLines[displayIdx]
			srcPrefix := indentPrefix
			if displayIdx == 0 {
				srcPrefix = promptPrefix
			}
			if strings.HasPrefix(src, srcPrefix) {
				lineText = src[len(srcPrefix):]
				hasSeg = true
			}
		} else if displayIdx >= 0 && displayIdx < len(ta.segments) {
			// Width <= 0 path (or unexpected mismatch): fall back to the existing cached layout.
			lineText = ta.segments[displayIdx].text
			hasSeg = true
		}
		seg := displaySegment{text: lineText}

		baseStyle := termformat.Style{Foreground: ta.ForegroundColor, Background: ta.BackgroundColor}
		textStyle := baseStyle
		if ta.contents == "" {
			textStyle = termformat.Style{Foreground: ta.PlaceholderColor, Background: ta.BackgroundColor}
		}

		line := ta.renderRow(prefix, seg, hasSeg, textStyle, baseStyle, displayIdx == caretLine, caretCol)
		rows = append(rows, line)
	}

	return strings.Join(rows, "\n")
}

// SetContents sets the contents of ta to s. s is sanitized with termformat.Sanitize (4 spaces), and \r is removed.
func (ta *TextArea) SetContents(s string) {
	if ta == nil {
		return
	}
	ta.contents = sanitizeTextAreaInput(s)
	ta.caretByte = len(ta.contents)
	ta.caretByte = clampToGraphemeBoundary(ta.contents, ta.caretByte)
	ta.layoutDirty = true
	ta.updatePreferredColFromCaret()
	ta.ensureCaretVisible()
}

// Contents returns the contents of the text area.
func (ta *TextArea) Contents() string {
	if ta == nil {
		return ""
	}
	return ta.contents
}

// DisplayLines returns the number of display lines (accounting for wrapping).
func (ta *TextArea) DisplayLines() int {
	if ta == nil {
		return 0
	}
	ta.rebuildLayoutIfNeeded()
	return len(ta.segments)
}

// ClippedDisplayContents returns the per-display-line user text that is currently displayed in the text area view (contains no \n). It reflects Contents() after
// wrapping and vertical clipping.
//
// This is useful as a testing hook.
func (ta *TextArea) ClippedDisplayContents() []string {
	if ta == nil || ta.height <= 0 {
		return nil
	}

	ta.rebuildLayoutIfNeeded()
	ta.ensureCaretVisible()

	availWidth := ta.availableTextWidth()
	segs := buildDisplaySegments(ta.contents, availWidth)
	if len(segs) == 0 {
		segs = []displaySegment{{text: "", startByte: 0, endByte: 0, endsLogicalLn: true}}
	}

	offset := ta.displayOffset
	if len(segs) <= ta.height {
		offset = 0
	} else {
		offset = clampInt(offset, 0, len(segs)-ta.height)
	}

	end := offset + ta.height
	if end > len(segs) {
		end = len(segs)
	}

	out := make([]string, 0, end-offset)
	for i := offset; i < end; i++ {
		out = append(out, cutPlainStringToWidth(segs[i].text, max0(availWidth)))
	}
	return out
}

// CaretPositionByteOffset returns the position of the caret (the location of the next inserted character) in Contents(), measured in bytes. This position must fall
// on a grapheme cluster boundary.
func (ta *TextArea) CaretPositionByteOffset() int {
	if ta == nil {
		return 0
	}
	pos := clampInt(ta.caretByte, 0, len(ta.contents))
	return clampToGraphemeBoundary(ta.contents, pos)
}

// CaretPositionCurrentLineByteOffset returns the byte index of the caret on the current logical line.
func (ta *TextArea) CaretPositionCurrentLineByteOffset() int {
	if ta == nil {
		return 0
	}
	if ta.contents == "" {
		return 0
	}
	pos := clampInt(ta.caretByte, 0, len(ta.contents))
	_, start, _ := logicalLineBoundsAt(ta.contents, pos)
	if start < 0 {
		start = 0
	}
	if start > pos {
		return 0
	}
	return pos - start
}

// CaretPositionRowCol returns the position of the caret based on 0-index-based rows/cols of terminal cells (logical line, cell column).
func (ta *TextArea) CaretPositionRowCol() (int, int) {
	if ta == nil {
		return 0, 0
	}
	row, start, _ := logicalLineBoundsAt(ta.contents, ta.caretByte)
	col := uni.TextWidth(ta.contents[start:ta.caretByte], nil)
	return row, col
}

// CaretDisplayPositionRowCol returns the caret position by display row/col. The row is in [0, DisplayLines()).
func (ta *TextArea) CaretDisplayPositionRowCol() (int, int) {
	if ta == nil {
		return 0, 0
	}
	return ta.effectiveCaretDisplayPos()
}

// SetCaretPosition sets the caret position to the logical row, col, clamping invalid values.
func (ta *TextArea) SetCaretPosition(row, col int) {
	if ta == nil {
		return
	}
	if row < 0 {
		row = 0
	}
	if col < 0 {
		col = 0
	}

	start, end, ok := logicalLineBoundsByRow(ta.contents, row)
	if !ok {
		ta.caretByte = len(ta.contents)
		ta.caretByte = clampToGraphemeBoundary(ta.contents, ta.caretByte)
		ta.updatePreferredColFromCaret()
		ta.ensureCaretVisible()
		return
	}

	ta.caretByte = start + byteIndexAtCellCol(ta.contents[start:end], col)
	ta.caretByte = clampToGraphemeBoundary(ta.contents, ta.caretByte)
	ta.updatePreferredColFromCaret()
	ta.ensureCaretVisible()
}

// InsertString inserts a string at the caret position.
func (ta *TextArea) InsertString(s string) {
	if ta == nil {
		return
	}
	if s == "" {
		return
	}
	s = sanitizeTextAreaInput(s)
	if s == "" {
		return
	}

	pos := clampInt(ta.caretByte, 0, len(ta.contents))
	pos = clampToGraphemeBoundary(ta.contents, pos)

	ta.contents = ta.contents[:pos] + s + ta.contents[pos:]
	ta.caretByte = pos + len(s)
	ta.caretByte = clampToGraphemeBoundary(ta.contents, ta.caretByte)

	ta.layoutDirty = true
	ta.updatePreferredColFromCaret()
	ta.ensureCaretVisible()
}

// InsertRune inserts a rune at the caret position.
func (ta *TextArea) InsertRune(r rune) {
	if ta == nil {
		return
	}
	ta.InsertString(string(r))
}

// updatePreferredColFromCaret stores the caret's current display column as the preferred column for vertical movement.
func (ta *TextArea) updatePreferredColFromCaret() {
	ta.hasPreferredCol = true
	_, col := ta.effectiveCaretDisplayPos()
	ta.preferredCol = col
}

// The ensureCaretVisible method adjusts vertical clipping so the caret is visible.
func (ta *TextArea) ensureCaretVisible() {
	if ta == nil {
		return
	}
	ta.rebuildLayoutIfNeeded()

	if ta.height <= 0 {
		ta.displayOffset = 0
		return
	}

	total := len(ta.segments)
	if total <= ta.height {
		ta.displayOffset = 0
		return
	}

	maxOffset := total - ta.height
	if maxOffset < 0 {
		maxOffset = 0
	}

	caretLine, _ := ta.effectiveCaretDisplayPos()
	if caretLine < ta.displayOffset {
		ta.displayOffset = caretLine
	} else if caretLine >= ta.displayOffset+ta.height {
		ta.displayOffset = caretLine - ta.height + 1
	}

	if ta.displayOffset < 0 {
		ta.displayOffset = 0
	}
	if ta.displayOffset > maxOffset {
		ta.displayOffset = maxOffset
	}
}

// MoveLeft moves the caret one grapheme cluster to the left.
func (ta *TextArea) MoveLeft() {
	if ta == nil || ta.contents == "" || ta.caretByte <= 0 {
		ta.caretByte = 0
		return
	}
	if ta.caretByte > 0 && ta.contents[ta.caretByte-1] == '\n' {
		ta.caretByte--
		ta.updatePreferredColFromCaret()
		ta.ensureCaretVisible()
		return
	}
	ta.caretByte = prevGraphemeBoundary(ta.contents, ta.caretByte)
	ta.updatePreferredColFromCaret()
	ta.ensureCaretVisible()
}

// MoveRight moves the caret one grapheme cluster to the right.
func (ta *TextArea) MoveRight() {
	if ta == nil || ta.contents == "" || ta.caretByte >= len(ta.contents) {
		ta.caretByte = len(ta.contents)
		return
	}
	if ta.contents[ta.caretByte] == '\n' {
		ta.caretByte++
		ta.updatePreferredColFromCaret()
		ta.ensureCaretVisible()
		return
	}
	ta.caretByte = nextGraphemeBoundary(ta.contents, ta.caretByte)
	ta.updatePreferredColFromCaret()
	ta.ensureCaretVisible()
}

// MoveUp moves the caret up by one display line (wrapped line), preserving visual column.
func (ta *TextArea) MoveUp() {
	if ta == nil || ta.contents == "" {
		return
	}
	ta.rebuildLayoutIfNeeded()

	curLine, _ := ta.effectiveCaretDisplayPos()
	if curLine <= 0 {
		return
	}
	target := curLine - 1

	col := ta.preferredCol
	if !ta.hasPreferredCol {
		_, curCol := ta.effectiveCaretDisplayPos()
		col = curCol
		ta.preferredCol = col
		ta.hasPreferredCol = true
	}
	ta.setCaretOnDisplayLine(target, col)
	ta.ensureCaretVisible()
}

// MoveDown moves the caret down by one display line (wrapped line), preserving visual column.
func (ta *TextArea) MoveDown() {
	if ta == nil || ta.contents == "" {
		return
	}
	ta.rebuildLayoutIfNeeded()

	curLine, _ := ta.effectiveCaretDisplayPos()
	if curLine >= len(ta.segments)-1 {
		return
	}
	target := curLine + 1

	col := ta.preferredCol
	if !ta.hasPreferredCol {
		_, curCol := ta.effectiveCaretDisplayPos()
		col = curCol
		ta.preferredCol = col
		ta.hasPreferredCol = true
	}
	ta.setCaretOnDisplayLine(target, col)
	ta.ensureCaretVisible()
}

// setCaretOnDisplayLine moves the caret to col on the cached display line at lineIdx.
func (ta *TextArea) setCaretOnDisplayLine(lineIdx, col int) {
	if ta == nil {
		return
	}
	if lineIdx < 0 {
		lineIdx = 0
	}
	if lineIdx >= len(ta.segments) {
		lineIdx = len(ta.segments) - 1
	}
	if lineIdx < 0 || lineIdx >= len(ta.segments) {
		return
	}
	seg := ta.segments[lineIdx]
	if seg.startByte < 0 || seg.endByte < 0 || seg.startByte > seg.endByte {
		return
	}
	ta.caretByte = seg.startByte + byteIndexAtCellCol(ta.contents[seg.startByte:seg.endByte], col)
	ta.caretByte = clampToGraphemeBoundary(ta.contents, ta.caretByte)
}

// MoveToBeginningOfLine moves the caret to the beginning of the current logical line.
func (ta *TextArea) MoveToBeginningOfLine() {
	if ta == nil {
		return
	}
	_, start, _ := logicalLineBoundsAt(ta.contents, ta.caretByte)
	ta.caretByte = start
	ta.updatePreferredColFromCaret()
	ta.ensureCaretVisible()
}

// MoveToEndOfLine moves the caret to the end of the current logical line.
func (ta *TextArea) MoveToEndOfLine() {
	if ta == nil {
		return
	}
	_, _, end := logicalLineBoundsAt(ta.contents, ta.caretByte)
	ta.caretByte = end
	ta.updatePreferredColFromCaret()
	ta.ensureCaretVisible()
}

// MoveWordLeft moves the caret to the beginning of the previous word (whitespace-delimited).
//
// Newlines are treated as whitespace, so word motion can cross logical line boundaries.
func (ta *TextArea) MoveWordLeft() {
	if ta == nil || ta.contents == "" || ta.caretByte <= 0 {
		return
	}
	pos := clampInt(ta.caretByte, 0, len(ta.contents))
	pos = clampToGraphemeBoundary(ta.contents, pos)

	ta.caretByte = moveWordLeftInLine(ta.contents[:pos])
	ta.caretByte = clampToGraphemeBoundary(ta.contents, ta.caretByte)
	ta.updatePreferredColFromCaret()
	ta.ensureCaretVisible()
}

// MoveWordRight moves the caret to the end of the next word (whitespace-delimited).
//
// Concretely:
//   - if the caret is in whitespace, skip whitespace then skip the next word.
//   - if the caret is in a word, skip to the end of the current word.
//
// Newlines are treated as whitespace, so word motion can cross logical line boundaries.
func (ta *TextArea) MoveWordRight() {
	if ta == nil || ta.contents == "" || ta.caretByte >= len(ta.contents) {
		return
	}
	pos := clampInt(ta.caretByte, 0, len(ta.contents))
	pos = clampToGraphemeBoundary(ta.contents, pos)

	newRel := moveWordRightInLine(ta.contents[pos:])
	ta.caretByte = pos + newRel
	ta.caretByte = clampToGraphemeBoundary(ta.contents, ta.caretByte)
	ta.updatePreferredColFromCaret()
	ta.ensureCaretVisible()
}

// MoveToBeginningOfText moves the caret to the beginning of the text.
func (ta *TextArea) MoveToBeginningOfText() {
	if ta == nil {
		return
	}
	ta.caretByte = 0
	ta.updatePreferredColFromCaret()
	ta.ensureCaretVisible()
}

// MoveToEndOfText moves the caret to the end of the text.
func (ta *TextArea) MoveToEndOfText() {
	if ta == nil {
		return
	}
	ta.caretByte = len(ta.contents)
	ta.caretByte = clampToGraphemeBoundary(ta.contents, ta.caretByte)
	ta.updatePreferredColFromCaret()
	ta.ensureCaretVisible()
}

// DeleteLeft deletes one grapheme cluster to the left of the caret (or a newline).
func (ta *TextArea) DeleteLeft() {
	if ta == nil || ta.contents == "" || ta.caretByte <= 0 {
		ta.caretByte = 0
		return
	}

	pos := clampInt(ta.caretByte, 0, len(ta.contents))
	if pos > 0 && ta.contents[pos-1] == '\n' {
		ta.contents = ta.contents[:pos-1] + ta.contents[pos:]
		ta.caretByte = pos - 1
		ta.layoutDirty = true
		ta.updatePreferredColFromCaret()
		ta.ensureCaretVisible()
		return
	}

	start := prevGraphemeBoundary(ta.contents, pos)
	ta.contents = ta.contents[:start] + ta.contents[pos:]
	ta.caretByte = start
	ta.layoutDirty = true
	ta.updatePreferredColFromCaret()
	ta.ensureCaretVisible()
}

// DeleteRight deletes one grapheme cluster to the right of the caret (or a newline).
func (ta *TextArea) DeleteRight() {
	if ta == nil || ta.contents == "" || ta.caretByte >= len(ta.contents) {
		return
	}

	pos := clampInt(ta.caretByte, 0, len(ta.contents))
	if pos < len(ta.contents) && ta.contents[pos] == '\n' {
		ta.contents = ta.contents[:pos] + ta.contents[pos+1:]
		ta.layoutDirty = true
		ta.updatePreferredColFromCaret()
		ta.ensureCaretVisible()
		return
	}

	end := nextGraphemeBoundary(ta.contents, pos)
	ta.contents = ta.contents[:pos] + ta.contents[end:]
	ta.layoutDirty = true
	ta.updatePreferredColFromCaret()
	ta.ensureCaretVisible()
}

// DeleteWordLeft deletes the previous word (whitespace-delimited).
//
// Newlines are treated as whitespace, so word deletion can cross logical line boundaries.
func (ta *TextArea) DeleteWordLeft() {
	if ta == nil || ta.contents == "" || ta.caretByte <= 0 {
		return
	}
	pos := clampInt(ta.caretByte, 0, len(ta.contents))
	pos = clampToGraphemeBoundary(ta.contents, pos)

	delStart := moveWordLeftInLine(ta.contents[:pos])
	delStart = clampInt(delStart, 0, pos)

	ta.contents = ta.contents[:delStart] + ta.contents[pos:]
	ta.caretByte = delStart
	ta.layoutDirty = true
	ta.updatePreferredColFromCaret()
	ta.ensureCaretVisible()
}

// DeleteWordRight deletes the next word (whitespace-delimited).
//
// Newlines are treated as whitespace, so word deletion can cross logical line boundaries.
func (ta *TextArea) DeleteWordRight() {
	if ta == nil || ta.contents == "" || ta.caretByte >= len(ta.contents) {
		return
	}
	pos := clampInt(ta.caretByte, 0, len(ta.contents))
	pos = clampToGraphemeBoundary(ta.contents, pos)

	suffix := ta.contents[pos:]
	if suffix == "" {
		return
	}
	delEnd := moveWordRightInLine(suffix)
	delEnd = clampInt(delEnd, 0, len(suffix))
	if delEnd == 0 {
		return
	}

	ta.contents = ta.contents[:pos] + ta.contents[pos+delEnd:]
	ta.layoutDirty = true
	ta.updatePreferredColFromCaret()
	ta.ensureCaretVisible()
}

// DeleteToEndOfLine deletes from the caret to the end of the current logical line.
func (ta *TextArea) DeleteToEndOfLine() {
	if ta == nil || ta.contents == "" {
		return
	}
	_, _, end := logicalLineBoundsAt(ta.contents, ta.caretByte)
	if end <= ta.caretByte {
		return
	}
	ta.contents = ta.contents[:ta.caretByte] + ta.contents[end:]
	ta.layoutDirty = true
	ta.updatePreferredColFromCaret()
	ta.ensureCaretVisible()
}

// DeleteToBeginningOfLine deletes from the caret to the beginning of the current logical line.
func (ta *TextArea) DeleteToBeginningOfLine() {
	if ta == nil || ta.contents == "" {
		return
	}
	_, start, _ := logicalLineBoundsAt(ta.contents, ta.caretByte)
	if start >= ta.caretByte {
		return
	}
	ta.contents = ta.contents[:start] + ta.contents[ta.caretByte:]
	ta.caretByte = start
	ta.layoutDirty = true
	ta.updatePreferredColFromCaret()
	ta.ensureCaretVisible()
}

// The effectiveText method returns the text used for layout and rendering: the contents when non-empty, otherwise Placeholder.
//
// It returns an empty string for a nil receiver.
func (ta *TextArea) effectiveText() string {
	if ta == nil {
		return ""
	}
	if ta.contents != "" {
		return ta.contents
	}
	return ta.Placeholder
}

// The promptWidth method returns the display width of Prompt in terminal cells, or zero for a nil receiver or empty prompt.
//
// The returned width is not clipped to the TextArea width.
func (ta *TextArea) promptWidth() int {
	if ta == nil || ta.Prompt == "" {
		return 0
	}
	return uni.TextWidth(ta.Prompt, nil)
}

// The availableTextWidth method returns the number of terminal cells available for user text after the prompt, clamped to zero.
//
// It returns zero for a nil receiver.
func (ta *TextArea) availableTextWidth() int {
	if ta == nil {
		return 0
	}
	avail := ta.width - ta.promptWidth()
	if avail < 0 {
		return 0
	}
	return avail
}

// The rebuildLayoutIfNeeded method refreshes the cached display layout when the effective text, prompt, or width changes.
//
// It is safe to call on a nil receiver. Rebuilding updates segments and promptedLines, clears layoutDirty, and normalizes the caret byte offset to a valid grapheme
// boundary in the current contents.
func (ta *TextArea) rebuildLayoutIfNeeded() {
	if ta == nil {
		return
	}

	text := ta.effectiveText()
	prompt := ta.Prompt
	if !ta.layoutDirty && ta.lastLayoutText == text && ta.lastPrompt == prompt && ta.lastWidth == ta.width {
		return
	}

	ta.lastLayoutText = text
	ta.lastPrompt = prompt
	ta.lastWidth = ta.width
	ta.layoutDirty = false

	availWidth := ta.availableTextWidth()
	ta.segments = buildDisplaySegments(text, availWidth)
	if len(ta.segments) == 0 {
		ta.segments = []displaySegment{{text: "", startByte: 0, endByte: 0, endsLogicalLn: true}}
	}

	promptPrefix := cutPlainStringToWidth(prompt, max0(ta.width))
	indentPrefix := strings.Repeat(" ", minInt(ta.promptWidth(), ta.width))
	ta.promptedLines = wrapPromptedTextFromSegments(promptPrefix, indentPrefix, availWidth, ta.segments)

	if ta.contents != "" {
		ta.caretByte = clampInt(ta.caretByte, 0, len(ta.contents))
		ta.caretByte = clampToGraphemeBoundary(ta.contents, ta.caretByte)
	} else {
		ta.caretByte = 0
	}
}

// The effectiveCaretDisplayPos method returns the caret's absolute display-line row and text-column cell within the wrapped layout.
//
// The column is measured from the first user-text cell, excluding Prompt and hanging indent. It returns (0, 0) for a nil receiver or empty contents, and rebuilds
// the layout before computing the position.
func (ta *TextArea) effectiveCaretDisplayPos() (int, int) {
	if ta == nil {
		return 0, 0
	}
	ta.rebuildLayoutIfNeeded()

	if len(ta.segments) == 0 {
		return 0, 0
	}

	if ta.contents == "" {
		return 0, 0
	}

	caret := clampInt(ta.caretByte, 0, len(ta.contents))
	for i := range ta.segments {
		seg := ta.segments[i]
		if caret < seg.startByte {
			continue
		}
		if caret < seg.endByte || (caret == seg.endByte && seg.endsLogicalLn) {
			col := uni.TextWidth(ta.contents[seg.startByte:caret], nil)
			return i, col
		}
	}

	last := len(ta.segments) - 1
	seg := ta.segments[last]
	col := 0
	if seg.startByte >= 0 && seg.startByte <= len(ta.contents) {
		end := clampInt(caret, seg.startByte, len(ta.contents))
		col = uni.TextWidth(ta.contents[seg.startByte:end], nil)
	}
	return last, col
}

// renderRow renders a single text-area row from a prefix and an optional wrapped display segment.
//
// The prefix is the prompt or hanging indent to write before the text and is expected to already fit in Width() cells. The hasSeg flag reports whether seg contains
// row content; when it is false, renderRow uses an empty line. The textStyle applies to the segment text, baseStyle supplies the row's base styling, and caretOnLine
// with caretCol controls caret highlighting. The caretCol value is measured in terminal cells within the segment text.
//
// renderRow clips the segment text to the cells remaining after prefix and finalizes the row with finishRow.
func (ta *TextArea) renderRow(prefix string, seg displaySegment, hasSeg bool, textStyle, baseStyle termformat.Style, caretOnLine bool, caretCol int) string {
	if ta.width <= 0 {
		return ""
	}

	avail := ta.width - uni.TextWidth(prefix, nil)
	if avail < 0 {
		avail = 0
	}

	var b strings.Builder

	baseOpen := baseStyle.OpeningControlCodes()
	textOpen := textStyle.OpeningControlCodes()

	styleUsed := baseOpen != "" || textOpen != "" || (ta.CaretColor != nil && ta.CaretColor.ANSISequence(true) != "")
	fillBG := ta.BackgroundColor != nil && ta.BackgroundColor.ANSISequence(true) != ""

	if baseOpen != "" {
		b.WriteString(baseOpen)
	}
	b.WriteString(prefix)

	if textOpen != "" && textOpen != baseOpen {
		b.WriteString(textOpen)
	}

	var lineText string
	if hasSeg {
		lineText = seg.text
	} else {
		lineText = ""
	}

	if avail == 0 {
		return ta.finishRow(&b, styleUsed, fillBG)
	}

	caretStyle := termformat.Style{Foreground: textStyle.Foreground, Background: ta.CaretColor}
	caretOpen := caretStyle.OpeningControlCodes()
	restore := termformat.ANSIReset + textOpen

	if !caretOnLine {
		b.WriteString(cutPlainStringToWidth(lineText, avail))
		return ta.finishRow(&b, styleUsed, fillBG)
	}
	if caretOpen == "" {
		b.WriteString(cutPlainStringToWidth(lineText, avail))
		return ta.finishRow(&b, styleUsed, fillBG)
	}

	caretCol = max0(caretCol)
	lineBefore, caretGrapheme, lineAfter := splitAtCellCol(lineText, caretCol)

	if lineBefore != "" {
		b.WriteString(cutPlainStringToWidth(lineBefore, avail))
	}

	usedCells := uni.TextWidth(lineBefore, nil)

	if usedCells >= avail {
		// If there's no room for a trailing caret cell, highlight the last cell of the visible text.
		visible := cutPlainStringToWidth(lineText, avail)
		if visible == "" {
			if caretOpen != "" {
				b.WriteString(caretOpen)
			}
			b.WriteByte(' ')
			return ta.finishRow(&b, styleUsed, fillBG)
		}
		left, last, _ := splitAtCellCol(visible, max0(avail-1))
		b.Reset()
		if baseOpen != "" {
			b.WriteString(baseOpen)
		}
		b.WriteString(prefix)
		if textOpen != "" && textOpen != baseOpen {
			b.WriteString(textOpen)
		}
		b.WriteString(left)
		if caretOpen != "" {
			b.WriteString(caretOpen)
		}
		b.WriteString(last)
		return ta.finishRow(&b, styleUsed, fillBG)
	}

	if caretGrapheme == "" {
		if caretOpen != "" {
			b.WriteString(caretOpen)
		}
		b.WriteByte(' ')
		if caretOpen != "" || textOpen != "" {
			b.WriteString(restore)
		}
	} else {
		if caretOpen != "" {
			b.WriteString(caretOpen)
		}
		b.WriteString(caretGrapheme)
		if caretOpen != "" || textOpen != "" {
			b.WriteString(restore)
		}
	}

	caretWidth := 1
	if caretGrapheme != "" {
		caretWidth = uni.TextWidth(caretGrapheme, nil)
	}
	remaining := avail - usedCells - caretWidth
	if remaining < 0 {
		remaining = 0
	}
	if lineAfter != "" && remaining > 0 {
		b.WriteString(cutPlainStringToWidth(lineAfter, remaining))
	}

	return ta.finishRow(&b, styleUsed, fillBG)
}

// finishRow completes a rendered text-area row and returns it. styleUsed reports whether emitted ANSI styling should be reset. If fillBG is true, finishRow pads
// the row to Width() terminal cells with the text area's background color. It appends termformat.ANSIReset when fillBG or styleUsed is true. If b is nil, finishRow
// returns "".
func (ta *TextArea) finishRow(b *strings.Builder, styleUsed, fillBG bool) string {
	if b == nil {
		return ""
	}
	if ta == nil || ta.width <= 0 {
		if styleUsed {
			b.WriteString(termformat.ANSIReset)
		}
		return b.String()
	}

	if fillBG {
		s := b.String()
		w := termformat.TextWidthWithANSICodes(s)
		if w < ta.width {
			bgOnly := termformat.Style{Background: ta.BackgroundColor}.OpeningControlCodes()
			if bgOnly != "" {
				b.WriteString(bgOnly)
			}
			b.WriteString(strings.Repeat(" ", ta.width-w))
		}
		b.WriteString(termformat.ANSIReset)
		return b.String()
	}

	if styleUsed {
		b.WriteString(termformat.ANSIReset)
	}
	return b.String()
}

// The buildDisplaySegments function splits text into logical lines, wraps each line to width cells, and returns the resulting display-line segments.
//
// The returned slice is never empty. Segment byte ranges refer to text, exclude logical newline bytes, and mark the final segment of each logical line.
func buildDisplaySegments(text string, width int) []displaySegment {
	if text == "" {
		return []displaySegment{{text: "", startByte: 0, endByte: 0, endsLogicalLn: true}}
	}
	if width < 0 {
		width = 0
	}

	var out []displaySegment

	for lineStart := 0; ; {
		lineEnd := strings.IndexByte(text[lineStart:], '\n')
		if lineEnd < 0 {
			lineEnd = len(text)
		} else {
			lineEnd = lineStart + lineEnd
		}

		line := text[lineStart:lineEnd]
		segs := wrapLineWordBoundary(line, width)
		if len(segs) == 0 {
			segs = []wrapSeg{{start: 0, end: 0}}
		}

		for i, seg := range segs {
			ds := displaySegment{
				text:      line[seg.start:seg.end],
				startByte: lineStart + seg.start,
				endByte:   lineStart + seg.end,
			}
			if i == len(segs)-1 {
				ds.endsLogicalLn = true
			}
			out = append(out, ds)
		}

		if lineEnd >= len(text) {
			break
		}
		lineStart = lineEnd + 1
	}

	if len(out) == 0 {
		out = []displaySegment{{text: "", startByte: 0, endByte: 0, endsLogicalLn: true}}
	}
	return out
}

// The wrapSeg type describes the half-open byte range [start, end) of a logical line that forms one wrapped display line.
type wrapSeg struct {
	start int // The start field is the inclusive byte offset within the logical line.
	end   int // The end field is the exclusive byte offset within the logical line.
}

// The wrapLineWordBoundary function wraps one logical line into display-line byte ranges.
//
// It applies TextArea's tailored word-break rules using grapheme-cell widths and, for widths of at least two cells, reserves the rightmost cell for whitespace and
// the caret. It falls back to grapheme boundaries to make progress. A nonpositive width disables wrapping, and an empty line returns one empty segment.
func wrapLineWordBoundary(line string, width int) []wrapSeg {
	if line == "" {
		return []wrapSeg{{start: 0, end: 0}}
	}
	if width <= 0 {
		return []wrapSeg{{start: 0, end: len(line)}}
	}

	// Wrapping follows SPEC.md:
	//   - Wrap at UAX #14-like break opportunities (tailored) with a fallback to grapheme boundaries.
	//   - The rightmost column is reserved for whitespace/caret: non-whitespace graphemes are wrapped
	//     as if the width were (width-1), when width >= 2.
	gs := graphemesForWrap(line)
	if len(gs) == 0 {
		return []wrapSeg{{start: 0, end: 0}}
	}

	maxGraphicWidth := width
	if width >= 2 {
		maxGraphicWidth = width - 1
	}

	breakAfter := make([]bool, len(gs))
	stickyToRight := make([]bool, len(gs))
	for i := range gs {
		breakAfter[i] = wrapBreakOpportunityAfter(gs, i)
		stickyToRight[i] = wrapStickyToRight(gs, i)
	}

	var out []wrapSeg

	segStartIdx := 0
	segStartByte := gs[0].start
	widthUsed := 0

	lastBreakNextIdx := -1 // index of next grapheme where a break is allowed (start of next segment)

	for i := 0; i < len(gs); {
		g := gs[i]

		allowed := width
		if g.width > 0 && !g.isSpace {
			allowed = maxGraphicWidth
		}

		if widthUsed+g.width > allowed {
			// If even the first grapheme can't fit, force it onto its own line to make forward progress.
			if i == segStartIdx {
				out = append(out, wrapSeg{start: segStartByte, end: g.end})
				segStartIdx = i + 1
				if segStartIdx >= len(gs) {
					return out
				}
				segStartByte = gs[segStartIdx].start
				i = segStartIdx
				widthUsed = 0
				lastBreakNextIdx = -1
				continue
			}

			breakIdx := -1
			if lastBreakNextIdx > segStartIdx {
				breakIdx = lastBreakNextIdx
			} else {
				breakIdx = i // break before current grapheme

				// Avoid ending a line with "." or "," when they are between alphanumerics; keep the
				// punctuation with the following word instead.
				prevIdx := i - 1
				if prevIdx >= segStartIdx && stickyToRight[prevIdx] {
					breakIdx = prevIdx
				}
			}

			adjBreakIdx := adjustWrapBreakIndexForWordJoiner(gs, segStartIdx, breakIdx)
			if adjBreakIdx > segStartIdx {
				breakIdx = adjBreakIdx
			}

			// If we still can't produce a non-empty segment, fall back to breaking before the
			// current grapheme.
			if breakIdx <= segStartIdx {
				breakIdx = i
			}

			out = append(out, wrapSeg{start: segStartByte, end: gs[breakIdx].start})
			segStartIdx = breakIdx
			segStartByte = gs[segStartIdx].start
			i = segStartIdx
			widthUsed = 0
			lastBreakNextIdx = -1
			continue
		}

		widthUsed += g.width
		if i < len(gs)-1 && breakAfter[i] {
			lastBreakNextIdx = i + 1
		}
		i++
	}

	if segStartByte < len(line) {
		out = append(out, wrapSeg{start: segStartByte, end: len(line)})
	} else {
		out = append(out, wrapSeg{start: len(line), end: len(line)})
	}

	return out
}

// The wrapGrapheme type describes one grapheme cluster and the properties used by TextArea wrapping.
type wrapGrapheme struct {
	start    int  // Start is the byte index of the cluster's first byte.
	end      int  // End is the byte index immediately after the cluster.
	width    int  // Width is the cluster's display width in terminal cells.
	isSpace  bool // IsSpace is true when the cluster contains Unicode whitespace.
	isAlnum  bool // IsAlnum is true when the cluster contains a Unicode letter or digit.
	hasWJ    bool // U+2060 WORD JOINER
	hasSHY   bool // U+00AD SOFT HYPHEN
	ascii    byte // only if single-rune ASCII; otherwise 0
	hasASCII bool // HasASCII is true when the cluster is exactly one ASCII rune.
}

// The graphemesForWrap function returns wrapping metadata for each grapheme cluster in s.
//
// Each item records byte bounds, terminal-cell width, and the character properties used by TextArea's line-breaking rules. It returns nil for an empty string.
func graphemesForWrap(s string) []wrapGrapheme {
	if s == "" {
		return nil
	}
	iter := uni.NewGraphemeIterator(s, nil)
	out := make([]wrapGrapheme, 0, 32)
	for iter.Next() {
		g := wrapGrapheme{start: iter.Start(), end: iter.End(), width: iter.TextWidth()}

		// Fast path: single-rune ASCII graphemes are the common case for break rules.
		gr := s[g.start:g.end]
		if r, size := utf8.DecodeRuneInString(gr); r != utf8.RuneError && size == len(gr) && r < 0x80 {
			g.ascii = byte(r)
			g.hasASCII = true
		}

		for _, r := range gr {
			if unicode.IsSpace(r) {
				g.isSpace = true
				// We still scan to pick up WORD JOINER, though it's unlikely to co-occur.
			}
			if r == '\u2060' {
				g.hasWJ = true
			}
			if r == '\u00ad' {
				g.hasSHY = true
			}
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				g.isAlnum = true
			}
		}
		out = append(out, g)
	}
	return out
}

// The wrapBreakOpportunityAfter function reports whether TextArea wrapping may break between gs[i] and gs[i+1].
//
// It returns false for out-of-range indexes, for the final grapheme, and across WORD JOINER. It permits breaks after Unicode whitespace; after `/`, `|`, `!`, `?`,
// and `}`; after `.` or `,` unless between alphanumeric graphemes; and after `-` only when between alphanumeric graphemes. It does not treat SOFT HYPHEN as a break
// opportunity.
func wrapBreakOpportunityAfter(gs []wrapGrapheme, i int) bool {
	if i < 0 || i >= len(gs)-1 {
		return false
	}

	// WORD JOINER prevents breaks between the surrounding characters.
	if gs[i].hasWJ || gs[i+1].hasWJ {
		return false
	}

	if gs[i].isSpace {
		return true
	}

	if gs[i].hasASCII {
		switch gs[i].ascii {
		case '-':
			// Tailoring: hyphen-minus isn't a break opportunity at the primary stage.
			// Secondary rule: allow splitting after "-" only when between alphanumerics.
			if i-1 >= 0 && i+1 < len(gs) && gs[i-1].isAlnum && gs[i+1].isAlnum {
				return true
			}
			return false
		case '/':
			return true
		case '|':
			return true
		case '!':
			return true
		case '?':
			return true
		case '}':
			return true
		case '.':
			if i-1 >= 0 && i+1 < len(gs) && gs[i-1].isAlnum && gs[i+1].isAlnum {
				return false
			}
			return true
		case ',':
			if i-1 >= 0 && i+1 < len(gs) && gs[i-1].isAlnum && gs[i+1].isAlnum {
				return false
			}
			return true
		}
	}

	// Soft hyphen tailoring: do not treat U+00AD as a break opportunity at this stage.
	if gs[i].hasSHY {
		return false
	}

	return false
}

func wrapStickyToRight(gs []wrapGrapheme, i int) bool {
	if i < 0 || i >= len(gs) {
		return false
	}
	if !gs[i].hasASCII {
		return false
	}
	if gs[i].ascii != '.' && gs[i].ascii != ',' {
		return false
	}
	if i-1 < 0 || i+1 >= len(gs) {
		return false
	}
	return gs[i-1].isAlnum && gs[i+1].isAlnum
}

// The adjustWrapBreakIndexForWordJoiner function moves a proposed break index left so the break does not cross a WORD JOINER.
//
// The breakIdx argument is the index of the first grapheme in the next segment, so the proposed break is between breakIdx-1 and breakIdx. The returned index is
// never greater than breakIdx and may equal segStartIdx if no safe break remains in the segment. Invalid or empty ranges are returned unchanged.
func adjustWrapBreakIndexForWordJoiner(gs []wrapGrapheme, segStartIdx, breakIdx int) int {
	if breakIdx <= segStartIdx {
		return breakIdx
	}
	if breakIdx > len(gs) {
		return breakIdx
	}
	// Break index is the index of the first grapheme in the next segment; the break is between
	// breakIdx-1 and breakIdx.
	for breakIdx > segStartIdx {
		left := breakIdx - 1
		right := breakIdx
		if right >= len(gs) {
			return breakIdx
		}
		if gs[left].hasWJ || gs[right].hasWJ {
			breakIdx--
			continue
		}
		return breakIdx
	}
	return breakIdx
}

func sanitizeTextAreaInput(s string) string {
	s = termformat.Sanitize(s, 4)
	if strings.IndexByte(s, '\r') >= 0 {
		s = strings.ReplaceAll(s, "\r", "")
	}
	return s
}

// The logicalLineBoundsAt function returns the zero-based logical line row and [start, end) byte range for the logical line around caretByte.
func logicalLineBoundsAt(s string, caretByte int) (row int, start int, end int) {
	if s == "" {
		return 0, 0, 0
	}
	caretByte = clampInt(caretByte, 0, len(s))

	lastNL := strings.LastIndexByte(s[:caretByte], '\n')
	if lastNL < 0 {
		start = 0
	} else {
		start = lastNL + 1
		row = strings.Count(s[:start], "\n")
	}

	nextNL := strings.IndexByte(s[caretByte:], '\n')
	if nextNL < 0 {
		end = len(s)
	} else {
		end = caretByte + nextNL
	}
	return row, start, end
}

// The logicalLineBoundsByRow function returns the byte bounds of the zero-indexed logical line row in s.
//
// Lines are delimited by `\n`, and the returned end excludes the terminating newline. Empty content has one logical line at row 0, and a trailing newline creates
// a final empty logical line. ok is false for negative rows and rows beyond the content.
func logicalLineBoundsByRow(s string, row int) (start int, end int, ok bool) {
	if row < 0 {
		return 0, 0, false
	}
	if s == "" {
		if row == 0 {
			return 0, 0, true
		}
		return 0, 0, false
	}

	curRow := 0
	start = 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '\n' {
			if curRow == row {
				return start, i, true
			}
			curRow++
			start = i + 1
		}
	}
	return 0, 0, false
}

// The byteIndexAtCellCol function returns the byte offset in s for the grapheme boundary at terminal cell column col.
//
// If col falls inside a grapheme cluster, it returns the boundary before that cluster. Columns before the start return 0, and columns past the end return len(s).
func byteIndexAtCellCol(s string, col int) int {
	if s == "" || col <= 0 {
		return 0
	}
	if col < 0 {
		col = 0
	}

	iter := uni.NewGraphemeIterator(s, nil)
	w := 0
	lastEnd := 0
	for iter.Next() {
		gw := iter.TextWidth()
		if w+gw > col {
			return lastEnd
		}
		w += gw
		lastEnd = iter.End()
		if w == col {
			return lastEnd
		}
	}
	return len(s)
}

func prevGraphemeBoundary(s string, pos int) int {
	if s == "" || pos <= 0 {
		return 0
	}
	pos = clampInt(pos, 0, len(s))
	prefix := s[:pos]
	iter := uni.NewGraphemeIterator(prefix, nil)
	prev := 0
	last := 0
	for iter.Next() {
		prev = last
		last = iter.End()
	}
	return prev
}

func nextGraphemeBoundary(s string, pos int) int {
	if s == "" {
		return 0
	}
	pos = clampInt(pos, 0, len(s))
	if pos >= len(s) {
		return len(s)
	}
	suffix := s[pos:]
	iter := uni.NewGraphemeIterator(suffix, nil)
	if !iter.Next() {
		return len(s)
	}
	return pos + iter.End()
}

// The clampToGraphemeBoundary function returns a byte offset in s suitable for editing: pos is clamped to [0, len(s)] and moved back to a grapheme-cluster boundary
// when needed.
func clampToGraphemeBoundary(s string, pos int) int {
	if s == "" || pos <= 0 {
		return 0
	}
	if pos >= len(s) {
		return len(s)
	}

	iter := uni.NewGraphemeIterator(s, nil)
	lastBoundary := 0
	for iter.Next() {
		end := iter.End()
		if end == pos {
			return pos
		}
		if end > pos {
			return lastBoundary
		}
		lastBoundary = end
	}
	return lastBoundary
}

// The graphemeToken type describes one grapheme cluster for word motion and deletion.
type graphemeToken struct {
	start int           // Start is the byte index of the token's first byte.
	end   int           // End is the byte index immediately after the token.
	kind  wordTokenKind // Kind classifies the token for word motion and deletion.
}

// The wordTokenKind type classifies graphemes for word-motion and word-deletion operations.
//
// Values distinguish whitespace, separator punctuation, and other text.
type wordTokenKind uint8

const (
	wordTokSpace wordTokenKind = iota
	wordTokSeparator
	wordTokOther
)

var asciiWordSeparator = func() [128]bool {
	var t [128]bool
	// Matches SPEC.md "Word separator" set (ASCII).
	for _, b := range []byte("`~!@#$%^&*()-=+[{]}\\|;:'\",.<>/?") {
		t[b] = true
	}
	return t
}()

// The graphemeTokens function tokenizes s into grapheme clusters for word motion and deletion.
//
// Tokens are classified as Unicode whitespace, ASCII word-separator punctuation from the TextArea word-navigation rules, or other text. It returns nil for an empty
// string.
func graphemeTokens(s string) []graphemeToken {
	if s == "" {
		return nil
	}
	iter := uni.NewGraphemeIterator(s, nil)
	out := make([]graphemeToken, 0, 16)
	for iter.Next() {
		token := graphemeToken{start: iter.Start(), end: iter.End(), kind: wordTokOther}
		gr := s[token.start:token.end]
		for _, r := range gr {
			if unicode.IsSpace(r) {
				token.kind = wordTokSpace
				break
			}
		}
		if token.kind != wordTokSpace {
			for _, r := range gr {
				if r < 0x80 && asciiWordSeparator[byte(r)] {
					token.kind = wordTokSeparator
					break
				}
			}
		}
		out = append(out, token)
	}
	return out
}

// The moveWordLeftInLine function returns the byte offset of the start of the previous word unit in prefix.
//
// It first skips trailing Unicode whitespace, including newlines, then skips the preceding run of either separator punctuation or non-separator text. It returns
// 0 if no previous word unit exists.
func moveWordLeftInLine(prefix string) int {
	toks := graphemeTokens(prefix)
	if len(toks) == 0 {
		return 0
	}

	i := len(toks) - 1
	for i >= 0 && toks[i].kind == wordTokSpace {
		i--
	}
	if i < 0 {
		return 0
	}

	target := toks[i].kind
	for i >= 0 && toks[i].kind == target {
		i--
	}
	if i < 0 {
		return 0
	}
	return toks[i+1].start
}

// The moveWordRightInLine function returns the byte offset at the end of the next word-navigation unit in suffix.
//
// It skips leading Unicode whitespace, then consumes the next contiguous run of either ASCII word-separator punctuation or non-separator text. The returned offset
// is relative to suffix and falls on a grapheme boundary; it is 0 for an empty suffix.
func moveWordRightInLine(suffix string) int {
	toks := graphemeTokens(suffix)
	if len(toks) == 0 {
		return 0
	}

	i := 0
	if toks[i].kind == wordTokSpace {
		for i < len(toks) && toks[i].kind == wordTokSpace {
			i++
		}
		if i >= len(toks) {
			return len(suffix)
		}
	}
	target := toks[i].kind
	for i < len(toks) && toks[i].kind == target {
		i++
	}
	if i >= len(toks) {
		return len(suffix)
	}
	return toks[i-1].end
}

// The cutPlainStringToWidth function returns the longest grapheme-cluster prefix of plain text s whose terminal display width is at most width.
func cutPlainStringToWidth(s string, width int) string {
	if s == "" || width <= 0 {
		return ""
	}
	if uni.TextWidth(s, nil) <= width {
		return s
	}
	iter := uni.NewGraphemeIterator(s, nil)
	w := 0
	lastEnd := 0
	for iter.Next() {
		gw := iter.TextWidth()
		if w+gw > width {
			break
		}
		w += gw
		lastEnd = iter.End()
		if w == width {
			break
		}
	}
	return s[:lastEnd]
}

// The splitAtCellCol function splits plain text s around the grapheme cluster at or spanning terminal cell column col.
func splitAtCellCol(s string, col int) (before string, grapheme string, after string) {
	if s == "" {
		return "", "", ""
	}
	if col <= 0 {
		iter := uni.NewGraphemeIterator(s, nil)
		if !iter.Next() {
			return "", "", ""
		}
		return "", s[iter.Start():iter.End()], s[iter.End():]
	}

	iter := uni.NewGraphemeIterator(s, nil)
	w := 0
	for iter.Next() {
		start, end := iter.Start(), iter.End()
		gw := iter.TextWidth()
		if w == col {
			return s[:start], s[start:end], s[end:]
		}
		if w+gw > col {
			return s[:start], s[start:end], s[end:]
		}
		w += gw
	}
	return s, "", ""
}

// The clampInt function returns n constrained to the inclusive range [lo, hi]; callers should pass lo <= hi.
func clampInt(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
