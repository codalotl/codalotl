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

## Tool presentation

- A tool may expose semantic display metadata via `Presenter() Presenter`.
- `nil` presenter is valid and means the tool has no custom presentation.
- This package itself does not know or care about any tool's presenter. These types are in this package as a convenience for tool implementers to build to a common spec.
- A presenter may also define how descendant subagent events are displayed by consumers. This affects presentation only; underlying agent events are unchanged.
- `Presentation.Summary` is usually the tool-level 1-line header.
- If `Presentation.Body` is `Diff`, presenters must leave `Summary` empty.
- Consumers that need a visible 1-line diff header should derive it from `Diff.Edits[0]`.
- `Diff.Edits` are in display order; first edit is the lead edit/header source.
- A **partial** `Presentation` type tree is below in Public API.

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

type ToolInfo struct {
	Name        string
	Description string
	Parameters  map[string]any
	Required    []string
	Kind        ToolKind
	Grammar     *ToolGrammar
}

// A Presenter can present a tool call (and optional result) in a "semantic" way - a tree representation that can be further styled for different modalities. As
// an analogy, it's the HTML (but not the CSS) of underlying data.
type Presenter interface {
	// Present presents call and result in a semantic way (no width decisions, no assumptions about ANSI terminals, colors). To present a tool call (no result yet),
	// call Present(call, nil). To present a call with result, call Present(call, result). For instance, for a read file tool, the call might return the equivalent of
	// "Reading file.go". The result might return the equivalent of "Read file.go (123 bytes)".
	Present(call ToolCall, result *ToolResult) Presentation

	// SubagentEventPolicy defines how descendant subagent events are displayed by consumers. Tools that do not launch subagents can return
	// SubagentEventPolicyDefault.
	SubagentEventPolicy(call ToolCall) SubagentEventPolicy
}

type SubagentEventPolicy string

const (
	SubagentEventPolicyDefault          SubagentEventPolicy = ""
	SubagentEventPolicyHideFinalMessage SubagentEventPolicy = "hide_final_message"
)

// CompletionBehavior indicates what happens when the tool completes. For instance, imagine a TUI:
//   - With Replace, the tool call presentation is replaced by the result presentation (ideal for quick and/or atomic operations like reading a file).
//   - With Append, the tool call is displayed. When the result comes in, it should also be displayed (ideal for subagents, which are long-lived and themselves emit
//     tool calls).
type CompletionBehavior string

const (
	CompletionBehaviorReplace CompletionBehavior = "replace"
	CompletionBehaviorAppend  CompletionBehavior = "append"
)

// A Presentation is a semantic representation of a tool call (with optional tool result).
//   - Strings should not contain ANSI escape sequences or colors.
//   - Do not include "•" (leading bullets typical in TUI event streams).
//   - Do not include "└" (common in Body blocks).
//   - Do not assume/include indentation.
//   - Do not worry about line width - a semantic `Line` will be split into multiple lines by the final formatter if necessary.
//   - Summary is usually the visible 1-line tool header. When Body is a Diff, Summary must be left empty; consumers should derive the header from Diff.Edits instead.
//
// By default, a ToolResult with IsError dose NOT need to present the error in Body - final formatters will automatically display an error based on IsError and SourceErr.
// To override this, set ErrorBehavior to ErrorBehaviorPresenterOwned.
type Presentation struct {
	Behavior       CompletionBehavior
	ErrorBehavior  ErrorBehavior
	NarrowBehavior PresentationNarrowBehavior
	Status         PresentationStatus

	// Summary is usually a 1-line tool header (ex: "Read path/to/file.go"; "Update Plan"; "Running go test ./..."). When Body is a Diff, leave Summary empty and let
	// consumers derive the header from the diff body.
	Summary Line

	// Tool details (ex: diff body; command output; checklist items). Diff bodies include enough metadata for consumers to synthesize their own header.
	Body Block
}

// ErrorBehavior indicates whether shared formatter-owned error rendering should still override presenter body content.
type ErrorBehavior string

const (
	// ErrorBehaviorDefault means the formatter should keep using shared default tool error rendering when the tool result is an error.
	ErrorBehaviorDefault ErrorBehavior = ""

	// ErrorBehaviorPresenterOwned means the presenter body already models the desired error presentation.
	ErrorBehaviorPresenterOwned ErrorBehavior = "presenter_owned"
)

// PresentationStatus indicates whether a presenter explicitly owns the visible success/failure state for completion rendering.
type PresentationStatus string

const (
	// PresentationStatusDefault means consumers should infer success/failure from the underlying ToolResult.
	PresentationStatusDefault PresentationStatus = ""

	// PresentationStatusSuccess means consumers should treat the presentation as successful.
	PresentationStatusSuccess PresentationStatus = "success"

	// PresentationStatusFailure means consumers should treat the presentation as failed.
	PresentationStatusFailure PresentationStatus = "failure"
)

// PresentationNarrowBehavior indicates whether a presenter wants the formatter's narrow-width fallback behavior adjusted.
type PresentationNarrowBehavior string

const (
	// PresentationNarrowBehaviorDefault keeps the formatter's default minimum-width TUI behavior for presenters.
	PresentationNarrowBehaviorDefault PresentationNarrowBehavior = ""

	// PresentationNarrowBehaviorPreferCLI asks consumers to keep using the formatter's CLI fallback at the minimum width boundary.
	PresentationNarrowBehaviorPreferCLI PresentationNarrowBehavior = "prefer_cli"
)

// Line is a single rendered line made of styled segments. If JoinWithSpace is true, consumers should join adjacent segments with a single space. Otherwise, Segment.Text
// owns any needed leading or trailing whitespace explicitly.
type Line struct {
	// JoinWithSpace indicates whether consumers should insert a single space between segments. When false, Segment.Text owns any needed leading or trailing whitespace.
	JoinWithSpace bool

	Segments []Segment
}

type Segment struct {
	Text string
	Role SegmentRole
}

// Block is an interface with a private method, to lock down possible Block implementors to the following:
//   - Paragraph
//   - Checklist
//   - Output
//   - Diff
type Block interface {
	isBlock()
}
```
