# PR

## User Summary (do not modify)

This PR is done in the `jn/check_conformance_ui` branch (as a prerequisite for `.prs/2026-04-16_3_check_conformance_ui.md`).

Read the other PR file. In this PR file, our goal is to extend `agent` with events to support `.prs/2026-04-16_3_check_conformance_ui.md`. The change is this file is necessary, but not sufficient, to enable `.prs/2026-04-16_3_check_conformance_ui.md`.

Add event: `EventTypeStartSubagent`. `Event` will have field `StartSubagent StartSubagent`, where

```go
type StartSubagent struct {
    CallingAgentID       string // ID of agent/subagent that is creating the subagent.
    ToolCallID           string // the tool call's CallID that is creating the subagent.
    Name                 string // Optional name of the subagent being started
}
```

Semantics:
- Event.StartSubagent is the zero value unless Event.Type is EventTypeStartSubagent.
- EventTypeStartSubagent events are only emitted during tool calls of an already-running agent. So there's no EventTypeStartSubagent for the root agent.
- During the subagent launch process, the calling tool can somehow set the subagent name.
- Ideally I'd like this to be optional for the tool call, so that current tools don't need to be changed to call this.
- `agent` ensures only one EventTypeStartSubagent event happens per subagent ID. I suspect the right time to fire this is during the first SendMessage (vs simply at creation time).
- I think (but am not sure) that for this event, `AgentMeta` should be the subagent's (`evt.AgentMeta.Parent == evt.StartSubagent.CallingAgentID`).

Note:
- add this situation to agent.
- make sure nothing blows up (it will start sending these events on existing subagent-based tools)
- adapt things like tui to handle these events if necessary (they shouldn't be displayed -- mostly just dropped for now)
- Probably need to manually modify the integration tests so that they expect these events
- Retrofit one traditional subagent-based tool like clarify_public_api to manually trigger this event to ensure that works.
- Retrofit check_spec_conformance to make sure we can ergonomically attach subagent names in actually concurrent-based subagent tools.

In terms of the `internal/agent` package itself:
- Let's unify New vs NewWithDefaultModel into just New, which accepts options, model is one. The other is a subagent name. That's how this ultimately gets into agent.
