package llmstream

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/codalotl/codalotl/internal/llmmodel"
	geminiapi "github.com/codalotl/codalotl/internal/llmstream/gemini"
)

const geminiEmptyStopMaxRetries = 3

type geminiAttemptFunc func(context.Context, chan Event, *SendOptions, llmmodel.ModelInfo) (Turn, *geminiapi.Content, error)

func (sc *streamingConversation) sendAsyncGemini(ctx context.Context, out chan Event, opt *SendOptions, modelInfo llmmodel.ModelInfo) (Turn, error) {
	return sc.sendAsyncGeminiWithAttempt(ctx, out, opt, modelInfo, sc.sendAsyncGeminiOnce)
}

func (sc *streamingConversation) sendAsyncGeminiWithAttempt(ctx context.Context, out chan Event, opt *SendOptions, modelInfo llmmodel.ModelInfo, attempt geminiAttemptFunc) (Turn, error) {
	for retry := 0; retry <= geminiEmptyStopMaxRetries; retry++ {
		if err := ctx.Err(); err != nil {
			return Turn{}, sc.LogWrappedErr("gemini_send_async.context", err)
		}

		turn, content, err := attempt(ctx, out, opt, modelInfo)
		if err != nil {
			return Turn{}, err
		}
		if !geminiIsEmptyStopTurn(turn) {
			if content != nil {
				sc.geminiContents = append(sc.geminiContents, content)
			}
			return turn, nil
		}

		if retry == geminiEmptyStopMaxRetries {
			return Turn{}, sc.LogNewErr("gemini_send_async.empty_stop_exhausted", "max_retries", geminiEmptyStopMaxRetries, "model_id", string(sc.modelID))
		}

		retryErr := sc.LogNewErr("gemini_send_async.empty_stop_retry", "retry", retry+1, "max_retries", geminiEmptyStopMaxRetries, "model_id", string(sc.modelID))
		if !trySendEvent(ctx, out, Event{Type: EventTypeRetry, Error: retryErr}) {
			return Turn{}, sc.LogWrappedErr("gemini_send_async.context", context.Canceled)
		}
	}

	return Turn{}, sc.LogNewErr("gemini_send_async.unreachable")
}

func (sc *streamingConversation) sendAsyncGeminiOnce(ctx context.Context, out chan Event, opt *SendOptions, modelInfo llmmodel.ModelInfo) (Turn, *geminiapi.Content, error) {
	if err := ctx.Err(); err != nil {
		return Turn{}, nil, sc.LogWrappedErr("gemini_send_async.context", err)
	}

	apiKey := llmmodel.GetAPIKey(sc.modelID)
	if apiKey == "" {
		return Turn{}, nil, sc.LogNewErr("gemini_send_async.api_key_missing", "model_id", string(sc.modelID), "provider", modelInfo.ProviderID)
	}

	modelID := strings.TrimSpace(modelInfo.ProviderModelID)
	if modelID == "" {
		return Turn{}, nil, sc.LogNewErr("gemini_send_async.model_missing", "model_id", string(sc.modelID))
	}

	contents, err := sc.geminiContentsForRequest()
	if err != nil {
		return Turn{}, nil, sc.LogWrappedErr("gemini_send_async.build_contents", err)
	}

	config, err := sc.buildGeminiGenerateContentConfig(modelInfo, opt)
	if err != nil {
		return Turn{}, nil, sc.LogWrappedErr("gemini_send_async.build_config", err)
	}

	clientConfig := &geminiapi.ClientConfig{
		APIKey:  apiKey,
		Backend: geminiapi.BackendGeminiAPI,
	}
	if baseURL := llmmodel.GetAPIEndpointURL(sc.modelID); baseURL != "" {
		clientConfig.HTTPOptions.BaseURL = baseURL
	}

	client, err := geminiapi.NewClient(ctx, clientConfig)
	if err != nil {
		return Turn{}, nil, sc.LogWrappedErr("gemini_send_async.client", err)
	}

	debugPrint(debugHTTPRequests, "HTTP REQUEST: gemini generateContentStream", map[string]any{
		"model":    modelID,
		"contents": contents,
		"config":   config,
	})

	startTime := time.Now()
	toDebouncer := make(chan Event, 1024)
	debounceDone := make(chan struct{})
	defer func() {
		debugPrint(debugEvents, "Func done - closing gemini toDebouncer", nil)
		close(toDebouncer)
		<-debounceDone
	}()
	go func() {
		debounceEvents(ctx, toDebouncer, out)
		debugPrint(debugEvents, "Done debouncing gemini. Closing debounceDone", nil)
		close(debounceDone)
	}()

	state := newGeminiStreamState()
	for resp, streamErr := range client.Models.GenerateContentStream(ctx, modelID, contents, config) {
		if streamErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return Turn{}, nil, sc.LogWrappedErr("gemini_send_async.context", ctxErr)
			}
			wrapped := sc.LogWrappedErr("gemini_send_async.stream", streamErr)
			if geminiIsRetryableStreamError(streamErr) {
				return Turn{}, nil, makeRetryable(wrapped)
			}
			return Turn{}, nil, wrapped
		}
		if resp == nil {
			continue
		}

		debugPrint(debugEvents, fmt.Sprintf("EVENT: gemini chunk; elapsed=%v", time.Since(startTime)), nil)
		debugPrint(debugHTTPResponses, "HTTP RESPONSE: gemini stream chunk", resp)

		events, err := state.processResponse(resp)
		if err != nil {
			return Turn{}, nil, sc.LogWrappedErr("gemini_send_async.event", err)
		}
		for _, event := range events {
			if !trySendEvent(ctx, toDebouncer, event) {
				return Turn{}, nil, sc.LogWrappedErr("gemini_send_async.context", context.Canceled)
			}
		}
	}

	finalEvents, finalTurn, exactContent, err := state.finalize()
	if err != nil {
		return Turn{}, nil, sc.LogWrappedErr("gemini_send_async.finalize", err)
	}

	for _, event := range finalEvents {
		if !trySendEvent(ctx, toDebouncer, event) {
			return Turn{}, nil, sc.LogWrappedErr("gemini_send_async.context", context.Canceled)
		}
	}

	if geminiIsEmptyStopTurn(finalTurn) {
		return finalTurn, exactContent, nil
	}

	if !trySendEvent(ctx, toDebouncer, Event{Type: EventTypeCompletedSuccess, Turn: &finalTurn}) {
		return Turn{}, nil, sc.LogWrappedErr("gemini_send_async.context", context.Canceled)
	}
	debugPrint(debugParsedResponses, "PARSED RESPONSE: gemini assistant response", finalTurn)

	return finalTurn, exactContent, nil
}

func geminiIsEmptyStopTurn(turn Turn) bool {
	return turn.FinishReason == FinishReasonEndTurn && len(turn.Parts) == 0
}

func geminiIsRetryableStreamError(err error) bool {
	if err == nil {
		return false
	}

	var apiErr *geminiapi.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Retryable()
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout() || netErr.Temporary()
	}

	return errors.Is(err, io.ErrUnexpectedEOF)
}

func (sc *streamingConversation) geminiContentsForRequest() ([]*geminiapi.Content, error) {
	if len(sc.geminiContents) == len(sc.turns)-1 {
		return cloneGeminiContents(sc.geminiContents), nil
	}
	if len(sc.geminiContents) > 0 {
		sc.Log("gemini.contents.desync", "gemini_contents", len(sc.geminiContents), "turns_without_system", len(sc.turns)-1)
	}

	contents := make([]*geminiapi.Content, 0, len(sc.turns)-1)
	for _, turn := range sc.turns[1:] {
		content, include, err := geminiBuildContentFromTurn(turn)
		if err != nil {
			return nil, err
		}
		if include {
			contents = append(contents, content)
		}
	}
	return contents, nil
}

func (sc *streamingConversation) buildGeminiGenerateContentConfig(modelInfo llmmodel.ModelInfo, opt *SendOptions) (*geminiapi.GenerateContentConfig, error) {
	config := &geminiapi.GenerateContentConfig{
		CandidateCount: 1,
	}

	systemInstruction := sc.turns[0].TextContent()
	if systemInstruction != "" {
		config.SystemInstruction = &geminiapi.Content{
			Parts: []*geminiapi.Part{{Text: systemInstruction}},
		}
	}
	if modelInfo.MaxOutput > 0 && modelInfo.MaxOutput <= math.MaxInt32 {
		config.MaxOutputTokens = int32(modelInfo.MaxOutput)
	}
	if opt != nil && opt.TemperaturePresent {
		temp := float32(opt.Temperature)
		config.Temperature = &temp
	}

	thinkingConfig := geminiBuildThinkingConfig(modelInfo, opt)
	if thinkingConfig != nil {
		config.ThinkingConfig = thinkingConfig
	}

	if len(sc.tools) > 0 {
		tools, err := buildGeminiToolParams(sc.tools)
		if err != nil {
			return nil, err
		}
		config.Tools = tools
	}

	return config, nil
}

func geminiBuildThinkingConfig(modelInfo llmmodel.ModelInfo, opt *SendOptions) *geminiapi.ThinkingConfig {
	effort := strings.TrimSpace(modelInfo.ReasoningEffort)
	if opt != nil && strings.TrimSpace(opt.ReasoningEffort) != "" {
		effort = strings.TrimSpace(opt.ReasoningEffort)
	}
	if effort == "" && !modelInfo.CanReason {
		return nil
	}

	config := &geminiapi.ThinkingConfig{
		IncludeThoughts: true,
	}
	if level := geminiMapThinkingLevel(effort); level != "" {
		config.ThinkingLevel = level
	}
	return config
}

func geminiMapThinkingLevel(effort string) geminiapi.ThinkingLevel {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "":
		return ""
	case "minimal":
		return geminiapi.ThinkingLevelMinimal
	case "low":
		return geminiapi.ThinkingLevelLow
	case "medium":
		return geminiapi.ThinkingLevelMedium
	case "high", "xhigh":
		return geminiapi.ThinkingLevelHigh
	default:
		return geminiapi.ThinkingLevel(strings.ToUpper(strings.TrimSpace(effort)))
	}
}

func buildGeminiToolParams(tools []Tool) ([]*geminiapi.Tool, error) {
	if len(tools) == 0 {
		return nil, nil
	}

	declarations := make([]*geminiapi.FunctionDeclaration, 0, len(tools))
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
			return nil, fmt.Errorf("gemini currently supports function tools only (tool=%s kind=%s)", info.Name, kind)
		}

		schema := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties":           map[string]any{},
		}
		if len(info.Parameters) > 0 {
			properties := make(map[string]any, len(info.Parameters))
			propertyOrdering := make([]string, 0, len(info.Parameters))
			for key, value := range info.Parameters {
				properties[key] = cloneAny(value)
				propertyOrdering = append(propertyOrdering, key)
			}
			sort.Strings(propertyOrdering)
			schema["properties"] = properties
			schema["propertyOrdering"] = propertyOrdering
		}
		if len(info.Required) > 0 {
			required := append([]string(nil), info.Required...)
			sort.Strings(required)
			schema["required"] = required
		}

		declaration := &geminiapi.FunctionDeclaration{
			Name:                 info.Name,
			Description:          info.Description,
			ParametersJsonSchema: schema,
		}
		declarations = append(declarations, declaration)
	}

	return []*geminiapi.Tool{{FunctionDeclarations: declarations}}, nil
}

func geminiBuildContentFromTurn(turn Turn) (*geminiapi.Content, bool, error) {
	role, ok := geminiMapTurnRole(turn.Role)
	if !ok {
		return nil, false, fmt.Errorf("unsupported turn role for gemini: %v", turn.Role)
	}

	parts := make([]*geminiapi.Part, 0, len(turn.Parts))
	for _, part := range turn.Parts {
		switch typed := part.(type) {
		case TextContent:
			if typed.Content == "" {
				continue
			}
			parts = append(parts, &geminiapi.Part{Text: typed.Content})
		case ReasoningContent:
			if typed.Content == "" && typed.ProviderState == "" {
				continue
			}
			signature, err := geminiDecodeThoughtSignature(typed.ProviderState)
			if err != nil {
				return nil, false, fmt.Errorf("reasoning provider_state: %w", err)
			}
			parts = append(parts, &geminiapi.Part{
				Text:             typed.Content,
				Thought:          true,
				ThoughtSignature: signature,
			})
		case ToolCall:
			part, err := geminiToolCallPart(typed)
			if err != nil {
				return nil, false, err
			}
			parts = append(parts, part)
		case ToolResult:
			part, err := geminiToolResultPart(typed)
			if err != nil {
				return nil, false, err
			}
			parts = append(parts, part)
		default:
			return nil, false, fmt.Errorf("unsupported content part type: %T", part)
		}
	}
	if len(parts) == 0 {
		return nil, false, nil
	}

	return &geminiapi.Content{
		Role:  string(role),
		Parts: parts,
	}, true, nil
}

func geminiMapTurnRole(role Role) (geminiapi.Role, bool) {
	switch role {
	case RoleUser:
		return geminiapi.RoleUser, true
	case RoleAssistant:
		return geminiapi.RoleModel, true
	default:
		return "", false
	}
}

func geminiToolCallPart(call ToolCall) (*geminiapi.Part, error) {
	if call.Name == "" {
		return nil, errors.New("tool call name is required")
	}
	args, err := geminiParseJSONObject(call.Input)
	if err != nil {
		return nil, fmt.Errorf("tool call %q has invalid input json: %w", call.Name, err)
	}

	functionCall := &geminiapi.FunctionCall{
		Name: call.Name,
		Args: args,
	}
	if call.CallID != "" {
		functionCall.ID = call.CallID
	} else if call.ProviderID != "" {
		functionCall.ID = call.ProviderID
	}
	return &geminiapi.Part{FunctionCall: functionCall}, nil
}

func geminiToolResultPart(result ToolResult) (*geminiapi.Part, error) {
	if result.CallID == "" {
		return nil, errors.New("tool result missing call_id")
	}
	if result.Name == "" {
		return nil, errors.New("tool result missing name")
	}

	key := "output"
	if result.IsError {
		key = "error"
	}
	functionResponse := &geminiapi.FunctionResponse{
		ID:       result.CallID,
		Name:     result.Name,
		Response: map[string]any{key: geminiParseJSONValueOrString(result.Result)},
	}
	return &geminiapi.Part{FunctionResponse: functionResponse}, nil
}

func geminiParseJSONObject(raw string) (map[string]any, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return map[string]any{}, nil
	}

	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return nil, err
	}
	asMap, ok := decoded.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected JSON object, got %T", decoded)
	}
	return asMap, nil
}

func geminiParseJSONValueOrString(raw string) any {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
		return decoded
	}
	return raw
}

func geminiEncodeThoughtSignature(signature []byte) string {
	if len(signature) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(signature)
}

func geminiDecodeThoughtSignature(encoded string) ([]byte, error) {
	if encoded == "" {
		return nil, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	return decoded, nil
}

func geminiConvertUsage(usage *geminiapi.GenerateContentResponseUsageMetadata) TokenUsage {
	if usage == nil {
		return TokenUsage{}
	}
	return TokenUsage{
		TotalInputTokens:         int64(usage.PromptTokenCount) + int64(usage.ToolUsePromptTokenCount),
		CachedInputTokens:        int64(usage.CachedContentTokenCount),
		CacheCreationInputTokens: 0,
		ReasoningTokens:          int64(usage.ThoughtsTokenCount),
		TotalOutputTokens:        int64(usage.CandidatesTokenCount) + int64(usage.ThoughtsTokenCount),
	}
}

func geminiMapFinishReason(reason geminiapi.FinishReason, hasToolCalls bool) FinishReason {
	switch reason {
	case geminiapi.FinishReasonStop:
		if hasToolCalls {
			return FinishReasonToolUse
		}
		return FinishReasonEndTurn
	case geminiapi.FinishReasonMaxTokens:
		return FinishReasonMaxTokens
	case geminiapi.FinishReasonSafety, geminiapi.FinishReasonBlocklist, geminiapi.FinishReasonProhibitedContent, geminiapi.FinishReasonSPII, geminiapi.FinishReasonImageSafety, geminiapi.FinishReasonImageProhibitedContent:
		return FinishReasonPermissionDenied
	case geminiapi.FinishReasonMalformedFunctionCall, geminiapi.FinishReasonUnexpectedToolCall:
		return FinishReasonError
	case "":
		if hasToolCalls {
			return FinishReasonToolUse
		}
		return FinishReasonUnknown
	default:
		return FinishReasonUnknown
	}
}

type geminiTextAccumulator struct {
	providerID string
	text       strings.Builder
}

type geminiReasoningAccumulator struct {
	providerID string
	text       strings.Builder
	signature  []byte
}

type geminiStreamState struct {
	createdSent   bool
	responseID    string
	usage         *geminiapi.GenerateContentResponseUsageMetadata
	finishReason  geminiapi.FinishReason
	finishMessage string
	promptBlocked string
	exactContent  *geminiapi.Content
	publicParts   []ContentPart
	nextPartIndex int
	openText      *geminiTextAccumulator
	openReasoning *geminiReasoningAccumulator
}

func newGeminiStreamState() *geminiStreamState {
	return &geminiStreamState{}
}

func (s *geminiStreamState) processResponse(resp *geminiapi.GenerateContentResponse) ([]Event, error) {
	if resp == nil {
		return nil, nil
	}

	if resp.ResponseID != "" {
		s.responseID = resp.ResponseID
	}
	if resp.UsageMetadata != nil {
		s.usage = resp.UsageMetadata
	}

	events := make([]Event, 0, 4)
	if !s.createdSent {
		s.createdSent = true
		turn := &Turn{
			Role:         RoleAssistant,
			ProviderID:   s.responseID,
			Usage:        geminiConvertUsage(s.usage),
			FinishReason: FinishReasonInProgress,
		}
		events = append(events, Event{Type: EventTypeCreated, Turn: turn})
	}

	if resp.PromptFeedback != nil && len(resp.Candidates) == 0 {
		s.promptBlocked = string(resp.PromptFeedback.BlockReason)
		if resp.PromptFeedback.BlockReasonMessage != "" {
			s.promptBlocked = resp.PromptFeedback.BlockReasonMessage
		}
	}
	if len(resp.Candidates) == 0 || resp.Candidates[0] == nil {
		return events, nil
	}

	candidate := resp.Candidates[0]
	if candidate.FinishReason != "" {
		s.finishReason = candidate.FinishReason
	}
	if candidate.FinishMessage != "" {
		s.finishMessage = candidate.FinishMessage
	}
	if candidate.Content == nil || len(candidate.Content.Parts) == 0 {
		return events, nil
	}

	if s.exactContent == nil {
		s.exactContent = &geminiapi.Content{Role: string(geminiapi.RoleModel)}
	}
	if candidate.Content.Role != "" {
		s.exactContent.Role = candidate.Content.Role
	}

	for _, part := range candidate.Content.Parts {
		if part == nil {
			continue
		}
		s.appendExactPart(part)

		partEvents, err := s.processPart(part)
		if err != nil {
			return nil, err
		}
		events = append(events, partEvents...)
	}

	return events, nil
}

func (s *geminiStreamState) processPart(part *geminiapi.Part) ([]Event, error) {
	switch {
	case part.FunctionCall != nil:
		events := s.closeOpenParts()
		toolCall, err := s.toolCallFromPart(part.FunctionCall)
		if err != nil {
			return nil, err
		}
		s.publicParts = append(s.publicParts, toolCall)
		return append(events, Event{Type: EventTypeToolUse, ToolCall: &toolCall}), nil
	case part.Thought:
		events := s.closeText()
		if s.openReasoning == nil {
			s.openReasoning = &geminiReasoningAccumulator{
				providerID: s.newProviderID("reasoning"),
			}
		}
		if len(part.ThoughtSignature) > 0 {
			s.openReasoning.signature = append(s.openReasoning.signature, part.ThoughtSignature...)
		}
		if part.Text == "" {
			return events, nil
		}
		s.openReasoning.text.WriteString(part.Text)
		reasoning := ReasoningContent{
			ProviderID:    s.openReasoning.providerID,
			Content:       s.openReasoning.text.String(),
			ProviderState: geminiEncodeThoughtSignature(s.openReasoning.signature),
		}
		return append(events, Event{Type: EventTypeReasoningDelta, Delta: part.Text, Reasoning: &reasoning}), nil
	case part.Text != "":
		events := s.closeReasoning()
		if s.openText == nil {
			s.openText = &geminiTextAccumulator{
				providerID: s.newProviderID("text"),
			}
		}
		s.openText.text.WriteString(part.Text)
		text := TextContent{
			ProviderID: s.openText.providerID,
			Content:    s.openText.text.String(),
		}
		return append(events, Event{Type: EventTypeTextDelta, Delta: part.Text, Text: &text}), nil
	default:
		return nil, nil
	}
}

func (s *geminiStreamState) toolCallFromPart(call *geminiapi.FunctionCall) (ToolCall, error) {
	if call == nil {
		return ToolCall{}, errors.New("missing function call")
	}
	if call.Name == "" {
		return ToolCall{}, errors.New("function call name is required")
	}

	inputBytes, err := json.Marshal(call.Args)
	if err != nil {
		return ToolCall{}, fmt.Errorf("marshal function args: %w", err)
	}

	providerID := call.ID
	if providerID == "" {
		providerID = s.newProviderID("tool_call")
	}
	callID := call.ID
	if callID == "" {
		callID = providerID
	}

	return ToolCall{
		ProviderID: providerID,
		CallID:     callID,
		Name:       call.Name,
		Type:       "function_call",
		Input:      string(inputBytes),
	}, nil
}

func (s *geminiStreamState) appendExactPart(part *geminiapi.Part) {
	if s.exactContent == nil {
		s.exactContent = &geminiapi.Content{Role: string(geminiapi.RoleModel)}
	}

	switch {
	case part.FunctionCall != nil || part.FunctionResponse != nil:
		s.exactContent.Parts = append(s.exactContent.Parts, cloneGeminiPart(part))
	case part.Thought:
		last := s.lastExactPart()
		if last != nil && last.Thought && last.FunctionCall == nil && last.FunctionResponse == nil {
			last.Text += part.Text
			if len(part.ThoughtSignature) > 0 {
				last.ThoughtSignature = append(last.ThoughtSignature, part.ThoughtSignature...)
			}
			return
		}
		if part.Text == "" && len(part.ThoughtSignature) == 0 {
			return
		}
		if part.Text == "" && len(part.ThoughtSignature) > 0 {
			return
		}
		s.exactContent.Parts = append(s.exactContent.Parts, cloneGeminiPart(part))
	case part.Text != "":
		last := s.lastExactPart()
		if last != nil && !last.Thought && last.FunctionCall == nil && last.FunctionResponse == nil {
			last.Text += part.Text
			return
		}
		s.exactContent.Parts = append(s.exactContent.Parts, cloneGeminiPart(part))
	default:
		return
	}
}

func (s *geminiStreamState) lastExactPart() *geminiapi.Part {
	if s.exactContent == nil || len(s.exactContent.Parts) == 0 {
		return nil
	}
	return s.exactContent.Parts[len(s.exactContent.Parts)-1]
}

func (s *geminiStreamState) finalize() ([]Event, Turn, *geminiapi.Content, error) {
	events := s.closeOpenParts()
	if s.promptBlocked != "" {
		return nil, Turn{}, nil, fmt.Errorf("gemini prompt blocked: %s", s.promptBlocked)
	}
	if s.finishReason == geminiapi.FinishReasonMalformedFunctionCall || s.finishReason == geminiapi.FinishReasonUnexpectedToolCall {
		msg := s.finishMessage
		if msg == "" {
			msg = string(s.finishReason)
		}
		return nil, Turn{}, nil, errors.New(msg)
	}
	if !s.createdSent && s.exactContent == nil {
		return nil, Turn{}, nil, errors.New("gemini stream produced no response")
	}

	exactContent := cloneGeminiContent(s.exactContent)
	if exactContent == nil {
		exactContent = &geminiapi.Content{Role: string(geminiapi.RoleModel)}
	}
	if exactContent.Role == "" {
		exactContent.Role = string(geminiapi.RoleModel)
	}

	turn := Turn{
		Role:         RoleAssistant,
		ProviderID:   s.responseID,
		Parts:        append([]ContentPart(nil), s.publicParts...),
		Usage:        geminiConvertUsage(s.usage),
		FinishReason: geminiMapFinishReason(s.finishReason, geminiHasToolCalls(s.publicParts)),
	}
	return events, turn, exactContent, nil
}

func (s *geminiStreamState) closeOpenParts() []Event {
	events := s.closeText()
	events = append(events, s.closeReasoning()...)
	return events
}

func (s *geminiStreamState) closeText() []Event {
	if s.openText == nil {
		return nil
	}
	text := TextContent{
		ProviderID: s.openText.providerID,
		Content:    s.openText.text.String(),
	}
	s.publicParts = append(s.publicParts, text)
	s.openText = nil
	return []Event{{
		Type: EventTypeTextDelta,
		Text: &text,
		Done: true,
	}}
}

func (s *geminiStreamState) closeReasoning() []Event {
	if s.openReasoning == nil {
		return nil
	}
	reasoning := ReasoningContent{
		ProviderID:    s.openReasoning.providerID,
		Content:       s.openReasoning.text.String(),
		ProviderState: geminiEncodeThoughtSignature(s.openReasoning.signature),
	}
	s.publicParts = append(s.publicParts, reasoning)
	s.openReasoning = nil
	return []Event{{
		Type:      EventTypeReasoningDelta,
		Reasoning: &reasoning,
		Done:      true,
	}}
}

func (s *geminiStreamState) newProviderID(kind string) string {
	id := geminiContentProviderID(s.responseID, kind, s.nextPartIndex)
	s.nextPartIndex++
	return id
}

func geminiContentProviderID(responseID, kind string, index int) string {
	if responseID == "" {
		return fmt.Sprintf("%s:%d", kind, index)
	}
	return fmt.Sprintf("%s:%s:%d", responseID, kind, index)
}

func geminiHasToolCalls(parts []ContentPart) bool {
	for _, part := range parts {
		if _, ok := part.(ToolCall); ok {
			return true
		}
	}
	return false
}

func cloneGeminiContents(contents []*geminiapi.Content) []*geminiapi.Content {
	if len(contents) == 0 {
		return nil
	}
	cloned := make([]*geminiapi.Content, 0, len(contents))
	for _, content := range contents {
		cloned = append(cloned, cloneGeminiContent(content))
	}
	return cloned
}

func cloneGeminiContent(content *geminiapi.Content) *geminiapi.Content {
	if content == nil {
		return nil
	}
	cloned := &geminiapi.Content{
		Role:  content.Role,
		Parts: make([]*geminiapi.Part, 0, len(content.Parts)),
	}
	for _, part := range content.Parts {
		cloned.Parts = append(cloned.Parts, cloneGeminiPart(part))
	}
	return cloned
}

func cloneGeminiPart(part *geminiapi.Part) *geminiapi.Part {
	if part == nil {
		return nil
	}
	cloned := &geminiapi.Part{
		Text:    part.Text,
		Thought: part.Thought,
	}
	if len(part.ThoughtSignature) > 0 {
		cloned.ThoughtSignature = append([]byte(nil), part.ThoughtSignature...)
	}
	if part.FunctionCall != nil {
		cloned.FunctionCall = &geminiapi.FunctionCall{
			ID:   part.FunctionCall.ID,
			Name: part.FunctionCall.Name,
		}
		if len(part.FunctionCall.Args) > 0 {
			cloned.FunctionCall.Args = cloneMapAny(part.FunctionCall.Args)
		}
	}
	if part.FunctionResponse != nil {
		cloned.FunctionResponse = &geminiapi.FunctionResponse{
			ID:   part.FunctionResponse.ID,
			Name: part.FunctionResponse.Name,
		}
		if len(part.FunctionResponse.Response) > 0 {
			cloned.FunctionResponse.Response = cloneMapAny(part.FunctionResponse.Response)
		}
	}
	return cloned
}

func cloneMapAny(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneAny(value)
	}
	return out
}

func cloneAny(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		return cloneMapAny(typed)
	case []any:
		cloned := make([]any, 0, len(typed))
		for _, item := range typed {
			cloned = append(cloned, cloneAny(item))
		}
		return cloned
	default:
		return typed
	}
}
