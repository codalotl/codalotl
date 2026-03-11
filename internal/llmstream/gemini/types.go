package gemini

import "net/http"

type Backend string

const (
	BackendUnspecified Backend = ""
	BackendGeminiAPI   Backend = "gemini-api"
)

type HTTPOptions struct {
	BaseURL string
	Headers http.Header
}

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

type Content struct {
	Parts []*Part `json:"parts,omitempty"`
	Role  string  `json:"role,omitempty"`
}

type Role string

const (
	RoleUser  Role = "user"
	RoleModel Role = "model"
)

type Part struct {
	Text             string            `json:"text,omitempty"`
	Thought          bool              `json:"thought,omitempty"`
	ThoughtSignature []byte            `json:"thoughtSignature,omitempty"`
	FunctionCall     *FunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *FunctionResponse `json:"functionResponse,omitempty"`
}

type FunctionCall struct {
	ID   string         `json:"id,omitempty"`
	Name string         `json:"name,omitempty"`
	Args map[string]any `json:"args,omitempty"`
}

type FunctionResponse struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name,omitempty"`
	Response map[string]any `json:"response,omitempty"`
}

type Tool struct {
	FunctionDeclarations []*FunctionDeclaration `json:"functionDeclarations,omitempty"`
}

type FunctionDeclaration struct {
	Description          string `json:"description,omitempty"`
	Name                 string `json:"name,omitempty"`
	ParametersJsonSchema any    `json:"parametersJsonSchema,omitempty"`
}

type ToolConfig struct {
	FunctionCallingConfig *FunctionCallingConfig `json:"functionCallingConfig,omitempty"`
}

type FunctionCallingConfig struct {
	AllowedFunctionNames []string                  `json:"allowedFunctionNames,omitempty"`
	Mode                 FunctionCallingConfigMode `json:"mode,omitempty"`
}

type FunctionCallingConfigMode string

const (
	FunctionCallingConfigModeAuto FunctionCallingConfigMode = "AUTO"
	FunctionCallingConfigModeAny  FunctionCallingConfigMode = "ANY"
	FunctionCallingConfigModeNone FunctionCallingConfigMode = "NONE"
)

type ThinkingConfig struct {
	IncludeThoughts bool          `json:"includeThoughts,omitempty"`
	ThinkingBudget  *int32        `json:"thinkingBudget,omitempty"`
	ThinkingLevel   ThinkingLevel `json:"thinkingLevel,omitempty"`
}

type ThinkingLevel string

const (
	ThinkingLevelMinimal ThinkingLevel = "MINIMAL"
	ThinkingLevelLow     ThinkingLevel = "LOW"
	ThinkingLevelMedium  ThinkingLevel = "MEDIUM"
	ThinkingLevelHigh    ThinkingLevel = "HIGH"
)

type FinishReason string

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

type GenerateContentResponse struct {
	Candidates     []*Candidate                           `json:"candidates,omitempty"`
	ModelVersion   string                                 `json:"modelVersion,omitempty"`
	PromptFeedback *GenerateContentResponsePromptFeedback `json:"promptFeedback,omitempty"`
	ResponseID     string                                 `json:"responseId,omitempty"`
	UsageMetadata  *GenerateContentResponseUsageMetadata  `json:"usageMetadata,omitempty"`
}

type Candidate struct {
	Content       *Content     `json:"content,omitempty"`
	FinishMessage string       `json:"finishMessage,omitempty"`
	TokenCount    int32        `json:"tokenCount,omitempty"`
	FinishReason  FinishReason `json:"finishReason,omitempty"`
	Index         int32        `json:"index,omitempty"`
}

type GenerateContentResponsePromptFeedback struct {
	BlockReason        BlockedReason `json:"blockReason,omitempty"`
	BlockReasonMessage string        `json:"blockReasonMessage,omitempty"`
}

type BlockedReason string

type GenerateContentResponseUsageMetadata struct {
	CachedContentTokenCount int32 `json:"cachedContentTokenCount,omitempty"`
	CandidatesTokenCount    int32 `json:"candidatesTokenCount,omitempty"`
	PromptTokenCount        int32 `json:"promptTokenCount,omitempty"`
	ThoughtsTokenCount      int32 `json:"thoughtsTokenCount,omitempty"`
	ToolUsePromptTokenCount int32 `json:"toolUsePromptTokenCount,omitempty"`
	TotalTokenCount         int32 `json:"totalTokenCount,omitempty"`
}

func Ptr[T any](v T) *T {
	return &v
}
