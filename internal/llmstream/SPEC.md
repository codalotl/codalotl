# llmstream

llmstream is an abstraction over LLM providers, offering a unified interface. Streaming only.

## Providers

### OpenAI

- Implements responses API only.

### Anthropic

- Only supports Opus/Sonnet 4.6+.
- Hard-codes 32k max_tokens
- Uses "adaptive" thinking type (budget omitted).
- `Options.ReasoningEffort` maps appropriately to `output_config { effort }`.

## Public API

```go
type StreamingConversation interface {
	LastTurn() Turn
	Turns() []Turn
	AddTools(tools []Tool) error
	AddUserTurn(text string) error
	AddToolResults(toolResults []ToolResult) error
	SendAsync(ctx context.Context, options ...SendOptions) <-chan Event
}

func NewConversation(modelID llmmodel.ModelID, systemMessage string) StreamingConversation

type SendOptions struct {
	ReasoningEffort    string
	ReasoningSummary   string
	TemperaturePresent bool
	Temperature        float64
	ServiceTier        string
	NoLink             bool
	NoStore            bool
}

type Role int

const (
	RoleUser Role = iota
	RoleSystem
	RoleAssistant
)

type Event struct {
	Type      EventType
	Turn      *Turn
	Error     error
	Delta     string
	Text      *TextContent
	Reasoning *ReasoningContent
	Done      bool
	ToolCall  *ToolCall
}

type Turn struct {
	Role         Role
	ProviderID   string
	Parts        []ContentPart
	Usage        TokenUsage
	FinishReason FinishReason
}

type ContentPart interface{ isPart() }

type TextContent struct {
	ProviderID string `json:"provider_id"`
	Content    string `json:"content"`
}

type ReasoningContent struct {
	ProviderID    string `json:"provider_id"`
	Content       string `json:"content"`
	ProviderState string `json:"provider_state,omitempty"`
}

type ToolCall struct {
	ProviderID string `json:"provider_id"`
	CallID     string `json:"call_id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Input      string `json:"input"`
}

type ToolResult struct {
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Result    string `json:"result"`
	IsError   bool   `json:"is_error"`
	SourceErr error  `json:"-"`
}

type Tool interface {
	Info() ToolInfo
	Name() string
	Run(ctx context.Context, params ToolCall) ToolResult
}

type ToolInfo struct {
	Name        string
	Description string
	Parameters  map[string]any
	Required    []string
	Kind        ToolKind
	Grammar     *ToolGrammar
}
```
