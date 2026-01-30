package llmstream

import "context"

type ToolKind string

const (
	ToolKindFunction ToolKind = "function"
	ToolKindCustom   ToolKind = "custom"
)

type ToolGrammarSyntax string

const (
	ToolGrammarSyntaxLark  ToolGrammarSyntax = "lark"
	ToolGrammarSyntaxRegex ToolGrammarSyntax = "regex"
)

// ToolGrammar defines a grammar-based input format for custom tools. The syntax is currently limited to Lark or Regex.
type ToolGrammar struct {
	Syntax     ToolGrammarSyntax
	Definition string
}

// ToolInfo describes a tool exposed to the LLM. By default, tools are treated as function calls that accept an object as the top-level parameter.
// Set Kind to ToolKindCustom with a Grammar to expose a grammar-based custom tool input.
//
// It does NOT map 1-1 with, ex, OpenAI's tool definition schema. In particular, OpenAI names "parameters" as the full argument. But this names
// parameters as the named parameters of the top-level parameter (which is forced to be an object).
//
// When added to providers, we automatically handle adding `null` as types to optional parameters. All tools will be added in strict mode. The
// "additionalProperties": false will automatically be added when appropriate.
type ToolInfo struct {
	Name        string
	Description string

	// Parameters is just named arguments to a function. That is all that is supported. Each named argument is an obj. Example:
	//
	//	{"path": {"type": "string", "description": "..."}, "showhidden": {"type": "bool", "description": "..."}}
	//
	// Note that this does NOT contain "type": "object" at the top level, nor "required", nor "additionalProperties". Any optional parameter does
	// NOT need `null`.
	Parameters map[string]any

	// Required is the keys of Parameters that are required.
	Required []string

	// Kind determines how the tool is exposed to providers. Defaults to ToolKindFunction.
	Kind ToolKind

	// Grammar configures a grammar input for ToolKindCustom tools. When nil, providers fall back to free-form text input.
	Grammar *ToolGrammar
}

type Tool interface {
	Info() ToolInfo
	Name() string

	// Run runs the tool. If the tool call results in an error of any kind, the result.IsError will be true, with result.Result containing a message
	// for the LLM. result.SourceErr may optionally be set for internal use, but won't be passed along to the LLM.
	Run(ctx context.Context, params ToolCall) ToolResult
}

func NewErrorToolResult(errMsg string, toolCall ToolCall) ToolResult {
	return ToolResult{
		CallID:  toolCall.CallID,
		Name:    toolCall.Name,
		Type:    toolCall.Type,
		Result:  errMsg,
		IsError: true,
	}
}
