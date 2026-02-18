# agent

An `agent.Agent` wraps `llmstream` and `tools` in a loop.

It abstracts and simplifies `llmstream` - not all capabilities of llmstream need to be exposed (for instance, content deltas). However, it need not *completely*
hermetically seal `llmstream` - callers of `agent` also know llmstream exists, and some details can come through.

It will be used to build a TUI-based coding agent. User types in a coding challenge, and we spin up an Agent. The goroutine responsible for updating the TUI will consume
these events in order to update it.

Security/Authorization is orthogonal to `agent` (users may implement in their tools).

## Basic Usage

```go
mainAgent, err := agent.NewAgent(model, prompt, tools)
fmt.Println("Session ID: ", mainAgent.SessionID()) // some unique identifier per NewAgent. Guid-like.
mainAgent.AddUserTurn(environmentStr) // Any string, which is just added as a user turn without sending it to the LLM.
out := mainAgent.SendUserMessage(ctx, message)
for ev := range out {
    switch ev.Type {
    case agent.EventTypeError:
        fmt.Println(ev.Error) // mainAgent has stopped with this error.
    case agent.EventTypeCanceled: // ctx cancellation or deadline
        fmt.Println(ev.Error)
    case agent.EventTypeDoneSuccess:
        fmt.Println("done") // mainAgent ended turn (not via tool use), in a successful manner.
    case agent.EventTypeAssistantText:
        fmt.Println("message: ", ev.TextContent.Content) // what did the mainAgent say?
    case agent.EventTypeAssistantReasoning:
        fmt.Println("reasoning: ", ev.ReasoningContent.Content)
    case agent.EventTypeToolCall:
        fmt.Println("tool call: ", ev.Tool, ev.ToolCall)
    case agent.EventTypeToolComplete:
        fmt.Println("tool: ", ev.Tool, ev.ToolCall, ev.ToolResult) // These are just the llmstream structs
    case agent.EventTypeAssistantTurnComplete:
        fmt.Println("turn: ", ev.Turn)
        fmt.Println("tokens used: ", ev.Turn.TokenUsage)
    }
}
```

## SubAgents

SubAgents can be constructed from an Agent (and SubAgents from SubAgents, etc), but **only** while servicing a tool call.
- The primary purpose of a SubAgent is to share the Event channel, so that a TUI can see events from both the Agent and its SubAgents.
- Tool authors request SubAgents by retrieving a `SubAgentCreator` from `ctx` during a tool's `Run` call.
- Once the `Run` function returns, all running SubAgents are canceled and any outstanding `SubAgentCreator`s panic if invoked, so the `Run` function must wait for its SubAgents to complete.
- FUTURE (not now): we may want to support an agent.SubAgentCloneFromContext, which keeps prompts, tools, and context/history.

```go
type exploreTool struct{}

func (t *exploreTool) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
    // If user code wanted to limit nesting to some max depth:
    depth := agent.SubAgentDepth(ctx) // 0: Run is being called by a root Agent. 1: first-level SubAgent, etc.
    if depth > 0 {
        // return error (too much SubAgent nesting)
    }
    subAgentCreator := agent.SubAgentCreatorFromContext(ctx)
    subAgent, err := subAgentCreator.NewWithDefaultModel(prompt, agent.AgentToolsFromContext(ctx))
    if err != nil {
        // ...
    }
    out := subAgent.SendUserMessage(ctx, createExploreMessage(call))
    for ev := range out {
        // process SubAgent events
    }
    return createResult(...)
}
```

- SubAgents accept their own system prompt and tool list.
- `SendUserMessage` returns `out`, a dedicated channel for the SubAgent; every event emitted on `out` is mirrored to the parent's channel so the TUI observes a unified stream.
- Multiple parallel SubAgents can be created inside a Run method.
- In addition to a SubAgent keeping track of its own usage, any usage is also automatically added to its parent (recursively).
- `AgentToolsFromContext` can be called to use the same tools as the parent.

The agent package contains an `AgentCreator` interface that a callee can accept, which will either create a primary agent or a SubAgent, based on how it was constructed.
- This enables a function to be created (ex: ResearchPlan(ac AgentCreator, plan string, ...)) that either operates as a root agent, or as a SubAgent, with the same signature.

```go
func NewAgentCreator() AgentCreator

type AgentCreator interface {
	New(model llmmodel.ModelID, systemPrompt string, tools []llmstream.Tool) (*Agent, error)

	// A SubAgent's default model is the same as the parent's model; otherwise, it's the package's default.
	NewWithDefaultModel(systemPrompt string, tools []llmstream.Tool) (*Agent, error)
}
```

Every `Event` includes metadata describing the originator so TUIs can attribute mirrored events:

```go
type AgentMeta struct {
	ID    string // stable, unique per Agent/SubAgent within a session
	Depth int    // 0 == root agent
}

type Event struct {
	Agent AgentMeta
	// ... other fields
}
```

## Notes

- The EventTypeAssistantText and EventTypeAssistantReasoning events are only for complete parts, not deltas.
- Stopping the agent early is accomplished with the context.
- An agent needs to be thread-safe. It runs in a different goroutine than its instantiator. All public methods should behave assuming multithreaded access.
- An agent may only run one active loop. A call to SendUserMessage when it's already running results in an error (on the channel of the 2nd call).
- Things like sandbox dirs and permissioning are orthogonal; they can (in theory) be configured in tools.
- AddUserTurn returns an error if the agent is running.

## Dependencies

- Uses codeai/llmstream (and not llmcomplete)
- Does not use 3rd party packages directly

## Public Interface

See Usage for implied interface. Additionally:

```go
// eg, running, stopped
func (a *Agent) Status() Status
```

```go
// Turns returns the underlying conversation's Turns. This should work even if the agent is running in a thread-safe way.
func (a *Agent) Turns() []llmstream.Turn
```

```go
// TokenUsage returns the cumulative token usage.
func (a *Agent) TokenUsage() llmstream.TokenUsage
```

```go
// ContextUsagePercent returns an int in [0, 100] indicating the percent of the context window used. 0 is unused, 100 is full.
func (a *Agent) ContextUsagePercent() int
```

NOTE: function signatures are hard requirements. Documentation in comments are **conceptual requirements** - the actual production comment must contain these ideas, but may use different words, and may contain additional, unspecified ideas, provided they are congruent with this design.


## Inactive

These are not requirements yet, but will be. They are inactive either because we lack certain infrastructure, or to simplify the design for initial implementation.

If possible, do not do anything that makes implementing them actively harder in the future.

```go
// ContextUsage returns the (current context usage, total context available), in tokens. Inactive because we lack context window data in llmstream (it's in llmcomplete).
func (a *Agent) ContextUsage() (int, int)
```

## Meta

- Tested
