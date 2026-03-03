package anthropic

import (
	"encoding/json"
	"fmt"
)

// MessageRequest is request shape for POST /v1/messages with stream=true.
type MessageRequest struct {
	Model         string
	MaxTokens     int64
	System        string
	Messages      []MessageParam
	Tools         []ToolParam
	ToolChoice    *ToolChoiceParam
	Temperature   *float64
	ServiceTier   string // "", "auto", or "standard_only"
	StopSequences []string
	Thinking      *ThinkingParam
	OutputConfig  *OutputConfigParam
	CacheControl  *CacheControlParam
}

type MessageParam struct {
	Role    string // "user" or "assistant"
	Content []ContentBlockParam
}

// ContentBlockParam covers block types needed by llmstream conversation encoding.
type ContentBlockParam struct {
	Type         string // "text", "tool_use", "tool_result", "thinking", "redacted_thinking"
	Text         string // text
	ID           string // tool_use
	Name         string
	Input        json.RawMessage
	ToolUseID    string // tool_result
	Result       string
	IsError      bool
	Thinking     string // thinking
	Signature    string
	CacheControl *CacheControlParam // CacheControl configures prompt caching for this content block.
}

type ToolParam struct {
	Name         string
	Description  string
	InputSchema  json.RawMessage // JSON Schema object
	CacheControl *CacheControlParam
}

type ToolChoiceParam struct {
	Type string // "auto", "any", "tool", or "none"
	Name string // required when Type == "tool"
}

type ThinkingParam struct {
	Type         string // "adaptive", "enabled", or "disabled"
	BudgetTokens int64  // required when Type == "enabled"
}
type OutputConfigParam struct {
	Effort string
	Format *OutputFormatParam
}
type OutputFormatParam struct {
	Type   string
	Schema json.RawMessage // JSON Schema object
}
type outputConfigParamJSON struct {
	Effort string             `json:"effort,omitempty"`
	Format *OutputFormatParam `json:"format,omitempty"`
}
type outputFormatParamJSON struct {
	Type   string          `json:"type"`
	Schema json.RawMessage `json:"schema"`
}

func (o OutputConfigParam) MarshalJSON() ([]byte, error) {
	return json.Marshal(outputConfigParamJSON(o))
}
func (o OutputFormatParam) MarshalJSON() ([]byte, error) {
	return json.Marshal(outputFormatParamJSON(o))
}

type CacheControlParam struct {
	Type string // "ephemeral"
	TTL  string // "5m" or "1h" ("5m" is default)
}

type messageParamJSON struct {
	Role    string              `json:"role"`
	Content []ContentBlockParam `json:"content"`
}

func (m MessageParam) MarshalJSON() ([]byte, error) {
	return json.Marshal(messageParamJSON(m))
}

type contentBlockParamJSON struct {
	Type         string             `json:"type"`
	Text         string             `json:"text,omitempty"`
	ID           string             `json:"id,omitempty"`
	Name         string             `json:"name,omitempty"`
	Input        json.RawMessage    `json:"input,omitempty"`
	ToolUseID    string             `json:"tool_use_id,omitempty"`
	Content      string             `json:"content,omitempty"`
	IsError      bool               `json:"is_error,omitempty"`
	Thinking     string             `json:"thinking,omitempty"`
	Signature    string             `json:"signature,omitempty"`
	CacheControl *CacheControlParam `json:"cache_control,omitempty"`
}

func (c ContentBlockParam) MarshalJSON() ([]byte, error) {
	return json.Marshal(contentBlockParamJSON{
		Type:         c.Type,
		Text:         c.Text,
		ID:           c.ID,
		Name:         c.Name,
		Input:        c.Input,
		ToolUseID:    c.ToolUseID,
		Content:      c.Result,
		IsError:      c.IsError,
		Thinking:     c.Thinking,
		Signature:    c.Signature,
		CacheControl: c.CacheControl,
	})
}

type toolParamJSON struct {
	Name         string             `json:"name"`
	Description  string             `json:"description,omitempty"`
	InputSchema  json.RawMessage    `json:"input_schema"`
	CacheControl *CacheControlParam `json:"cache_control,omitempty"`
}
type toolChoiceParamJSON struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}
type thinkingParamJSON struct {
	Type         string `json:"type"`
	BudgetTokens int64  `json:"budget_tokens,omitempty"`
}
type cacheControlParamJSON struct {
	Type string `json:"type"`
	TTL  string `json:"ttl,omitempty"`
}

func (t ToolParam) MarshalJSON() ([]byte, error) {
	return json.Marshal(toolParamJSON(t))
}

func (t ToolChoiceParam) MarshalJSON() ([]byte, error) {
	return json.Marshal(toolChoiceParamJSON(t))
}

func (t ThinkingParam) MarshalJSON() ([]byte, error) {
	return json.Marshal(thinkingParamJSON(t))
}
func (c CacheControlParam) MarshalJSON() ([]byte, error) {
	return json.Marshal(cacheControlParamJSON(c))
}

type EventType string

const (
	EventTypeMessageStart      EventType = "message_start"
	EventTypeContentBlockStart EventType = "content_block_start"
	EventTypeContentBlockDelta EventType = "content_block_delta"
	EventTypeContentBlockStop  EventType = "content_block_stop"
	EventTypeMessageDelta      EventType = "message_delta"
	EventTypeMessageStop       EventType = "message_stop"
	EventTypePing              EventType = "ping"
	EventTypeError             EventType = "error"
)

// Event is one decoded Anthropic streaming event.
type Event struct {
	Type         EventType
	Raw          json.RawMessage
	Index        int                // Populated when event type carries an index (content block events).
	Message      *Message           // Populated for message_start.
	ContentBlock *ContentBlock      // Populated for content_block_start.
	Delta        *ContentBlockDelta // Populated for content_block_delta.
	MessageDelta *MessageDelta      // Populated for message_delta.
	Error        *APIError          // Populated for error.
}

type ContentBlockDelta struct {
	Type        string // "text_delta", "thinking_delta", "input_json_delta", "signature_delta"
	Text        string
	Thinking    string
	PartialJSON string
	Signature   string
}

type MessageDelta struct {
	StopReason   string
	StopSequence string
	Usage        Usage
}

// Message is Anthropic message object in stream events.
type Message struct {
	ID           string
	Role         string // "assistant"
	Model        string
	Content      []ContentBlock
	StopReason   string
	StopSequence string
	Usage        Usage
}

type ContentBlock struct {
	Type      string // "text", "tool_use", "thinking", "redacted_thinking"
	Text      string // text
	ID        string // tool_use
	Name      string
	Input     json.RawMessage
	Thinking  string // thinking
	Signature string
}

// https://platform.claude.com/docs/en/build-with-claude/prompt-caching total_input_tokens = cache_read_input_tokens + cache_creation_input_tokens + input_tokens
// Also, cache_creation_input_tokens and input_tokens are disjoint.
type Usage struct {
	InputTokens              int64
	CacheCreationInputTokens int64
	CacheReadInputTokens     int64
	OutputTokens             int64
	CacheCreation            CacheCreationUsage
}
type CacheCreationUsage struct {
	Ephemeral5mInputTokens int64
	Ephemeral1hInputTokens int64
}

// APIError is the "error" object from Anthropic error events/responses.
type APIError struct {
	Type    string
	Message string
}

func (e *APIError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Type != "" && e.Message != "" {
		return fmt.Sprintf("%s: %s", e.Type, e.Message)
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Type != "" {
		return e.Type
	}
	return "anthropic API error"
}

type messageJSON struct {
	ID           string         `json:"id"`
	Role         string         `json:"role"`
	Model        string         `json:"model"`
	Content      []ContentBlock `json:"content"`
	StopReason   string         `json:"stop_reason"`
	StopSequence string         `json:"stop_sequence"`
	Usage        Usage          `json:"usage"`
}

func (m *Message) UnmarshalJSON(data []byte) error {
	var payload messageJSON
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	m.ID = payload.ID
	m.Role = payload.Role
	m.Model = payload.Model
	m.Content = payload.Content
	m.StopReason = payload.StopReason
	m.StopSequence = payload.StopSequence
	m.Usage = payload.Usage
	return nil
}

type contentBlockJSON struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	Thinking  string          `json:"thinking"`
	Signature string          `json:"signature"`
}

func (c *ContentBlock) UnmarshalJSON(data []byte) error {
	var payload contentBlockJSON
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	c.Type = payload.Type
	c.Text = payload.Text
	c.ID = payload.ID
	c.Name = payload.Name
	c.Input = payload.Input
	c.Thinking = payload.Thinking
	c.Signature = payload.Signature
	return nil
}

type usageJSON struct {
	InputTokens              int64                  `json:"input_tokens"`
	CacheCreationInputTokens int64                  `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64                  `json:"cache_read_input_tokens"`
	OutputTokens             int64                  `json:"output_tokens"`
	CacheCreation            cacheCreationUsageJSON `json:"cache_creation"`
}
type cacheCreationUsageJSON struct {
	Ephemeral5mInputTokens int64 `json:"ephemeral_5m_input_tokens"`
	Ephemeral1hInputTokens int64 `json:"ephemeral_1h_input_tokens"`
}

func (u *Usage) UnmarshalJSON(data []byte) error {
	var payload usageJSON
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	u.InputTokens = payload.InputTokens
	u.CacheCreationInputTokens = payload.CacheCreationInputTokens
	u.CacheReadInputTokens = payload.CacheReadInputTokens
	u.OutputTokens = payload.OutputTokens
	u.CacheCreation = CacheCreationUsage{
		Ephemeral5mInputTokens: payload.CacheCreation.Ephemeral5mInputTokens,
		Ephemeral1hInputTokens: payload.CacheCreation.Ephemeral1hInputTokens,
	}
	return nil
}
