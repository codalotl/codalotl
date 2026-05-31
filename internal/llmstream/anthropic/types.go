package anthropic

import (
	"encoding/json"
	"fmt"
)

// MessageRequest is request shape for POST /v1/messages with stream=true.
type MessageRequest struct {
	Model         string             // Model is the Anthropic model name.
	MaxTokens     int64              // MaxTokens is the maximum number of tokens to generate.
	System        string             // System is the optional system prompt.
	Messages      []MessageParam     // Messages is the conversation history to send.
	Tools         []ToolParam        // Tools is the set of tools available to the model.
	ToolChoice    *ToolChoiceParam   // ToolChoice controls whether and how the model may use tools.
	Temperature   *float64           // Temperature controls sampling; nil omits the parameter.
	ServiceTier   string             // "", "auto", or "standard_only"
	StopSequences []string           // StopSequences are custom sequences that stop generation.
	Thinking      *ThinkingParam     // Thinking configures Anthropic thinking when set.
	OutputConfig  *OutputConfigParam // OutputConfig configures Anthropic output options when set.
	CacheControl  *CacheControlParam // CacheControl configures prompt caching for the request.
}

// MessageParam is one input message in a Messages API request.
type MessageParam struct {
	Role    string              // "user" or "assistant"
	Content []ContentBlockParam // Content is the ordered message content blocks.
}

// ContentBlockParam covers block types needed by llmstream conversation encoding.
type ContentBlockParam struct {
	Type         string             // "text", "tool_use", "tool_result", "thinking", "redacted_thinking"
	Text         string             // text
	ID           string             // tool_use
	Name         string             // Name is the tool name for a tool_use block.
	Input        json.RawMessage    // Input is the raw JSON input for a tool_use block.
	ToolUseID    string             // tool_result
	Result       string             // Result is the tool_result content sent as Anthropic's content field.
	IsError      bool               // IsError marks a tool_result block as an error result.
	Thinking     string             // thinking
	Signature    string             // Signature verifies a thinking block when Anthropic provides one.
	CacheControl *CacheControlParam // CacheControl configures prompt caching for this content block.
}

// ToolParam describes a tool available to the model.
type ToolParam struct {
	Name         string             // Name is the tool name exposed to the model.
	Description  string             // Description explains what the tool does and when to use it.
	InputSchema  json.RawMessage    // JSON Schema object
	CacheControl *CacheControlParam // CacheControl configures prompt caching for this tool definition.
}

// ToolChoiceParam controls whether and how the model may use tools.
type ToolChoiceParam struct {
	Type string // "auto", "any", "tool", or "none"
	Name string // required when Type == "tool"
}

// ThinkingParam configures Anthropic thinking for a request.
type ThinkingParam struct {
	Type         string // "adaptive", "enabled", or "disabled"
	BudgetTokens int64  // required when Type == "enabled"
}

// OutputConfigParam configures Anthropic output options.
type OutputConfigParam struct {
	Effort string             // Effort selects the requested output effort when supported.
	Format *OutputFormatParam // Format requests a structured output format when set.
}

// OutputFormatParam configures the requested output format.
type OutputFormatParam struct {
	Type   string          // Type identifies the output format, such as "json_schema".
	Schema json.RawMessage // JSON Schema object
}

// outputConfigParamJSON is the JSON wire shape for OutputConfigParam.
type outputConfigParamJSON struct {
	Effort string             `json:"effort,omitempty"` // Effort selects the requested output effort when supported.
	Format *OutputFormatParam `json:"format,omitempty"` // Format requests a structured output format when set.
}

// outputFormatParamJSON is the JSON wire shape for OutputFormatParam.
type outputFormatParamJSON struct {
	Type   string          `json:"type"`   // Type identifies the output format.
	Schema json.RawMessage `json:"schema"` // Schema is the JSON Schema object for the output format.
}

// MarshalJSON encodes o as an Anthropic output_config object.
func (o OutputConfigParam) MarshalJSON() ([]byte, error) {
	return json.Marshal(outputConfigParamJSON(o))
}

// MarshalJSON encodes o as an Anthropic output format object.
func (o OutputFormatParam) MarshalJSON() ([]byte, error) {
	return json.Marshal(outputFormatParamJSON(o))
}

// CacheControlParam configures prompt caching for a request object.
type CacheControlParam struct {
	Type string // "ephemeral"
	TTL  string // "5m" or "1h" ("5m" is default)
}

// messageParamJSON is the JSON wire shape for MessageParam.
type messageParamJSON struct {
	Role    string              `json:"role"`    // Role is the message role, such as "user" or "assistant".
	Content []ContentBlockParam `json:"content"` // Content is the ordered message content blocks.
}

// MarshalJSON encodes m as an Anthropic message object.
func (m MessageParam) MarshalJSON() ([]byte, error) {
	return json.Marshal(messageParamJSON(m))
}

// contentBlockParamJSON is the JSON wire shape for ContentBlockParam.
type contentBlockParamJSON struct {
	Type         string             `json:"type"`                    // Type is the content block kind.
	Text         string             `json:"text,omitempty"`          // Text is the text block content.
	ID           string             `json:"id,omitempty"`            // ID identifies a tool_use block.
	Name         string             `json:"name,omitempty"`          // Name is the tool name for a tool_use block.
	Input        json.RawMessage    `json:"input,omitempty"`         // Input is the raw JSON input for a tool_use block.
	ToolUseID    string             `json:"tool_use_id,omitempty"`   // ToolUseID identifies the tool_use block answered by a tool_result block.
	Content      string             `json:"content,omitempty"`       // Content is the tool_result content.
	IsError      bool               `json:"is_error,omitempty"`      // IsError marks a tool_result block as an error result.
	Thinking     string             `json:"thinking,omitempty"`      // Thinking is the reasoning text for a thinking block.
	Signature    string             `json:"signature,omitempty"`     // Signature verifies a thinking block when Anthropic provides one.
	CacheControl *CacheControlParam `json:"cache_control,omitempty"` // CacheControl configures prompt caching for this content block.
}

// MarshalJSON encodes c as an Anthropic content block object.
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

// toolParamJSON is the JSON wire shape for ToolParam.
type toolParamJSON struct {
	Name         string             `json:"name"`                    // Name is the tool name exposed to the model.
	Description  string             `json:"description,omitempty"`   // Description explains what the tool does and when to use it.
	InputSchema  json.RawMessage    `json:"input_schema"`            // InputSchema is the JSON Schema object for the tool input.
	CacheControl *CacheControlParam `json:"cache_control,omitempty"` // CacheControl configures prompt caching for this tool definition.
}

// toolChoiceParamJSON is the JSON wire shape for ToolChoiceParam.
type toolChoiceParamJSON struct {
	Type string `json:"type"`           // Type is the tool choice mode.
	Name string `json:"name,omitempty"` // Name is the selected tool name when Type is "tool".
}

// thinkingParamJSON is the JSON wire shape for ThinkingParam.
type thinkingParamJSON struct {
	Type         string `json:"type"`                    // Type is the thinking mode.
	BudgetTokens int64  `json:"budget_tokens,omitempty"` // BudgetTokens is the requested thinking token budget.
}

// cacheControlParamJSON is the JSON wire shape for CacheControlParam.
type cacheControlParamJSON struct {
	Type string `json:"type"`          // Type is the cache control type.
	TTL  string `json:"ttl,omitempty"` // TTL is the cache lifetime.
}

// MarshalJSON encodes t as an Anthropic tool object.
func (t ToolParam) MarshalJSON() ([]byte, error) {
	return json.Marshal(toolParamJSON(t))
}

// MarshalJSON encodes t as an Anthropic tool_choice object.
func (t ToolChoiceParam) MarshalJSON() ([]byte, error) {
	return json.Marshal(toolChoiceParamJSON(t))
}

// MarshalJSON encodes t as an Anthropic thinking object.
func (t ThinkingParam) MarshalJSON() ([]byte, error) {
	return json.Marshal(thinkingParamJSON(t))
}

// MarshalJSON encodes c as an Anthropic cache_control object.
func (c CacheControlParam) MarshalJSON() ([]byte, error) {
	return json.Marshal(cacheControlParamJSON(c))
}

// EventType identifies the kind of Anthropic streaming event.
type EventType string

// Event types emitted by the Anthropic streaming Messages API.
const (
	EventTypeMessageStart      EventType = "message_start"       // EventTypeMessageStart begins a streamed message and carries the initial Message.
	EventTypeContentBlockStart EventType = "content_block_start" // EventTypeContentBlockStart begins a content block and carries its index.
	EventTypeContentBlockDelta EventType = "content_block_delta" // EventTypeContentBlockDelta carries incremental content for a content block.
	EventTypeContentBlockStop  EventType = "content_block_stop"  // EventTypeContentBlockStop ends a content block and carries its index.
	EventTypeMessageDelta      EventType = "message_delta"       // EventTypeMessageDelta carries message-level stop and usage updates.
	EventTypeMessageStop       EventType = "message_stop"        // EventTypeMessageStop marks the end of a streamed message.
	EventTypePing              EventType = "ping"                // EventTypePing is a keepalive event.
	EventTypeError             EventType = "error"               // EventTypeError carries an Anthropic API error.
)

// Event is one decoded Anthropic streaming event.
type Event struct {
	Type         EventType          // Type identifies the streaming event kind.
	Raw          json.RawMessage    // Raw is the original JSON event payload.
	Index        int                // Populated when event type carries an index (content block events).
	Message      *Message           // Populated for message_start.
	ContentBlock *ContentBlock      // Populated for content_block_start.
	Delta        *ContentBlockDelta // Populated for content_block_delta.
	MessageDelta *MessageDelta      // Populated for message_delta.
	Error        *APIError          // Populated for error.
}

// ContentBlockDelta is an incremental update for a streamed content block.
type ContentBlockDelta struct {
	Type        string // "text_delta", "thinking_delta", "input_json_delta", "signature_delta"
	Text        string // Text is the appended text for a text delta.
	Thinking    string // Thinking is the appended reasoning text for a thinking delta.
	PartialJSON string // PartialJSON is the appended JSON fragment for a tool input delta.
	Signature   string // Signature is the appended signature data for a signature delta.
}

// MessageDelta contains message-level updates from a message_delta event.
type MessageDelta struct {
	StopReason   string // StopReason is the reason generation stopped when Anthropic reports one.
	StopSequence string // StopSequence is the stop sequence that ended generation when one matched.
	Usage        Usage  // Usage is token accounting reported with the delta.
}

// Message is Anthropic message object in stream events.
type Message struct {
	ID           string         // ID is Anthropic's message identifier.
	Role         string         // "assistant"
	Model        string         // Model is the Anthropic model that generated the message.
	Content      []ContentBlock // Content is the ordered output content blocks.
	StopReason   string         // StopReason is the reason generation stopped when Anthropic reports one.
	StopSequence string         // StopSequence is the stop sequence that ended generation when one matched.
	Usage        Usage          // Usage is token accounting for the message.
}

// ContentBlock is a content block returned in Anthropic stream events.
type ContentBlock struct {
	Type      string          // "text", "tool_use", "thinking", "redacted_thinking"
	Text      string          // text
	ID        string          // tool_use
	Name      string          // Name is the tool name for a tool_use block.
	Input     json.RawMessage // Input is the raw JSON input for a tool_use block.
	Thinking  string          // thinking
	Signature string          // Signature verifies a thinking block when Anthropic provides one.
}

// https://platform.claude.com/docs/en/build-with-claude/prompt-caching total_input_tokens = cache_read_input_tokens + cache_creation_input_tokens + input_tokens
// Also, cache_creation_input_tokens and input_tokens are disjoint.
type Usage struct {
	InputTokens              int64              // InputTokens is the number of non-cache input tokens.
	CacheCreationInputTokens int64              // CacheCreationInputTokens is the number of input tokens written to cache.
	CacheReadInputTokens     int64              // CacheReadInputTokens is the number of input tokens read from cache.
	OutputTokens             int64              // OutputTokens is the number of output tokens.
	CacheCreation            CacheCreationUsage // CacheCreation breaks cache creation tokens down by cache lifetime.
}

// CacheCreationUsage reports cache creation token counts by cache lifetime.
type CacheCreationUsage struct {
	Ephemeral5mInputTokens int64 // Ephemeral5mInputTokens is the number of input tokens written to 5-minute ephemeral cache entries.
	Ephemeral1hInputTokens int64 // Ephemeral1hInputTokens is the number of input tokens written to 1-hour ephemeral cache entries.
}

// APIError is the "error" object from Anthropic error events/responses.
type APIError struct {
	Type    string // Type is Anthropic's machine-readable error type.
	Message string // Message is Anthropic's human-readable error message.
}

// Error returns a human-readable Anthropic API error string, or "<nil>" for a nil receiver.
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

// messageJSON is the JSON wire shape for Message.
type messageJSON struct {
	ID           string         `json:"id"`            // ID is Anthropic's message identifier.
	Role         string         `json:"role"`          // Role is the message role.
	Model        string         `json:"model"`         // Model is the Anthropic model that generated the message.
	Content      []ContentBlock `json:"content"`       // Content is the ordered output content blocks.
	StopReason   string         `json:"stop_reason"`   // StopReason is the reason generation stopped when Anthropic reports one.
	StopSequence string         `json:"stop_sequence"` // StopSequence is the stop sequence that ended generation when one matched.
	Usage        Usage          `json:"usage"`         // Usage is token accounting for the message.
}

// UnmarshalJSON decodes an Anthropic message object into m.
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

// contentBlockJSON is the JSON wire shape for ContentBlock.
type contentBlockJSON struct {
	Type      string          `json:"type"`      // Type is the content block kind.
	Text      string          `json:"text"`      // Text is the text block content.
	ID        string          `json:"id"`        // ID identifies a tool_use block.
	Name      string          `json:"name"`      // Name is the tool name for a tool_use block.
	Input     json.RawMessage `json:"input"`     // Input is the raw JSON input for a tool_use block.
	Thinking  string          `json:"thinking"`  // Thinking is the reasoning text for a thinking block.
	Signature string          `json:"signature"` // Signature verifies a thinking block when Anthropic provides one.
}

// UnmarshalJSON decodes an Anthropic content block into c.
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

// usageJSON is the JSON wire shape for Usage.
type usageJSON struct {
	InputTokens              int64                  `json:"input_tokens"`                // InputTokens is the number of non-cache input tokens.
	CacheCreationInputTokens int64                  `json:"cache_creation_input_tokens"` // CacheCreationInputTokens is the number of input tokens written to cache.
	CacheReadInputTokens     int64                  `json:"cache_read_input_tokens"`     // CacheReadInputTokens is the number of input tokens read from cache.
	OutputTokens             int64                  `json:"output_tokens"`               // OutputTokens is the number of output tokens.
	CacheCreation            cacheCreationUsageJSON `json:"cache_creation"`              // CacheCreation breaks cache creation tokens down by cache lifetime.
}

// cacheCreationUsageJSON is the JSON wire shape for CacheCreationUsage.
type cacheCreationUsageJSON struct {
	// Ephemeral5mInputTokens is the number of input tokens written to 5-minute ephemeral cache entries.
	Ephemeral5mInputTokens int64 `json:"ephemeral_5m_input_tokens"`

	// Ephemeral1hInputTokens is the number of input tokens written to 1-hour ephemeral cache entries.
	Ephemeral1hInputTokens int64 `json:"ephemeral_1h_input_tokens"`
}

// UnmarshalJSON decodes Anthropic usage JSON into u.
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
