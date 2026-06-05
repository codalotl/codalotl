package llmstream

import "context"

// ToolKind identifies how a tool is exposed to a provider.
type ToolKind string

// Tool kind values describe how a tool is exposed to a provider.
const (
	ToolKindFunction ToolKind = "function" // ToolKindFunction exposes a tool as a structured function call.
	ToolKindCustom   ToolKind = "custom"   // ToolKindCustom exposes a tool as a provider-specific custom tool.
)

// ToolGrammarSyntax identifies the grammar language used by a ToolGrammar definition. Supported values are ToolGrammarSyntaxLark and ToolGrammarSyntaxRegex.
type ToolGrammarSyntax string

// Tool grammar syntax values identify supported custom-tool grammar languages.
const (
	ToolGrammarSyntaxLark  ToolGrammarSyntax = "lark"  // ToolGrammarSyntaxLark selects Lark grammar syntax.
	ToolGrammarSyntaxRegex ToolGrammarSyntax = "regex" // ToolGrammarSyntaxRegex selects regular-expression grammar syntax.
)

// ToolGrammar defines a grammar-based input format for custom tools. The syntax is currently limited to Lark or Regex.
type ToolGrammar struct {
	Syntax     ToolGrammarSyntax // Syntax identifies the grammar language used by Definition.
	Definition string            // Definition is the grammar text sent to the provider.
}

// ToolInfo describes a tool exposed to the LLM. By default, tools are treated as function calls that accept an object as the top-level parameter. Set Kind to ToolKindCustom
// with a Grammar to expose a grammar-based custom tool input.
//
// It does NOT map 1-1 with, ex, OpenAI's tool definition schema. In particular, OpenAI names "parameters" as the full argument. But this names parameters as the
// named parameters of the top-level parameter (which is forced to be an object).
//
// When added to providers, we automatically handle adding `null` as types to optional parameters. All tools will be added in strict mode. The "additionalProperties":
// false will automatically be added when appropriate.
type ToolInfo struct {
	// Name is the provider-visible tool name and must be non-empty.
	Name string

	// Description tells the model what the tool does and when to use it.
	Description string

	// Parameters is just named arguments to a function. That is all that is supported. Each named argument is an obj. Example:
	//
	//	{"path": {"type": "string", "description": "..."}, "showhidden": {"type": "bool", "description": "..."}}
	//
	// Note that this does NOT contain "type": "object" at the top level, nor "required", nor "additionalProperties". Any optional parameter does NOT need `null`.
	Parameters map[string]any

	Required []string     // Required is the keys of Parameters that are required.
	Kind     ToolKind     // Kind determines how the tool is exposed to providers. Defaults to ToolKindFunction.
	Grammar  *ToolGrammar // Grammar configures a grammar input for ToolKindCustom tools. When nil, providers fall back to free-form text input.
}

// Tool is an executable capability that can be exposed to an LLM.
type Tool interface {
	// Info returns the provider-facing metadata used to describe the tool to the model.
	Info() ToolInfo

	// Name returns the provider-visible tool name. It should match Info().Name and is used to identify the tool.
	Name() string

	// Presenter returns the optional semantic presenter for this tool. A nil presenter means the tool has no custom presentation.
	Presenter() Presenter

	// Run runs the tool. If the tool call results in an error of any kind, the result.IsError will be true, with result.Result containing a message for the LLM. result.SourceErr
	// may optionally be set for internal use, but won't be passed along to the LLM.
	Run(ctx context.Context, params ToolCall) ToolResult
}

// NewErrorToolResult returns an error ToolResult for toolCall with errMsg as the model-visible result.
func NewErrorToolResult(errMsg string, toolCall ToolCall) ToolResult {
	return ToolResult{
		CallID:  toolCall.CallID,
		Name:    toolCall.Name,
		Type:    toolCall.Type,
		Result:  errMsg,
		IsError: true,
	}
}
