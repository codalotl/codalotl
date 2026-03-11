# gemini

The gemini package implements a minimal client to perform streaming requests to the Gemini LLM.

## Dependencies

No third party "Google SDK"-style modules are used. This package should not make net-new deps that the rest of the repo does not need.

- `internal/q/sseclient` for SSE.

NOTE: Currently, the `google.golang.org/genai` is in this module, as well as all its dependent modules. NONE of those may be used here. This package is being written to replace those.

## Scope

Only the portion of the API needed to implement `llmstream` will be implemented:
- Only `streamGenerateContent`

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

```go
type Content struct {
    Parts []*Part
    Role  string
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
    Text             string
    Thought          bool
    ThoughtSignature []byte
    FunctionCall     *FunctionCall
    FunctionResponse *FunctionResponse
}
```

```go
type FunctionCall struct {
    ID   string
    Name string
    Args map[string]any
}
```

```go
type FunctionResponse struct {
    ID       string
    Name     string
    Response map[string]any
}
```

```go
type Tool struct {
    FunctionDeclarations []*FunctionDeclaration
}
```

```go
type FunctionDeclaration struct {
    Description          string
    Name                 string
    ParametersJsonSchema any
}
```

```go
type ToolConfig struct {
    FunctionCallingConfig *FunctionCallingConfig
}
```

```go
type FunctionCallingConfig struct {
    AllowedFunctionNames []string
    Mode                 FunctionCallingConfigMode
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
type ThinkingConfig struct {
    IncludeThoughts bool
    ThinkingBudget  *int32
    ThinkingLevel   ThinkingLevel
}
```

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
    Candidates     []*Candidate
    ModelVersion   string
    PromptFeedback *GenerateContentResponsePromptFeedback
    ResponseID     string
    UsageMetadata  *GenerateContentResponseUsageMetadata
}
```

```go
type Candidate struct {
    Content       *Content
    FinishMessage string
    TokenCount    int32
    FinishReason  FinishReason
    Index         int32
}
```

```go
type GenerateContentResponsePromptFeedback struct {
    BlockReason        BlockedReason
    BlockReasonMessage string
}
```

```go
type GenerateContentResponseUsageMetadata struct {
    CachedContentTokenCount int32
    CandidatesTokenCount    int32
    PromptTokenCount        int32
    ThoughtsTokenCount      int32
    ToolUsePromptTokenCount int32
    TotalTokenCount         int32
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
