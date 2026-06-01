package noninteractive

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
)

// A jsonEventWriter writes newline-delimited JSON events to an output stream.
type jsonEventWriter struct {
	out io.Writer // out receives one marshaled JSON event per line.
}

func newJSONEventWriter(out io.Writer) *jsonEventWriter {
	return &jsonEventWriter{out: out}
}

// jsonAgent identifies the agent that produced an event.
type jsonAgent struct {
	ID    string `json:"id"`    // ID is the agent identifier.
	Depth int    `json:"depth"` // Depth is the agent nesting depth, where the root agent is 0.
}

// jsonTool describes a tool call in the JSON event stream.
type jsonTool struct {
	CallID string `json:"call_id"`         // CallID identifies this invocation of the tool.
	Name   string `json:"name"`            // Name is the tool or function name.
	Type   string `json:"type"`            // Type is the provider tool-call type.
	Input  string `json:"input,omitempty"` // Input is the raw tool input, usually JSON-serialized parameters.
}

// jsonResult describes the completed result of a tool call.
type jsonResult struct {
	Output  string `json:"output"`   // Output is the raw tool result text.
	IsError bool   `json:"is_error"` // IsError reports whether the tool result represents an error.
}

// jsonTokenUsage reports token usage counters in the JSON event stream.
type jsonTokenUsage struct {
	Input       int64 `json:"input"`        // Input is the non-cached input token count.
	CachedInput int64 `json:"cached_input"` // CachedInput is the cached input token count.
	CacheWrites int64 `json:"cache_writes"` // CacheWrites is the input token count written to cache.
	Output      int64 `json:"output"`       // Output is the output token count.
	Total       int64 `json:"total"`        // Total is the sum of input, cached input, and output tokens.
}

// A jsonStartEvent is the JSON payload emitted when a run step starts.
type jsonStartEvent struct {
	Type        string           `json:"type"`         // Type is the event type and is always "start".
	CWD         string           `json:"cwd"`          // CWD is the normalized sandbox directory for the run.
	PackagePath string           `json:"package_path"` // PackagePath is the package path relative to CWD, or empty outside package mode.
	ModelID     llmmodel.ModelID `json:"model_id"`     // ModelID is the effective model ID used by the run.
}

// jsonUserMessageEvent reports the end-user prompt supplied to the run.
type jsonUserMessageEvent struct {
	Type string `json:"type"` // Type is "user_message".
	Text string `json:"text"` // Text is the visible user prompt.
}

// jsonAssistantContentEvent reports assistant text or reasoning content.
type jsonAssistantContentEvent struct {
	Type    string    `json:"type"`    // Type is the assistant content event type.
	Agent   jsonAgent `json:"agent"`   // Agent identifies the agent that produced the content.
	Content string    `json:"content"` // Content is the streamed assistant text or reasoning fragment.
}

// jsonToolCallEvent reports that an agent requested a tool call.
type jsonToolCallEvent struct {
	Type  string    `json:"type"`  // Type is "tool_call".
	Agent jsonAgent `json:"agent"` // Agent identifies the agent that requested the tool.
	Tool  jsonTool  `json:"tool"`  // Tool describes the requested tool call.
}

// jsonToolCompleteEvent reports that a tool call completed.
type jsonToolCompleteEvent struct {
	Type   string     `json:"type"`   // Type is "tool_complete".
	Agent  jsonAgent  `json:"agent"`  // Agent identifies the agent that owns the tool call.
	Tool   jsonTool   `json:"tool"`   // Tool describes the completed tool call.
	Result jsonResult `json:"result"` // Result contains the raw tool result and error status.
}

// jsonToolOutputEvent reports display-only output emitted by a tool.
type jsonToolOutputEvent struct {
	Type    string    `json:"type"`    // Type is "tool_output".
	Agent   jsonAgent `json:"agent"`   // Agent identifies the agent that owns the tool call.
	Tool    jsonTool  `json:"tool"`    // Tool describes the tool that emitted the output.
	Content string    `json:"content"` // Content is the visible tool output.
}

// jsonPermissionEvent reports an automatic response to a permission prompt.
type jsonPermissionEvent struct {
	Type      string `json:"type"`      // Type is "permission".
	Prompt    string `json:"prompt"`    // Prompt is the permission prompt shown to the user.
	Decision  string `json:"decision"`  // Decision is "allow" or "disallow".
	Automatic bool   `json:"automatic"` // Automatic reports whether the decision was made without interactive input.
}

// jsonStatusEvent reports a warning, retry, error, or cancellation from an agent.
type jsonStatusEvent struct {
	Type    string    `json:"type"`    // Type is the status event type.
	Agent   jsonAgent `json:"agent"`   // Agent identifies the agent that produced the status.
	Message string    `json:"message"` // Message is the human-readable status text.
}

// jsonDoneEvent reports successful completion of a run.
type jsonDoneEvent struct {
	Type            string          `json:"type"`                        // Type is "done".
	TokenUsage      jsonTokenUsage  `json:"token_usage"`                 // TokenUsage is the actual reported token usage.
	IdealTokenUsage *jsonTokenUsage `json:"ideal_token_usage,omitempty"` // IdealTokenUsage is the optional ideal-caching token usage.
}

// WriteStart writes the initial start event for a noninteractive run.
func (w *jsonEventWriter) WriteStart(cwd string, pkgRelPath string, modelID llmmodel.ModelID) error {
	return w.writeLine(jsonStartEvent{
		Type:        "start",
		CWD:         cwd,
		PackagePath: pkgRelPath,
		ModelID:     modelID,
	})
}

// WriteUserMessage writes a user_message event containing text.
func (w *jsonEventWriter) WriteUserMessage(text string) error {
	return w.writeLine(jsonUserMessageEvent{
		Type: "user_message",
		Text: text,
	})
}

// WritePermission writes an automatic permission decision event for prompt.
func (w *jsonEventWriter) WritePermission(prompt string, autoYes bool) error {
	decision := "disallow"
	if autoYes {
		decision = "allow"
	}
	return w.writeLine(jsonPermissionEvent{
		Type:      "permission",
		Prompt:    prompt,
		Decision:  decision,
		Automatic: true,
	})
}

// WriteAgentEvent writes the JSON representation of a supported agent event.
func (w *jsonEventWriter) WriteAgentEvent(ev agent.Event) error {
	switch ev.Type {
	case agent.EventTypeAssistantText:
		return w.writeLine(jsonAssistantContentEvent{
			Type:    "assistant_text",
			Agent:   jsonAgentFromMeta(ev.Agent),
			Content: ev.TextContent.Content,
		})
	case agent.EventTypeAssistantReasoning:
		return w.writeLine(jsonAssistantContentEvent{
			Type:    "assistant_reasoning",
			Agent:   jsonAgentFromMeta(ev.Agent),
			Content: ev.ReasoningContent.Content,
		})
	case agent.EventTypeToolCall:
		return w.writeLine(jsonToolCallEvent{
			Type:  "tool_call",
			Agent: jsonAgentFromMeta(ev.Agent),
			Tool:  jsonToolFromEvent(ev),
		})
	case agent.EventTypeToolComplete:
		return w.writeLine(jsonToolCompleteEvent{
			Type:   "tool_complete",
			Agent:  jsonAgentFromMeta(ev.Agent),
			Tool:   jsonToolFromEvent(ev),
			Result: jsonResultFromToolResult(ev.ToolResult),
		})
	case agent.EventTypeToolOutput:
		return w.writeLine(jsonToolOutputEvent{
			Type:    "tool_output",
			Agent:   jsonAgentFromMeta(ev.Agent),
			Tool:    jsonToolFromEvent(ev),
			Content: ev.ToolOutput.Content,
		})
	case agent.EventTypeWarning, agent.EventTypeRetry, agent.EventTypeError, agent.EventTypeCanceled:
		return w.writeLine(jsonStatusEvent{
			Type:    string(ev.Type),
			Agent:   jsonAgentFromMeta(ev.Agent),
			Message: errorString(ev.Error),
		})
	case agent.EventTypeStartSubagent:
		return nil
	default:
		return nil
	}
}

// The jsonToolFromEvent function extracts best-effort tool metadata from ev for the JSON event stream. It prefers the registered tool name when available and includes
// raw input only for tool-call metadata.
func jsonToolFromEvent(ev agent.Event) jsonTool {
	name := toolNameFromEvent(ev)

	switch ev.Type {
	case agent.EventTypeToolCall:
		if ev.ToolCall != nil {
			tool := jsonToolFromCall(ev.ToolCall)
			tool.Name = name
			return tool
		}
		if ev.ToolResult != nil {
			return jsonTool{
				CallID: ev.ToolResult.CallID,
				Name:   name,
				Type:   ev.ToolResult.Type,
			}
		}
	case agent.EventTypeToolComplete:
		if ev.ToolResult != nil {
			tool := jsonToolFromResult(ev.ToolResult)
			tool.Name = name
			return tool
		}
		if ev.ToolCall != nil {
			return jsonTool{
				CallID: ev.ToolCall.CallID,
				Name:   name,
				Type:   ev.ToolCall.Type,
			}
		}
	case agent.EventTypeToolOutput:
		if ev.ToolCall != nil {
			tool := jsonToolFromCall(ev.ToolCall)
			tool.Name = name
			tool.Input = ""
			return tool
		}
		if ev.ToolResult != nil {
			tool := jsonToolFromResult(ev.ToolResult)
			tool.Name = name
			return tool
		}
	}

	if name == "" {
		return jsonTool{}
	}
	return jsonTool{Name: name}
}

// WriteDone writes the terminal done event with actual and optional ideal token usage.
func (w *jsonEventWriter) WriteDone(actualUsage llmstream.TokenUsage, idealUsage *llmstream.TokenUsage) error {
	done := jsonDoneEvent{
		Type:       "done",
		TokenUsage: buildJSONTokenUsage(actualUsage),
	}
	if idealUsage != nil {
		usage := buildJSONTokenUsage(*idealUsage)
		done.IdealTokenUsage = &usage
	}
	return w.writeLine(done)
}

// The writeLine method marshals v as JSON and writes it as one newline-delimited event.
func (w *jsonEventWriter) writeLine(v any) error {
	if w == nil || w.out == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal json event: %w", err)
	}
	b = append(b, '\n')
	_, err = w.out.Write(b)
	return err
}

func jsonAgentFromMeta(meta agent.AgentMeta) jsonAgent {
	return jsonAgent{
		ID:    meta.ID,
		Depth: meta.Depth,
	}
}

func jsonToolFromCall(call *llmstream.ToolCall) jsonTool {
	if call == nil {
		return jsonTool{}
	}
	return jsonTool{
		CallID: call.CallID,
		Name:   call.Name,
		Type:   call.Type,
		Input:  call.Input,
	}
}

func jsonToolFromResult(result *llmstream.ToolResult) jsonTool {
	if result == nil {
		return jsonTool{}
	}
	return jsonTool{
		CallID: result.CallID,
		Name:   result.Name,
		Type:   result.Type,
	}
}

func jsonResultFromToolResult(result *llmstream.ToolResult) jsonResult {
	if result == nil {
		return jsonResult{}
	}
	return jsonResult{
		Output:  result.Result,
		IsError: result.IsError,
	}
}

func buildJSONTokenUsage(u llmstream.TokenUsage) jsonTokenUsage {
	input := u.TotalInputTokens - u.CachedInputTokens
	if input < 0 {
		input = 0
	}
	return jsonTokenUsage{
		Input:       input,
		CachedInput: u.CachedInputTokens,
		CacheWrites: u.CacheCreationInputTokens,
		Output:      u.TotalOutputTokens,
		Total:       input + u.CachedInputTokens + u.TotalOutputTokens,
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
