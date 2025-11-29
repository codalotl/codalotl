package llmstream

type ContentPart interface {
	isPart()
}

// ReasoningContent represents the typically hidden reasoning/thinking tokens.
//
// Different providers may represent/model reasoning differently:
//   - If a provider just provides a single ID-less textual reasoning blob in a response, ID will be "" and Reasoning will contain the entire
//     reasoning. There will be one ReasoningContent.
//   - If providers have IDs for their reasoning objects, ID will be set.
//   - If providers have multiple reasoning items per ID (ex: OpenAI Responses), there may be multiple ReasoningContents with the same ID.
type ReasoningContent struct {
	ProviderID string `json:"provider_id"` // ProviderID is provider-specific (if the provider IDs its reasoning text).
	Content    string `json:"content"`
}

func (c ReasoningContent) String() string {
	return c.Content
}
func (c ReasoningContent) isPart() {}

// TextContent represents the primary message response LLMs give.
//
// Similar to Reasoning, providers may represent/model text responses differently:
//   - There may be no ProviderID.
//   - There may be multiple TextContent per message.
//   - There may be multiple TextContent per ProviderID.
type TextContent struct {
	ProviderID string `json:"provider_id"`
	Content    string `json:"content"`
}

func (c TextContent) String() string {
	return c.Content
}

func (c TextContent) isPart() {}

type ToolCall struct {
	ProviderID string `json:"provider_id"` // Provider ID.
	CallID     string `json:"call_id"`     // Ex: "call_EMPsaazgTBezpruyU2FkwCMC".
	Name       string `json:"name"`        // Name of the function/tool. Ex: "store_message".
	Type       string `json:"type"`        // Ex: "function_call" vs "custom_tool_call".
	Input      string `json:"input"`       // Input to function. For functions, JSON-serialized params.
}

func (ToolCall) isPart() {}

// ToolResult is the result of a ToolCall. CallID/Name/Type should match the call.
type ToolResult struct {
	CallID string `json:"call_id"`
	Name   string `json:"name"`
	Type   string `json:"type"` // Matches type of corresponding ToolCall (ex: "function_call").

	// Can either be raw string (ex: markdown; some text; a bulleted list) or JSON-serialized string; depends on Tool.
	Result string `json:"result"`

	IsError bool `json:"is_error"`

	// If IsError, SourceError may optionally be set if the error was due to a Go-ism that returned an error.
	// For instance, if os.Open fails to open a file and returns an error, we can store the error here.
	// On th eother hand, if a `read_file` tool indicates a path that is a directory, we could detect it with IsDir and
	// return an error result, but no SourceErr would exist.
	SourceErr error `json:"-"`
}

func (c ToolResult) isPart() {}
