# agentformatter

agentformatter formats events from agents for insertion into a fixed-width TUI, or for printing in a normal stdout-based CLI.

## Notes

- Tabs are converted to spaces (default 4 spaces per tab).
- If an event contains the ESC byte or similar control codes, they must be escaped are escaped. Note that they may be escaped in JSON, but when the JSON is parsed, they become a control byte again.

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
- All coloring must be done with ANSI 256-color codes, not true color.

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

### EventTypeToolCall - shell commands

This event happens when a tool call is **initiated** - as such, the tool call is in progress.

Default shell call formatting will follow this format:

```
• Running go test .
```

- Bullet is accent
- Running is Bold, Colorful.
- The command itself (`go test .` above) is normal

### EventTypeToolComplete - shell commands

Complete tool calls have a call and a result.

```
• Ran go test .
  └ ok      axi/q/termformat    0.002s
    ?       axi/q/termformat/cmd    [no test files]
```

- Status is indicated by bullet color.
- `└` is Accent.
- Ran is Bold, Colorful.
- The command itself is normal.
- If there is no output, it is just one line. Ex: `• Ran gofmt -w .`.
- If the output is more than 5 lines, show the first five, followed by `… +13 lines` (for example).

### EventTypeToolComplete - error messages

Unless otherwise stated, a command that results in an error will show an error message in red text below the command. Example:

```
• Ran go test .
  └ Error: go command not found.
```

Note: this case doesn't apply to commands that run successfully but have, for instance, failing tests. This case is for when the tool itself has an error.

### EventTypeToolCall and EventTypeToolComplete - ls

```
• List some/path 
```

- Bullet indicates status (Accent -> Red or Green). Green shows no output.
- List is Bold, Colorful. some/path is normal.

### EventTypeToolCall and EventTypeToolComplete - read_file

```
• Read some/file.go
```

- Bullet indicates status (Accent -> Red or Green). Green shows no output.
- Read is Bold. some/file.go is normal.

### EventTypeToolCall and EventTypeToolComplete - update_plan

```
• Update Plan
  └ Need to align CodeUnit authorizer with updated SPEC behavior for read-only restrictions and adjust tests accordingly.
    ✔ Inspect SPEC changes and current CodeUnit authorizer implementation
    □ Update codeunit authorizer logic to apply read restrictions only to read_file tool and keep write restrictions for all tools
    □ Revise tests to cover new behavior and run go test for package
```

- Update Plan is Bold, Colorful.
- The `└` and the message afterwards is always Accent.
- If the message is blank, start printing the bullets right after the `└` (the └ shouldn't be on a line all by itself).
- The FIRST uncompleted todo is Colorful and bold.
- All other bulleted lines are Accent (all completed todos are always Accent; uncompleted todos are Accent unless they're the first uncompleted todo).

### EventTypeToolCall and EventTypeToolComplete - get_public_api

```
• Read Public API axi/some/pkg
```

- Read Public API is Bold, Colorful.
- axi/some/pkg is normal

```
• Read Public API axi/some/pkg
  └ SomeType, DoThingFunc
```

- If get_public_api is called with specific identifiers, list them underneath in Accent, comma separated.

### EventTypeToolCall and EventTypeToolComplete - clarify_public_api

The EventTypeToolCall looks like this:

```
• Clarifying someIdentifier in /path/to/something
  └ What does someIdentifier return in its second parameter? Give an example.
```

- Clarifying API is Bold, Colorful
- someIdentifier is normal; /path/to/something is normal
- "in" is Accent.
- The question is Accent.

The EventTypeToolComplete looks like this:

```
• Clarified someIdentifier in /path/to/something
  └ The someIdentifier returns a description in the 2nd parameter. For example, ...
```

- Clarified API is Bold, Colorful
- someIdentifier is normal; /path/to/something is normal
- "in" is Accent.
- The answer is Accent.
- The question is not repeated.

### EventTypeToolCall and EventTypeToolComplete - update_usage

The EventTypeToolCall looks like this:

```
• Updating Usage in some/path, other/path, third/path (4 more)
  └ Update the callsites to conform to this new API...
```

- Updating Usage is Bold, Colorful
- "in" is Accent.
- some/path (et al) is normal; (4 more) is Accent
- If there'e more than 3 paths, add a parenthetical for how many more there are. Otherwise, don't show the parenthetical.
- The instructions is Accent.

The EventTypeToolComplete looks like this:

```
• Updated Usage in some/path, other/path, third/path (4 more)
```

- NOTE: no body (i.e., no └ below the line). Might change later.

### EventTypeToolCall and EventTypeToolComplete - change_api

The EventTypeToolCall looks like this:

```
• Changing API in axi/some/pkg
  └ Add a new method SomeType.DoThing so downstream callers can avoid duplicating this logic...
```

- Changing API is Bold, Colorful
- "in" is Accent.
- axi/some/pkg is normal
- The instructions is Accent.

The EventTypeToolComplete looks like this:

```
• Changed API in axi/some/pkg
```

- NOTE: no body (i.e., no └ below the line). Might change later.

### EventTypeToolCall and EventTypeToolComplete - get_usage

```
• Read Usage axi/some/pkg *SomeType.SomeFunc
  └ Found 12 results.
```
- The number of results is determined by counting the number of matches of this regexp: /^\d+:/ (beginning of line, number followed by colon)

### EventTypeToolCall and EventTypeToolComplete - module_info

The EventTypeToolCall looks like this:

```
• Read Module Info
```

- Read Module Info is Bold, Colorful.

If either option is provided and non-zero-value, print a single Accent line underneath with the selected options:

```
• Read Module Info
  └ Search: agentformatter; Deps: true
```

- The `└` and the entire options line are Accent.
- Only show a Search if it's present and non empty. Only show Deps if it's true.
- EventTypeToolComplete is the same as the Call (except it resolves to a status).
- Bullet indicates status (Green on success; Red on error).

### EventTypeToolCall and EventTypeToolComplete - run_tests

```
• Ran Tests some/pkg -v -run Some
  └ $ go test ./codeai/gocodecontext
    ok  	axi/some/pkg	0.374s
```

- Ran Tests API is Bold, Colorful.
- some/pkg is normal
- Bullet is Red or Green based on test outcome
- The output is stripped of the XML tag

### EventTypeToolCall and EventTypeToolComplete - apply_patch

Example:

```
• Edit some/file.go
     18  	"axi/codeai/llmstream"
     19 +	"axi/codeai/prompt"
     20  	"axi/codeai/tools/coretools"
        ⋮
     23
     24 +const agentName = "CodAgent"
     25 +
     26  func main() {
        ⋮
     47
     45 -	systemPrompt, err := loadSystemPrompt(sandboxDir)
     46 -	if err != nil {
     47 -		return err
     48 -	}
     48 +	modelID := llmstream.ModelID("gpt-5")
     49 +	systemPrompt := prompt.GetFullPrompt(agentName, modelID)
     50
     50 -	agentInstance, err := agent.NewAgent(llmstream.ModelID("gpt-5"), systemPrompt, buildTools(sandboxDir))
     51 +	agentInstance, err := agent.NewAgent(modelID, systemPrompt, buildTools(sandboxDir))
     52  	if err != nil {
        ⋮
    117  	return filepath.Clean(cwd), nil
    117 -}
    118 -
    119 -func loadSystemPrompt(repoRoot string) (string, error) {
    120 -	path := filepath.Join(repoRoot, "codeai", "agent-prototype", "generic-prompt.md")
    121 -	data, err := os.ReadFile(path)
    122 -	if err != nil {
    123 -		return "", err
    124 -	}
    125 -	return string(data), nil
    118  }
```

- No hunks anchors are shown (eg, `@@ func SomeAnchor() {`).
- Line numbers and `⋮` are accent-colored.
- Context lines (` `) are normal; `+` lines are green; `-` lines are red.

Delete example:

```
• Delete some/file.go
```

Rename example (no hunks are changed):
```
• Rename some/file.go → some/other.go
```

- `→` is accent.

Rename example (hunks are changed):
```
• Edit some/file.go → some/other.go
     18  	"axi/codeai/llmstream"
     19 +	"axi/codeai/prompt"
     20  	"axi/codeai/tools/coretools"
```

If a line exceeds the tuiWidth in TUI width mode, it will be wrapped:

```
• Edit some/file.go
     24 +const description = "This line is very long. It will wrap eventua
         lly."
     25 +
     26  func main() {
```

### EventTypeToolCall and EventTypeToolComplete - other unhandled tools

If a tool isn't especially handled, here's example output:

```
• Tool some_tool {"path": "path/to/file.go"}
  └ {
      "field": "value"
    }
```

### EventTypeError, EventTypeWarning, EventTypeCanceled

Examples, relating to these style of events:

```
• Error: some error has occurred.
• Warning: some warning has occurred.
• Canceled: deadline exceeded.
```

- In the above examples, the first word is bold (ex: `Error`).
- The colon and onwards is nonbold.
- All text is red.

### EventTypeAssistantTurnComplete

- Not useful to print (returns "")

## Dependencies

- Uses codeai/llmstream
- Uses codeai/agent
- Markdown parser from github.com/yuin/goldmark
- Uses q/termformat for terminal formatting / ANSI codes.
- Cell width calculation uses q/termformat and q/uni.

## Public Interface

```go
const MinTerminalWidth = 30

type Formatter interface {
	// FormatEvent returns the content to print in a chat window or stdout-based CLI.
	//
	// If terminalWidth > MinTerminalWidth (aka TUI formatting), newlines will be inserted precisely so that nothing wraps. Otherwise, paragraphs will not contain newlines and the caller can wrap themselves or insert the text in a container
	// that can deal with long strings.
	FormatEvent(e agent.Event, terminalWidth int) string
}

// Config controls the terminal colorization options. We need to know the intended bg/fg, so we can create other colors that are consistent.
// For instance, if we want to colorize backtick-wrapped paths/identifiers/code different, can modify ForegroundColor to be closer to BackgroundColor.
type Config struct {
    PlainText bool // true: disable colors and ANSI escape characters (bold, italics, etc).
    BackgroundColor termformat.Color // the terminal's background color. If nil, uses termformat.DefaultFBBGColor.
    ForegroundColor termformat.Color // the terminal's foreground color. If nil, uses termformat.DefaultFBBGColor.
    AccentColor termformat.Color // If nil, derived from fg/bg and downsampled to the detected color profile.
    ColorfulColor termformat.Color // If nil, derived from fg/bg and downsampled to the detected color profile.
}

// creates a new Formatter
func NewTUIFormatter(c Config) Formatter
```
