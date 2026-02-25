# anthropic

The anthropic package implements a minimal client to perform streaming requests to the Anthropic LLM.

## Dependencies

No third party "Anthropic SDK"-style modules are used. This package should not make net-new deps that the rest of the repo does not need.

- `internal/q/sseclient` for SSE.

## Scope

Only the portion of the API needed to implement `llmstream` will be implemented:
- `/v1/messages`
- Only streaming

Not:
- `/v1/messages/batches`, `/v1/messages/count_tokens`, `/v1/models`, `/v1/skills`, `/v1/files` (this list is not exhaustive)

## Testing

Employs both stubbed tests (don't hit actual endpoints) and integration tests (hit anthropic endpoints).

Integration tests are gated behind the `INTEGRATION_TEST` env var. When this is set (to any non-empty value), it reads and uses `ANTHROPIC_API_KEY`.

## Docs

API documentation from Anthropic can be found on disk (saved 2026-02-25):
- https://platform.claude.com/docs/en/api/overview - `docs/api_overview.md`
- https://platform.claude.com/docs/en/api/messages - `docs/create_message.md`

## Public API

```go
// DefaultBaseURL is Anthropic's direct API endpoint.
const DefaultBaseURL = "https://api.anthropic.com"

// DefaultVersion is sent in anthropic-version when no override is configured.
const DefaultVersion = "2023-06-01"

type Option func(*Client)

// WithHTTPClient sets the HTTP client used for requests.
func WithHTTPClient(hc *http.Client) Option

// WithBaseURL overrides API origin (ex: proxy/testing endpoint).
func WithBaseURL(baseURL string) Option

// WithVersion overrides anthropic-version header value.
func WithVersion(version string) Option

// WithBeta appends an anthropic-beta header value for all requests.
func WithBeta(beta string) Option

// Client sends streaming requests to Anthropic Messages API.
type Client struct {
	// contains filtered or unexported fields
}

// New constructs a Client. apiKey is sent as x-api-key.
func New(apiKey string, opts ...Option) *Client

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
}

type MessageParam struct {
	Role    string // "user" or "assistant"
	Content []ContentBlockParam
}

// ContentBlockParam covers block types needed by llmstream conversation encoding.
type ContentBlockParam struct {
	Type      string // "text", "tool_use", "tool_result", "thinking", "redacted_thinking"
	Text      string // text
	ID        string // tool_use
	Name      string
	Input     json.RawMessage
	ToolUseID string // tool_result
	Result    string
	IsError   bool
	Thinking  string // thinking
	Signature string
}

type ToolParam struct {
	Name        string
	Description string
	InputSchema json.RawMessage // JSON Schema object
}

type ToolChoiceParam struct {
	Type string // "auto", "any", "tool", or "none"
	Name string // required when Type == "tool"
}

type ThinkingParam struct {
	Type         string // "enabled" or "disabled"
	BudgetTokens int64  // required when Type == "enabled"
}

// StreamMessages starts POST /v1/messages in streaming mode.
func (c *Client) StreamMessages(ctx context.Context, req MessageRequest) (*Stream, error)

// Stream decodes SSE events for one streaming request.
type Stream struct {
	// contains filtered or unexported fields
}

// Recv blocks until next stream event or end-of-stream. Returns io.EOF after message_stop.
func (s *Stream) Recv() (Event, error)

// RecvContext is like Recv but with per-call cancellation/deadline control.
func (s *Stream) RecvContext(ctx context.Context) (Event, error)

// Close closes stream body. Idempotent.
func (s *Stream) Close() error

// Response returns HTTP response metadata.
func (s *Stream) Response() *http.Response

// RequestID returns request-id response header value.
func (s *Stream) RequestID() string

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

type Usage struct {
	InputTokens              int64
	CacheCreationInputTokens int64
	CacheReadInputTokens     int64
	OutputTokens             int64
}

// APIError is the "error" object from Anthropic error events/responses.
type APIError struct {
	Type    string
	Message string
}

func (e *APIError) Error() string
```
