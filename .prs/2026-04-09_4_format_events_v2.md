# PR

## User Summary (do not modify)

Problem statement:
Today, events are formatted with `internal/agentformatter`. In particular, tool calls are formatted here. This ~works, but it has weaknesses:
- adding a new tool anywhere requires a corresponding change in agentformatter. So there's this phantom coupling.
- I want to support user-defined tools. the internal/agentbuilder and it's data/config.yml can quickly define tools. In the future, end-users can have their own tool definitions, which are basically tool calls that invoke an agent with a custom tool set and custom prompts/messages.

Similarly, "subagents" are hard-coded in internal/tui (not sure about internal/noninteractive?), and are formatted specially (to print call AND result instead of replacing call with result.)

Goal:
Tools need to own the concept of "how they're displayed".

Modify

```go
type Tool interface {
	Info() ToolInfo
	Name() string
	Run(ctx context.Context, params ToolCall) ToolResult
}
```

to include `Presenter() Presenter`.

I am not sure of the exact shape of `Presenter`, other than it's probably an interface of some kind.

Note that "how a tool call/result is displayed" is a function of:
- the actual ToolCall and ToolResult
- parameters that we currently pass to agentformatter. For instance, color schemes, plain text vs ANSI formatting, TUI vs CLI, etc.

I don't think I want to burden a Presenter interface with those parameter concerns. Instead, i think i want to invent some sort of semantic presentation, that can later be applied with color schemes, ansi formatting, width concoerns, maybe HTML in the future, etc.

PR Requirements:
- fully design the Presenter interface and "semantic presentation" idea in this PR file before any implementation.
    - Verify it works by "mentally checking" against current tools
    - Verify it indicates subagents and we can remove hard-coded lists of subagents in internal/tui
- Implement
- The user behavior should be unchanged. when we merge this PR, users should not notice a difference.
    - Exception: if we can **significantly simplify** the design in some way, we can evaluate reasonable changes to UX. For instance, maybe some tools are actually weirdly inconsistent today, some designs of Presenter make them pleasantly more consistent. Who knows?

Out of scope:
- We don't necessarily need to invent a complicated new yaml language for presenter designs in config-based tool definitions. Do something pragmatic here.

## Plan

### [DONE] Planning and scoping

- Locate the current hard-coded tool-formatting seam in `internal/agentformatter`.
- Locate the current hard-coded "replace tool call with result vs print both" seam in `internal/tui`.
- Confirm this PR will be design-first: write the Presenter/semantic-presentation design in this PR file before any Go implementation.

### [DONE] PR-file design

- Fully design the Presenter interface and the semantic presentation model in this PR file before touching implementation code.
- Define how a tool call/result declares both:
  - the semantic content to render
  - whether the result replaces the call or is shown in addition to it
- Mentally check the design against the current built-in tool families:
  - file/shell/edit tools
  - package-analysis and package-update tools
  - orchestrator `implement` / `review`
  - YAML-backed command tools and YAML-backed subagent tools

### `internal/llmstream`

- Extend `llmstream.Tool` with `Presenter() Presenter`.
- Put the semantic presentation types directly in `internal/llmstream`; do not add a separate `internal/toolpresentation` package.
- Keep provider-facing tool definitions and raw `ToolCall` / `ToolResult` payloads unchanged; this work is only about local event presentation.

### `internal/agent`

- Attach the concrete `llmstream.Tool` to `EventTypeToolCall` and `EventTypeToolComplete` events.
- Delete the existing `Event.Tool string` field and replace it with `Event.Tool llmstream.Tool`.
- Add focused tests covering event emission with tool references for both root-agent and subagent tool activity.

### `internal/tools/...` and `internal/agentbuilder`

- Give built-in tools presenters that describe their own semantic display instead of relying on `internal/agentformatter` name switches.
- Give YAML-backed command and subagent tools pragmatic default presenters so user-defined tools render sensibly without inventing a new YAML presenter DSL.
- Ensure subagent-oriented tools can declare the non-replacing call/result behavior so `internal/tui` can drop its hard-coded subagent tool list.

### `internal/agentformatter`

- Refactor formatting to ask the event's tool presenter for semantic presentation data while keeping color, width, ANSI/plain-text, and TUI-vs-CLI concerns local to the formatter.
- Preserve current user-visible output unless the final Presenter design reveals a clearly simpler and still-consistent representation.
- Keep a narrow fallback path for tools/events that do not carry a tool or do not provide a presenter during the migration.

### `internal/tui` and `internal/noninteractive`

- Update `internal/tui` to use tool-owned presentation behavior, not tool-name special cases, when deciding whether a tool result replaces the prior call.
- Keep noninteractive human-readable output driven by the same semantic presentation data.
- Keep JSON output stable unless there is a deliberate reason to expose new presentation fields; if JSON changes, update replay fixtures intentionally.

### Validation

- Add or update focused tests in the packages touched by the design: `internal/agent`, `internal/agentformatter`, `internal/tui`, `internal/agentbuilder`, and tool packages that gain presenters.
- Run focused package tests during implementation, then run broader regression coverage including noninteractive replay/integration tests and any manually patched fixtures required by intentional event-shape changes.
- Manually verify that representative tool calls still look the same in both TUI and noninteractive output, especially subagent-backed tools.

## Decisions

- Semantic presentation should be structured tool-owned data, not pre-rendered text or ANSI output emitted by tools.
- The "replace tool call with result vs show both" choice should come from tool-owned presentation behavior, so built-in and YAML-defined tools can participate without hard-coded tool-name lists in `internal/tui`.

### Presenter shape

Add `Presenter() Presenter` to `llmstream.Tool`, and keep the semantic presentation data types in `internal/llmstream` too. This keeps the presentation API next to `ToolCall`, `ToolResult`, and `Tool`, and avoids inventing another package just to shuffle a few small types around.

Proposed shape:

```go
type Tool interface {
	Info() ToolInfo
	Name() string
	Presenter() Presenter
	Run(ctx context.Context, params ToolCall) ToolResult
}
```

```go
type Presenter interface {
	Present(call ToolCall, result *ToolResult) Presentation
}
```

- `result == nil` means "tool call in progress".
- `result != nil` means "tool completed".
- Presenter output must be deterministic from `ToolCall` and optional `ToolResult`.
- Presenter output is semantic only: no ANSI, no width decisions, no direct terminal colors.

Rationale:
- A single `Present(call, result)` method keeps the pairing logic in one place and lets the presenter decide whether call and completion share structure.
- The presenter sees both raw call input and raw tool result, plus `ToolResult.IsError` / `SourceErr`, which is enough to preserve current behavior.
- Keeping the semantic data model in `llmstream` keeps the import surface simple and matches where these types are actually consumed.
- Keeping presentation off of `ToolCall` / `ToolResult` themselves preserves the raw conversation/tool payloads for provider I/O, JSON mode, and tests.

### Semantic presentation model

The presenter should return a line-oriented semantic tree that is rich enough for current formatting, but intentionally much simpler than a full document renderer.

Proposed shape:

```go
package llmstream

type CompletionBehavior string

const (
	CompletionBehaviorReplace CompletionBehavior = "replace"
	CompletionBehaviorAppend  CompletionBehavior = "append"
)

type Presentation struct {
	Behavior CompletionBehavior
	Summary  Line
	Body     []Block
}

type Line struct {
	Segments []Segment
}

type Segment struct {
	Text string
	Role SegmentRole
}

type SegmentRole string

const (
	RoleNormal   SegmentRole = "normal"
	RoleAccent   SegmentRole = "accent"
	RoleAction   SegmentRole = "action"
	RoleSuccess  SegmentRole = "success"
	RoleError    SegmentRole = "error"
	RoleCode     SegmentRole = "code"
	RoleEmphasis SegmentRole = "emphasis"
)

// Block is implemented by Paragraph, Checklist, Output, and Diff.
type Block interface{ isBlock() }

// Paragraph implements Block.
type Paragraph struct {
	Lines []Line
}

// Checklist implements Block.
type Checklist struct {
	Items []ChecklistItem
}

type ChecklistItem struct {
	Status ChecklistStatus
	Line   Line
}

type ChecklistStatus string

const (
	ChecklistDone       ChecklistStatus = "done"
	ChecklistInProgress ChecklistStatus = "in_progress"
	ChecklistPending    ChecklistStatus = "pending"
)

// Output implements Block.
type Output struct {
	Lines []OutputLine
}

type OutputLine struct {
	Line Line
	Role OutputRole
}

type OutputRole string

const (
	OutputRoleNormal  OutputRole = "normal"
	OutputRoleSuccess OutputRole = "success"
	OutputRoleError   OutputRole = "error"
	OutputRoleAccent  OutputRole = "accent"
)

// Diff implements Block.
type Diff struct {
	Files []DiffFile
}

type DiffFile struct {
	Kind       DiffFileKind
	Path       string
	ToPath     string
	ReplaceAll bool
	Lines      []DiffLine
}

type DiffFileKind string

const (
	DiffFileAdd        DiffFileKind = "add"
	DiffFileDelete     DiffFileKind = "delete"
	DiffFileEdit       DiffFileKind = "edit"
	DiffFileRenameOnly DiffFileKind = "rename_only"
)

type DiffLine struct {
	Kind DiffLineKind
	Text string
}

type DiffLineKind string

const (
	DiffLineContext DiffLineKind = "context"
	DiffLineAdd     DiffLineKind = "add"
	DiffLineRemove  DiffLineKind = "remove"
	DiffLineGap     DiffLineKind = "gap"
)
```

Semantics:
- `Summary` is the first bullet line. It corresponds to the current "Running go test .", "Read foo.go", "Clarifying API ...", etc.
- `Body` holds the semantic continuation lines that today are printed under `└` / follow-on indentation.
- `Behavior` tells consumers whether a completion replaces the earlier call line or appends as a new message.

This is intentionally not a general-purpose markdown/HTML AST. It only models the structures we already render:
- one summary line
- explanatory paragraphs
- checklist items
- literal/summarized output lines
- compact file-change diffs

### Why put the tool on the event

The semantic presentation should be derived from the event's tool, not persisted into `llmstream.ToolCall` / `ToolResult` and not precomputed onto `agent.Event`.

Proposed `agent.Event` change:

```go
type Event struct {
	Agent AgentMeta
	Type  EventType
	// existing fields...

	Tool llmstream.Tool
}
```

Rationale:
- The same raw `ToolCall` and `ToolResult` should stay reusable for conversation history, provider round-tripping, JSON output, and debug tools.
- Presentation is local UI metadata, derived on demand from the actual tool implementation.
- Attaching the actual `llmstream.Tool` lets the formatter and TUI use the same presenter logic without duplicating a second event-only presentation cache.
- This avoids polluting lower-level `llmstream` content parts with TUI/CLI concerns.

### Agent behavior

`internal/agent` should resolve presenters from the actual tool instances it already owns.

Proposed dispatch behavior:
- On `EventTypeToolCall`, look up the tool by `ToolCall.Name`.
  - If found, set `ev.Tool = tool`.
  - If not found, leave `ev.Tool == nil`.
- On `EventTypeToolComplete`, use the same lookup and set the same `ev.Tool`.
- Downstream consumers derive `Presentation` from `ev.Tool.Presenter().Present(...)` when needed.

If a tool is unknown:
- behavior defaults to `CompletionBehaviorReplace`
- summary/body use the existing generic formatter semantics: `Tool <name> <raw input>` plus summarized result lines

This keeps unknown tools working and gives user-defined tools a sane baseline even before they grow custom presenters.

### Formatter responsibilities after v2

After this change, `internal/agentformatter` should stop switching on tool names to decide what a tool "means". Its job becomes:
- Resolve the event presentation from `ev.Tool` when present, then render `Presentation.Summary` and `Presentation.Body`.
- Apply palette, ANSI/plain-text styling, and wrapping.
- Apply indentation for `ev.Agent.Depth`.
- Preserve the current `└` continuation style for paragraphs/checklists/output blocks.
- Keep small compatibility fallbacks for older events or temporary migration gaps.

What stays in the formatter:
- line wrapping
- bullet/continuation glyph choices
- mapping semantic roles like `RoleAction` or `OutputRoleError` into concrete colors/styles
- special assistant-text handling unrelated to tool events, such as the existing narrow suppression of raw review JSON from subagent assistant text

What moves out of the formatter:
- tool-name switch statements
- parsing tool-specific JSON/XML payloads solely to decide human-visible tool formatting
- hard-coded knowledge that some tools are "special subagent tools"

### TUI / noninteractive responsibilities after v2

`internal/tui` should stop hard-coding tool names in `shouldReplaceToolCallWithResult`.

Instead:
- if `ev.Tool == nil`, preserve current fallback behavior
- otherwise, derive a `Presentation` from `ev.Tool.Presenter()` and use `Presentation.Behavior`
  - `replace`: completion replaces the earlier tool call message
  - `append`: completion is appended as a new message

This is the key mechanism that removes the current hard-coded list of `change_api`, `update_usage`, `clarify_public_api`, `implement`, and `review`.

`internal/noninteractive` does not currently do replace-in-place; it prints call and completion as they arrive. That behavior can stay. It should still render both through the same semantic `Presentation`, so human-readable CLI and TUI stay visually aligned.

JSON output should stay raw/stable:
- keep emitting `tool_call` with raw `tool.input`
- keep emitting `tool_complete` with raw `result.output`
- do not add presentation fields to JSON mode in this PR unless a later need is clear

### Built-in presenter strategy

Built-in Go tools should each own a small presenter implementation near the tool code.

That means:
- `shell`, `ls`, `read_file` keep the same user-visible summaries they already have
- `edit`, `write`, `delete`, and `apply_patch` should present as `Summary + Diff`, with one `DiffFile` per changed file
- `diagnostics`, `fix_lints`, `run_tests`, `run_project_tests`, `module_info`, `get_public_api`, `get_usage`, `update_plan` move their tool-specific result parsing into presenter code owned by those tools (or adjacent helper files in their packages)
- `clarify_public_api`, `update_usage`, `change_api`, `implement`, and `review` explicitly return:
  - `Behavior: CompletionBehaviorAppend`

The last point is the concrete proof that subagent-like lifecycle behavior is present in the tool-owned presentation rather than inferred from tool names.

### YAML-backed tool strategy

Do something pragmatic, not a new YAML presenter DSL.

For `yamlCommandTool`:
- default presenter should be generic-but-usable
- `Behavior: CompletionBehaviorReplace`
- summary format:
  - call: `Tool <name>`
  - completion: `Tool <name>`
- if the call input is short, include it similarly to the current generic formatter
- completion body uses summarized output lines from the raw result, just as current generic formatting does

For `yamlSubagentTool`:
- default presenter should signal subagent semantics without needing YAML syntax changes
- `Behavior: CompletionBehaviorAppend`
- summary format remains generic (`Tool <name>` plus short input when practical)
- completion body summarizes the final returned assistant text/result

This gives user-defined tools correct lifecycle behavior immediately:
- command-like tools replace
- subagent-like tools append

If we later want richer YAML presentation, that can be an additive follow-up on top of this event model.

### Mental check against current tools

Shell / file / edit tools:
- `shell`, `ls`, and `read_file` fit `Summary + Output`.
- `edit`, `write`, `delete`, and `apply_patch` need `Summary + Diff`, not generic `Output`.
- This is important for multi-file `apply_patch`: one event can render multiple file-level changes, each with its own header and diff lines.
- `DiffLineGap` covers the current rendered `⋮` between hunks; hunk headers like `@@` remain omitted from semantic presentation.
- success/failure bullet coloring is preserved by semantic roles and output roles.
- `authdomain.ErrCodeUnitPathOutside` remains representable because the presenter sees `ToolResult.SourceErr`.

Structured status tools:
- `diagnostics`, `fix_lints`, `module_info`, `run_tests`, `run_project_tests`, `get_usage`, and `get_public_api` all fit `Summary + Paragraph/Output`.
- `update_plan` naturally maps to `Summary + Paragraph + Checklist`.

Subagent-backed tools:
- `clarify_public_api`, `change_api`, `update_usage`, `implement`, and `review` all fit `Summary + Paragraph/Output` with `Behavior: Append`.
- This is enough for `internal/tui` to stop hard-coding them.
- `review` can still parse its structured JSON result into semantic blocks owned by the `review` presenter rather than by `internal/agentformatter`.

YAML-defined tools:
- command-backed tools get a no-surprises generic presenter
- subagent-backed tools get append semantics by default, which is the missing generalization we want for user-defined tools

### Compatibility notes

User-visible behavior should remain unchanged in this PR except where a presenter-backed implementation reveals an obvious simplification that is clearly better and still consistent.

Important compatibility constraints:
- keep current raw JSON mode stable
- keep details views based on raw call/result payloads, not the summarized presentation
- keep current indentation rules based on `ev.Agent.Depth`
- keep the existing narrow assistant-text suppression for raw review JSON unless this PR uncovers a cleaner general mechanism

## Review

- Not run yet.

## Summary

- Planning step only. No implementation yet.
