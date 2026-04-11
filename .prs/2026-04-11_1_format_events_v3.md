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

Phase 2: tbd, don't start/plan yet

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

### Phase 1 - internal/tools/coretools

- Implement presenters for `shell`, `skill_shell`, `ls`, `read_file`, `update_plan`, `apply_patch`, `edit`, `write`, and `delete`.
- Preserve current event semantics, including replace-vs-append behavior and concise error presentations.

### Phase 1 - internal/tools/exttools

- Implement presenters for `diagnostics`, `fix_lints`, `run_tests`, and `run_project_tests`.
- Encode their special summary/result shapes semantically rather than relying on `agentformatter`-specific parsing.

### Phase 1 - internal/tools/pkgtools and internal/agentbuilder

- Implement presenters for package/orchestrator tools (`get_public_api`, `clarify_public_api`, `get_usage`, `module_info`, `update_usage`, `change_api`, `review`, `implement`) plus dynamic subagent tooling.
- Keep raw tool results unchanged while making concise summaries available through presentations.

### Phase 1 - validation

- Add or update targeted tests in the touched packages.
- Defer consumer-side rendering changes to a later phase unless required to keep Phase 1 coherent.

## Decisions

- Phase 0 is API plumbing only. Tool-owned rendering and completion-behavior changes stay out of scope until phase 1.
- Phase 1 keeps `Presentation.Body` as `[]Block`. Current tool shapes need mixed bodies such as paragraph + checklist, and diff/output blocks fit naturally without collapsing to a single block.

## Review

## Summary
