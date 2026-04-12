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

Phase 5: assess
- NOTE to orchestrator: I did phase 4 out of band, not updating the pr file. So your task here is to do an overall assessment of where we are in the process and update plan.

phase 6: tbd, don't start yet

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

### [DONE] Phase 4 - internal/agentformatter: presenter bodies

- Render presenter-provided body blocks for replace-style tool completions instead of only showing the summary line.
- Preserve existing bullet/status behavior and shared output formatting conventions so `shell`/`skill_shell` show command output via their presenter bodies.
- Add focused formatter coverage for presenter-driven completion bodies in both success and error cases.

### [DONE] Phase 4 - internal/agentformatter: semantic body blocks

- Add presenter-body rendering for `Paragraph`, `Checklist`, and `Diff`.
- Preserve current `update_plan` explanation/checklist emphasis semantics and current patch/edit diff styling.
- Keep shared bullet/status and shared error handling unchanged.
- Use this as the prerequisite for wiring `update_plan`, `apply_patch`, `edit`, `write`, and `delete` presenters.

### [DONE] Phase 4 - internal/tools/coretools: update_plan

- Move `update_plan` formatting contract into `internal/tools/coretools/SPEC.md`.
- Add a replace-style presenter for `update_plan` that preserves the current `Update Plan` summary plus explanation/plan-item body rendering.
- Keep shared formatter-owned bullet/status and error handling intact; add focused `internal/tools/coretools` coverage for explanation and plan-item emphasis semantics.

### [DONE] Phase 4 - internal/agentformatter: update_plan

- Remove explicit `update_plan` formatter branches now that `internal/tools/coretools` owns the replace-style presenter.
- Keep presenter-driven explanation/checklist rendering on the shared semantic block path, including next-up and in-progress emphasis.
- Preserve shared tool-error rendering over presenter bodies and keep focused formatter coverage on the presenter path.

### [DONE] Phase 4 - internal/tools/coretools: delete

- Move `delete` formatting contract into `internal/tools/coretools/SPEC.md`.
- Add a replace-style presenter for `delete` that preserves the current `Delete <path>` summary and shared formatter-owned error handling.
- Add focused `internal/tools/coretools` coverage for presenter and fallback behavior.

### [DONE] Phase 4 - internal/tools/coretools: apply_patch

- Move `apply_patch` formatting contract into `internal/tools/coretools/SPEC.md`.
- Add a replace-style presenter that renders semantic `Diff` bodies so formatter-owned diff rendering drives add/edit/delete/rename headers and hunks.
- Keep shared formatter ownership of out-of-package handling and shared tool-error rendering; add focused presenter coverage around patch parsing and fallback behavior.

### [DONE] Phase 4 - internal/tools/coretools: edit

- Move `edit` formatting contract into `internal/tools/coretools/SPEC.md`.
- Add a replace-style presenter that renders the requested file edit as a semantic `Diff`, including replace-all and post-edit error details.
- Keep shared formatter-owned diff rendering and shared tool-error/out-of-package behavior; add focused presenter coverage.

### [DONE] Phase 4 - internal/tools/coretools: write

- Move `write` formatting contract into `internal/tools/coretools/SPEC.md`.
- Add a replace-style presenter that renders file creation/replacement as a semantic `Diff`, including post-write error details.
- Keep shared formatter-owned diff rendering and shared tool-error/out-of-package behavior; add focused presenter coverage.

### [DONE] Phase 4 - internal/agentformatter: ls

- Remove explicit `ls` formatter branches now that `internal/tools/coretools` owns the replace-style presenter.
- Update focused formatter tests so `ls` coverage exercises presenter-driven rendering rather than formatter-owned path parsing.

### [DONE] Phase 4 - internal/agentformatter: delete

- Remove explicit `delete` formatter branches now that `internal/tools/coretools` owns the replace-style presenter.
- Keep focused formatter coverage for `delete`, but exercise the presenter-driven path rather than formatter-owned special casing.
- Preserve shared tool-error and out-of-package rendering for `delete`.

### [DONE] Phase 4 - internal/agentformatter: remaining migrated coretools

- Remove explicit formatter branches for migrated coretools once presenter coverage is in place.
- Preserve generic fallback behavior, out-of-package errors, and shared completion/error rendering that presenter-owned summaries still rely on.

### [DONE] Phase 4 - internal/tools/exttools and internal/tools/pkgtools

- Presenter adoption has expanded beyond coretools: current `exttools` and `pkgtools` tool implementations also expose non-nil presenters and formatter coverage exercises those presenter-driven summaries and bodies.
- `internal/agentformatter` is now operating primarily as a renderer of semantic presentations rather than a registry of per-tool formatting branches.
- Targeted validation currently passes for `internal/agentformatter`, `internal/tools/coretools`, `internal/tools/exttools`, `internal/tools/pkgtools`, and `internal/agentbuilder`.

### [DONE] Phase 5 - assessment

- The codebase is ahead of the PR file: all current concrete tools under `internal/tools/*` now expose presenters, and `internal/agentformatter` no longer contains explicit formatter branches for the migrated built-in tools.
- Functional migration is effectively complete for the current built-in tool set, with presenter-owned formatting exercised by targeted tests.
- That documentation gap was handled in a follow-up step by backfilling terse package specs under `internal/tools/*`.

### [DONE] Phase 6 - package spec follow-up

- Backfill terse `SPEC.md` files for `internal/tools/exttools` and `internal/tools/pkgtools`.
- Keep the new package specs presentation-only: describe how the tools present, not the overall tool behavior.
- Backfill missing presentation entries in `internal/tools/coretools/SPEC.md` for already-migrated file-edit presenters.

## Learnings

- `implement` targeted at `internal/tools/coretools` could not also modify `internal/agentformatter`, so Phase 3 needs separate implementation steps for the tool package and the formatter package.
- A tool-only `update_plan` presenter pass is not usable: `internal/agentformatter` prefers presenter-owned rendering before tool-specific branches, but presenter bodies currently only render `Output` blocks. `update_plan` needs `Paragraph`/`Checklist` support first.
- The repository has moved ahead of the PR notes: presenter implementations now exist across `coretools`, `exttools`, and `pkgtools`, so the main remaining assessment question is documentation scope rather than formatter plumbing.
- For tool packages that did not already have a `SPEC.md`, the right scope here was minimal presentation-only guidance rather than backfilling full package/tool specs.

## Decisions

- Phase 0 is API plumbing only. Tool-owned rendering and completion-behavior changes stay out of scope until phase 1.
- Phase 1 keeps `Presentation.Body` as `[]Block`. Current tool shapes need mixed bodies such as paragraph + checklist, and diff/output blocks fit naturally without collapsing to a single block.
- `shell` and `skill_shell` currently share formatter behavior via normalized tool names. Their formatter cleanup likely needs to land together, or keep a temporary explicit `skill_shell` path.
- Treat the current presenter migration as functionally complete for built-in tools; package-spec backfill is supporting documentation, not a blocker on the formatter/presenter architecture.
- Backfilled non-core tool specs should stay terse and presentation-focused; full tool behavior remains out of scope for those new spec files.

## Review

## Summary
