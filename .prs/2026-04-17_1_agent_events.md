# PR

## User Summary (do not modify)

This PR is done in the `jn/check_conformance_ui` branch (as a pre-req for .prs/2026-04-16_3_check_conformance_ui.md).

Read the other PR file. In this PR file, our goal is to extend `agent` with events to support .prs/2026-04-16_3_check_conformance_ui.md

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
- During a tool call that intends to launch subagents, the tool can somehow make a call that triggers a EventTypeStartSubagent event.
- Ideally i'd like this to be optional for the tool call, so that current tools don't need to be changed to call this.
- If `agent` notices that subagents have in fact been created in a tool call without a EventTypeStartSubagent event, it automatically creates a EventTypeStartSubagent event.
    - This forms an invariant for event consumers: we only increase depth of subagents by starting with a EventTypeStartSubagent event.
- `agent` ensues only one EventTypeStartSubagent event happens per tool ToolCallID.
- I think (but am not sure) that for this event, AgentMeta should be the subagent's (evt.AgentMeta.Parent == evt.CallingAgentID)

Note:
- add this situation to agent.
- make sure nothing blows up (it wills start sending these events on existing subagent-based tools)
- adapt things like tui to handle these events if necessary
- Probably need to manually modify the integration tests so that it expects these events
