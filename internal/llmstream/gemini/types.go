package gemini

import (
	"encoding/json"
	"fmt"
)

// InteractionRequest is the request shape for POST /v1beta/interactions with stream=true.
//
// This package intentionally supports only the subset of request fields needed by llmstream.
type InteractionRequest struct {
	Model                 string            `json:"model"`
	Input                 []Turn            `json:"input"`
	SystemInstruction     string            `json:"system_instruction,omitempty"`
	Tools                 []Tool            `json:"tools,omitempty"`
	ResponseFormat        map[string]any    `json:"response_format,omitempty"`
	ResponseMIMEType      string            `json:"response_mime_type,omitempty"`
	Store                 *bool             `json:"store,omitempty"`
	GenerationConfig      *GenerationConfig `json:"generation_config,omitempty"`
	PreviousInteractionID string            `json:"previous_interaction_id,omitempty"`
}

// Turn is one conversational turn in InteractionRequest.Input or Interaction.Input.
type Turn struct {
	Role    string    `json:"role,omitempty"` // "user" or "model"
	Content []Content `json:"content"`
}

// Content is the subset of Gemini Interactions content blocks needed by llmstream.
type Content struct {
	Type      string               `json:"type"`
	Text      string               `json:"text,omitempty"`
	Summary   []ThoughtSummaryPart `json:"summary,omitempty"`
	Signature string               `json:"signature,omitempty"`
	Name      string               `json:"name,omitempty"`
	ID        string               `json:"id,omitempty"`
	Arguments map[string]any       `json:"arguments,omitempty"`
	CallID    string               `json:"call_id,omitempty"`
	Result    any                  `json:"result,omitempty"`
	IsError   bool                 `json:"is_error,omitempty"`
	Data      string               `json:"data,omitempty"`
	URI       string               `json:"uri,omitempty"`
	MIMEType  string               `json:"mime_type,omitempty"`
}

// ThoughtSummaryPart is one item in a thought summary. llmstream uses text summaries, but the type field is preserved.
type ThoughtSummaryPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	URI      string `json:"uri,omitempty"`
	MIMEType string `json:"mime_type,omitempty"`
}

// Tool declares a function tool.
type Tool struct {
	Type        string         `json:"type"` // "function"
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// GenerationConfig configures model behavior for an interaction.
type GenerationConfig struct {
	Temperature       *float64 `json:"temperature,omitempty"`
	TopP              *float64 `json:"top_p,omitempty"`
	Seed              *int64   `json:"seed,omitempty"`
	StopSequences     []string `json:"stop_sequences,omitempty"`
	ThinkingLevel     string   `json:"thinking_level,omitempty"`
	ThinkingSummaries string   `json:"thinking_summaries,omitempty"`
	MaxOutputTokens   *int64   `json:"max_output_tokens,omitempty"`
	ToolChoice        any      `json:"tool_choice,omitempty"`
}

// GetInteractionOptions configures GET /v1beta/interactions/{id} in streaming mode.
type GetInteractionOptions struct {
	LastEventID  string
	IncludeInput bool
}

type EventType string

const (
	EventTypeInteractionStart        EventType = "interaction.start"
	EventTypeInteractionComplete     EventType = "interaction.complete"
	EventTypeInteractionStatusUpdate EventType = "interaction.status_update"
	EventTypeContentStart            EventType = "content.start"
	EventTypeContentDelta            EventType = "content.delta"
	EventTypeContentStop             EventType = "content.stop"
	EventTypeError                   EventType = "error"
)

// Event is one decoded Gemini interactions SSE event.
type Event struct {
	Type EventType
	Raw  json.RawMessage

	// EventID is the JSON event_id field used for stream resumption.
	EventID string

	Interaction   *Interaction
	InteractionID string
	Status        string
	Index         int
	Content       *Content
	Delta         *Delta
	Error         *APIError
}

// Delta is the union shape carried by content.delta events.
type Delta struct {
	Type           string              `json:"type"`
	Text           string              `json:"text,omitempty"`
	SummaryContent *ThoughtSummaryPart `json:"content,omitempty"`
	Signature      string              `json:"signature,omitempty"`
	Name           string              `json:"name,omitempty"`
	ID             string              `json:"id,omitempty"`
	Arguments      map[string]any      `json:"arguments,omitempty"`
	CallID         string              `json:"call_id,omitempty"`
	Result         any                 `json:"result,omitempty"`
	IsError        bool                `json:"is_error,omitempty"`
}

// Interaction is the interaction resource carried in start/complete events and GET responses.
type Interaction struct {
	ID                    string    `json:"id,omitempty"`
	Model                 string    `json:"model,omitempty"`
	Object                string    `json:"object,omitempty"`
	Status                string    `json:"status,omitempty"`
	Role                  string    `json:"role,omitempty"`
	Created               string    `json:"created,omitempty"`
	Updated               string    `json:"updated,omitempty"`
	Outputs               []Content `json:"outputs,omitempty"`
	Input                 []Turn    `json:"input,omitempty"`
	SystemInstruction     string    `json:"system_instruction,omitempty"`
	PreviousInteractionID string    `json:"previous_interaction_id,omitempty"`
	Tools                 []Tool    `json:"tools,omitempty"`
	Usage                 *Usage    `json:"usage,omitempty"`
}

// Usage contains token accounting returned by Gemini interactions.
type Usage struct {
	InputTokensByModality  []ModalityTokens `json:"input_tokens_by_modality,omitempty"`
	OutputTokensByModality []ModalityTokens `json:"output_tokens_by_modality,omitempty"`
	TotalCachedTokens      int64            `json:"total_cached_tokens,omitempty"`
	TotalInputTokens       int64            `json:"total_input_tokens,omitempty"`
	TotalOutputTokens      int64            `json:"total_output_tokens,omitempty"`
	TotalThoughtTokens     int64            `json:"total_thought_tokens,omitempty"`
	TotalTokens            int64            `json:"total_tokens,omitempty"`
	TotalToolUseTokens     int64            `json:"total_tool_use_tokens,omitempty"`
}

type ModalityTokens struct {
	Modality string `json:"modality,omitempty"`
	Tokens   int64  `json:"tokens,omitempty"`
}

// APIError is the error object from Gemini interaction error events.
type APIError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

func (e *APIError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Code != "" && e.Message != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Code != "" {
		return e.Code
	}
	return "gemini API error"
}
