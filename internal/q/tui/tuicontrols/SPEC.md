# tuicontrols

`tuicontrols` is a package that contains reusable controls for the `q/tui` package. Examples: text areas, scrollable views, tables, radio boxes, etc.

## Dependencies

- `github.com/codalotl/codalotl/internal/q/tui`
- `github.com/codalotl/codalotl/internal/q/termformat`

## KeyMap

A `KeyMap` represents a set of key events that map to semantic events. We use strings as the values of these events to minimize ceremony.

Semantic event names are application-defined; `tuicontrols` does not define a canonical set. The only requirement is that the application treats the strings as stable identifiers (e.g. `"pageup"`, `"accept"`, `"cancel"`).

```go
type KeyMap struct {
    // ...
}
func NewKeyMap() *KeyMap

// Add adds a mapping from key to semanticEvent. For example, to map Page Down to "pagedown":
// Add(tui.KeyEvent{ControlKey: tui.ControlKeyPageDown}, "pagedown")
//
// If the same key is added multiple times, the last mapping wins.
func (km *KeyMap) Add(key tui.KeyEvent, semanticEvent string)

// Process maps m to one of the semantic events added in Add.
// If m is not a key event, or doesn't match a configured mapping, "" is returned.
func (km *KeyMap) Process(m tui.Message) string
```

After a KeyMap has been configured, it can be used in an `Update(t *tui.TUI, m tui.Message)` method:

```go
func (v *SomeControl) Update(t *tui.TUI, m tui.Message) {
    switch v.keyMap.Process(m) {
    case "pageup":
        v.PageUp()
    // ...
    }
}
```

## View

A scrollable view has a fixed size (width, height), and is filled with lines of text. The view shows up to `height` lines at once. Text is clipped horizontally (no word wrapping).

Currently, there is no horizontal scrolling. We may add this at some point.

The text can contain ANSI control codes (e.g. colors). If text is clipped horizontally, we'll use `termformat.Cut` to intelligently deal with the ANSI control codes and character widths.

Vertical text clipping does NOT use any intelligent ANSI-control-code-aware logic.

Lines are delimited by `\n`. Empty content is treated as a single empty line.

At any given time, the view is scrolled to a certain point, as measured by its line offset. A line offset of 0 means the first line of the text is at the top; 1 means the first line is hidden, and the top of the view is line 2.

If the application sends input to the view, the view uses that to adjust scrolling. Only certain keys are processed (e.g. runes do nothing, but up arrow scrolls up).

Public API:

```go
// A View represents a scrollable view.
//
// Invariants:
//   - The Offset() must be in the range of [0, number of lines).
//     - Empty content counts as 1 line.
//     - Updating content may cause Offset() to be clamped to preserve this invariant.
type View struct {
    // ...
}

// NewView returns a new view of the given size.
func NewView(width, height int) *View

// Init implements tui.Model's Init.
func (v *View) Init(t *tui.TUI)

// Update implements tui.Model's Update.
//
// Default KeyMap:
//   - Page Up calls PageUp()
//   - Page Down calls PageDown()
//   - Up Arrow calls ScrollUp(1)
//   - Down Arrow calls ScrollDown(1)
//   - Home calls ScrollToTop()
//   - End calls ScrollToBottom()
func (v *View) Update(t *tui.TUI, m tui.Message)

// View implements tui.Model's View. Renders the content clipped to the view size and current offset.
//
// The rendered output always contains exactly Height() rows, but does not pad lines to Width() cells. Each rendered row contains at most
// Width() visible cells (after accounting for ANSI control codes and character widths).
func (v *View) View() string

// SetSize sets the width and height of the view to w, h. Does not affect Offset(); may affect ScrollPercent.
func (v *View) SetSize(w, h int)

// Width returns the width.
func (v *View) Width() int

// Height returns the height.
func (v *View) Height() int

// SetEmptyLineBackgroundColor sets the background color for rows that have no content.
//
// If a line does not extend all the way to v.Width() (including if a line is just a newline), this background color is NOT set. It's only
// used if there aren't enough lines in the view to fill v.Height().
//
// If v's content is "", this color is used for all lines in the view.
func (v *View) SetEmptyLineBackgroundColor(c termformat.Color)

// Offset returns the offset of the view in lines (e.g. 0 -> unscrolled; 1 -> scrolled down 1 line).
func (v *View) Offset() int

// ScrollPercent returns the scroll percent in [0, 100]. 0 means the first line is visible. 100 means the last line is fully visible.
// If the content fits entirely in the view (so both the first and last line are visible), ScrollPercent returns 0.
func (v *View) ScrollPercent() int

// ScrollUp scrolls up n lines.
func (v *View) ScrollUp(n int)

// ScrollDown scrolls down n lines.
func (v *View) ScrollDown(n int)

// PageUp scrolls up one page: ScrollUp(max(1, v.Height()-1)).
func (v *View) PageUp()

// PageDown scrolls down one page: ScrollDown(max(1, v.Height()-1)).
func (v *View) PageDown()

// ScrollToTop sets the offset to 0.
func (v *View) ScrollToTop()

// ScrollToBottom scrolls to the bottom, and normalizes the offset so that the most lines possible are visible.
//
// Concretely, the offset is set so that (when possible) the last line is shown at the bottom row of the view rather than leaving empty rows
// below the content.
func (v *View) ScrollToBottom()

// AtTop returns true if the view is showing the first line.
func (v *View) AtTop() bool

// AtBottom returns true if the view is showing the last line.
func (v *View) AtBottom() bool

// SetContent sets the content to s. This won't change Offset() unless it violates the offset invariant.
//
// To implement a chat-style view:
//   - isAtBottom := v.AtBottom()
//   - v.SetContent(existingContentsAndNewMessage)
//   - if isAtBottom { v.ScrollToBottom() }
//
// If ANSI control code styling is used:
//   - applications should apply it on a per-line basis, with each line ending in a reset state, and re-established on the following line.
//   - if background colors are used, applications must pad each line with spaces to width characters.
//   - SetEmptyLineBackgroundColor can be used for content whose height is less than Height().
func (v *View) SetContent(s string)
```

## Text Area

A text area lets users enter multi-line text, and navigate it with common keyboard navigation.

The caret is implemented as a background color. It (usually) indicates where the next character will be inserted. It does not blink. Since the next character is typically "" (user types sequentially), it looks like a chunky rectangular block after the last letter.

If BackgroundColor is set, the rendered output pads every row with spaces so the full (width x height) area is that background color.

Performance is critical.

Word wrapping details:
- The rightmost column of the text area may never have a graphic character (putting pixels in the cell). It may have a whitespace character.
    - However, the caret can be placed in this column. When it's in this column, typing a graphic character either places it in the leftmost column of the next line, or wraps the current word to the next line.
- If a string is typed that doesn't have multiple successive whitespace characters, the leftmost column of the text area (following the prompt) will never have a space character in it.
    - (The space is considered printed in the rightmost column).
- Navigation (Option/Alt+Left/Right)
      - Whitespace: treat any Unicode whitespace (spaces, tabs, newlines, etc.) as separators that are skipped over before selecting a target word unit.
      - "Word separator" characters (ASCII):
        ` ~ ! @ # $ % ^ & * ( ) - = + [ { ] } \ | ; : ' " , . < > / ?
      - Word units: after skipping whitespace, group characters into the largest contiguous run where each character is either (a) in the "word separator" set, or (b) not in that set; i.e., punctuation-runs are a unit, and non-punctuation-runs are a unit.
      - Option/Alt+Left: move to the start of the previous word unit (ignoring any whitespace immediately left of the cursor).
      - Option/Alt+Right: move to the end of the next word unit (ignoring any whitespace immediately right of the cursor).
  - Word breaks (line wrapping)
      - Primary rule: compute line-break opportunities using the Unicode Line Breaking Algorithm (UAX #14) on the plain text.
      - Hyphen tailoring: do not treat hyphen-minus - (U+002D) or soft hyphen (U+00AD) as break opportunities at the UAX #14 stage.
      - Hyphen splitting (secondary rule): allow splitting inside a word at an existing - only when it is between two alphanumeric characters; the split point is after the - (so the hyphen stays at the end of the line).
      - Empirical punctuation results (alnum–punct–alnum cases): breaks occur after `/` and `|` (also after `!`, `?`, and `}`); breaks do not occur at `.` or `,` in that context.
      - Word-joiner: U+2060 (WORD JOINER) prevents breaks between the surrounding characters.

Public API:

```go
// TextArea lets users enter multi-line text in a terminal area. The text is represented as UTF-8, interpreted as grapheme clusters, and displayed in cells (a cluster's width is given by `uni.TextWidth`).
//
// Text is wrapped at word boundaries, falling back to grapheme boundaries to prevent overflowing. This means there are logical lines (divided by \n in the contents) and display lines (what the user sees).
//
// If the number of display lines exceeds height, some text will be clipped out of view.
//
// Invariants:
//   - Stored contents must be valid UTF-8 and cannot contain ASCII control characters (bytes <= 0x1F or 0x7F), except for \n.
//   - Input is sanitized: \t is converted to 4 spaces, \r is removed, and other ASCII control characters are escaped (e.g. "\x1B").
//   - If text is clipped vertically, all height rows of the text area show display lines of contents (i.e. there are no completely blank rows). As contents change (e.g. deletions), the vertical clip/scroll is adjusted to preserve this.
type TextArea struct {
    // Placeholder is shown as text (in PlaceholderColor) if the TextArea's contents is "".
    Placeholder string

    BackgroundColor termformat.Color
    ForegroundColor termformat.Color
    PlaceholderColor termformat.Color

    // CaretColor is the color of the caret/cursor. It should be visible on the background color.
    CaretColor termformat.Color

    // Prompt is the first characters to display in the upper-left of the box. The user's first character typed would immediately follow it.
    // Subsequent lines don't have Prompt, but the user's text is aligned to the column of their first character.
    //
    // For example, if Prompt is "› ", and the user types "hello\nworld", the text area would show:
    //
    //	› hello
    //	  world
    Prompt string
}

// NewTextArea returns a new TextArea of the given size.
func NewTextArea(width, height int) *TextArea

// SetSize sets the width and height of the ta to w, h.
func (ta *TextArea) SetSize(w, h int)

// Width returns the width.
func (ta *TextArea) Width() int

// Height returns the height.
func (ta *TextArea) Height() int

// Init implements tui.Model's Init.
func (ta *TextArea) Init(t *tui.TUI)

// Update implements tui.Model's Update.
//
// Default KeyMap:
//   - Rune input and paste insert at the caret.
//   - Left/Right move by grapheme cluster.
//   - Ctrl-B/Ctrl-F alias Left/Right.
//   - Up/Down move by display lines (wrapped lines), preserving visual column.
//   - Ctrl-P/Ctrl-N alias Up/Down.
//   - Home/End move to beginning/end of the current logical line.
//   - Ctrl-A/Ctrl-E alias Home/End.
//   - Alt-Left/Alt-B move to beginning of previous word (whitespace-delimited). Newlines are treated as whitespace.
//   - Alt-Right/Alt-F move to end of next word (whitespace-delimited). Newlines are treated as whitespace.
//   - Ctrl-Home/Ctrl-End move to beginning/end of text.
//   - Backspace/Ctrl-H delete one grapheme cluster left (or newline).
//   - Delete/Ctrl-D delete one grapheme cluster right (or newline).
//   - Alt-Backspace/Ctrl-W delete the previous word (whitespace-delimited). Newlines are treated as whitespace.
//   - Alt-D/Alt-Delete delete the next word (whitespace-delimited). Newlines are treated as whitespace.
//   - Ctrl-U deletes to beginning of line; Ctrl-K deletes to end of line.
//   - Enter and Ctrl-J insert "\n".
//   - Tab inserts "\t" (sanitized to 4 spaces).
func (ta *TextArea) Update(t *tui.TUI, m tui.Message)

// View implements tui.Model's View.
//
// The rendered output always contains exactly Height() rows.
//
// If BackgroundColor is set, each rendered row is padded with spaces to exactly Width() visible cells so the full (width x height) area has that background.
func (ta *TextArea) View() string

// SetContents sets the contents of ta to s. Input is sanitized (tabs become 4 spaces; \r removed; other ASCII control characters escaped) before being stored.
func (ta *TextArea) SetContents(s string)

// Contents returns the contents of the text area (only user content - not placeholder/prompt/styles).
func (ta *TextArea) Contents() string

// ClippedDisplayContents returns the per-display-line user text that is currently displayed in the text area view (contains no \n). It reflects Contents() after wrapping and vertical clipping.
//
// This is useful as a testing hook.
func (ta *TextArea) ClippedDisplayContents() []string

// DisplayLines returns the number of display lines (accounting for wrapping), including those clipped out of view.
func (ta *TextArea) DisplayLines() int

// CaretPositionByteOffset returns the position of the caret (the location of the next inserted character) in Contents(), measured in bytes. This position must fall on a grapheme cluster boundary.
func (ta *TextArea) CaretPositionByteOffset() int

// CaretPositionCurrentLineByteOffset returns the byte index of the caret on the current logical line.
func (ta *TextArea) CaretPositionCurrentLineByteOffset() int

// CaretPositionRowCol returns the logical position of the caret based on 0-indexed rows/cols of terminal cells. Note that 0,0 is not necessarily the top-left of the TextArea itself, due to Prompt.
// The row is equivalent to the current logical-line index.
func (ta *TextArea) CaretPositionRowCol() (int, int)

// CaretDisplayPositionRowCol returns the caret position by display row/col. The row is in [0, DisplayLines()).
func (ta *TextArea) CaretDisplayPositionRowCol() (int, int)

// InsertString inserts a string at the caret position.
func (ta *TextArea) InsertString(s string)

// InsertRune inserts a rune at the caret position.
func (ta *TextArea) InsertRune(r rune)

// SetCaretPosition sets the caret position to the logical row, col, clamping invalid values. CaretPositionRowCol returns the same values, assuming they are valid.
func (ta *TextArea) SetCaretPosition(row, col int)

// MoveLeft moves the caret one grapheme cluster to the left.
func (ta *TextArea) MoveLeft()

// MoveRight moves the caret one grapheme cluster to the right.
func (ta *TextArea) MoveRight()

// MoveUp moves the caret up by one display line (wrapped line), preserving visual column.
func (ta *TextArea) MoveUp()

// MoveDown moves the caret down by one display line (wrapped line), preserving visual column.
func (ta *TextArea) MoveDown()

// MoveToBeginningOfLine moves the caret to the beginning of the current logical line.
func (ta *TextArea) MoveToBeginningOfLine()

// MoveToEndOfLine moves the caret to the end of the current logical line.
func (ta *TextArea) MoveToEndOfLine()

// MoveToBeginningOfText moves the caret to the beginning of the text.
func (ta *TextArea) MoveToBeginningOfText()

// MoveToEndOfText moves the caret to the end of the text.
func (ta *TextArea) MoveToEndOfText()

// MoveWordLeft moves the caret to the beginning of the previous word (whitespace-delimited).
//
// Newlines are treated as whitespace, so word motion can cross logical line boundaries.
func (ta *TextArea) MoveWordLeft()

// MoveWordRight moves the caret to the end of the next word (whitespace-delimited).
//
// Concretely:
//   - if the caret is in whitespace, skip whitespace then skip the next word.
//   - if the caret is in a word, skip to the end of the current word.
//
// Newlines are treated as whitespace, so word motion can cross logical line boundaries.
func (ta *TextArea) MoveWordRight()

// Deleting text

// DeleteLeft deletes one grapheme cluster to the left of the caret (or a newline).
func (ta *TextArea) DeleteLeft()

// DeleteRight deletes one grapheme cluster to the right of the caret (or a newline).
func (ta *TextArea) DeleteRight()

// DeleteWordLeft deletes the previous word (whitespace-delimited).
//
// Newlines are treated as whitespace, so word deletion can cross logical line boundaries.
func (ta *TextArea) DeleteWordLeft()

// DeleteWordRight deletes the next word (whitespace-delimited).
//
// Newlines are treated as whitespace, so word deletion can cross logical line boundaries.
func (ta *TextArea) DeleteWordRight()

// DeleteToEndOfLine deletes from the caret to the end of the current logical line.
func (ta *TextArea) DeleteToEndOfLine()

// DeleteToBeginningOfLine deletes from the caret to the beginning of the current logical line.
func (ta *TextArea) DeleteToBeginningOfLine()

```
