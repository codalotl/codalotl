package llmstream

// ContentPart is a package-defined item that can appear in Turn.Parts.
//
// Only types in this package can implement ContentPart.
type ContentPart interface {
	// isPart marks package-defined values that can be stored in Turn.Parts.
	isPart()
}

// ReasoningContent represents the typically hidden reasoning/thinking tokens.
//
// Different providers may represent/model reasoning differently:
//   - If a provider just provides a single ID-less textual reasoning blob in a response, ID will be "" and Reasoning will contain the entire reasoning. There will
//     be one ReasoningContent.
//   - If providers have IDs for their reasoning objects, ID will be set.
//   - If providers have multiple reasoning items per ID (ex: OpenAI Responses), there may be multiple ReasoningContents with the same ID.
type ReasoningContent struct {
	// ProviderID is provider-specific (if the provider IDs its reasoning text).
	ProviderID string `json:"provider_id"`

	// Content is the reasoning text or summary.
	Content string `json:"content"`

	// ProviderState carries provider-specific opaque reasoning state needed to safely round-trip reasoning across turns. For Anthropic this stores the thinking signature;
	// future providers may use other opaque formats.
	ProviderState string `json:"provider_state,omitempty"`
}

// String returns c.Content.
func (c ReasoningContent) String() string {
	return c.Content
}

// isPart marks ReasoningContent as a ContentPart.
func (c ReasoningContent) isPart() {}

// TextContent represents the primary message response LLMs give.
//
// Similar to Reasoning, providers may represent/model text responses differently:
//   - There may be no ProviderID.
//   - There may be multiple TextContent per message.
//   - There may be multiple TextContent per ProviderID.
type TextContent struct {
	ProviderID string `json:"provider_id"` // ProviderID is the provider's identifier for this text item, when one exists.
	Content    string `json:"content"`     // Content is the visible text.
}

// String returns c.Content.
func (c TextContent) String() string {
	return c.Content
}

// isPart marks TextContent as a ContentPart.
func (c TextContent) isPart() {}

// CompactionContent stores opaque provider compaction state for stateless replay.
type CompactionContent struct {
	ProviderID    string `json:"provider_id"`              // ProviderID is the provider's identifier for this compaction item, when one exists.
	ProviderState string `json:"provider_state,omitempty"` // ProviderState is the opaque compaction payload to preserve for replay.
}

// isPart marks CompactionContent as a ContentPart.
func (c CompactionContent) isPart() {}

// ToolCall represents a tool invocation requested by an assistant turn.
type ToolCall struct {
	ProviderID string `json:"provider_id"` // Provider ID.
	CallID     string `json:"call_id"`     // Ex: "call_EMPsaazgTBezpruyU2FkwCMC".
	Name       string `json:"name"`        // Name of the function/tool. Ex: "store_message".
	Type       string `json:"type"`        // Ex: "function_call" vs "custom_tool_call".
	Input      string `json:"input"`       // Input to function. For functions, JSON-serialized params.
}

// isPart marks ToolCall as a ContentPart.
func (ToolCall) isPart() {}

// ToolResult is the result of a ToolCall. CallID/Name/Type should match the call.
type ToolResult struct {
	CallID string `json:"call_id"` // CallID identifies the ToolCall this result answers.
	Name   string `json:"name"`    // Name is the tool name and must match the corresponding ToolCall.Name.
	Type   string `json:"type"`    // Matches type of corresponding ToolCall (ex: "function_call").
	Result string `json:"result"`  // Can either be raw string (ex: markdown; some text; a bulleted list) or JSON-serialized string; depends on Tool.

	// Did the tool call fail? NOTE: IsError should be false for things like failed tests, or shell commands which returned a non-zero error code (but which were otherwise
	// successfully attempted).
	IsError bool `json:"is_error"`

	// If IsError, SourceError may optionally be set if the error was due to a Go-ism that returned an error. For instance, if os.Open fails to open a file and returns
	// an error, we can store the error here. On the other hand, if a `read_file` tool call's path is a directory (instead of a file), we could detect it with IsDir
	// and return an error result, but no SourceErr would exist.
	SourceErr error `json:"-"`
}

// isPart marks ToolResult as a ContentPart.
func (c ToolResult) isPart() {}
