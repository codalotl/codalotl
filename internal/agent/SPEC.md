# agent

An `agent.Agent` wraps `llmstream` and `tools` in a loop.

It abstracts and simplifies `llmstream` - not all capabilities of llmstream need to be exposed (for instance, content deltas). However, it need not *completely*
hermetically seal `llmstream` - callers of `agent` also know llmstream exists, and some details can come through.

It will be used to build a TUI-based coding agent. User types in a coding challenge, and we spin up an Agent. The goroutine responsible for updating the TUI will consume
these events in order to update it.

Security/Authorization is orthogonal to `agent` (users may implement in their tools).

## Basic Usage

```go
mainAgent, err := agent.New(prompt, tools, agent.NewOptions{Model: model})
fmt.Println("Session ID: ", mainAgent.SessionID()) // some unique identifier per new root Agent. Guid-like.
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
        fmt.Println("message: ", ev.TextContent.Content, ev.AssistantTextFinalizing) // what did the mainAgent say? is it the finalizing message?
    case agent.EventTypeAssistantReasoning:
        fmt.Println("reasoning: ", ev.ReasoningContent.Content)
    case agent.EventTypeToolCall:
        fmt.Println("tool call: ", ev.Tool.Name(), ev.ToolCall)
    case agent.EventTypeToolComplete:
        fmt.Println("tool: ", ev.Tool.Name(), ev.ToolCall, ev.ToolResult) // ev.Tool is the concrete llmstream.Tool
    case agent.EventTypeStartSubagent:
        fmt.Println("subagent started: ", ev.StartSubagent.Label, ev.StartSubagent.ToolCallID)
    case agent.EventTypeAssistantTurnComplete:
        fmt.Println("turn: ", ev.Turn)
        fmt.Println("tokens used: ", ev.Turn.TokenUsage)
    case agent.EventTypeUserMessageQueued:
        fmt.Println("queued: ", ev.UserMessage)
    case agent.EventTypeQueuedUserMessageSent:
        fmt.Println("queued sent: ", ev.UserMessage)
    }
}
```

## SubAgents

SubAgents can be constructed from an Agent (and SubAgents from SubAgents, etc), but **only** while servicing a tool call.
- The primary purpose of a SubAgent is to share the Event channel, so that a TUI can see events from both the Agent and its SubAgents.
- Tool authors request SubAgents by retrieving a `SubAgentCreator` from `ctx` during a tool's `Run` call.
- Once the `Run` function returns, all running SubAgents are canceled and any outstanding `SubAgentCreator`s panic if invoked, so the `Run` function must wait for its SubAgents to complete.

```go
type exploreTool struct{}

func (t *exploreTool) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
    // If user code wanted to limit nesting to some max depth:
    depth := agent.SubAgentDepth(ctx) // 0: Run is being called by a root Agent. 1: first-level SubAgent, etc.
    if depth > 0 {
        // return error (too much SubAgent nesting)
    }
    subAgentCreator := agent.SubAgentCreatorFromContext(ctx)
    subAgent, err := subAgentCreator.New(
        prompt,
        agent.AgentToolsFromContext(ctx),
        agent.NewOptions{SubagentLabel: "Explore package metadata"},
    )
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
- SubAgents may be constructed with an optional display label.
- SubAgent start events:
    - `Event.StartSubagent` is the zero value unless `Event.Type == EventTypeStartSubagent`.
    - `EventTypeStartSubagent` is only emitted for SubAgents, never for the root agent.
    - Exactly one `EventTypeStartSubagent` event happens per subagent ID.
    - It is emitted when that subagent's `SendUserMessage` call is accepted, not at construction time.
    - It is the first event produced by that subagent in the shared event stream.
    - Creating a SubAgent without calling `SendUserMessage` on it does not emit it.
    - `AddUserTurn` on the SubAgent does not emit it.
    - For this event, `Event.Agent.Parent == Event.StartSubagent.CallingAgentID`.

The agent package contains an `AgentCreator` interface that a callee can accept, which will either create a primary agent or a SubAgent, based on how it was constructed.
- This enables a function to be created (ex: ResearchPlan(ac AgentCreator, plan string, ...)) that either operates as a root agent, or as a SubAgent, with the same signature.

```go
func NewAgentCreator() AgentCreator

type AgentCreator interface {
	// Model omitted: root creators use package default model; SubAgent creators use parent model.
	New(systemPrompt string, tools []llmstream.Tool, options ...NewOptions) (*Agent, error)
}

type NewOptions struct {
	Model         llmmodel.ModelID
	SubagentLabel string
}
```

Every `Event` includes metadata describing the originator so TUIs can attribute mirrored events:

```go
type AgentMeta struct {
	ID     string // stable, unique per Agent/SubAgent within a session
	Depth  int    // 0 == root agent
	Parent string // parent Agent/SubAgent ID; "" for root agent
}

type Event struct {
	Agent                   AgentMeta
	Tool                    llmstream.Tool // nil on non-tool events
	StartSubagent           StartSubagent
	AssistantTextFinalizing bool // only meaningful when Type == EventTypeAssistantText

	// ... other fields
}

type StartSubagent struct {
	CallingAgentID string // ID of agent/subagent creating the subagent.
	ToolCallID     string // tool call ID creating the subagent.
	Label          string // optional display label
}
```

## Assistant text events

`EventTypeAssistantText` is a streaming/presentation event for one logical assistant text message within one provider send.

- It is not a delta.
- It is not an arbitrary `llmstream` text block fragment.
- Adjacent completed provider text blocks may be coalesced into one `EventTypeAssistantText`.
- `Event.AssistantTextFinalizing` is only meaningful when `Event.Type == EventTypeAssistantText`.
- `AssistantTextFinalizing=true` means this assistant text is the trailing assistant text of the completed turn. If any reasoning, tool call, or other turn content follows the text, it is not finalizing.
- `EventTypeAssistantTurnComplete` remains canonical completed-turn history. `EventTypeAssistantText` is for streaming and presentation.

## Notes

- `EventTypeAssistantReasoning` is for complete reasoning parts, not deltas.
- Stopping the agent early is accomplished with the context.
- An agent needs to be thread-safe. It runs in a different goroutine than its instantiator. All public methods should behave assuming multithreaded access.
- An agent may only run one active loop. A call to SendUserMessage when it's already running results in an error (on the channel of the 2nd call).
- Things like sandbox dirs and permissioning are orthogonal; they can (in theory) be configured in tools.
- AddUserTurn returns an error if the agent is running.
- QueueUserMessage can be used to enqueue user messages while an agent is running (see its doc comment for semantics and event emission).

## Dependencies

- Uses `internal/llmstream` (and not llmcomplete)
- Does not use 3rd party packages directly

## Public API

See Usage for implied interface. Additionally:

```go
// New constructs a root Agent.
func New(systemPrompt string, tools []llmstream.Tool, options ...NewOptions) (*Agent, error)

// NewAgentCreator returns an AgentCreator that constructs root agents.
func NewAgentCreator() AgentCreator

type NewOptions struct {
	Model         llmmodel.ModelID
	SubagentLabel string
}

type AgentCreator interface {
	// Model omitted: root creators use package default model; SubAgent creators use parent model.
	New(systemPrompt string, tools []llmstream.Tool, options ...NewOptions) (*Agent, error)
}

// Status reports whether the agent is currently processing a turn.
func (a *Agent) Status() Status

// Turns returns a snapshot of the conversation turns so far.
func (a *Agent) Turns() []llmstream.Turn

// TokenUsage returns the cumulative token usage across assistant turns.
func (a *Agent) TokenUsage() llmstream.TokenUsage

// ContextUsagePercent estimates how much of the model's context window is consumed based on the latest assistant turn. Returns 0 when unknown.
func (a *Agent) ContextUsagePercent() int

// QueueUserMessage queues a user message to be appended to the conversation the next time the agent reaches a safe boundary (after tool results are appended, or
// after an assistant end-of-turn completes).
//
// If the agent is currently executing a tool (including any subagents created by that tool), the message is queued and will not be appended until after the tool
// returns; messages are never injected into subagent tool calls/responses.
//
// When QueueUserMessage is accepted, the agent emits EventTypeUserMessageQueued with Event.UserMessage set. When the queued message is appended to the conversation
// (and will be included in the next provider send), the agent emits EventTypeQueuedUserMessageSent with Event.UserMessage set.
//
// Note: EventTypeUserMessageQueued is emitted asynchronously by the agent's run loop (it may not be emitted before QueueUserMessage returns). This avoids deadlocks
// when QueueUserMessage is called by the same goroutine that is draining the event stream.
//
// QueueUserMessage does not start a new run loop and does not return an event stream; it extends the currently running SendUserMessage call.
//
// It returns ErrNotRunning when the agent is idle, or when a run is finishing and no longer accepting queued messages (to avoid races where the message would be
// accepted but never delivered).
func (a *Agent) QueueUserMessage(message string) error
```
