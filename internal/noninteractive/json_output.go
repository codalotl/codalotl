package noninteractive

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
)

type jsonEventWriter struct {
	out io.Writer
}

func newJSONEventWriter(out io.Writer) *jsonEventWriter {
	return &jsonEventWriter{out: out}
}

type jsonAgent struct {
	ID    string `json:"id"`
	Depth int    `json:"depth"`
}

type jsonTool struct {
	CallID string `json:"call_id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Input  string `json:"input,omitempty"`
}

type jsonResult struct {
	Output  string `json:"output"`
	IsError bool   `json:"is_error"`
}

type jsonTokenUsage struct {
	Input       int64 `json:"input"`
	CachedInput int64 `json:"cached_input"`
	CacheWrites int64 `json:"cache_writes"`
	Output      int64 `json:"output"`
	Total       int64 `json:"total"`
}

type jsonStartEvent struct {
	Type        string           `json:"type"`
	CWD         string           `json:"cwd"`
	PackagePath string           `json:"package_path"`
	ModelID     llmmodel.ModelID `json:"model_id"`
}

type jsonUserMessageEvent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type jsonAssistantContentEvent struct {
	Type    string    `json:"type"`
	Agent   jsonAgent `json:"agent"`
	Content string    `json:"content"`
}

type jsonToolCallEvent struct {
	Type  string    `json:"type"`
	Agent jsonAgent `json:"agent"`
	Tool  jsonTool  `json:"tool"`
}

type jsonToolCompleteEvent struct {
	Type   string     `json:"type"`
	Agent  jsonAgent  `json:"agent"`
	Tool   jsonTool   `json:"tool"`
	Result jsonResult `json:"result"`
}

type jsonPermissionEvent struct {
	Type      string `json:"type"`
	Prompt    string `json:"prompt"`
	Decision  string `json:"decision"`
	Automatic bool   `json:"automatic"`
}

type jsonStatusEvent struct {
	Type    string    `json:"type"`
	Agent   jsonAgent `json:"agent"`
	Message string    `json:"message"`
}

type jsonDoneEvent struct {
	Type            string          `json:"type"`
	TokenUsage      jsonTokenUsage  `json:"token_usage"`
	IdealTokenUsage *jsonTokenUsage `json:"ideal_token_usage,omitempty"`
}

func (w *jsonEventWriter) WriteStart(cwd string, pkgRelPath string, modelID llmmodel.ModelID) error {
	return w.writeLine(jsonStartEvent{
		Type:        "start",
		CWD:         cwd,
		PackagePath: pkgRelPath,
		ModelID:     modelID,
	})
}

func (w *jsonEventWriter) WriteUserMessage(text string) error {
	return w.writeLine(jsonUserMessageEvent{
		Type: "user_message",
		Text: text,
	})
}

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
	case agent.EventTypeWarning, agent.EventTypeRetry, agent.EventTypeError, agent.EventTypeCanceled:
		return w.writeLine(jsonStatusEvent{
			Type:    string(ev.Type),
			Agent:   jsonAgentFromMeta(ev.Agent),
			Message: errorString(ev.Error),
		})
	default:
		return nil
	}
}

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
	}

	if name == "" {
		return jsonTool{}
	}
	return jsonTool{Name: name}
}

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
