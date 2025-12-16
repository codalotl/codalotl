package tuicontrols

import (
	"strings"
	"unicode"

	"github.com/codalotl/codalotl/internal/q/termformat"
	"github.com/codalotl/codalotl/internal/q/tui"
	"github.com/codalotl/codalotl/internal/q/uni"
)

// TextArea lets user enter multiline text in a terminal area.
//
// Text is wrapped at word boundaries (or grapheme boundaries when needed to
// prevent overflowing). Wrapping is based on display cells (uni.TextWidth).
//
// The caret is rendered as a background color at the location where the next
// character would be inserted. It does not blink.
type TextArea struct {
	// Placeholder is shown as text (in PlaceholderColor) if the TextArea's contents is "".
	Placeholder string

	BackgroundColor  termformat.Color
	ForegroundColor  termformat.Color
	PlaceholderColor termformat.Color

	// CaretColor is the color of the caret/cursor. It should be visible on the background color.
	CaretColor termformat.Color

	// Prompt is the first characters to display in the upper-left of the box. The user's first character typed would immediately follow it.
	// Subsequent lines don't have Prompt, but the user's text is aligned to the column of their first character.
	Prompt string

	width  int
	height int

	contents  string
	caretByte int

	displayOffset int

	// preferredCol is the desired visual column (cells) used for vertical caret navigation.
	preferredCol    int
	hasPreferredCol bool

	keyMap *KeyMap

	layoutDirty    bool
	lastLayoutText string
	lastPrompt     string
	lastWidth      int

	segments []displaySegment
}

type displaySegment struct {
	text          string
	startByte     int
	endByte       int
	endsLogicalLn bool
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

	rows := make([]string, 0, ta.height)

	caretLine, caretCol := ta.effectiveCaretDisplayPos()

	for row := 0; row < ta.height; row++ {
		displayIdx := ta.displayOffset + row
		var seg displaySegment
		hasSeg := displayIdx >= 0 && displayIdx < len(ta.segments)
		if hasSeg {
			seg = ta.segments[displayIdx]
		}

		prefix := ""
		if row == 0 {
			prefix = cutPlainStringToWidth(ta.Prompt, max0(ta.width))
		} else {
			prefix = strings.Repeat(" ", minInt(prefixWidthCells, ta.width))
		}

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

// ClippedDisplayContents returns the per-display-line user text that is currently displayed in the text area view (contains no \n).
// It reflects Contents() after wrapping and vertical clipping.
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

// CaretPositionByteOffset returns the position of the caret (the location of the next inserted character) in Contents(), measured in bytes.
// This position must fall on a grapheme cluster boundary.
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

func (ta *TextArea) updatePreferredColFromCaret() {
	ta.hasPreferredCol = true
	_, col := ta.effectiveCaretDisplayPos()
	ta.preferredCol = col
}

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

func (ta *TextArea) effectiveText() string {
	if ta == nil {
		return ""
	}
	if ta.contents != "" {
		return ta.contents
	}
	return ta.Placeholder
}

func (ta *TextArea) promptWidth() int {
	if ta == nil || ta.Prompt == "" {
		return 0
	}
	return uni.TextWidth(ta.Prompt, nil)
}

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

	if ta.contents != "" {
		ta.caretByte = clampInt(ta.caretByte, 0, len(ta.contents))
		ta.caretByte = clampToGraphemeBoundary(ta.contents, ta.caretByte)
	} else {
		ta.caretByte = 0
	}
}

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

type wrapSeg struct {
	start int
	end   int
}

func wrapLineWordBoundary(line string, width int) []wrapSeg {
	if line == "" {
		return []wrapSeg{{start: 0, end: 0}}
	}
	if width <= 0 {
		return []wrapSeg{{start: 0, end: len(line)}}
	}

	type token struct {
		start   int
		end     int
		width   int
		isSpace bool
	}

	var tokens []token
	iter := uni.NewGraphemeIterator(line, nil)
	for iter.Next() {
		start, end := iter.Start(), iter.End()
		gw := iter.TextWidth()
		space := isBreakableGrapheme(line[start:end])
		if len(tokens) == 0 || tokens[len(tokens)-1].isSpace != space {
			tokens = append(tokens, token{start: start, end: end, width: gw, isSpace: space})
			continue
		}
		tokens[len(tokens)-1].end = end
		tokens[len(tokens)-1].width += gw
	}

	if len(tokens) == 0 {
		return []wrapSeg{{start: 0, end: 0}}
	}

	var out []wrapSeg
	curStart := tokens[0].start
	curWidth := 0

	flush := func(end int) {
		if end <= curStart {
			return
		}
		out = append(out, wrapSeg{start: curStart, end: end})
	}

	for _, tok := range tokens {
		if tok.width > width {
			flush(tok.start)
			out = append(out, wrapByGraphemeWidth(line[tok.start:tok.end], width, tok.start)...)
			curStart = tok.end
			curWidth = 0
			continue
		}

		if curWidth+tok.width <= width {
			curWidth += tok.width
			continue
		}

		if curWidth == 0 {
			curWidth = tok.width
			continue
		}

		flush(tok.start)
		curStart = tok.start
		curWidth = tok.width
	}

	flush(len(line))

	if len(out) == 0 {
		return []wrapSeg{{start: 0, end: 0}}
	}
	return out
}

func wrapByGraphemeWidth(s string, width int, base int) []wrapSeg {
	if s == "" {
		return []wrapSeg{{start: base, end: base}}
	}
	if width <= 0 {
		return []wrapSeg{{start: base, end: base + len(s)}}
	}

	var out []wrapSeg
	segStart := 0
	curWidth := 0

	iter := uni.NewGraphemeIterator(s, nil)
	for iter.Next() {
		gStart, gEnd := iter.Start(), iter.End()
		gw := iter.TextWidth()

		if gw > width {
			if segStart < gStart {
				out = append(out, wrapSeg{start: base + segStart, end: base + gStart})
			}
			out = append(out, wrapSeg{start: base + gStart, end: base + gEnd})
			segStart = gEnd
			curWidth = 0
			continue
		}

		if curWidth+gw > width && segStart < gStart {
			out = append(out, wrapSeg{start: base + segStart, end: base + gStart})
			segStart = gStart
			curWidth = 0
		}

		curWidth += gw
		if curWidth == width {
			out = append(out, wrapSeg{start: base + segStart, end: base + gEnd})
			segStart = gEnd
			curWidth = 0
		}
	}

	if segStart < len(s) {
		out = append(out, wrapSeg{start: base + segStart, end: base + len(s)})
	}
	if len(out) == 0 {
		out = []wrapSeg{{start: base, end: base}}
	}
	return out
}

func isBreakableGrapheme(gr string) bool {
	for _, r := range gr {
		if unicode.IsSpace(r) {
			return true
		}
	}
	return false
}

func sanitizeTextAreaInput(s string) string {
	s = termformat.Sanitize(s, 4)
	if strings.IndexByte(s, '\r') >= 0 {
		s = strings.ReplaceAll(s, "\r", "")
	}
	return s
}

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

type graphemeToken struct {
	start   int
	end     int
	isSpace bool
}

func graphemeTokens(s string) []graphemeToken {
	if s == "" {
		return nil
	}
	iter := uni.NewGraphemeIterator(s, nil)
	out := make([]graphemeToken, 0, 16)
	for iter.Next() {
		token := graphemeToken{start: iter.Start(), end: iter.End()}
		for _, r := range s[token.start:token.end] {
			if unicode.IsSpace(r) {
				token.isSpace = true
				break
			}
		}
		out = append(out, token)
	}
	return out
}

func moveWordLeftInLine(prefix string) int {
	toks := graphemeTokens(prefix)
	if len(toks) == 0 {
		return 0
	}

	i := len(toks) - 1
	for i >= 0 && toks[i].isSpace {
		i--
	}
	for i >= 0 && !toks[i].isSpace {
		i--
	}
	if i < 0 {
		return 0
	}
	return toks[i+1].start
}

func moveWordRightInLine(suffix string) int {
	toks := graphemeTokens(suffix)
	if len(toks) == 0 {
		return 0
	}

	i := 0
	for i < len(toks) && toks[i].isSpace {
		i++
	}
	if i >= len(toks) {
		return len(suffix)
	}

	for i < len(toks) && !toks[i].isSpace {
		i++
	}
	if i >= len(toks) {
		return len(suffix)
	}
	return toks[i].start
}

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
