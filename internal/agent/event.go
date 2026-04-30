package agent

import "github.com/codalotl/codalotl/internal/llmstream"

// EventType categorises agent events emitted from SendUserMessage.
type EventType string

const (
	EventTypeError                 EventType = "error"
	EventTypeCanceled              EventType = "canceled"
	EventTypeDoneSuccess           EventType = "done_success"
	EventTypeUserMessageQueued     EventType = "user_message_queued"
	EventTypeQueuedUserMessageSent EventType = "queued_user_message_sent"
	EventTypeStartSubagent         EventType = "start_subagent"
	EventTypeAssistantText         EventType = "assistant_text"
	EventTypeAssistantReasoning    EventType = "assistant_reasoning"
	EventTypeToolCall              EventType = "tool_call"
	EventTypeToolOutput            EventType = "tool_output"
	EventTypeToolComplete          EventType = "tool_complete"
	EventTypeAssistantTurnComplete EventType = "assistant_turn_complete"
	EventTypeWarning               EventType = "warning"
	EventTypeRetry                 EventType = "retry"
)

// Event conveys progress or status updates from the agent loop. Which fields are set depends on the Type.
type Event struct {
	Agent                   AgentMeta
	Type                    EventType
	Error                   error
	UserMessage             string
	StartSubagent           StartSubagent
	AssistantTextFinalizing bool
	TextContent             llmstream.TextContent
	ReasoningContent        llmstream.ReasoningContent
	Tool                    llmstream.Tool
	ToolCall                *llmstream.ToolCall
	ToolOutput              ToolOutput
	ToolResult              *llmstream.ToolResult
	Turn                    *llmstream.Turn
}

// AgentMeta carries metadata describing which agent produced an event.
type AgentMeta struct {
	ID     string
	Depth  int
	Parent string
}

// StartSubagent describes the start of a subagent run within a tool call.
type StartSubagent struct {
	CallingAgentID string
	ToolCallID     string
	Label          string
}

// ToolOutput is display-only output emitted by a running tool.
type ToolOutput struct {
	Content string
}
