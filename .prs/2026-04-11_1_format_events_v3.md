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

to include `Presenter() Presenter`:

```go
// A Presenter can present a tool call (and optional result) in a "semantic" way - a tree representation that can be further styled for different modalities.
// As an analogy, it's the HTML (but not the CSS) of underlying data.
type Presenter interface {
    // Present presents call and result in a semantic way (no width decisions, no assumptions about ANSI terminals, colors). Result == nil means the tool call is in progress; otherwise its complete.
	Present(call ToolCall, result *ToolResult) Presentation
}
```

Partial implementation:

```go
// Completion behavior indicates what happens when the tool completes. For instance, imagine a TUI:
//   -  With Replace, the tool call presentation is replaced by the result presentation (ideal for quick and/or atomic operations like reading a file).
//   -  With Append, the tool call is displayed. When the result comes in, it should also be displayed (ideal for subagents, which are long-lived and themselves emit tool calls).
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
```

This PR will be implemented in phases.

Phase 0:
- Add the Presenter interface to llmstream and tool definition.
- Each Tool in the codebase can just return a nil presenter

Phase 1:
- Refine Presenter interface.
- Go through each tool in agentformatter and make sure it's representable by a Presentation.
    - For instance, we probably need type Diff struct
    - need a way to display command output
    - etc
- Evaluate whehter we need `Body     []Block` vs just `Body     Block`
- Don't edit the SPEC.md of llmstream unless we change existing stuff there (ex: if we change Body to non-array). If you add a Diff type for instance, don't add it to spec.md. The SPEC.md isn't meant to be exhaustive.

Phase 2:
- Update internal/agent: change Event's `Tool             string` to `Tool             llmstream.Tool`
- Update callsites

Phase 3:
- Use this for a single tool. Let's just pick read_file.
- The read_file tool should define its presenter.
- update agentformatter as follows:
    - Remove explicit handling of read_file
    - If the event's tool has a presenter, use that.

Phase 4:
- In this phase, we go tool by tool, starting with tools in core tools.
- Move the tool formatting specification from agentformatter to the tool packge's SPEC (i did an example with read_file).
- Each tool in coretools needs a separate implementation commit
- Then a separate commmit in agentformatter to remove explicit support
- A tool is migrated to the new system if:
	- it is NOT referenced explicitely in agentformatter (whether it be SPEC.md or implementation)
		- HOWEVER, KEEP tests for tools in agentformatter, assuming they're still reasonable. We can deal with them later. For now, it's nice to have extra validation.
	- it IS specified in a corresponding tools/* package's SPEC.md
	- The presenter is implemented on the tool, which MATCHES the impl of the original

Phase 5: tbd, don't plan here yet

## Plan

### [DONE] Phase 0 - internal/llmstream

- Add the phase-0 presentation types and extend `Tool` with `Presenter() Presenter`.
- Keep phase-0 behavior inert: `nil` presenters are allowed and should preserve current tool execution semantics.
- Update the package spec for the public API change and add focused coverage around the expanded tool contract.

### [DONE] Phase 0 - Tool implementations and test doubles

- Update concrete tool implementations in `internal/tools/coretools`, `internal/tools/exttools`, `internal/tools/pkgtools`, and dynamic tools in `internal/agentbuilder` to satisfy the new interface by returning `nil`.
- Update test helper tools and stubs across affected packages so the repo builds and tests with the new interface.

### [DONE] Phase 0 - Event consumers

- Keep `internal/agentformatter`, `internal/tui`, and `internal/noninteractive` behavior unchanged in phase 0; they should compile against the new interface but not consume presenters yet.
- Verify the phase-0 plumbing and unchanged consumers with `go test ./...`.

### [DONE] Phase 1 - internal/llmstream

- Refine the presentation tree so every current tool shape in `internal/agentformatter` is representable semantically.
- Keep `Presentation.Body` as `[]Block` unless implementation reality shows a blocker; add missing block/value types such as diff- and output-oriented structures.
- Add focused tests around new presentation primitives and any parsing/rendering assumptions moved into shared types.

### [DONE] Phase 2 - internal/agent

- Change `agent.Event.Tool` from tool name string to `llmstream.Tool`, and have agent-emitted tool events carry the concrete tool object that produced the call/result.
- Update `internal/agent/SPEC.md` and focused agent tests for the public API change while keeping non-tool events unchanged.

### [DONE] Phase 2 - Event consumers and helpers

- Update `internal/agentformatter`, `internal/tui`, and `internal/noninteractive` to compile against `Event.Tool llmstream.Tool` while preserving current human-readable output, JSON output, and timer/replace behavior.
- Update fakes, fixtures, and targeted tests that currently construct tool events with only a tool name string.

### [DONE] Phase 3 - internal/tools/coretools

- Implement a `Presenter` for `read_file` in `internal/tools/coretools`, keeping the underlying tool result payload unchanged.
- Add focused `internal/tools/coretools` coverage for the new presenter shape and replace behavior.

### [DONE] Phase 3 - internal/agentformatter

- Update `internal/agentformatter` to prefer tool-owned presentation when `Event.Tool` exposes a non-nil presenter, and remove the dedicated `read_file` formatter path.
- Preserve existing replace-style completion semantics for `read_file`; defer broader presenter adoption and append-style subagent behavior to later phases.

### [DONE] Phase 3 - validation

- Add or update focused tests in `internal/tools/coretools`, `internal/agentformatter`, `internal/tui`, and `internal/noninteractive` as needed to cover the new event payload and read-file presenter path.
- Run targeted package tests while implementing each phase; keep a final broader test pass for after the consumer and formatter changes settle.

### [DONE] Phase 4 - internal/tools/coretools: ls

- Move `ls` formatting contract into `internal/tools/coretools/SPEC.md`.
- Add a replace-style `Presenter` for `ls` that preserves the current `List <path>` summary and current formatter-owned error handling.
- Add focused `internal/tools/coretools` coverage for the presenter shape and fallback behavior.

### [DONE] Phase 4 - internal/tools/coretools: shell

- Move `shell` formatting contract into `internal/tools/coretools/SPEC.md`.
- Add a presenter that renders `Running <command>` while in progress and `Ran <command>` on completion, while preserving current shared output/error handling.
- Add focused `internal/tools/coretools` coverage for command extraction and fallback behavior.

### [DONE] Phase 4 - internal/tools/coretools: skill_shell

- Move `skill_shell` formatting contract into `internal/tools/coretools/SPEC.md`.
- Add a presenter that matches `shell` summary behavior and intentionally omits skill metadata from the summary line.
- Add focused `internal/tools/coretools` coverage for command extraction and fallback behavior.

### Phase 4 - internal/agentformatter: presenter bodies

- Render presenter-provided body blocks for replace-style tool completions instead of only showing the summary line.
- Preserve existing bullet/status behavior and shared output formatting conventions so `shell`/`skill_shell` show command output via their presenter bodies.
- Add focused formatter coverage for presenter-driven completion bodies in both success and error cases.

### Phase 4 - internal/tools/coretools: remaining core tools

- Migrate `update_plan`, `apply_patch`, `edit`, `write`, and `delete` one tool at a time, with one implementation commit per tool.
- Move each migrated tool's formatting contract into `internal/tools/coretools/SPEC.md`.
- Add focused presenter coverage per migrated tool; keep formatter cleanup separate.

### [DONE] Phase 4 - internal/agentformatter: ls

- Remove explicit `ls` formatter branches now that `internal/tools/coretools` owns the replace-style presenter.
- Update focused formatter tests so `ls` coverage exercises presenter-driven rendering rather than formatter-owned path parsing.

### Phase 4 - internal/agentformatter: remaining migrated coretools

- Remove explicit formatter branches for migrated coretools once presenter coverage is in place.
- Preserve generic fallback behavior, out-of-package errors, and shared completion/error rendering that presenter-owned summaries still rely on.

## Learnings

- `implement` targeted at `internal/tools/coretools` could not also modify `internal/agentformatter`, so Phase 3 needs separate implementation steps for the tool package and the formatter package.

## Decisions

- Phase 0 is API plumbing only. Tool-owned rendering and completion-behavior changes stay out of scope until phase 1.
- Phase 1 keeps `Presentation.Body` as `[]Block`. Current tool shapes need mixed bodies such as paragraph + checklist, and diff/output blocks fit naturally without collapsing to a single block.
- `shell` and `skill_shell` currently share formatter behavior via normalized tool names. Their formatter cleanup likely needs to land together, or keep a temporary explicit `skill_shell` path.

## Review

## Summary
