# PR

## User Summary (do not modify)

### Background / Problem:

During a recent PR, redid formatters for all tools as presenters (See .prs/2026-04-11_1_format_events_v3.md). For the review tool, we instruct the agent to have its last message be JSON. The problem is that the user sees a wall of JSON s the last message from the subagent. The review tool then correctly prints the tool result.

In an ideal world, the user just wouldn't see this final message. This isn't a bug in the review tool presenter itself per se. Rather, a limitation of the system.

Related problem: in the future, I will want to make parallel subagents. I will need to change the TUI UI for this, since I don't think ~randomly interleaving agent messages is useful. In my head, the future UI will look like:

* Refactoring 7 packages (internal/foo, internal/bar, ...)
  * Refactoring internal/foo
    * Read File internal/foo/file.go
  * Refactoring internal/foo, internal/bar
    * Thinking about next step
  * Refactoring internal/baz
    * Edit internal/baz/core.go
      └ + if isTest {
        - if isTest && otherondition {

In other words, only show the last event per subagent, which gets replaced as a new event comes in for that subagent.

At the same time, this task isn't about agent event suppression in general. We may want multiple consumers of the agent event stream: the UI, loggers, debuggers, etc. So I believe all events must happen. This is a UI concern: TUI and noninteractive just need to choose how to display the events.

### Requirements of this PR

* Concretely, I want to fix the review tool displaying that blob of JSON to the end-user. It should not be displayed in TUI, nor noninteractive.


### Sketch of design

Edit internal/llmstream. Add to `Presenter` interface. No, don't make a separate interface.

```go
// A Presenter enables tools to define UI formats and policies for how the tool call/result are displayed, as well as how to subagent event streams that come "from" a tool call.
//   - It can present a tool call (and optional result) in a "semantic" way - a tree representation that can be further styled for different modalities. As
//     an analogy, it's the HTML (but not the CSS) of underlying data.
//   - When a tool call launches a subagent, the tool can define policies for how that subagent's events are displayed. For instance, we may want to display all subagent events (the default),
//     or selectively hide some events (i.e., the last message).
type Presenter interface {
	// Present presents call and result in a semantic way (no width decisions, no assumptions about ANSI terminals, colors). To present a tool call (no result yet),
	// call Present(call, nil). To present a call with result, call Present(call, result). For instance, for a read file tool, the call might return the equivalent of
	// "Reading file.go". The result might return the equivalent of "Read file.go (123 bytes)".
	Present(call ToolCall, result *ToolResult) Presentation

  // SubagentEventPolicy defines how events are displayed. Only relevant for tools that launch subagents (all other tools can safely return SubagentEventPolicyDefault). The
  // argument `call` is the tool call that launched the subagent (not a call made by the subagent) - it lets the policy be a function of the input parameters.
  SubagentEventPolicy(call ToolCall) SubagentEventPolicy
}

type SubagentEventPolicy string

const (
    SubagentEventPolicyDefault          SubagentEventPolicy = ""
  	SubagentEventPolicyHideFinalMessage SubagentEventPolicy = "hide_final_message"
)
```

NOTE: this design enables `SubagentEventPolicyHideAll          SubagentEventPolicy = "hide_all"` and `SubagentEventPolicyLast             SubagentEventPolicy = "last"`, but those policies are out of scope here.

Based on that:
- make review tool return SubagentEventPolicyHideFinalMessage
- Update TUI to handle
- Update noninteractive to handle

## Plan

### [DONE] Design / spec updates

- Treat this as a display concern, not an agent-event suppression change.
- Presenter owns the display policy for descendant subagent events.
- Updated package specs in `internal/llmstream`, `internal/agentbuilder`, `internal/tui`, and `internal/noninteractive`.

### [DONE] Implement presenter-owned subagent visibility policy

#### [DONE] Package `internal/llmstream`
- Extend `Presenter` with `SubagentEventPolicy(call ToolCall) SubagentEventPolicy`.
- Add `SubagentEventPolicyDefault` and `SubagentEventPolicyHideFinalMessage`.
- Keep the underlying agent event stream unchanged; this API is for display consumers.

#### [DONE] Package `internal/agentbuilder`
- Make the built-in `review` presenter return `SubagentEventPolicyHideFinalMessage`.
- Keep other YAML presenter presets at the default policy.
- Update presenter coverage accordingly.

#### [DONE] Package `internal/tui`
- Respect the presenter policy when deciding which descendant subagent events become visible messages.
- For `hide_final_message`, suppress only the descendant subagent's terminal assistant-text presentation while keeping descendant tool activity and the outer tool result visible.
- Add regression coverage around the `review` tool flow.

#### [DONE] Package `internal/noninteractive`
- Apply the same policy in human-readable output.
- Apply the same policy in JSON output, since it is also a user-facing display stream rather than a raw internal event dump.
- Keep outer tool call/result behavior unchanged.
- Add regression coverage for both modes.

#### Validation
- Run focused tests for `internal/agentbuilder`, `internal/tui`, and `internal/noninteractive`.
- Run targeted package tests for `internal/llmstream`, `internal/agentbuilder`, `internal/tui`, and `internal/noninteractive`.
- Completed after interface rollout:
  - `go test ./internal/llmstream ./internal/agentbuilder ./internal/tools/coretools ./internal/tools/exttools ./internal/tools/pkgtools ./internal/agentformatter ./internal/noninteractive ./internal/tui`
- Completed after TUI implementation:
  - `go test ./internal/tui`
- Completed after noninteractive implementation:
  - `go test ./internal/noninteractive`

## Decisions

- Follow the user's presenter-interface design rather than a tool-specific UI hack.
- Scope is one policy only: `hide_final_message`.
- Hidden events remain available on the underlying agent event stream; only display consumers omit them.

## Review

Review run against `origin/main`.

Overall verdict: patch is incorrect.

Actionable findings:

1. `internal/tui/tui.go`
   - `HideFinalMessage` currently hides only the last `AssistantText` chunk.
   - If the hidden descendant's final reply is split across multiple `AssistantText` events in one turn, earlier chunks can still be shown before the final chunk arrives.
   - Follow-up: buffer the whole hidden descendant assistant turn and decide visibility on `AssistantTurnComplete`.

2. `internal/tui/tui.go`
   - When a hidden descendant emits assistant text and then ends with `error` or `canceled`, buffered text is currently flushed before the terminal event.
   - Follow-up: suppress the buffered final assistant text for hidden descendants on terminal error/cancel paths too.

## Summary

## State

- `internal/llmstream/presentation.go` now exposes `Presenter.SubagentEventPolicy` plus `Default` and `HideFinalMessage`.
- `internal/agentbuilder/yaml_presenter.go` now makes `review` return `HideFinalMessage`; other YAML presenter presets return `Default`.
- `internal/agentbuilder/data/config.yml` defines the `review` tool as a JSON-returning subagent.
- `internal/tui/tui.go` now tracks active tool display scopes and buffers descendant assistant text so `HideFinalMessage` can drop only the final subagent message while still showing earlier descendant activity.
- `internal/noninteractive/session.go` now applies the same policy for human-readable and JSON output, buffering descendant assistant text and dropping only the final hidden subagent message.
- Review found two TUI follow-ups: whole-turn buffering for chunked final assistant replies, and suppression of buffered hidden text on descendant error/cancel termination.
