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

### PR-file design

- Fully design the Presenter interface and the semantic presentation model in this PR file before touching implementation code.
- Define how a tool call/result declares both:
  - the semantic content to render
  - whether the result replaces the call or is shown in addition to it
- Mentally check the design against the current built-in tool families:
  - file/shell/edit tools
  - package-analysis and package-update tools
  - orchestrator `implement` / `review`
  - YAML-backed command tools and YAML-backed subagent tools

### `internal/llmstream` and shared presentation types

- Extend `llmstream.Tool` with `Presenter() Presenter`.
- Introduce a shared semantic-presentation package (likely a new `internal/toolpresentation` package) so tools, `internal/agent`, `internal/agentformatter`, and `internal/tui` can share presentation types without import cycles.
- Keep provider-facing tool definitions and raw `ToolCall` / `ToolResult` payloads unchanged; this work is only about local event presentation.

### `internal/agent`

- Resolve each tool's presenter when dispatching `EventTypeToolCall` and `EventTypeToolComplete`.
- Attach semantic presentation data to agent events so downstream consumers do not have to infer behavior from tool names.
- Add focused tests covering event emission with presentation metadata for both root-agent and subagent tool activity.

### `internal/tools/...` and `internal/agentbuilder`

- Give built-in tools presenters that describe their own semantic display instead of relying on `internal/agentformatter` name switches.
- Give YAML-backed command and subagent tools pragmatic default presenters so user-defined tools render sensibly without inventing a new YAML presenter DSL.
- Ensure subagent-oriented tools can declare the non-replacing call/result behavior so `internal/tui` can drop its hard-coded subagent tool list.

### `internal/agentformatter`

- Refactor formatting to render semantic presentation data while keeping color, width, ANSI/plain-text, and TUI-vs-CLI concerns local to the formatter.
- Preserve current user-visible output unless the final Presenter design reveals a clearly simpler and still-consistent representation.
- Keep a narrow fallback path for tools/events that do not yet provide semantic presentation during the migration.

### `internal/tui` and `internal/noninteractive`

- Update `internal/tui` to use event presentation metadata, not tool-name special cases, when deciding whether a tool result replaces the prior call.
- Keep noninteractive human-readable output driven by the same semantic presentation data.
- Keep JSON output stable unless there is a deliberate reason to expose new presentation fields; if JSON changes, update replay fixtures intentionally.

### Validation

- Add or update focused tests in the packages touched by the design: `internal/agent`, `internal/agentformatter`, `internal/tui`, `internal/agentbuilder`, and tool packages that gain presenters.
- Run focused package tests during implementation, then run broader regression coverage including noninteractive replay/integration tests and any manually patched fixtures required by intentional event-shape changes.
- Manually verify that representative tool calls still look the same in both TUI and noninteractive output, especially subagent-backed tools.

## Decisions

- Semantic presentation should be structured tool-owned data, not pre-rendered text or ANSI output emitted by tools.
- The "replace tool call with result vs show both" choice should come from tool presentation metadata, so built-in and YAML-defined tools can participate without hard-coded tool-name lists in `internal/tui`.

## Review

- Not run yet.

## Summary

- Planning step only. No implementation yet.
