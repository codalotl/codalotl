# agentformatter

agentformatter formats events from agents for insertion into a fixed-width TUI, or for printing in a normal stdout-based CLI.

## Notes

- Tabs are converted to spaces (default 4 spaces per tab).
- If an event contains the ESC byte or similar control codes, they are escaped. Note that they may be escaped in JSON, but when the JSON is parsed, they become a control byte again.

## Format

- Colors (assuming !PlainText):
    - Normal: foreground color
    - Accent: dimly accented foreground color. Example: if FG=white and BG=black, Accent would be a grey. Used for lower-important text and backtick wrapped text (ex: agent bullets; file references)
    - Green: used for successes and additions in diffs
    - Red: used for errors and deletions in diffs
    - Colorful: used for tool calls; more important calls-to-action (for instance). If FG=white and BG=black, Colorful might be a light blue.
- Agent bullets (`•`) are typically Accent-colored; completed tools are Green vs Red. (NOTE: not to be confused with bullets WITHIN messages - aka markdown bullets - which are just normal `-`).
- The tool response indicator `└` is Accent.
- Text within AssistantText will convert backtick-wrapped text to colorized Accent text, dropping the backticks.
- Coloring (ex: 256 color; true color) must be converted to the terminal's color profile (assuming !PlainText).

## SubAgent Events

The event either comes from a root agent (ev.Agent.Depth == 0) or a SubAgent (ev.Agent.Depth > 0). The rest of this document assumes events are coming from root agents (i.e., the example formatting
shows what an event looks like when formatted as a root agent). But if the event comes from a SubAgent, add 2 spaces of indentation **per ev.Agent.Depth**. Here's an example of a `run_tests` tool call
when it's from a SubAgent of Depth=1:

```
  • Ran Tests some/pkg -v -run Some
    └ $ go test ./codeai/gocodecontext
      ok  	axi/some/pkg	0.374s
```

Notice how everything just has 2 spaces of leading indentation.

## User Message Events

The agent can emit events when a user message is queued while a turn is already running (see `agent.QueueUserMessage`).

### EventTypeUserMessageQueued

Render the queued user message as a user-authored line (not an agent bullet), prefixed with a leading space and a chevron:

```
 › this is a message (queued)
```

- ` (queued)` is appended to the text.
- The chevron (`›`) is Accent-colored.
- The message text is Normal-colored.
- In TUI width mode, wrap lines to the available width. Continuation lines are indented to align with the message text (3 spaces: `   `).

### EventTypeQueuedUserMessageSent

- Prints the same as EventTypeUserMessageQueued, except with no `(queued)` suffix.

### EventTypeAssistantText

Example:

```
• I'll explain that Codex uses the full pulldown-cmark Markdown parser combined with targeted regex rewriting for
  specific citation patterns like 【F:...】, which get converted into clickable links. Inline code marked by backticks
  is only styled as dim text, not linkified, and the linking mechanics rely on actual Markdown links or those citations
  rather than scanning inline code for paths. I'll reference key code locations that handle these steps for clarity.
```

Notes:
- Notice the leading bullet and space. Future lines are indented two spaces.
- The indentation only applies for TUI-based formatting. Stdout-based formatting is `• I'll explain ... clarity.` (single line).

Markdown bullets are rendered as:

```
• - Codex streams every assistant delta through MarkdownStreamCollector, which flushes complete lines only after a
    newline and renders them by calling append_markdown (codex-rs/tui/src/markdown_stream.rs:38-86, codex-rs/tui/src/
    markdown.rs:32-60).
  - append_markdown delegates to render_markdown_text_with_citations, which runs the text through pulldown_cmark::Parser
    with the usual CommonMark options, so the UI is backed by a full Markdown parser rather than ad‑hoc regexes (codex-
    rs/tui/src/markdown_render.rs:21-118).
```

### EventTypeAssistantReasoning

An event's ReasoningContent.Content will often look like:

```
**Answering a simple question**

I'm looking at a straightforward question: the capital of France is Paris. It's a well-known fact, and I want to keep it brief and to the point. Paris is known for its history, art, and culture, so it might be nice to add just a hint of that charm. But mostly, I'll aim to focus on delivering a clear and direct answer, ensuring the user gets what they're looking for without any extra fluff.
```

or

```
**Answering a simple question**
```

If it follows this general format (leading **Some Summary Text** line, or only that line), then we format it as:

```
• Answering a simple question
```

- Text (`Answering a simple question` above) is in italics.

If it follows some other format, just render the whole thing as if it were an EventTypeAssistantText, but print it all in italics.

### EventTypeToolComplete - error messages

Unless otherwise stated, a command that results in an error will show an error message in red text below the command. Example:

```
• Ran go test .
  └ Error: go command not found.
```

Note: this case doesn't apply to commands that run successfully but have, for instance, failing tests. This case is for when the tool itself has an error.

If the underlying error is `errors.Is(e.ToolResult.SourceErr, authdomain.ErrCodeUnitPathOutside)`, we format the message on one line like:

```
• Silly LLM tried read_file on some/file.go outside of package.
```

- Bullet is Red. Everything else is Accent.
- It is fine to just print the tool name (ex: read_file, ls, apply_patch).
- The only data displayed is the tool name, and if present, the `path` argument. If no path, it reads (for instance): `Silly LLM tried apply_patch outside of package`.

### Presenter-driven tool formatting

- If `Event.Tool` exposes a non-nil `Presenter`, formatter must render from that semantic presentation.
- Do not keep parallel per-tool formatting specs here once a tool package owns its presentation.
- Presenter summaries still use the tool event bullet/status behavior from this package: Accent while running, Green/Red on completion.
- If a presenter sets an explicit `Status`, use that status for completion bullet color instead of inferring from the raw tool result.
- If a presenter returns `Body` blocks, render them beneath the summary using the same `└`/continuation structure used elsewhere in this package.
- `Paragraph` blocks render their lines in order using line/segment roles, sharing the same body indentation rules.
- `Checklist` blocks render one item per line:
    - If `Overview` is non-empty, render it first as a normal body line
    - Completed items use `✔`
    - Pending and in-progress items use `□`
    - In-progress items add emphasis on top of any segment roles
- `Diff` blocks render using the shared diff rules below.
- For `Output` blocks, print the provided visible lines in order, and if `OmittedLineCount > 0`, append `… +N lines`.
- Shared tool-error rendering still wins over presenter body content when the tool result is an error, unless `ErrorBehavior` is presenter-owned.

### EventTypeToolOutput

Tool output is display-only output for a running tool. Render it as nested tool output, not assistant text.

Example:

```
• Running codalotl docs add --public-only internal/lints
  • data:
    - field1: value
    - field2: value
```

- Each `EventTypeToolOutput` renders as one nested output message; embedded newlines are continuation lines under the same nested bullet.
- Output text is Normal under an Accent bullet.
- Sanitize tabs/control bytes using the same rules as other text events.
- Subagent depth indentation still applies outside the nested output indentation.

#### Rendering `Diff` blocks

`Diff` blocks are the shared presentation for file edits. They are rendered beneath the presenter summary line and are not specific to any one tool.

- For presenter-owned `Diff` bodies, `Summary.Segments` must be nil. The formatter derives the visible summary/header from the first diff edit instead of from `Summary`.

Example:

```
• Edit some/file.go
     - old line
     + new line
```

- Use change verbs like `Add`, `Delete`, `Rename`, and `Edit` based on the semantic diff summary.
- If a diff edit is marked `ReplaceAll`, append ` (replace all)` to the first-line header.
- Line numbers are not shown.
- `⋮` is accent-colored.
- Context lines (` `) are normal; `+` lines are green; `-` lines are red.

Delete example:

```
• Delete some/file.go
```

Rename example (no line changes):

```
• Rename some/file.go → some/other.go
```

- `→` is accent.

Rename example (with line changes):

```
• Edit some/file.go → some/other.go
     - old line
     + new line
```

If a line exceeds the tuiWidth in TUI width mode, wrap it:

```
• Edit some/file.go
     +This line is very long. It will wrap eventua
       lly.
```

### EventTypeToolCall and EventTypeToolComplete - other unhandled tools

If a tool isn't especially handled, here's example output:

```
• Tool some_tool {"path": "path/to/file.go"}
  └ {
      "field": "value"
    }
```

### EventTypeError, EventTypeWarning, EventTypeRetry, EventTypeCanceled

Examples, relating to these style of events:

```
• Error: some error has occurred.
• Warning: some warning has occurred.
• Retry: transient error.
• Canceled: deadline exceeded.
```

- Bullet indicates severity:
    - Warning: Accent
    - Retry: Colorful
    - Canceled/Error: Red

### EventTypeAssistantTurnComplete

Print a single line summarizing the turn usage:

`• Turn complete: finish=<finishReason> input=<n> output=<n> reasoning=<n> cached_input=<n>`

## Dependencies

- Uses codeai/llmstream
- Uses codeai/agent
- Markdown parser from github.com/yuin/goldmark
- Uses q/termformat for terminal formatting / ANSI codes.
- Cell width calculation uses q/termformat and q/uni.

## Public API

```go
const MinTerminalWidth = 30

type Formatter interface {
	// FormatEvent returns the content to print in a chat window or stdout-based CLI.
	//
	// If terminalWidth > MinTerminalWidth, newlines will be inserted precisely so that nothing wraps. Otherwise, paragraphs will not contain newlines and the caller
	// can wrap themselves or insert the text in a container that can deal with long strings.
	FormatEvent(e agent.Event, terminalWidth int) string
}

// Config controls the terminal colorization options. We need to know the intended bg/fg, so we can create other colors that are consistent. For instance, if we
// want to colorize backtick-wrapped paths/identifiers/code differently, can modify ForegroundColor to be closer to BackgroundColor.
type Config struct {
	PlainText       bool             // true: disable colors and ANSI escape characters (bold, italics, etc).
	BackgroundColor termformat.Color // the terminal's background color. If nil, uses termformat.DefaultFBBGColor.
	ForegroundColor termformat.Color // the terminal's foreground color. If nil, uses termformat.DefaultFBBGColor.
	AccentColor     termformat.Color // If nil, derived from fg/bg and downsampled to the detected color profile.
	ColorfulColor   termformat.Color // If nil, derived from fg/bg and downsampled to the detected color profile.
	SuccessColor    termformat.Color // If nil, uses a default green suitable for terminals, downsampled to the detected color profile.
	ErrorColor      termformat.Color // If nil, uses a default red suitable for terminals, downsampled to the detected color profile.
}

// NewTUIFormatter creates a new Formatter configured for chat/TUI rendering.
//
// If ForegroundColor/BackgroundColor in c aren't passed in, they're determined by sending OSC codes to the terminal. The terminal can't be in Alt mode at this time.
func NewTUIFormatter(c Config) Formatter
```
