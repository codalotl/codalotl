# llmstream

llmstream is an abstraction over LLM providers, offering a unified interface. Streaming only.

## Providers

### OpenAI

- Implements responses API only.

### Anthropic

- Only supports Opus/Sonnet 4.6+.
- Uses model metadata `MaxOutput` for `max_tokens` (falls back to 32k when unknown)
- Uses "adaptive" thinking type (budget omitted).
- `Options.ReasoningEffort` maps appropriately to `output_config { effort }`.

### Gemini

- Uses the internal Gemini streaming client in `internal/llmstream/gemini`.
- Keeps exact Gemini `Content` history in parallel with `Turn`s for resend/retry, including thought signatures.
- Resends prior model turns in Gemini-native shape, including function calls and thinking parts.
- If Gemini returns `STOP` with no text, reasoning, or tool calls, retries same conversation state up to 3 times. If still empty, returns error.

## Diagnostic Hooks

To support diagnostics and request/response recording, hooks are available (scoped at package level, to avoid polluting the primary API).

Currently only supported for:
- OpenAI responses

```go {api}
// DiagnosticHookReceiver receives AddTurn calls with a request/response pair. The request is the JSON-ish into, for instance, OpenAI's /v1/responses. The response
// is the completed response object (or potentially, an error object). Even though responses are streamed, the `response` here represents the completed object, as
// if there was no streaming (ex: `{"id": "resp_123", "object": "response", ...}`).
//
// This method may be called eagerly as soon as we know the response object, but must be called before SendAsync returns.
type DiagnosticHookReceiver interface {
	AddTurn(request map[string]any, response map[string]any)
}

// AddDiagnosticHook adds recv to a list of hook receivers, which will be called when a turn is complete (we have a request/response pair). It returns an unregister
// function that removes this hook. The unregister function is safe to call multiple times.
func AddDiagnosticHook(recv DiagnosticHookReceiver) (unregister func())
```

## Tool Presentation

Tools own semantic presentation of their call/completion lifecycle.

- Presentation is structured data, not pre-rendered ANSI or width-specific text.
- Formatter and TUI concerns stay outside `llmstream`.
- Presenter output is deterministic from `ToolCall` and optional `ToolResult`.
- `nil` result means tool call in progress.
- Completion behavior indicates whether a completion replaces the earlier call entry or appends as a new entry.

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
	Presenter() Presenter
	Run(ctx context.Context, params ToolCall) ToolResult
}

type Presenter interface {
	Present(call ToolCall, result *ToolResult) Presentation
}

type CompletionBehavior string

const (
	CompletionBehaviorReplace CompletionBehavior = "replace"
	CompletionBehaviorAppend  CompletionBehavior = "append"
)

type Presentation struct {
	Behavior CompletionBehavior
	Summary  Line
	Body     []Block
}

type Line struct {
	Segments []Segment
}

type Segment struct {
	Text string
	Role SegmentRole
}

type SegmentRole string

const (
	RoleNormal   SegmentRole = "normal"
	RoleAccent   SegmentRole = "accent"
	RoleAction   SegmentRole = "action"
	RoleSuccess  SegmentRole = "success"
	RoleError    SegmentRole = "error"
	RoleCode     SegmentRole = "code"
	RoleEmphasis SegmentRole = "emphasis"
)

type Block interface{ isBlock() }

type Paragraph struct {
	Lines []Line
}

type Checklist struct {
	Items []ChecklistItem
}

type ChecklistItem struct {
	Status ChecklistStatus
	Line   Line
}

type ChecklistStatus string

const (
	ChecklistDone       ChecklistStatus = "done"
	ChecklistInProgress ChecklistStatus = "in_progress"
	ChecklistPending    ChecklistStatus = "pending"
)

type Output struct {
	Lines []OutputLine
}

type OutputLine struct {
	Line Line
	Role OutputRole
}

type OutputRole string

const (
	OutputRoleNormal  OutputRole = "normal"
	OutputRoleSuccess OutputRole = "success"
	OutputRoleError   OutputRole = "error"
	OutputRoleAccent  OutputRole = "accent"
)

type Diff struct {
	Files []DiffFile
}

type DiffFile struct {
	Kind       DiffFileKind
	Path       string
	ToPath     string
	ReplaceAll bool
	Lines      []DiffLine
}

type DiffFileKind string

const (
	DiffFileAdd        DiffFileKind = "add"
	DiffFileDelete     DiffFileKind = "delete"
	DiffFileEdit       DiffFileKind = "edit"
	DiffFileRenameOnly DiffFileKind = "rename_only"
)

type DiffLine struct {
	Kind DiffLineKind
	Text string
}

type DiffLineKind string

const (
	DiffLineContext DiffLineKind = "context"
	DiffLineAdd     DiffLineKind = "add"
	DiffLineRemove  DiffLineKind = "remove"
	DiffLineGap     DiffLineKind = "gap"
)

type ToolInfo struct {
	Name        string
	Description string
	Parameters  map[string]any
	Required    []string
	Kind        ToolKind
	Grammar     *ToolGrammar
}
```
