package agent

import "github.com/codalotl/codalotl/internal/llmstream"

// EventType categorises agent events emitted from SendUserMessage.
type EventType string

const (
	EventTypeError                 EventType = "error"
	EventTypeCanceled              EventType = "canceled"
	EventTypeDoneSuccess           EventType = "done_success"
	EventTypeAssistantText         EventType = "assistant_text"
	EventTypeAssistantReasoning    EventType = "assistant_reasoning"
	EventTypeToolCall              EventType = "tool_call"
	EventTypeToolComplete          EventType = "tool_complete"
	EventTypeAssistantTurnComplete EventType = "assistant_turn_complete"
	EventTypeWarning               EventType = "warning"
	EventTypeRetry                 EventType = "retry"
)

// Event conveys progress or status updates from the agent loop.
type Event struct {
	Agent AgentMeta

	Type  EventType
	Error error

	TextContent llmstream.TextContent

	ReasoningContent llmstream.ReasoningContent

	Tool       string
	ToolCall   *llmstream.ToolCall
	ToolResult *llmstream.ToolResult

	Turn *llmstream.Turn
}

// AgentMeta carries metadata describing which agent produced an event.
type AgentMeta struct {
	ID    string
	Depth int
}
