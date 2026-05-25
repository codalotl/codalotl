# llmstream

llmstream is an abstraction over LLM providers, offering a unified interface. Provider calls are streaming-based.

## Providers

### OpenAI

- Implements responses API only.
- Uses provider subscription auth from `llmmodel` when available for OpenAI models that do not have explicit model-level auth overrides.
- OpenAI subscription auth:
	- uses subscription access token, account ID, and backend URL
	- forces no-store/ZDR request behavior
	- sends system turns as root `instructions` instead of input array items
- `SendOptions.NoStore` uses OpenAI Responses ZDR semantics:
	- Sends `store=false`.
	- Does not send `previous_response_id`.
	- Does not retain response IDs for future linking.
	- Requests `reasoning.encrypted_content` and replays encrypted reasoning content across stateless turns.
	- Sends full replayable local conversation history on every request: visible messages, tool calls, and tool results are replayed by value.
	- Does not replay provider output item IDs or provider reasoning items that require stored OpenAI state without encrypted content.
	- Retained no-store assistant turns omit provider IDs and only keep reasoning state that is safe for stateless encrypted replay.
- Default OpenAI behavior stores/links responses server-side where supported.

### Anthropic

- Uses Anthropic Messages API.
- Opus/Sonnet 4.6+ are primary supported/tested models (but registered `llmmodel` Anthropic API-shape models may be dispatched, including custom aliases).
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
// This method may be called eagerly as soon as we know the response object, but must be called before the channel returned by SendAsync closes.
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
- `Presentation.Summary` is usually the tool-level 1-line header;  `Presentation.Body` is optional details.
- If `Presentation.Body` is `Diff`, presenters must leave `Summary` empty.
- Consumers that need a visible 1-line diff header should derive it from `Diff.Edits[0]`.
- `Diff.Edits` are in display order; first edit is the lead edit/header source.
- A presenter may also implement `SubagentFinalMessagePresenter` (callers can attempt a type assertion). Example use case:
	- A tool launches a subagent, whose final message is JSON. This JSON can be parsed and formatted for the user.
- A **partial** `Presentation` type tree is below in Public API.

## Public API

```go
// Completer provides one-shot completions.
type Completer interface {
	// Complete sends systemMessage and userMessage to modelID, returning the final assistant turn.
	Complete(ctx context.Context, modelID llmmodel.ModelID, systemMessage, userMessage string, options ...SendOptions) (Turn, error)
}

// NewCompleter returns a Completer.
func NewCompleter() Completer

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
//
// NOTE: llmstream package does NOT have any additional information about how to use Presenter or Presentation -- consuming packages should NOT interrogate this
// package with clarify_public_api concerning Presenter or its types. These types are provided as-is for packages to build upon.
type Presenter interface {
	// Present presents call and result in a semantic way (no width decisions, no assumptions about ANSI terminals, colors). To present a tool call (no result yet),
	// call Present(call, nil). To present a call with result, call Present(call, result). For instance, for a read file tool, the call might return the equivalent of
	// "Reading file.go". The result might return the equivalent of "Read file.go (123 bytes)".
	Present(call ToolCall, result *ToolResult) Presentation
}

// SubagentFinalMessagePresenter optionally customizes the final message of a descendant subagent launched directly by call. The interface is defined in terms of
// that direct tool-call/subagent relationship. Consumers that collapse deeper descendant activity into the direct subagent's visible slot may reuse the same presentation
// for that slot's terminal visible message.
//
// Consumers should type-assert a tool presenter to this interface. When the presenter does not implement it, the descendant subagent final message should be shown
// as plain text. Returning nil suppresses the descendant final message. Returning a non-nil Block replaces the plain-text rendering with a semantic block.
type SubagentFinalMessagePresenter interface {
	SubagentFinalMessage(call ToolCall, subagentLabel string, finalMessage string) Block
}

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
	Behavior      CompletionBehavior
	ErrorBehavior ErrorBehavior
	Status        PresentationStatus

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
