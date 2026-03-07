# gemini

The gemini package implements a minimal client to perform streaming requests to Google Gemini API.

## Dependencies

No third party "Google SDK"-style modules are used. This package should not make net-new deps that the rest of the repo does not need.

- `internal/q/sseclient` for SSE.

## Scope

Only the portion of the API needed to implement `llmstream` will be implemented:
- Use the "interactions" API.
- Only streaming

## Testing

Employs both stubbed tests (don't hit actual endpoints) and integration tests (hit google endpoints).

Integration tests are gated behind the `INTEGRATION_TEST` env var. When this is set (to any non-empty value), it reads and uses `GEMINI_API_KEY`.

## Public API

```go
const DefaultBaseURL = "https://generativelanguage.googleapis.com"
const DefaultAPIVersion = "v1beta"

type Option func(*Client)

func WithHTTPClient(hc *http.Client) Option
func WithBaseURL(baseURL string) Option
func WithAPIVersion(version string) Option

type Client struct {
	// contains filtered or unexported fields
}

func New(apiKey string, opts ...Option) *Client

type InteractionRequest struct {
	Model                 string
	Input                 []Turn
	SystemInstruction     string
	Tools                 []Tool
	ResponseFormat        map[string]any
	ResponseMIMEType      string
	Store                 *bool
	GenerationConfig      *GenerationConfig
	PreviousInteractionID string
}

type Turn struct {
	Role    string
	Content []Content
}

type Content struct {
	Type      string
	Text      string
	Summary   []ThoughtSummaryPart
	Signature string
	Name      string
	ID        string
	Arguments map[string]any
	CallID    string
	Result    any
	IsError   bool
	Data      string
	URI       string
	MIMEType  string
}

type ThoughtSummaryPart struct {
	Type     string
	Text     string
	Data     string
	URI      string
	MIMEType string
}

type Tool struct {
	Type        string
	Name        string
	Description string
	Parameters  map[string]any
}

type GenerationConfig struct {
	Temperature       *float64
	TopP              *float64
	Seed              *int64
	StopSequences     []string
	ThinkingLevel     string
	ThinkingSummaries string
	MaxOutputTokens   *int64
	ToolChoice        any
}

type GetInteractionOptions struct {
	LastEventID  string
	IncludeInput bool
}

func (c *Client) CreateInteraction(ctx context.Context, req InteractionRequest) (*Stream, error)
func (c *Client) GetInteractionStream(ctx context.Context, interactionID string, opt *GetInteractionOptions) (*Stream, error)

type Stream struct {
	// contains filtered or unexported fields
}

func (s *Stream) Recv() (Event, error)
func (s *Stream) RecvContext(ctx context.Context) (Event, error)
func (s *Stream) Close() error
func (s *Stream) Response() *http.Response
func (s *Stream) LastEventID() string

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

type Event struct {
	Type          EventType
	Raw           json.RawMessage
	EventID       string
	Interaction   *Interaction
	InteractionID string
	Status        string
	Index         int
	Content       *Content
	Delta         *Delta
	Error         *APIError
}

type Delta struct {
	Type           string
	Text           string
	SummaryContent *ThoughtSummaryPart
	Signature      string
	Name           string
	ID             string
	Arguments      map[string]any
	CallID         string
	Result         any
	IsError        bool
}

type Interaction struct {
	ID                    string
	Model                 string
	Object                string
	Status                string
	Role                  string
	Created               string
	Updated               string
	Outputs               []Content
	Input                 []Turn
	SystemInstruction     string
	PreviousInteractionID string
	Tools                 []Tool
	Usage                 *Usage
}

type Usage struct {
	InputTokensByModality  []ModalityTokens
	OutputTokensByModality []ModalityTokens
	TotalCachedTokens      int64
	TotalInputTokens       int64
	TotalOutputTokens      int64
	TotalThoughtTokens     int64
	TotalTokens            int64
	TotalToolUseTokens     int64
}

type ModalityTokens struct {
	Modality string
	Tokens   int64
}

type APIError struct {
	Code    string
	Message string
}

func (e *APIError) Error() string
```
