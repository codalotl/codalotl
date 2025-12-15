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

## Scrollable View

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
