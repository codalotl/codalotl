# gemini

The gemini package implements a minimal client to perform streaming requests to the Gemini LLM.

## Dependencies

No third party "Google SDK"-style modules are used. This package should not make net-new deps that the rest of the repo does not need.

- `internal/q/sseclient` for SSE.

NOTE: Currently, the `google.golang.org/genai` is in this module, as well as all its dependent modules. NONE of those may be used here. This package is being written to replace those.

## Scope

Only the portion of the API needed to implement `llmstream` will be implemented:
- Only `streamGenerateContent`

## Compatibility with google.golang.org/genai

This package exposes a smaller, genai-like surface. It is not a full drop-in replacement for `google.golang.org/genai`.

Intentionally unsupported or reduced areas:
- Vertex AI config and auth
- Extra `HTTPOptions` features beyond `BaseURL` and `Headers`
- Richer `GenerateContentConfig` fields not listed here
- Richer `Part` variants not listed here
- Richer response metadata not listed here
- Extra `FunctionResponse` fields such as `Scheduling`, `WillContinue`, and `Parts`

## Testing

Employs both stubbed tests (don't hit actual endpoints) and integration tests (hit gemini endpoints).

Integration tests are gated behind the `INTEGRATION_TEST` env var. When this is set (to any non-empty value), it reads and uses `GEMINI_API_KEY`.

## Public API

The package intentionally exposes a small, genai-like surface.

### Construction

```go
func NewClient(ctx context.Context, cfg *ClientConfig) (*Client, error)
```

```go
type ClientConfig struct {
	APIKey      string
	Backend     Backend
	HTTPClient  *http.Client
	HTTPOptions HTTPOptions
}
```

Supported backend:

```go
const BackendGeminiAPI Backend = "gemini-api"
```

If `APIKey` is empty, `NewClient` reads `GOOGLE_API_KEY` first, then `GEMINI_API_KEY`.

### Client Shape

```go
type Client struct {
	Models Models
}
```

```go
type Models struct{}
```

### Streaming Call

```go
func (m Models) GenerateContentStream(
	ctx context.Context,
	model string,
	contents []*Content,
	config *GenerateContentConfig,
) iter.Seq2[*GenerateContentResponse, error]
```

This sends a REST request to Gemini `streamGenerateContent?alt=sse` and yields decoded `GenerateContentResponse` values from SSE `data:` events.

Each yielded item corresponds to one decoded SSE event. The client does not accumulate prior chunks. Callers that want response-so-far behavior must accumulate text, tool, and thought state across yielded events themselves.

Open errors, non-2xx responses, mid-stream read failures, and JSON decode failures are yielded as `(nil, err)`. After yielding an error, iteration stops.

Model names may be passed either as bare IDs like `gemini-2.5-flash` or fully-prefixed IDs like `models/gemini-2.5-flash`.

### Request Types

```go
type GenerateContentConfig struct {
	HTTPOptions       *HTTPOptions
	SystemInstruction *Content
	Temperature       *float32
	CandidateCount    int32
	MaxOutputTokens   int32
	StopSequences     []string
	Tools             []*Tool
	ToolConfig        *ToolConfig
	ThinkingConfig    *ThinkingConfig
}
```

```go
type HTTPOptions struct {
	BaseURL string
	Headers http.Header
}
```

`BaseURL` is an unversioned root such as `https://host` or `https://host/custom-prefix`. Do not pass a versioned root such as `https://host/v1beta`. This package appends `/v1beta/...` itself.

```go
type Content struct {
	Parts []*Part `json:"parts,omitempty"`
	Role  string  `json:"role,omitempty"`
}
```

```go
const (
	RoleUser  Role = "user"
	RoleModel Role = "model"
)
```

```go
type Part struct {
	Text             string            `json:"text,omitempty"`
	Thought          bool              `json:"thought,omitempty"`
	ThoughtSignature []byte            `json:"thoughtSignature,omitempty"`
	FunctionCall     *FunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *FunctionResponse `json:"functionResponse,omitempty"`
}
```

```go
type FunctionCall struct {
	ID   string         `json:"id,omitempty"`
	Name string         `json:"name,omitempty"`
	Args map[string]any `json:"args,omitempty"`
}
```

```go
// FunctionResponse is the supported subset of Gemini function responses.
//
// Only ID, Name, and Response are preserved. Fields exposed by google.golang.org/genai such as Scheduling, WillContinue, Parts, and other unsupported fields are
// discarded during unmarshal.
type FunctionResponse struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name,omitempty"`
	Response map[string]any `json:"response,omitempty"`
}
```

Only `ID`, `Name`, and `Response` are preserved. Extra `google.golang.org/genai` fields such as `Scheduling`, `WillContinue`, `Parts`, and other unsupported JSON fields are discarded during unmarshal.

```go
type Tool struct {
	FunctionDeclarations []*FunctionDeclaration `json:"functionDeclarations,omitempty"`
}
```

```go
type FunctionDeclaration struct {
	Description          string `json:"description,omitempty"`
	Name                 string `json:"name,omitempty"`
	ParametersJsonSchema any    `json:"parametersJsonSchema,omitempty"`
}
```

```go
type ToolConfig struct {
	FunctionCallingConfig *FunctionCallingConfig `json:"functionCallingConfig,omitempty"`
}
```

```go
type FunctionCallingConfig struct {
	AllowedFunctionNames []string                  `json:"allowedFunctionNames,omitempty"`
	Mode                 FunctionCallingConfigMode `json:"mode,omitempty"`
}
```

```go
const (
	FunctionCallingConfigModeAuto FunctionCallingConfigMode = "AUTO"
	FunctionCallingConfigModeAny  FunctionCallingConfigMode = "ANY"
	FunctionCallingConfigModeNone FunctionCallingConfigMode = "NONE"
)
```

```go
// ThinkingConfig is passed through to generationConfig.thinkingConfig.
//
// ThinkingLevel is serialized as-is. IncludeThoughts only requests thoughts; this client surfaces Thought and ThoughtSignature only when the API returns them. The
// client does not aggregate or reconstruct thought text or thought signatures across stream events.
type ThinkingConfig struct {
	IncludeThoughts bool          `json:"includeThoughts,omitempty"`
	ThinkingBudget  *int32        `json:"thinkingBudget,omitempty"`
	ThinkingLevel   ThinkingLevel `json:"thinkingLevel,omitempty"`
}
```

`ThinkingConfig` is pass-through. `ThinkingLevel` is serialized as-is. `IncludeThoughts=true` only requests thoughts; the client surfaces `Part.Thought` and `Part.ThoughtSignature` only if the API returns them, without client-side aggregation or reconstruction.

```go
const (
	ThinkingLevelMinimal ThinkingLevel = "MINIMAL"
	ThinkingLevelLow     ThinkingLevel = "LOW"
	ThinkingLevelMedium  ThinkingLevel = "MEDIUM"
	ThinkingLevelHigh    ThinkingLevel = "HIGH"
)
```

### Response Types

```go
type GenerateContentResponse struct {
	Candidates     []*Candidate                           `json:"candidates,omitempty"`
	ModelVersion   string                                 `json:"modelVersion,omitempty"`
	PromptFeedback *GenerateContentResponsePromptFeedback `json:"promptFeedback,omitempty"`
	ResponseID     string                                 `json:"responseId,omitempty"`
	UsageMetadata  *GenerateContentResponseUsageMetadata  `json:"usageMetadata,omitempty"`
}
```

```go
type Candidate struct {
	Content       *Content     `json:"content,omitempty"`
	FinishMessage string       `json:"finishMessage,omitempty"`
	TokenCount    int32        `json:"tokenCount,omitempty"`
	FinishReason  FinishReason `json:"finishReason,omitempty"`
	Index         int32        `json:"index,omitempty"`
}
```

```go
type GenerateContentResponsePromptFeedback struct {
	BlockReason        BlockedReason `json:"blockReason,omitempty"`
	BlockReasonMessage string        `json:"blockReasonMessage,omitempty"`
}
```

```go
type GenerateContentResponseUsageMetadata struct {
	CachedContentTokenCount int32 `json:"cachedContentTokenCount,omitempty"`
	CandidatesTokenCount    int32 `json:"candidatesTokenCount,omitempty"`
	PromptTokenCount        int32 `json:"promptTokenCount,omitempty"`
	ThoughtsTokenCount      int32 `json:"thoughtsTokenCount,omitempty"`
	ToolUsePromptTokenCount int32 `json:"toolUsePromptTokenCount,omitempty"`
	TotalTokenCount         int32 `json:"totalTokenCount,omitempty"`
}
```

Supported finish reasons exposed by the package:

```go
const (
	FinishReasonStop                   FinishReason = "STOP"
	FinishReasonMaxTokens              FinishReason = "MAX_TOKENS"
	FinishReasonSafety                 FinishReason = "SAFETY"
	FinishReasonRecitation             FinishReason = "RECITATION"
	FinishReasonBlocklist              FinishReason = "BLOCKLIST"
	FinishReasonProhibitedContent      FinishReason = "PROHIBITED_CONTENT"
	FinishReasonSPII                   FinishReason = "SPII"
	FinishReasonMalformedFunctionCall  FinishReason = "MALFORMED_FUNCTION_CALL"
	FinishReasonUnexpectedToolCall     FinishReason = "UNEXPECTED_TOOL_CALL"
	FinishReasonOther                  FinishReason = "OTHER"
	FinishReasonNoImage                FinishReason = "NO_IMAGE"
	FinishReasonImageSafety            FinishReason = "IMAGE_SAFETY"
	FinishReasonImageProhibitedContent FinishReason = "IMAGE_PROHIBITED_CONTENT"
)
```

### Helper

```go
func Ptr[T any](v T) *T
```
