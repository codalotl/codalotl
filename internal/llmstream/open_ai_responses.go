package llmstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/codalotl/codalotl/internal/llmmodel"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

// sendAsyncOpenAIResponses sends sc.responses to OpenAI using the Responses API + Streaming, and sends events back on out. Returns a new response
// that the caller can append to sc.responses. ctx is used for deadlines/cancellation.
//
// Division of responsibility:
//   - If an error occurs, log it and return it. The caller will send the error on the channel and maybe retry.
//   - Retryable errors are marked with makeRetryable. (Not used in this function - client retries.)
//   - On success, return a new Response. The caller will append to sc.responses.
//   - This function may stream multiple events (ex: deltas and a final completion) to out during processing.
//   - This function may write to provider-specific fields on sc (ex: providerConversationID).
func (sc *streamingConversation) sendAsyncOpenAIResponses(ctx context.Context, out chan Event, opt *SendOptions, modelInfo llmmodel.ModelInfo) (Turn, error) {
	if err := ctx.Err(); err != nil {
		return Turn{}, sc.LogWrappedErr("open_ai_send_async.context", err)
	}

	apiKey := llmmodel.GetAPIKey(sc.modelID)
	if apiKey == "" {
		return Turn{}, sc.LogNewErr("open_ai_send_async.api_key_missing", "model_id", string(sc.modelID), "provider", modelInfo.ProviderID)
	}

	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithMaxRetries(3),
	}
	if baseURL := llmmodel.GetAPIEndpointURL(sc.modelID); baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	client := openai.NewClient(opts...)

	params, err := sc.buildOpenAIResponsesParams(modelInfo)
	if err != nil {
		return Turn{}, sc.LogWrappedErr("open_ai_send_async.build_params", err)
	}
	// Link with previous response ID if available
	if sc.providerConversationID != "" {
		params.PreviousResponseID = param.NewOpt(sc.providerConversationID)
	}
	if err := openAIResponsesApplySendOptions(&params, modelInfo, opt); err != nil {
		return Turn{}, sc.LogWrappedErr("open_ai_send_async.options", err)
	}

	params.ParallelToolCalls = param.NewOpt(true)

	debugPrint(debugHTTPRequests, "HTTP REQUEST: create response(streaming=true)", params)

	startTime := time.Now()

	stream := client.Responses.NewStreaming(ctx, params)
	if stream == nil {
		return Turn{}, sc.LogNewErr("open_ai_send_async.stream_unavailable")
	}
	defer stream.Close()

	// Route all provider events through a debouncer before reaching caller's out
	toDebouncer := make(chan Event, 1024)
	debounceDone := make(chan struct{})
	defer func() {
		debugPrint(debugEvents, "Func done - closing toDebouncer", nil)
		close(toDebouncer)
		<-debounceDone // ensure debouncer flushes and exits before we return, so caller can safely close out
	}()
	go func() {
		debounceEvents(ctx, toDebouncer, out)
		debugPrint(debugEvents, "Done debouncing. Closing debounceDone", nil)
		close(debounceDone)
	}()

	if err := stream.Err(); err != nil {
		// NOTE: to look at actual HTTP error: if apiErr, ok := err.(*responses.Error); ok { ... }
		debugPrint(debugHTTPResponses, "HTTP RESPONSE ERROR: create response(streaming=true)", err)
		// NOTE: we don't make anything retryable, because the client retries at this stage.
		return Turn{}, sc.LogWrappedErr("open_ai_send_async.stream_init", err)
	}

	builders := &openAIResponsesContentBuilders{
		idToTextBuilder:      make(map[string]*strings.Builder),
		idToReasoningBuilder: make(map[string]*strings.Builder),
		idToTextDone:         make(map[string]bool),
		idToReasoningDone:    make(map[string]bool),
	}
	var finalResp *Turn

	// I've observed that the LLM sometimes gets stuck in an endless loop of sending "response.function_call_arguments.delta"
	// with deltas like "\r",  "\t\t\t\t\t  ", "\t\t ", "\n\n\n\n", and so on. When this happens, it appears to hang.
	// So let's detect this state and retry if we get into it.
	const tooManyBlankFCDelta = 100
	blankFCDeltaCount := 0

	finished := false
	for stream.Next() {
		if finished {
			// Drain any remaining events so that the server can finish persisting the
			// response. Without doing this we risk closing the stream before OpenAI has
			// stored the response, which can race with subsequent requests that rely on
			// previous_response_id.
			debugPrint(debugEvents, "Finished but stream.Next() - continuing", nil)
			continue
		}
		evt := stream.Current()

		debugPrint(debugEvents, fmt.Sprintf("EVENT: %s; elapsed=%v", evt.Type, time.Since(startTime)), nil)
		if evt.Type == "response.output_item.added" {
			debugPrint(debugEvents, "response.output_item.added", debugDescribeOutputItemAdded(evt.AsResponseOutputItemAdded()))
		}

		// Detect broken state in OpenAI (observed on 2025/10/28)
		if evt.Type == "response.function_call_arguments.delta" {
			if strings.TrimSpace(evt.AsResponseFunctionCallArgumentsDelta().Delta) == "" {
				blankFCDeltaCount++
				if blankFCDeltaCount >= tooManyBlankFCDelta {
					return Turn{}, makeRetryable(sc.LogNewErr("got too many consequtive blank lines in response.function_call_arguments.delta - LLM is broken"))
				}
			} else {
				blankFCDeltaCount = 0
			}
		}

		processedEvent, cont, err := openAIResponsesProcessEvent(evt, builders)
		if err != nil {
			// Provider-sent error (ex: failed/incomplete). Not retryable by default.
			return Turn{}, sc.LogWrappedErr("open_ai_send_async.event", err)
		}
		if processedEvent != nil {

			if processedEvent.Type == EventTypeCompletedSuccess {
				debugPrint(debugParsedResponses, "PARSED RESPONSE: EventTypeCompletedSuccess", processedEvent)
				finalResp = processedEvent.Turn
			}
			if !trySendEvent(ctx, toDebouncer, *processedEvent) {
				return Turn{}, sc.LogWrappedErr("open_ai_send_async.context", context.Canceled)
			}
		}
		if !cont {
			debugPrint(debugEvents, "Setting finished=true", nil)
			finished = true
		}
	}

	if err := stream.Err(); err != nil {
		// Only retry on actually retryable errors; unknown (non-HTTP) transport failures are considered retryable.
		// TODO: Look into retries here. I know the client does retries, but I'm unsure what happens once we're in the SSE phase of the request.
		return Turn{}, sc.LogWrappedErr("open_ai_send_async.stream", err)
	}

	// Only produce a message on successful completion
	if finalResp == nil {
		return Turn{}, sc.LogNewErr("open_ai_send_async.not_completed")
	}

	// Record the latest response ID so the next turn can link via PreviousResponseID
	// NOTE: may want to make this a slice?
	if !(opt != nil && opt.NoLink) {
		sc.providerConversationID = finalResp.ProviderID
	}

	resp := *finalResp
	resp.Role = RoleAssistant
	return resp, nil
}

func openAIResponsesApplySendOptions(params *responses.ResponseNewParams, modelInfo llmmodel.ModelInfo, opt *SendOptions) error {
	params.Store = param.NewOpt(true)
	params.Reasoning.Summary = responses.ReasoningSummaryAuto
	if eff := strings.TrimSpace(modelInfo.ReasoningEffort); eff != "" {
		params.Reasoning.Effort = shared.ReasoningEffort(eff)
	}

	// Apply service tier from the model registry as a default. This is important
	// because most callers don't set SendOptions at all, and custom models are
	// expected to carry their overrides via llmmodel.ModelInfo.
	//
	// Precedence:
	//   1) modelInfo.ServiceTier (default)
	//   2) opt.ServiceTier, when non-empty (explicit override)
	//      - "auto" explicitly clears any default to provider behavior.
	serviceTier := strings.TrimSpace(modelInfo.ServiceTier)
	if serviceTier == "auto" {
		serviceTier = ""
	}
	if opt != nil {
		if st := strings.TrimSpace(opt.ServiceTier); st != "" {
			if st == "auto" {
				serviceTier = ""
			} else {
				serviceTier = st
			}
		}
	}
	switch serviceTier {
	case "":
		// No-op: provider defaults to auto if unset.
	case "priority", "flex":
		params.ServiceTier = responses.ResponseNewParamsServiceTier(serviceTier)
	default:
		return fmt.Errorf("invalid service tier %q (must be \"\", \"auto\", \"priority\", or \"flex\")", serviceTier)
	}

	if opt == nil {
		return nil
	}

	if opt.NoStore {
		params.Store = param.NewOpt(false)
	}
	if opt.ReasoningEffort != "" {
		params.Reasoning.Effort = shared.ReasoningEffort(opt.ReasoningEffort)
	}
	if opt.ReasoningSummary != "" {
		params.Reasoning.Summary = shared.ReasoningSummary(opt.ReasoningSummary)
	}
	if opt.TemperaturePresent {
		params.Temperature = param.NewOpt(opt.Temperature)
	}

	return nil
}

type openAIResponsesContentBuilders struct {
	idToTextBuilder      map[string]*strings.Builder
	idToReasoningBuilder map[string]*strings.Builder
	idToTextDone         map[string]bool
	idToReasoningDone    map[string]bool
}

// openAIResponsesProcessEvent processes evt, returning:
//   - An Event to send (but not an error event). This may have a finalized Response.
//   - shouldContinue: false indicates the stream has finished from the provider's perspective. Callers may still choose to drain remaining events
//     from the transport before exiting their loop.
//   - An error. Errors do NOT get built events - we just return the error, and the caller deals with it.
func openAIResponsesProcessEvent(evt responses.ResponseStreamEventUnion, builders *openAIResponsesContentBuilders) (*Event, bool, error) {
	switch evt.Type {
	case "response.queued":
		return &Event{Type: EventTypeQueued}, true, nil
	case "response.created":
		evtCreated := evt.AsResponseCreated()
		return &Event{Type: EventTypeCreated, Turn: openaiResponesBuildResponse(evtCreated.Response)}, true, nil
	case "response.completed":
		evtCompleted := evt.AsResponseCompleted()
		debugPrint(debugHTTPResponses, "HTTP RESPONSE: response.completed", json.RawMessage(evt.RawJSON()))
		return &Event{Type: EventTypeCompletedSuccess, Turn: openaiResponesBuildResponse(evtCompleted.Response)}, false, nil
	case "response.failed":
		evtFailed := evt.AsResponseFailed()
		code := string(evtFailed.Response.Error.Code)
		msg := evtFailed.Response.Error.Message
		if msg == "" {
			msg = "openai response failed"
		}
		return nil, false, fmt.Errorf("%s (code=%s)", msg, code)
	case "response.incomplete":
		evtIncomplete := evt.AsResponseIncomplete()
		resp := evtIncomplete.Response
		reason := resp.IncompleteDetails.Reason
		if reason == "" {
			reason = "incomplete"
		}
		return nil, false, fmt.Errorf("incomplete. reason=%s", reason)
	case "error":
		errEvt := evt.AsError()
		msg := errEvt.Message
		if msg == "" {
			msg = "openai streaming error"
		}
		return nil, false, fmt.Errorf("%s (code=%s)", msg, errEvt.Code)
	case "response.output_text.delta":
		evtDelta := evt.AsResponseOutputTextDelta()
		if evtDelta.Delta != "" {
			itemID := evtDelta.ItemID
			// TODO: warn if id is done (return slice)
			builder := builders.idToTextBuilder[itemID]
			if builder == nil {
				builder = &strings.Builder{}
				builders.idToTextBuilder[itemID] = builder
			}
			builder.WriteString(evtDelta.Delta)
			return &Event{Type: EventTypeTextDelta, Delta: evtDelta.Delta, Text: &TextContent{ProviderID: itemID, Content: builder.String()}, Done: false}, true, nil
		}
	case "response.output_text.done":
		evtDone := evt.AsResponseOutputTextDone()
		if evtDone.Text != "" {
			itemID := evtDone.ItemID
			// TODO: warn if id is done already somehow

			builders.idToTextDone[itemID] = true

			// Compute deltaTxt (we add to idToTextBuilder just for consistency).
			builder := builders.idToTextBuilder[itemID]
			if builder == nil {
				builder = &strings.Builder{}
				builders.idToTextBuilder[itemID] = builder
			}
			deltaTxt := strings.TrimPrefix(evtDone.Text, builder.String())
			return &Event{Type: EventTypeTextDelta, Delta: deltaTxt, Text: &TextContent{ProviderID: itemID, Content: evtDone.Text}, Done: true}, true, nil
		}
	case "response.reasoning_text.done":
		debugPrint(debugHTTPResponses, "HTTP RESPONSE: response.reasoning_text.done", json.RawMessage(evt.RawJSON()))
	case "response.reasoning_summary_text.delta":
		evtDelta := evt.AsResponseReasoningSummaryTextDelta()
		// Very noisy (uncomment if you'd like):
		// printDebugJsonable(debugHTTPResponses, "RESPONSE EVENT RAW: response.reasoning_summary_text.delta", json.RawMessage(evt.RawJSON()))
		if evtDelta.Delta != "" {
			// In OpenAI Responses, a reasoning summary has an ID like "rs_6806bfca0b2481918a5748308061a2600d3ce51bdffd5476" (itemID)
			// and has a content array of multiple texts. SummaryIndex is the index into this array.
			// We map this to our data model, which has multiple ReasoningContent per itemID (one ReasoningContent per content array item).
			// Note that we index into idToReasoningBuilder with subItemID but send a ReasoningDelta event with ID: itemID.
			itemID := evtDelta.ItemID
			subItemID := fmt.Sprintf("%s:%d", itemID, evtDelta.SummaryIndex)
			// TODO: warn if id is done
			builder := builders.idToReasoningBuilder[subItemID]
			if builder == nil {
				builder = &strings.Builder{}
				builders.idToReasoningBuilder[subItemID] = builder
			}
			builder.WriteString(evtDelta.Delta)
			return &Event{Type: EventTypeReasoningDelta, Delta: evtDelta.Delta, Reasoning: &ReasoningContent{ProviderID: itemID, Content: builder.String()}, Done: false}, true, nil
		}
		return nil, true, nil
	case "response.reasoning_summary_part.done": // NOTE: this is PART.done, not TEXT.done
		evtDone := evt.AsResponseReasoningSummaryPartDone()
		debugPrint(debugHTTPResponses, "HTTP RESPONSE: response.reasoning_summary_part.done", json.RawMessage(evt.RawJSON()))
		if evtDone.Part.Text != "" {
			itemID := evtDone.ItemID
			subItemID := fmt.Sprintf("%s:%d", itemID, evtDone.SummaryIndex)
			// TODO: warn if id is done somehow
			builders.idToReasoningDone[subItemID] = true

			// Compute deltaTxt (we add to idToTextBuilder just for consistency).
			builder := builders.idToReasoningBuilder[subItemID]
			if builder == nil {
				builder = &strings.Builder{}
				builders.idToTextBuilder[subItemID] = builder
			}
			deltaTxt := strings.TrimPrefix(evtDone.Part.Text, builder.String())
			return &Event{Type: EventTypeReasoningDelta, Delta: deltaTxt, Reasoning: &ReasoningContent{ProviderID: itemID, Content: evtDone.Part.Text}, Done: true}, true, nil
		}
	case "response.output_item.done":
		evtOutputItem := evt.AsResponseOutputItemDone()
		item := evtOutputItem.Item
		switch item.Type {
		case "function_call":
			fn := item.AsFunctionCall()
			tc := &ToolCall{
				ProviderID: item.ID,
				CallID:     fn.CallID,
				Name:       fn.Name,
				Input:      fn.Arguments,
				Type:       item.Type,
			}
			return &Event{Type: EventTypeToolUse, ToolCall: tc}, true, nil
		case "custom_tool_call":
			custom := item.AsCustomToolCall()
			tc := &ToolCall{
				ProviderID: item.ID,
				CallID:     custom.CallID,
				Name:       custom.Name,
				Input:      custom.Input,
				Type:       item.Type,
			}
			return &Event{Type: EventTypeToolUse, ToolCall: tc}, true, nil
		}
	case "response.function_call_arguments.delta":
		evtFCDelta := evt.AsResponseFunctionCallArgumentsDelta()
		debugPrint(debugEvents, "response.function_call_arguments.delta", evtFCDelta)
	}

	// Unknown or unhandled event types should not break the stream; continue
	return nil, true, nil
}

func (sc *streamingConversation) buildOpenAIResponsesParams(modelInfo llmmodel.ModelInfo) (responses.ResponseNewParams, error) {
	modelID := modelInfo.ProviderModelID
	if modelID == "" {
		return responses.ResponseNewParams{}, fmt.Errorf("model %q missing provider model id", string(sc.modelID))
	}
	// If we are linking to a previous response, only send responses AFTER the last assistant response.
	// The previous_response_id will provide the assistant content to the provider.
	resps := sc.turns
	respsToEncode := resps
	if sc.providerConversationID != "" {
		lastAssistantIdx := -1
		for i := len(resps) - 1; i >= 0; i-- {
			if resps[i].Role == RoleAssistant {
				lastAssistantIdx = i
				break
			}
		}
		if lastAssistantIdx >= 0 && lastAssistantIdx+1 < len(resps) {
			respsToEncode = resps[lastAssistantIdx+1:]
		}
	}

	inputItems := make(responses.ResponseInputParam, 0, len(respsToEncode))
	for _, resp := range respsToEncode {

		// Collect all text parts. A text part maps to a message, for a single msg on our side, we only want to make one message on their side.
		// I have no idea these parts exist in practice, but in theory they could be interleaved in msg.Parts. So the first part we see, we write a message with all text parts.
		// Note that we want to insert this message in the order it goes based on msg.parts, so we can't just dump in the message first.
		var allTextParts []TextContent
		for _, part := range resp.Parts {
			switch tpart := part.(type) {
			case TextContent:
				allTextParts = append(allTextParts, tpart)
			}
		}

		// We need to group reasoning parts by ID, b/c that's Responses data model.
		idToReasoningParts := map[string][]ReasoningContent{}
		for _, part := range resp.Parts {
			switch tpart := part.(type) {
			case ReasoningContent:
				idToReasoningParts[tpart.ProviderID] = append(idToReasoningParts[tpart.ProviderID], tpart)
			}
		}
		idToAddedResponse := map[string]bool{} // keep track if we added the ID ("rs_blahblah") yet

		for _, part := range resp.Parts {
			switch tpart := part.(type) {
			case TextContent:
				contentList := make(responses.ResponseInputMessageContentListParam, 0, len(allTextParts))
				for _, tp := range allTextParts {
					paramUnion := responses.ResponseInputContentParamOfInputText(tp.Content)
					if textParam := paramUnion.OfInputText; textParam != nil {
						if resp.Role == RoleAssistant {
							textParam.Type = "output_text"
						} else {
							textParam.Type = "input_text"
						}
					}
					contentList = append(contentList, paramUnion)
				}
				message := responses.EasyInputMessageParam{
					Role:    openaiResponesMapMessageRole(resp.Role),
					Type:    "message",
					Content: responses.EasyInputMessageContentUnionParam{OfInputItemContentList: contentList},
				}
				inputItems = append(inputItems, responses.ResponseInputItemUnionParam{OfMessage: &message})
			case ToolCall:
				switch tpart.Type {
				case "function_call":
					var functionCall responses.ResponseFunctionToolCallParam
					functionCall.Arguments = tpart.Input
					functionCall.CallID = tpart.CallID
					functionCall.Name = tpart.Name
					if tpart.ProviderID != "" {
						functionCall.ID = param.NewOpt(tpart.ProviderID)
					}
					inputItems = append(inputItems, responses.ResponseInputItemUnionParam{OfFunctionCall: &functionCall})
				case "custom_tool_call":
					var customToolCall responses.ResponseCustomToolCallParam
					customToolCall.Input = tpart.Input
					customToolCall.CallID = tpart.CallID
					customToolCall.Name = tpart.Name
					if tpart.ProviderID != "" {
						customToolCall.ID = param.NewOpt(tpart.ProviderID)
					}
					inputItems = append(inputItems, responses.ResponseInputItemUnionParam{OfCustomToolCall: &customToolCall})
				default:
					return responses.ResponseNewParams{}, fmt.Errorf("unknown tool call type: %s", tpart.Type)
				}
			case ToolResult:
				// Convert ToolResult into function_call_output or custom_tool_call_output
				if tpart.CallID == "" {
					return responses.ResponseNewParams{}, errors.New("tool result is missing tool_call_id")
				}
				switch tpart.Type {
				case "function_call":
					outUnion := responses.ResponseInputItemFunctionCallOutputOutputUnionParam{OfString: param.NewOpt(tpart.Result)}
					item := responses.ResponseInputItemFunctionCallOutputParam{CallID: tpart.CallID, Output: outUnion}
					inputItems = append(inputItems, responses.ResponseInputItemUnionParam{OfFunctionCallOutput: &item})
				case "custom_tool_call":
					outUnion := responses.ResponseCustomToolCallOutputOutputUnionParam{OfString: param.NewOpt(tpart.Result)}
					item := responses.ResponseCustomToolCallOutputParam{CallID: tpart.CallID, Type: "custom_tool_call_output", Output: outUnion}
					inputItems = append(inputItems, responses.ResponseInputItemUnionParam{OfCustomToolCallOutput: &item})
				default:
					return responses.ResponseNewParams{}, fmt.Errorf("unknown or missing call type for tool result %s", tpart.CallID)
				}
			case ReasoningContent:
				if !idToAddedResponse[tpart.ProviderID] {
					idToAddedResponse[tpart.ProviderID] = true
					summaryList := []responses.ResponseReasoningItemSummaryParam{}
					for _, rp := range idToReasoningParts[tpart.ProviderID] {
						summaryList = append(summaryList, responses.ResponseReasoningItemSummaryParam{Text: rp.Content})
					}
					item := responses.ResponseInputItemParamOfReasoning(tpart.ProviderID, summaryList)
					inputItems = append(inputItems, item)
				}
			default:
				panic(fmt.Errorf("unknown part type: %v", part))
			}
		}
	}

	req := responses.ResponseNewParams{
		Model: modelID,
		Input: responses.ResponseNewParamsInputUnion{OfInputItemList: inputItems},
	}
	if sc.promptCacheKey != "" {
		req.PromptCacheKey = param.NewOpt(sc.promptCacheKey)
	}

	// Include tools if configured
	if len(sc.tools) > 0 {
		toolParams, err := openaiResponesBuildToolParams(sc.tools)
		if err != nil {
			return responses.ResponseNewParams{}, err
		}
		if len(toolParams) > 0 {
			req.Tools = toolParams
		}
	}
	return req, nil
}

func openaiResponesMapMessageRole(role Role) responses.EasyInputMessageRole {
	switch role {
	case RoleSystem:
		return responses.EasyInputMessageRoleSystem
	case RoleUser:
		return responses.EasyInputMessageRoleUser
	case RoleAssistant:
		return responses.EasyInputMessageRoleAssistant
	default:
		panic(fmt.Sprintf("unknown role: %v", role))
	}
}

// openaiResponesBuildResponse maps an OpenAI responses.Response into our Response type.
func openaiResponesBuildResponse(resp responses.Response) *Turn {

	parts := make([]ContentPart, 0, len(resp.Output))
	hasToolCalls := false
	for _, item := range resp.Output {
		switch item.Type {
		case "function_call":
			fn := item.AsFunctionCall()
			tc := ToolCall{ProviderID: item.ID, CallID: fn.CallID, Name: fn.Name, Input: fn.Arguments, Type: "function_call"}
			parts = append(parts, tc)
			hasToolCalls = true
		case "custom_tool_call":
			custom := item.AsCustomToolCall()
			tc := ToolCall{ProviderID: item.ID, CallID: custom.CallID, Name: custom.Name, Input: custom.Input, Type: "custom_tool_call"}
			parts = append(parts, tc)
			hasToolCalls = true
		case "message":
			message := item.AsMessage()
			for _, messageContent := range message.Content {
				switch messageContent.Type {
				case "output_text":
					mcOutput := messageContent.AsOutputText()
					parts = append(parts, TextContent{Content: mcOutput.Text, ProviderID: message.ID})
				}
			}
		case "reasoning":
			reasoning := item.AsReasoning()
			for _, summaryItem := range reasoning.Summary {
				parts = append(parts, ReasoningContent{Content: summaryItem.Text, ProviderID: reasoning.ID})
			}
		}
	}

	return &Turn{
		ProviderID:   resp.ID,
		Parts:        parts,
		Usage:        openaiResponesConvertUsage(resp.Usage),
		FinishReason: openaiResponesMapFinishReason(resp, hasToolCalls),
	}
}

func openaiResponesBuildToolParams(tools []Tool) ([]responses.ToolUnionParam, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	result := make([]responses.ToolUnionParam, 0, len(tools))
	for _, tool := range tools {
		info := tool.Info()
		if info.Name == "" {
			return nil, errors.New("tool name is required")
		}

		kind := info.Kind
		if kind == "" {
			kind = ToolKindFunction
		}

		var (
			union responses.ToolUnionParam
			err   error
		)

		switch kind {
		case ToolKindFunction:
			union, err = buildOpenAIFunctionToolParam(info)
		case ToolKindCustom:
			union, err = buildOpenAICustomToolParam(info)
		default:
			err = fmt.Errorf("unsupported tool kind: %s", kind)
		}
		if err != nil {
			return nil, err
		}
		result = append(result, union)
	}
	return result, nil
}

func buildOpenAIFunctionToolParam(info ToolInfo) (responses.ToolUnionParam, error) {
	// Build a JSON schema with parameters as properties. The provided Parameters map
	// contains only parameter definitions, not a full schema.
	schema := make(map[string]any)
	schema["type"] = "object"
	schema["additionalProperties"] = false
	// Always include properties, even if empty, to satisfy OpenAI validation.
	props := make(map[string]any, len(info.Parameters))
	if len(info.Parameters) > 0 {

		// Create a fast lookup set for required keys
		requiredSet := make(map[string]struct{}, len(info.Required))
		for _, r := range info.Required {
			requiredSet[r] = struct{}{}
		}

		for paramName, rawProp := range info.Parameters {
			// Copy the property definition so we don't mutate the tool's own schema
			switch prop := rawProp.(type) {
			case map[string]any:
				copied := make(map[string]any, len(prop))
				for k, v := range prop {
					copied[k] = v
				}

				// If parameter is optional (not in Required), automatically add null to its type
				if _, isRequired := requiredSet[paramName]; !isRequired {
					if t, ok := copied["type"]; ok {
						switch tv := t.(type) {
						case string:
							if tv != "null" {
								copied["type"] = []any{tv, "null"}
							}
						case []any:
							// ensure "null" is present
							hasNull := false
							for _, x := range tv {
								if s, ok := x.(string); ok && s == "null" {
									hasNull = true
									break
								}
							}
							if !hasNull {
								copied["type"] = append(tv, "null")
							}
						case []string:
							hasNull := false
							for _, s := range tv {
								if s == "null" {
									hasNull = true
									break
								}
							}
							if !hasNull {
								// convert to []any to mix types
								newTypes := make([]any, 0, len(tv)+1)
								for _, s := range tv {
									newTypes = append(newTypes, s)
								}
								newTypes = append(newTypes, "null")
								copied["type"] = newTypes
							}
						}
					}
				}

				props[paramName] = copied
			default:
				// If it's not an object, pass-through as-is
				props[paramName] = rawProp
			}
		}
	}
	// Always set properties (could be empty).
	schema["properties"] = props
	if len(info.Parameters) > 0 {
		// OpenAI Responses strict=true requires 'required' to include every key in properties.
		// Optional (not required) parameters are made nullable above via type: [<type>, "null"].
		required := make([]string, 0, len(info.Parameters))
		for k := range info.Parameters {
			required = append(required, k)
		}
		sort.Strings(required)
		schema["required"] = required
	}
	function := responses.FunctionToolParam{
		Name:       info.Name,
		Parameters: schema,
		Strict:     param.NewOpt(true),
		Type:       "function",
	}
	if info.Description != "" {
		function.Description = param.NewOpt(info.Description)
	}
	return responses.ToolUnionParam{OfFunction: &function}, nil
}

func buildOpenAICustomToolParam(info ToolInfo) (responses.ToolUnionParam, error) {
	custom := responses.CustomToolParam{
		Name: info.Name,
		Type: "custom",
	}
	if info.Description != "" {
		custom.Description = param.NewOpt(info.Description)
	}

	if info.Grammar != nil {
		definition := strings.TrimSpace(info.Grammar.Definition)
		if definition == "" {
			return responses.ToolUnionParam{}, fmt.Errorf("tool %q: grammar definition is required", info.Name)
		}
		syntax := strings.TrimSpace(strings.ToLower(string(info.Grammar.Syntax)))
		if syntax != string(ToolGrammarSyntaxLark) && syntax != string(ToolGrammarSyntaxRegex) {
			return responses.ToolUnionParam{}, fmt.Errorf("tool %q: unsupported grammar syntax %q", info.Name, info.Grammar.Syntax)
		}

		grammarParam := shared.CustomToolInputFormatGrammarParam{
			Definition: definition,
			Syntax:     syntax,
			Type:       "grammar",
		}
		custom.Format = shared.CustomToolInputFormatUnionParam{OfGrammar: &grammarParam}
	}

	return responses.ToolUnionParam{OfCustom: &custom}, nil
}

func openaiResponesConvertUsage(usage responses.ResponseUsage) TokenUsage {
	return TokenUsage{
		TotalInputTokens:  usage.InputTokens,
		CachedInputTokens: usage.InputTokensDetails.CachedTokens,
		ReasoningTokens:   usage.OutputTokensDetails.ReasoningTokens,
		TotalOutputTokens: usage.OutputTokens,
	}
}

func openaiResponesMapFinishReason(resp responses.Response, hasToolCalls bool) FinishReason {
	switch resp.Status {
	case "completed":
		if hasToolCalls {
			return FinishReasonToolUse
		}
		return FinishReasonEndTurn
	case "incomplete":
		reason := strings.ToLower(resp.IncompleteDetails.Reason)
		switch reason {
		case "max_output_tokens", "max_tokens":
			return FinishReasonMaxTokens
		case "content_filter":
			return FinishReasonPermissionDenied
		default:
			return FinishReasonUnknown
		}
	case "cancelled":
		return FinishReasonCanceled
	case "failed":
		return FinishReasonError
	case "in_progress":
		return FinishReasonInProgress
	case "queued":
		return FinishReasonInProgress
	default:
		return FinishReasonUnknown
	}
}
