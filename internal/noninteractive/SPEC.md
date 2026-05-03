# noninteractive

The `noninteractive` package implements a noninteractive agent. It's the analogue of `tui`, except non-interactive and via standard CLI prints, instead of an alt screen.

## Tool Calls

The agent issues events for tool calls and tool call results. Usually running the tool is fast, so the Call -> Result takes just milliseconds (for instance, reading a file).

To avoid printing "duplicate tool calls" serially (ex: `• Read foo/bar.go`, first with a grey bullet, then a green bullet), we do the following:
- Upon getting a tool call, start a 3 second timer.
- If we get the corresponding result within 3 seconds, only print the result and cancel the timer.
- If the three seconds elapses without getting the result, print the tool call. When the result comes in, immediately print that as well.
- If visible display-only tool output arrives before the call has printed, print the call immediately, then print the output in arrival order.
- This 3 second rule only applies to tool calls/results.
- See exception below re: Concurrent Labeled Subagents.

Some tools launch subagents. If that tool's presenter implements the optional `llmstream.SubagentFinalMessagePresenter`, respect it (otherwise print plain text).
- Reason: some subagents end with raw JSON or other machine formatted text. We don't want to show JSON to the user.
- NOTE: subagents can call tools which launch subagents, etc. `llmstream.SubagentFinalMessagePresenter` format's the final message of the directly calling tool.

### Concurrent Labeled Subagents

When a subagent starts with `EventTypeStartSubagent` with a non-empty label, we display that tool call with a special Concurrent Labeled Subagents UI.
- Display parent tool call immediately if it hasn't been displayed yet (breaking 3 second rule above).
- Print a nested, labeled text like `<label>: started`
- While active, hide all other descendent events in the tool call's scope.
	- Includes deeper descendants. They route into the nearest active labeled ancestor; they do not create their own visible event stream inside that scope.
- When that labeled subagent finishes, print one label-prefixed terminal entry.
- On `EventTypeDoneSuccess`, terminal entry uses presenter-customized finalizing assistant text when available, otherwise plain finalizing assistant text, otherwise fallback text like `<label>: finished`.
- On `EventTypeError` or `EventTypeCanceled`, terminal entry must explicitly say `error` or `canceled` and include the error text when present.

## Finishing a session

Upon finishing a session, print a line like this:

`• Agent finished the turn. Tokens: input=10042 cached_input=32000 output=1043 total=43085`

## Reusable sessions

`Exec` is the one-shot entrypoint. The package may also expose a reusable session API for callers that want to run multiple top-level user messages against the same underlying agent conversation.

- Reusing a session preserves conversation history, token usage, and context-usage tracking across `SendUserMessage` calls.
- Each send still prints the same human-readable or JSON event stream shape that `Exec` uses for a one-shot run.
- Sessions own authorizer/request-loop resources and should be closed when the caller is done with them.

## JSON mode

If `Options.OutputJSON` is true, output is newline-delimited JSON: one object per line, no surrounding array.

JSON mode is a structured log, not a 1:1 dump of every internal `agent.Event`. It emits a small stable event set for external consumers.

- Tool calls do not have any delay. Emit call and result as they happen.
- Every object has a `"type"` field.
- `start` is first event.
- `done`, `error`, or `canceled` is terminal event.
- Validation errors before session start still return an error and print nothing.
- `user_message` is only the end-user prompt passed to `Exec`. Internal setup messages are not emitted as JSON `user_message` events.
- Descendant non-final assistant text streams immediately.
- Descendant finalizing assistant-text output respects optional `llmstream.SubagentFinalMessagePresenter`.
- When a tool presenter does not implement that interface, descendant finalizing assistant text is emitted as plain text.
- The internal finalizing flag on `agent.EventTypeAssistantText` is not exposed as a JSON field.

### Shared objects

- `agent`
	- `id` string
	- `depth` int
- `tool`
	- `call_id` string
	- `name` string
	- `type` string
	- `input` string. Present on `tool_call`. Usually JSON-serialized params for function tools.
- `result`
	- `output` string. Raw tool result string.
	- `is_error` bool
- `token_usage`
	- `input` int
	- `cached_input` int
	- `cache_writes` int
	- `output` int
	- `total` int

### Event types

- `start`
	- `cwd` string
	- `package_path` string. `""` when not in package mode.
	- `model_id` string
- `user_message`
	- `text` string
- `assistant_text`
	- `agent`
	- `content` string
- `assistant_reasoning`
	- `agent`
	- `content` string
- `tool_call`
	- `agent`
	- `tool`
- `tool_complete`
	- `agent`
	- `tool`
	- `result`
- `tool_output`
	- `agent`
	- `tool`
	- `content` string
- `permission`
	- `prompt` string
	- `decision` string. `"allow"` or `"disallow"`.
	- `automatic` bool
- `warning`
	- `agent`
	- `message` string
- `retry`
	- `agent`
	- `message` string
- `error`
	- `agent`
	- `message` string
- `canceled`
	- `agent`
	- `message` string
- `done`
	- `token_usage`
	- `ideal_token_usage` optional `token_usage`. Only when ideal-caching reporting is enabled.

Example Output:

```json
{"type": "start", "cwd": "/some/path", "package_path": "internal/somepkg", "model_id": "gpt-5.5-high"}
{"type": "user_message", "text": "fix failing test"}
{"type": "tool_call", "agent": {"id": "root", "depth": 0}, "tool": {"call_id": "call_1", "name": "read_file", "type": "function_call", "input": "{\"path\":\"foo.go\"}"}}
{"type": "tool_complete", "agent": {"id": "root", "depth": 0}, "tool": {"call_id": "call_1", "name": "read_file", "type": "function_call"}, "result": {"output": "package foo\n...", "is_error": false}}
{"type": "assistant_text", "agent": {"id": "root", "depth": 0}, "content": "I found the issue..."}
{"type": "done", "token_usage": {"input": 123, "cached_input": 45, "cache_writes": 0, "output": 67, "total": 235}}
```

## Public API

```go
// IsPrinted returns true if err has already been printed to the screen.
func IsPrinted(err error) bool

type Options struct {
	// working directory / sandbox dir. If "", uses os.Getwd()
	CWD string

	// PackagePath sets package mode with the path to a package vs CWD. If "", does not use package mode. PackagePath can be any filesystem path (ex: "."; "/foo/bar";
	// "foo/bar"; "./foo/bar"). It must be rooted inside of CWD.
	PackagePath string

	// SlashCommand applies a TUI-style slash command at session start. Supported values are "", "orchestrate", and "/orchestrate".
	//
	// "orchestrate" and "/orchestrate" start a fresh generic-mode orchestrator session around the built-in orchestrator agent, matching the TUI's `/orchestrate` behavior.
	// PackagePath is ignored for orchestrate mode.
	SlashCommand string

	ModelID   llmmodel.ModelID // ModelID selects the LLM model for this run. If empty, uses the existing default model behavior.
	LintSteps []lints.Step     // LintSteps controls which lint steps the agent runs.
	AutoYes   bool             // Answers 'Yes' to any permission check. If false, we answer 'No' to any permission check. The end-user is never asked.

	// NoFormatting=true means any prints do NOT use colors or other ANSI control codes to format. Only outputs plain text. Otherwise, we default to the color scheme
	// of the terminal and print colorized/formatted text.
	NoFormatting bool

	// OutputJSON outputs newline-delimited JSON instead of human-readable text. If set, NoFormatting is ignored.
	OutputJSON bool

	// If Out != nil, any prints we do will use Out; otherwise will use Stdout. If Exec encounters errors during its run (eg: cannot talk to LLM; cannot write file),
	// we'd still just print to Out (instead of something like Stderr).
	Out io.Writer
}

type Result struct {
	TerminalEventType   agent.EventType      // Terminal event for this step's run.
	FinalAssistantText  string               // Final top-level finalizing assistant text emitted for this step.
	TokenUsage          llmstream.TokenUsage // Cumulative session token usage after this step, not a per-step delta.
	ContextUsagePercent int                  // Overall session context usage after this step, based on the latest assistant turn.
}

type Session struct{}

// NewSession validates opts, prepares the underlying agent, and returns a reusable noninteractive session.
func NewSession(opts Options) (*Session, error)

// Close releases any resources owned by the session. It is safe to call multiple times.
func (s *Session) Close() error

// SendUserMessage runs one top-level user message on an existing session, writes output according to the session options, and returns structured step metadata.
func (s *Session) SendUserMessage(ctx context.Context, userPrompt string) (Result, error)

// Exec runs the agent with prompt and opts. It prints messages, tool calls, and so on to the screen.
//
// `userPrompt` is the initial end-user message. It is required unless `Options.SlashCommand` starts a session that can run without an initial message.
//
// If there's any validation error (anything before the agent actually starts), an error is returned and nothing is nothing is printed. If there's an unhandled error
// and the agent cannot complete its run (ex: cannot talk to LLM, even after retries), a message may be printed AND returned via err. Callers can use IsPrinted to
// determine if an error has already been printed. Finally, note that many "errors" happen in the course of typical agent runs. For instance, the agent will ask
// to read non-existant files; shell commands will fail; etc. These do not typically constitute errors worthy of being returned (instead, the LLM is just told a
// file doesn't exist).
func Exec(userPrompt string, opts Options) error
```
