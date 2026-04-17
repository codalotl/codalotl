# PR

## User Summary (do not modify)

This PR is done in the `jn/check_conformance_ui` branch (as a prerequisite for `.prs/2026-04-16_3_check_conformance_ui.md`).

Read the other PR file. In this PR file, our goal is to extend `agent` with events to support `.prs/2026-04-16_3_check_conformance_ui.md`. The change is this file is necessary, but not sufficient, to enable `.prs/2026-04-16_3_check_conformance_ui.md`.

Add event: `EventTypeStartSubagent`. `Event` will have field `StartSubagent StartSubagent`, where

```go
type StartSubagent struct {
    CallingAgentID       string // ID of agent/subagent that is creating the subagent.
    ToolCallID           string // the tool call's CallID that is creating the subagent.
    Label                string // Optional tool-supplied display label for the subagent being started
}
```

Semantics:
- Event.StartSubagent is the zero value unless Event.Type is EventTypeStartSubagent.
- EventTypeStartSubagent events are only emitted during tool calls of an already-running agent. So there's no EventTypeStartSubagent for the root agent.
- During the subagent launch process, the calling tool may optionally provide a subagent label.
- This should be optional, so existing tools do not need to change unless they want custom labeling. If omitted, StartSubagent.Label is the zero value.
- `agent` ensures exactly one EventTypeStartSubagent event happens per subagent ID.
- The event is emitted when that subagent's SendUserMessage call is accepted, not at construction time.
- EventTypeStartSubagent is the first event produced by that subagent in the shared stream.
- Creating a subagent without ever calling SendUserMessage on it does not emit EventTypeStartSubagent.
- AddUserTurn on the subagent does not emit EventTypeStartSubagent.
- For this event, AgentMeta should be the subagent's, so `evt.AgentMeta.Parent == evt.StartSubagent.CallingAgentID`.

Note:
- add this situation to agent.
- make sure nothing blows up (it will start sending these events on existing subagent-based tools)
- adapt things like tui to handle these events if necessary (they shouldn't be displayed -- mostly just dropped for now)
- Probably need to manually modify the integration tests so that they expect these events
- Retrofit one traditional subagent-based tool like clarify_public_api to supply this label and ensure that works.
- Retrofit check_spec_conformance to make sure we can ergonomically attach subagent labels in actually concurrent-based subagent tools.

In terms of the `internal/agent` package itself:
- Let's unify New vs NewWithDefaultModel into just New, which accepts options; model is one option, and subagent label is another. That's how this ultimately gets into agent.
