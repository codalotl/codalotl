package agent

import "github.com/codalotl/codalotl/internal/llmstream"

// EventType categorises agent events emitted from SendUserMessage.
type EventType string

// Event type constants classify events emitted by Agent.SendUserMessage. Each run emits exactly one terminal event: EventTypeDoneSuccess, EventTypeCanceled, or
// EventTypeError.
const (
	// EventTypeError is the terminal event for a run that failed for a reason other than cancellation.
	EventTypeError EventType = "error"

	// EventTypeCanceled is the terminal event for context cancellation, deadline expiration, or provider-reported cancellation.
	EventTypeCanceled EventType = "canceled"

	// EventTypeDoneSuccess is the terminal event for a run that reached a normal end of turn with no queued user messages remaining.
	EventTypeDoneSuccess EventType = "done_success"

	// EventTypeUserMessageQueued reports that QueueUserMessage accepted a message; Event.UserMessage contains the message.
	EventTypeUserMessageQueued EventType = "user_message_queued"

	// EventTypeQueuedUserMessageSent reports that a queued message was appended to the conversation; Event.UserMessage contains the message.
	EventTypeQueuedUserMessageSent EventType = "queued_user_message_sent"

	// EventTypeStartSubagent reports that a subagent has begun running for a tool call; Event.StartSubagent identifies it.
	EventTypeStartSubagent EventType = "start_subagent"

	// EventTypeAssistantText reports completed assistant text content in Event.TextContent.
	EventTypeAssistantText EventType = "assistant_text"

	// EventTypeAssistantReasoning reports completed assistant reasoning content in Event.ReasoningContent.
	EventTypeAssistantReasoning EventType = "assistant_reasoning"

	// EventTypeToolCall reports that the provider requested a tool; Event.ToolCall contains the call.
	EventTypeToolCall EventType = "tool_call"

	// EventTypeToolOutput reports display-only output emitted by a running tool; Event.ToolOutput contains the output.
	EventTypeToolOutput EventType = "tool_output"

	// EventTypeToolComplete reports that a tool returned a result; Event.ToolResult contains the result.
	EventTypeToolComplete EventType = "tool_complete"

	// EventTypeAssistantTurnComplete reports that a completed assistant turn was appended to the agent history.
	EventTypeAssistantTurnComplete EventType = "assistant_turn_complete"

	// EventTypeWarning reports a non-terminal provider warning; Event.Error contains the warning detail when available.
	EventTypeWarning EventType = "warning"

	// EventTypeRetry reports that the provider is retrying a send after a recoverable error.
	EventTypeRetry EventType = "retry"
)

// Event conveys progress or status updates from the agent loop. Which fields are set depends on the Type.
type Event struct {
	Agent                   AgentMeta                  // Agent identifies the agent that emitted the event.
	Type                    EventType                  // Type classifies the event and determines which event-specific fields are meaningful.
	Error                   error                      // Error contains the failure, cancellation reason, warning, or retry cause when available.
	UserMessage             string                     // UserMessage contains the accepted or sent queued user message.
	StartSubagent           StartSubagent              // StartSubagent identifies the subagent started for EventTypeStartSubagent.
	AssistantTextFinalizing bool                       // AssistantTextFinalizing reports whether TextContent is the trailing assistant text run in the completed assistant turn.
	TextContent             llmstream.TextContent      // TextContent contains assistant text for EventTypeAssistantText.
	ReasoningContent        llmstream.ReasoningContent // ReasoningContent contains assistant reasoning for EventTypeAssistantReasoning.
	Tool                    llmstream.Tool             // Tool is the registered tool associated with a tool event, or nil when the requested tool is unknown.
	ToolCall                *llmstream.ToolCall        // ToolCall describes the provider-requested tool call associated with a tool event.
	ToolOutput              ToolOutput                 // ToolOutput contains display-only output emitted by a running tool.
	ToolResult              *llmstream.ToolResult      // ToolResult contains the result returned by a tool for EventTypeToolComplete.
	Turn                    *llmstream.Turn            // Turn contains the completed assistant turn for EventTypeAssistantTurnComplete.
}

// AgentMeta carries metadata describing which agent produced an event.
type AgentMeta struct {
	ID     string // ID is the unique ID of the agent within the session.
	Depth  int    // Depth is 0 for a root agent and increases by one for each subagent level.
	Parent string // Parent is the parent agent ID, or empty for a root agent.
}

// StartSubagent describes the start of a subagent run within a tool call.
type StartSubagent struct {
	CallingAgentID string // CallingAgentID is the ID of the parent agent whose tool call created the subagent.
	ToolCallID     string // ToolCallID is the provider tool-call ID that created the subagent.
	Label          string // Label is the optional label supplied with NewOptions.SubagentLabel.
}

// ToolOutput is display-only output emitted by a running tool.
type ToolOutput struct {
	Content string // Content is display-only text emitted by the tool and is not appended to the model conversation.
}
