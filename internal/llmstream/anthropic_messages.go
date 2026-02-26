package llmstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/codalotl/codalotl/internal/llmmodel"
	anthropicapi "github.com/codalotl/codalotl/internal/llmstream/anthropic"
	"io"
	"sort"
	"strings"
	"time"
)

const anthropicMaxTokens int64 = 32000

func (sc *streamingConversation) sendAsyncAnthropic(ctx context.Context, out chan Event, opt *SendOptions, modelInfo llmmodel.ModelInfo) (Turn, error) {
	if err := ctx.Err(); err != nil {
		return Turn{}, sc.LogWrappedErr("anthropic_send_async.context", err)
	}
	apiKey := llmmodel.GetAPIKey(sc.modelID)
	if apiKey == "" {
		return Turn{}, sc.LogNewErr("anthropic_send_async.api_key_missing", "model_id", string(sc.modelID), "provider", modelInfo.ProviderID)
	}
	req, err := sc.buildAnthropicMessageRequest(modelInfo, opt)
	if err != nil {
		return Turn{}, sc.LogWrappedErr("anthropic_send_async.build_params", err)
	}
	opts := []anthropicapi.Option{}
	if baseURL := llmmodel.GetAPIEndpointURL(sc.modelID); baseURL != "" {
		opts = append(opts, anthropicapi.WithBaseURL(baseURL))
	}
	client := anthropicapi.New(apiKey, opts...)
	debugPrint(debugHTTPRequests, "HTTP REQUEST: create anthropic message(stream=true)", req)
	startTime := time.Now()
	stream, err := client.StreamMessages(ctx, req)
	if err != nil {
		return Turn{}, sc.LogWrappedErr("anthropic_send_async.stream_start", err)
	}
	defer stream.Close()
	toDebouncer := make(chan Event, 1024)
	debounceDone := make(chan struct{})
	defer func() {
		debugPrint(debugEvents, "Func done - closing anthropic toDebouncer", nil)
		close(toDebouncer)
		<-debounceDone
	}()
	go func() {
		debounceEvents(ctx, toDebouncer, out)
		debugPrint(debugEvents, "Done debouncing anthropic. Closing debounceDone", nil)
		close(debounceDone)
	}()
	state := newAnthropicStreamState()
	var finalTurn *Turn
	for {
		evt, err := stream.RecvContext(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return Turn{}, sc.LogWrappedErr("anthropic_send_async.context", ctxErr)
			}
			return Turn{}, makeRetryable(sc.LogWrappedErr("anthropic_send_async.recv", err))
		}
		debugPrint(debugEvents, fmt.Sprintf("EVENT: anthropic:%s; elapsed=%v", evt.Type, time.Since(startTime)), nil)
		processedEvent, _, err := state.processEvent(evt)
		if err != nil {
			return Turn{}, sc.LogWrappedErr("anthropic_send_async.event", err)
		}
		if processedEvent != nil {
			if processedEvent.Type == EventTypeCompletedSuccess {
				finalTurn = processedEvent.Turn
				debugPrint(debugParsedResponses, "PARSED RESPONSE: anthropic EventTypeCompletedSuccess", processedEvent)
			}
			if !trySendEvent(ctx, toDebouncer, *processedEvent) {
				return Turn{}, sc.LogWrappedErr("anthropic_send_async.context", context.Canceled)
			}
		}
	}
	if finalTurn == nil {
		return Turn{}, makeRetryable(sc.LogNewErr("anthropic_send_async.not_completed"))
	}
	resp := *finalTurn
	resp.Role = RoleAssistant
	return resp, nil
}
func (sc *streamingConversation) buildAnthropicMessageRequest(modelInfo llmmodel.ModelInfo, opt *SendOptions) (anthropicapi.MessageRequest, error) {
	modelID := strings.TrimSpace(modelInfo.ProviderModelID)
	if modelID == "" {
		return anthropicapi.MessageRequest{}, fmt.Errorf("model %q missing provider model id", string(sc.modelID))
	}
	system := sc.turns[0].TextContent()
	messages := make([]anthropicapi.MessageParam, 0, len(sc.turns)-1)
	for _, turn := range sc.turns[1:] {
		msg, include, err := anthropicBuildMessageParam(turn)
		if err != nil {
			return anthropicapi.MessageRequest{}, err
		}
		if include {
			messages = append(messages, msg)
		}
	}
	req := anthropicapi.MessageRequest{
		Model:     modelID,
		MaxTokens: anthropicMaxTokens,
		System:    system,
		Messages:  messages,
	}
	if len(sc.tools) > 0 {
		toolParams, err := buildAnthropicToolParams(sc.tools)
		if err != nil {
			return anthropicapi.MessageRequest{}, err
		}
		req.Tools = toolParams
	}
	if err := anthropicApplySendOptions(&req, modelInfo, opt); err != nil {
		return anthropicapi.MessageRequest{}, err
	}
	return req, nil
}
func anthropicBuildMessageParam(turn Turn) (anthropicapi.MessageParam, bool, error) {
	role, ok := anthropicMapTurnRole(turn.Role)
	if !ok {
		return anthropicapi.MessageParam{}, false, fmt.Errorf("unsupported turn role for anthropic: %v", turn.Role)
	}
	blocks := make([]anthropicapi.ContentBlockParam, 0, len(turn.Parts))
	for _, part := range turn.Parts {
		switch typed := part.(type) {
		case TextContent:
			if typed.Content == "" {
				continue
			}
			blocks = append(blocks, anthropicapi.ContentBlockParam{
				Type: "text",
				Text: typed.Content,
			})
		case ToolCall:
			if typed.Name == "" {
				return anthropicapi.MessageParam{}, false, errors.New("tool call name is required")
			}
			inputJSON, err := normalizeToolCallInputJSON(typed.Input)
			if err != nil {
				return anthropicapi.MessageParam{}, false, fmt.Errorf("tool call %q has invalid input json: %w", typed.Name, err)
			}
			toolUseID := typed.CallID
			if toolUseID == "" {
				toolUseID = typed.ProviderID
			}
			if toolUseID == "" {
				return anthropicapi.MessageParam{}, false, fmt.Errorf("tool call %q is missing call id", typed.Name)
			}
			blocks = append(blocks, anthropicapi.ContentBlockParam{
				Type:  "tool_use",
				ID:    toolUseID,
				Name:  typed.Name,
				Input: json.RawMessage(inputJSON),
			})
		case ToolResult:
			if typed.CallID == "" {
				return anthropicapi.MessageParam{}, false, errors.New("tool result missing call_id")
			}
			blocks = append(blocks, anthropicapi.ContentBlockParam{
				Type:      "tool_result",
				ToolUseID: typed.CallID,
				Result:    typed.Result,
				IsError:   typed.IsError,
			})
		case ReasoningContent:
			// Anthropic thinking blocks require a signature to be echoed back. We don't
			// currently model signatures in ReasoningContent, so skip these on encode.
			continue
		default:
			return anthropicapi.MessageParam{}, false, fmt.Errorf("unsupported content part type: %T", part)
		}
	}
	if len(blocks) == 0 {
		return anthropicapi.MessageParam{}, false, nil
	}
	return anthropicapi.MessageParam{Role: role, Content: blocks}, true, nil
}
func anthropicMapTurnRole(role Role) (string, bool) {
	switch role {
	case RoleUser:
		return "user", true
	case RoleAssistant:
		return "assistant", true
	default:
		return "", false
	}
}
func normalizeToolCallInputJSON(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		trimmed = "{}"
	}
	if !json.Valid([]byte(trimmed)) {
		return "", fmt.Errorf("invalid JSON: %q", trimmed)
	}
	var b bytes.Buffer
	if err := json.Compact(&b, []byte(trimmed)); err != nil {
		return "", err
	}
	return b.String(), nil
}
func anthropicApplySendOptions(req *anthropicapi.MessageRequest, modelInfo llmmodel.ModelInfo, opt *SendOptions) error {
	req.Thinking = &anthropicapi.ThinkingParam{Type: "adaptive"}
	effort := ""
	serviceTier := strings.TrimSpace(modelInfo.ServiceTier)
	if opt != nil {
		if opt.TemperaturePresent {
			temp := opt.Temperature
			req.Temperature = &temp
		}
		if opt.ReasoningEffort != "" {
			effort = opt.ReasoningEffort
		}
		if strings.TrimSpace(opt.ServiceTier) != "" {
			serviceTier = strings.TrimSpace(opt.ServiceTier)
		}
	}
	effort = anthropicMapReasoningEffort(effort)
	if effort != "" {
		req.OutputConfig = &anthropicapi.OutputConfigParam{Effort: effort}
	}
	switch serviceTier {
	case "", "auto", "standard_only":
		req.ServiceTier = serviceTier
	default:
		return fmt.Errorf("invalid anthropic service tier %q (must be \"\", \"auto\", or \"standard_only\")", serviceTier)
	}
	return nil
}
func anthropicMapReasoningEffort(effort string) string {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "":
		return ""
	case "minimal", "low":
		return "low"
	case "medium":
		return "medium"
	case "high", "xhigh":
		return "high"
	default:
		return strings.ToLower(strings.TrimSpace(effort))
	}
}
func buildAnthropicToolParams(tools []Tool) ([]anthropicapi.ToolParam, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	result := make([]anthropicapi.ToolParam, 0, len(tools))
	for _, tool := range tools {
		info := tool.Info()
		if info.Name == "" {
			return nil, errors.New("tool name is required")
		}
		kind := info.Kind
		if kind == "" {
			kind = ToolKindFunction
		}
		if kind != ToolKindFunction {
			return nil, fmt.Errorf("anthropic currently supports function tools only (tool=%s kind=%s)", info.Name, kind)
		}
		properties := make(map[string]any, len(info.Parameters))
		for k, v := range info.Parameters {
			properties[k] = v
		}
		schema := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties":           properties,
		}
		if len(info.Required) > 0 {
			required := append([]string(nil), info.Required...)
			sort.Strings(required)
			schema["required"] = required
		}
		inputSchema, err := json.Marshal(schema)
		if err != nil {
			return nil, fmt.Errorf("tool %q has invalid input schema: %w", info.Name, err)
		}
		toolParam := anthropicapi.ToolParam{
			Name:        info.Name,
			Description: info.Description,
			InputSchema: inputSchema,
		}
		result = append(result, toolParam)
	}
	return result, nil
}

type anthropicStreamState struct {
	messageID    string
	usage        anthropicapi.Usage
	stopReason   string
	stopSequence string
	blocks       map[int]*anthropicBlockState
}
type anthropicBlockState struct {
	kind              string
	providerID        string
	text              strings.Builder
	thinking          strings.Builder
	toolName          string
	toolCallID        string
	toolInputJSON     string
	sawInputJSONDelta bool
}

func newAnthropicStreamState() *anthropicStreamState {
	return &anthropicStreamState{
		blocks: make(map[int]*anthropicBlockState),
	}
}
func (s *anthropicStreamState) processEvent(evt anthropicapi.Event) (*Event, bool, error) {
	switch evt.Type {
	case anthropicapi.EventTypeMessageStart:
		if evt.Message == nil {
			return nil, false, errors.New("message_start event missing message")
		}
		s.messageID = evt.Message.ID
		s.usage = mergeAnthropicUsage(s.usage, evt.Message.Usage)
		if evt.Message.StopReason != "" {
			s.stopReason = evt.Message.StopReason
		}
		if evt.Message.StopSequence != "" {
			s.stopSequence = evt.Message.StopSequence
		}
		turn := &Turn{
			Role:         RoleAssistant,
			ProviderID:   s.messageID,
			Usage:        anthropicConvertUsage(s.usage),
			FinishReason: FinishReasonInProgress,
		}
		return &Event{Type: EventTypeCreated, Turn: turn}, false, nil
	case anthropicapi.EventTypeContentBlockStart:
		return s.onContentBlockStart(evt.Index, evt.ContentBlock)
	case anthropicapi.EventTypeContentBlockDelta:
		return s.onContentBlockDelta(evt.Index, evt.Delta)
	case anthropicapi.EventTypeContentBlockStop:
		return s.onContentBlockStop(evt.Index)
	case anthropicapi.EventTypeMessageDelta:
		if evt.MessageDelta != nil {
			s.usage = mergeAnthropicUsage(s.usage, evt.MessageDelta.Usage)
			if evt.MessageDelta.StopReason != "" {
				s.stopReason = evt.MessageDelta.StopReason
			}
			if evt.MessageDelta.StopSequence != "" {
				s.stopSequence = evt.MessageDelta.StopSequence
			}
		}
		return nil, false, nil
	case anthropicapi.EventTypeMessageStop:
		turn, err := s.buildTurn()
		if err != nil {
			return nil, true, err
		}
		return &Event{Type: EventTypeCompletedSuccess, Turn: &turn}, true, nil
	case anthropicapi.EventTypeError:
		msg := "anthropic streaming error"
		errType := ""
		if evt.Error != nil {
			if strings.TrimSpace(evt.Error.Message) != "" {
				msg = evt.Error.Message
			}
			errType = evt.Error.Type
		}
		if errType != "" {
			return nil, true, fmt.Errorf("%s (type=%s)", msg, errType)
		}
		return nil, true, errors.New(msg)
	case anthropicapi.EventTypePing:
		return nil, false, nil
	default:
		return nil, false, nil
	}
}
func (s *anthropicStreamState) onContentBlockStart(index int, block *anthropicapi.ContentBlock) (*Event, bool, error) {
	if block == nil {
		return nil, false, errors.New("content_block_start missing content block")
	}
	state := s.ensureBlock(index, block.Type)
	switch block.Type {
	case "text":
		if block.Text == "" {
			return nil, false, nil
		}
		state.text.WriteString(block.Text)
		return &Event{
			Type:  EventTypeTextDelta,
			Delta: block.Text,
			Text: &TextContent{
				ProviderID: state.providerID,
				Content:    state.text.String(),
			},
		}, false, nil
	case "thinking":
		if block.Thinking == "" {
			return nil, false, nil
		}
		state.thinking.WriteString(block.Thinking)
		return &Event{
			Type:  EventTypeReasoningDelta,
			Delta: block.Thinking,
			Reasoning: &ReasoningContent{
				ProviderID: state.providerID,
				Content:    state.thinking.String(),
			},
		}, false, nil
	case "tool_use":
		state.toolName = block.Name
		state.toolCallID = block.ID
		if block.ID != "" {
			state.providerID = block.ID
		}
		state.toolInputJSON = strings.TrimSpace(string(block.Input))
		state.sawInputJSONDelta = false
		return nil, false, nil
	default:
		return nil, false, nil
	}
}
func (s *anthropicStreamState) onContentBlockDelta(index int, delta *anthropicapi.ContentBlockDelta) (*Event, bool, error) {
	if delta == nil {
		return nil, false, nil
	}
	state, ok := s.blocks[index]
	if !ok {
		return nil, false, fmt.Errorf("content_block_delta references unknown index %d", index)
	}
	switch delta.Type {
	case "text_delta":
		if delta.Text == "" {
			return nil, false, nil
		}
		state.text.WriteString(delta.Text)
		return &Event{
			Type:  EventTypeTextDelta,
			Delta: delta.Text,
			Text: &TextContent{
				ProviderID: state.providerID,
				Content:    state.text.String(),
			},
		}, false, nil
	case "thinking_delta":
		if delta.Thinking == "" {
			return nil, false, nil
		}
		state.thinking.WriteString(delta.Thinking)
		return &Event{
			Type:  EventTypeReasoningDelta,
			Delta: delta.Thinking,
			Reasoning: &ReasoningContent{
				ProviderID: state.providerID,
				Content:    state.thinking.String(),
			},
		}, false, nil
	case "input_json_delta":
		if state.kind != "tool_use" {
			return nil, false, nil
		}
		if !state.sawInputJSONDelta {
			if state.toolInputJSON == "" || state.toolInputJSON == "{}" {
				state.toolInputJSON = ""
			}
			state.sawInputJSONDelta = true
		}
		state.toolInputJSON += delta.PartialJSON
		return nil, false, nil
	case "signature_delta":
		// We don't currently expose signature details on ReasoningContent.
		return nil, false, nil
	default:
		return nil, false, nil
	}
}
func (s *anthropicStreamState) onContentBlockStop(index int) (*Event, bool, error) {
	state, ok := s.blocks[index]
	if !ok {
		return nil, false, fmt.Errorf("content_block_stop references unknown index %d", index)
	}
	switch state.kind {
	case "text":
		return &Event{
			Type: EventTypeTextDelta,
			Text: &TextContent{
				ProviderID: state.providerID,
				Content:    state.text.String(),
			},
			Done: true,
		}, false, nil
	case "thinking":
		return &Event{
			Type: EventTypeReasoningDelta,
			Reasoning: &ReasoningContent{
				ProviderID: state.providerID,
				Content:    state.thinking.String(),
			},
			Done: true,
		}, false, nil
	case "tool_use":
		input, err := normalizeToolCallInputJSON(state.toolInputJSON)
		if err != nil {
			return nil, false, fmt.Errorf("tool %q input: %w", state.toolName, err)
		}
		callID := state.toolCallID
		if callID == "" {
			callID = state.providerID
		}
		tc := &ToolCall{
			ProviderID: state.providerID,
			CallID:     callID,
			Name:       state.toolName,
			Type:       "function_call",
			Input:      input,
		}
		return &Event{Type: EventTypeToolUse, ToolCall: tc}, false, nil
	default:
		return nil, false, nil
	}
}
func (s *anthropicStreamState) ensureBlock(index int, kind string) *anthropicBlockState {
	state, ok := s.blocks[index]
	if !ok {
		state = &anthropicBlockState{
			kind:       kind,
			providerID: anthropicContentProviderID(s.messageID, kind, index),
		}
		s.blocks[index] = state
		return state
	}
	state.kind = kind
	if state.providerID == "" {
		state.providerID = anthropicContentProviderID(s.messageID, kind, index)
	}
	return state
}
func anthropicContentProviderID(messageID, kind string, index int) string {
	if messageID == "" {
		return fmt.Sprintf("%s:%d", kind, index)
	}
	return fmt.Sprintf("%s:%s:%d", messageID, kind, index)
}
func (s *anthropicStreamState) buildTurn() (Turn, error) {
	indexes := make([]int, 0, len(s.blocks))
	for idx := range s.blocks {
		indexes = append(indexes, idx)
	}
	sort.Ints(indexes)
	parts := make([]ContentPart, 0, len(indexes))
	hasToolCalls := false
	for _, idx := range indexes {
		state := s.blocks[idx]
		switch state.kind {
		case "text":
			parts = append(parts, TextContent{
				ProviderID: state.providerID,
				Content:    state.text.String(),
			})
		case "thinking":
			parts = append(parts, ReasoningContent{
				ProviderID: state.providerID,
				Content:    state.thinking.String(),
			})
		case "tool_use":
			input, err := normalizeToolCallInputJSON(state.toolInputJSON)
			if err != nil {
				return Turn{}, fmt.Errorf("tool %q input: %w", state.toolName, err)
			}
			callID := state.toolCallID
			if callID == "" {
				callID = state.providerID
			}
			parts = append(parts, ToolCall{
				ProviderID: state.providerID,
				CallID:     callID,
				Name:       state.toolName,
				Type:       "function_call",
				Input:      input,
			})
			hasToolCalls = true
		}
	}
	return Turn{
		Role:         RoleAssistant,
		ProviderID:   s.messageID,
		Parts:        parts,
		Usage:        anthropicConvertUsage(s.usage),
		FinishReason: anthropicMapFinishReason(s.stopReason, hasToolCalls),
	}, nil
}
func mergeAnthropicUsage(base, delta anthropicapi.Usage) anthropicapi.Usage {
	if delta.InputTokens != 0 {
		base.InputTokens = delta.InputTokens
	}
	if delta.CacheCreationInputTokens != 0 {
		base.CacheCreationInputTokens = delta.CacheCreationInputTokens
	}
	if delta.CacheReadInputTokens != 0 {
		base.CacheReadInputTokens = delta.CacheReadInputTokens
	}
	if delta.OutputTokens != 0 {
		base.OutputTokens = delta.OutputTokens
	}
	if delta.CacheCreation.Ephemeral5mInputTokens != 0 {
		base.CacheCreation.Ephemeral5mInputTokens = delta.CacheCreation.Ephemeral5mInputTokens
	}
	if delta.CacheCreation.Ephemeral1hInputTokens != 0 {
		base.CacheCreation.Ephemeral1hInputTokens = delta.CacheCreation.Ephemeral1hInputTokens
	}
	return base
}
func anthropicConvertUsage(usage anthropicapi.Usage) TokenUsage {
	cacheCreationTokens := usage.CacheCreationInputTokens
	if cacheCreationTokens == 0 {
		cacheCreationTokens = usage.CacheCreation.Ephemeral5mInputTokens + usage.CacheCreation.Ephemeral1hInputTokens
	}
	cachedTokens := usage.CacheReadInputTokens + cacheCreationTokens
	return TokenUsage{
		TotalInputTokens:  usage.InputTokens + cachedTokens,
		CachedInputTokens: cachedTokens,
		ReasoningTokens:   0,
		TotalOutputTokens: usage.OutputTokens,
	}
}
func anthropicMapFinishReason(stopReason string, hasToolCalls bool) FinishReason {
	switch strings.ToLower(strings.TrimSpace(stopReason)) {
	case "tool_use":
		return FinishReasonToolUse
	case "max_tokens":
		return FinishReasonMaxTokens
	case "end_turn", "stop_sequence":
		return FinishReasonEndTurn
	case "pause_turn":
		return FinishReasonInProgress
	case "refusal":
		return FinishReasonPermissionDenied
	case "":
		if hasToolCalls {
			return FinishReasonToolUse
		}
		return FinishReasonUnknown
	default:
		return FinishReasonUnknown
	}
}
