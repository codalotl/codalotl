package llmstream

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/llmmodel"

	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponesBuildResponse_SetsRoleAssistant(t *testing.T) {
	turn := openaiResponesBuildResponse(responses.Response{
		ID:     "resp_123",
		Status: "completed",
	})
	require.NotNil(t, turn)
	assert.Equal(t, RoleAssistant, turn.Role)
}

func TestOpenAIResponesBuildResponse_CapturesEncryptedReasoningState(t *testing.T) {
	var resp responses.Response
	require.NoError(t, json.Unmarshal([]byte(`{
		"id": "resp_123",
		"status": "completed",
		"output": [
			{
				"id": "rs_123",
				"type": "reasoning",
				"summary": [
					{"type": "summary_text", "text": "reasoning summary"}
				],
				"encrypted_content": "encrypted_reasoning_blob"
			},
			{
				"id": "msg_123",
				"type": "message",
				"role": "assistant",
				"status": "completed",
				"content": [
					{"type": "output_text", "text": "answer"}
				]
			}
		]
	}`), &resp))

	turn := openaiResponesBuildResponse(resp)

	require.NotNil(t, turn)
	assert.Contains(t, turn.Parts, ReasoningContent{ProviderID: "rs_123", Content: "reasoning summary"})
	assert.Contains(t, turn.Parts, ReasoningContent{ProviderID: "rs_123", ProviderState: "encrypted_reasoning_blob"})
	assert.Contains(t, turn.Parts, TextContent{ProviderID: "msg_123", Content: "answer"})
}

func TestOpenAIResponsesProcessEvent_CompletedFallsBackToDoneOutputItems(t *testing.T) {
	builders := newOpenAIResponsesContentBuildersForTest()
	itemDone := mustOpenAIResponseStreamEvent(t, `{
		"type": "response.output_item.done",
		"output_index": 0,
		"sequence_number": 1,
		"item": {
			"id": "fc_123",
			"type": "function_call",
			"status": "completed",
			"call_id": "call_123",
			"name": "update_plan",
			"arguments": "{\"plan\":[]}"
		}
	}`)

	ev, cont, err := openAIResponsesProcessEvent(itemDone, builders)
	require.NoError(t, err)
	assert.True(t, cont)
	require.NotNil(t, ev)
	assert.Equal(t, EventTypeToolUse, ev.Type)
	require.NotNil(t, ev.ToolCall)
	assert.Equal(t, "call_123", ev.ToolCall.CallID)

	completed := mustOpenAIResponseStreamEvent(t, `{
		"type": "response.completed",
		"sequence_number": 2,
		"response": {
			"id": "resp_123",
			"object": "response",
			"created_at": 1779486596,
			"status": "completed",
			"output": [],
			"usage": {
				"input_tokens": 10,
				"input_tokens_details": {"cached_tokens": 0},
				"output_tokens": 5,
				"output_tokens_details": {"reasoning_tokens": 0},
				"total_tokens": 15
			}
		}
	}`)

	ev, cont, err = openAIResponsesProcessEvent(completed, builders)
	require.NoError(t, err)
	assert.False(t, cont)
	require.NotNil(t, ev)
	require.NotNil(t, ev.Turn)
	assert.Equal(t, FinishReasonToolUse, ev.Turn.FinishReason)
	assert.Equal(t, []ToolCall{{
		ProviderID: "fc_123",
		CallID:     "call_123",
		Name:       "update_plan",
		Input:      `{"plan":[]}`,
		Type:       "function_call",
	}}, ev.Turn.ToolCalls())
}

func TestOpenAIResponesConvertUsage_TotalOutputIncludesReasoning(t *testing.T) {
	usage := responses.ResponseUsage{
		InputTokens: 12,
		InputTokensDetails: responses.ResponseUsageInputTokensDetails{
			CachedTokens: 3,
		},
		OutputTokens: 40,
		OutputTokensDetails: responses.ResponseUsageOutputTokensDetails{
			ReasoningTokens: 7,
		},
	}

	got := openaiResponesConvertUsage(usage)

	assert.EqualValues(t, 12, got.TotalInputTokens)
	assert.EqualValues(t, 3, got.CachedInputTokens)
	assert.EqualValues(t, 7, got.ReasoningTokens)
	assert.EqualValues(t, 40, got.TotalOutputTokens)
	assert.GreaterOrEqual(t, got.TotalOutputTokens, got.ReasoningTokens)
}

func TestBuildOpenAIResponsesRequestParams_DefaultLinksAndTrimsHistory(t *testing.T) {
	sc := openAIRequestShapeConversation(t)

	params, err := sc.buildOpenAIResponsesRequestParams(openAIRequestShapeModelInfo(), nil)
	require.NoError(t, err)

	req, reqJSON := mustMarshalOpenAIResponsesRequest(t, params)
	assert.Equal(t, true, req["store"])
	assert.Equal(t, true, req["parallel_tool_calls"])
	assert.Equal(t, "resp_first", req["previous_response_id"])

	input := openAIResponsesRequestInput(t, req)
	require.Len(t, input, 1)
	assert.Contains(t, reqJSON, "second question")
	assert.NotContains(t, reqJSON, "system instructions")
	assert.NotContains(t, reqJSON, "first question")
	assert.NotContains(t, reqJSON, "first answer")
}

func TestBuildOpenAIResponsesRequestParams_NoStoreDisablesLinkAndSendsFullHistory(t *testing.T) {
	sc := openAIRequestShapeConversation(t)

	params, err := sc.buildOpenAIResponsesRequestParams(openAIRequestShapeModelInfo(), &SendOptions{NoStore: true})
	require.NoError(t, err)

	req, reqJSON := mustMarshalOpenAIResponsesRequest(t, params)
	assert.Equal(t, false, req["store"])
	assert.Equal(t, true, req["parallel_tool_calls"])
	assert.NotContains(t, req, "previous_response_id")
	assert.NotContains(t, req, "include")

	input := openAIResponsesRequestInput(t, req)
	require.Len(t, input, 4)
	assert.Contains(t, reqJSON, "system instructions")
	assert.Contains(t, reqJSON, "first question")
	assert.Contains(t, reqJSON, "first answer")
	assert.Contains(t, reqJSON, "second question")
}

func TestBuildOpenAIResponsesRequestParams_OpenAISubscriptionForcesNoStoreAndRootInstructions(t *testing.T) {
	llmmodel.SetProviderSubscription(llmmodel.ProviderIDOpenAI, llmmodel.ProviderSubscription{
		AccessToken:      "access-token",
		AccountID:        "account-id",
		APIEndpointURL:   "https://chatgpt.com/backend-api/codex",
		RequiresNoStore:  true,
		RootInstructions: true,
	})
	t.Cleanup(func() { llmmodel.ClearProviderSubscription(llmmodel.ProviderIDOpenAI) })

	sc := openAIRequestShapeConversation(t)

	params, err := sc.buildOpenAIResponsesRequestParams(openAIRequestShapeModelInfo(), nil)
	require.NoError(t, err)

	req, reqJSON := mustMarshalOpenAIResponsesRequest(t, params)
	assert.Equal(t, false, req["store"])
	assert.Equal(t, "system instructions", req["instructions"])
	assert.NotContains(t, req, "previous_response_id")

	input := openAIResponsesRequestInput(t, req)
	require.Len(t, input, 3)
	assert.Contains(t, reqJSON, "first question")
	assert.Contains(t, reqJSON, "first answer")
	assert.Contains(t, reqJSON, "second question")
	assert.NotContains(t, reqJSON, `"role":"system"`)
}

func TestOpenAIResponsesSubscriptionUsesOpenAISubscriptionForDefaultModelAuth(t *testing.T) {
	llmmodel.SetProviderSubscription(llmmodel.ProviderIDOpenAI, llmmodel.ProviderSubscription{
		AccessToken:    "access-token",
		APIEndpointURL: "https://chatgpt.com/backend-api/codex",
	})
	t.Cleanup(func() { llmmodel.ClearProviderSubscription(llmmodel.ProviderIDOpenAI) })

	sub, ok := openAIResponsesSubscription(openAIRequestShapeModelInfo())

	require.True(t, ok)
	assert.Equal(t, "access-token", sub.AccessToken)
	assert.Equal(t, "https://chatgpt.com/backend-api/codex", sub.APIEndpointURL)
}

func TestOpenAIResponsesSubscriptionSkipsExplicitModelAuth(t *testing.T) {
	llmmodel.SetProviderSubscription(llmmodel.ProviderIDOpenAI, llmmodel.ProviderSubscription{
		AccessToken:    "access-token",
		APIEndpointURL: "https://chatgpt.com/backend-api/codex",
	})
	t.Cleanup(func() { llmmodel.ClearProviderSubscription(llmmodel.ProviderIDOpenAI) })

	for _, tc := range []struct {
		name      string
		overrides llmmodel.ModelOverrides
	}{
		{
			name:      "actual_key",
			overrides: llmmodel.ModelOverrides{APIActualKey: "custom-key"},
		},
		{
			name:      "env_key",
			overrides: llmmodel.ModelOverrides{APIEnvKey: "CUSTOM_OPENAI_KEY"},
		},
		{
			name:      "endpoint",
			overrides: llmmodel.ModelOverrides{APIEndpointURL: "https://example.test/v1"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			modelInfo := openAIRequestShapeModelInfo()
			modelInfo.ModelOverrides = tc.overrides

			sub, ok := openAIResponsesSubscription(modelInfo)

			assert.False(t, ok)
			assert.Empty(t, sub)
		})
	}
}

func TestBuildOpenAIResponsesRequestParams_NoStoreReplaysEncryptedReasoningWithoutProviderIDs(t *testing.T) {
	sc := NewConversation(llmmodel.ModelID("gpt-4o-mini"), "system instructions").(*streamingConversation)
	require.NoError(t, sc.AddUserTurn("first question"))
	sc.turns = append(sc.turns, Turn{
		Role:       RoleAssistant,
		ProviderID: "resp_unstored",
		Parts: []ContentPart{
			ReasoningContent{ProviderID: "rs_unstored", Content: "private reasoning summary", ProviderState: "encrypted_reasoning_blob"},
			TextContent{ProviderID: "msg_unstored", Content: "first answer"},
		},
	})
	require.NoError(t, sc.AddUserTurn("second question"))
	sc.providerConversationID = "resp_unstored"

	params, err := sc.buildOpenAIResponsesRequestParams(openAIReasoningRequestShapeModelInfo(), &SendOptions{NoStore: true})
	require.NoError(t, err)

	req, reqJSON := mustMarshalOpenAIResponsesRequest(t, params)
	assert.Equal(t, false, req["store"])
	assert.NotContains(t, req, "previous_response_id")
	assertOpenAIRequestIncludesEncryptedReasoning(t, req)

	reasoningItems := openAIResponsesRequestReasoningInputItems(t, req)
	require.Len(t, reasoningItems, 1)
	assert.Equal(t, "reasoning", reasoningItems[0]["type"])
	assert.Equal(t, "encrypted_reasoning_blob", reasoningItems[0]["encrypted_content"])
	assert.NotContains(t, reasoningItems[0], "id")
	assert.Empty(t, reasoningItems[0]["summary"])
	assert.NotContains(t, reasoningItems[0], "content")

	assert.Contains(t, reqJSON, "system instructions")
	assert.Contains(t, reqJSON, "first question")
	assert.Contains(t, reqJSON, "first answer")
	assert.Contains(t, reqJSON, "second question")
	assert.NotContains(t, reqJSON, "resp_unstored")
	assert.NotContains(t, reqJSON, "rs_unstored")
	assert.NotContains(t, reqJSON, "private reasoning summary")
	assert.NotContains(t, reqJSON, "msg_unstored")
}

func TestOpenAIResponsesRequestPathIncludesBaseURLPath(t *testing.T) {
	assert.Equal(t, "/backend-api/codex/responses", openAIResponsesRequestPath("https://chatgpt.com/backend-api/codex"))
	assert.Equal(t, "/v1/responses", openAIResponsesRequestPath("https://api.openai.com/v1/"))
	assert.Equal(t, "/responses", openAIResponsesRequestPath(""))
}

func TestBuildOpenAIResponsesRequestParams_NoStoreOmitsPersistedProviderItemIDs(t *testing.T) {
	sc := openAIProviderItemReplayConversation(t)

	params, err := sc.buildOpenAIResponsesRequestParams(openAIRequestShapeModelInfo(), &SendOptions{NoStore: true})
	require.NoError(t, err)

	req, reqJSON := mustMarshalOpenAIResponsesRequest(t, params)
	assert.Equal(t, false, req["store"])
	assert.NotContains(t, req, "previous_response_id")

	input := openAIResponsesRequestInput(t, req)
	require.NotEmpty(t, input)
	assert.Contains(t, reqJSON, "system instructions")
	assert.Contains(t, reqJSON, "first question")
	assert.Contains(t, reqJSON, "assistant value answer")
	assert.Contains(t, reqJSON, "lookup_weather")
	assert.Contains(t, reqJSON, "structured_answer")
	assert.Contains(t, reqJSON, "call_function_value")
	assert.Contains(t, reqJSON, "call_custom_value")
	assert.Contains(t, reqJSON, "Paris")
	assert.Contains(t, reqJSON, "answer:7")
	assert.Contains(t, reqJSON, "72 F")
	assert.Contains(t, reqJSON, "acknowledged 7")
	assert.NotContains(t, reqJSON, "resp_first")
	assert.NotContains(t, reqJSON, "rs_persisted")
	assert.NotContains(t, reqJSON, "private reasoning summary")
	assert.NotContains(t, reqJSON, "msg_persisted")
	assert.NotContains(t, reqJSON, "fc_persisted")
	assert.NotContains(t, reqJSON, "ct_persisted")
	assert.NotContains(t, reqJSON, `"type":"reasoning"`)
}

func TestBuildOpenAIResponsesRequestParams_DefaultFullHistoryKeepsPersistedProviderItemIDs(t *testing.T) {
	sc := openAIProviderItemReplayConversation(t)
	sc.providerConversationID = ""

	params, err := sc.buildOpenAIResponsesRequestParams(openAIRequestShapeModelInfo(), nil)
	require.NoError(t, err)

	req, reqJSON := mustMarshalOpenAIResponsesRequest(t, params)
	assert.Equal(t, true, req["store"])
	assert.NotContains(t, req, "previous_response_id")
	assert.Contains(t, reqJSON, "rs_persisted")
	assert.Contains(t, reqJSON, "private reasoning summary")
	assert.Contains(t, reqJSON, "fc_persisted")
	assert.Contains(t, reqJSON, "ct_persisted")
}

func TestOpenAIResponsesPrepareCompletedSuccessEvent_NoStoreScrubsEventTurn(t *testing.T) {
	functionCall := ToolCall{
		ProviderID: "fc_unstored",
		CallID:     "call_function_value",
		Name:       "lookup_weather",
		Type:       "function_call",
		Input:      `{"city":"Paris"}`,
	}
	customCall := ToolCall{
		ProviderID: "ct_unstored",
		CallID:     "call_custom_value",
		Name:       "structured_answer",
		Type:       "custom_tool_call",
		Input:      "answer:7",
	}
	usage := TokenUsage{
		TotalInputTokens:  11,
		ReasoningTokens:   2,
		TotalOutputTokens: 7,
	}
	originalTurn := Turn{
		Role:       RoleUser,
		ProviderID: "resp_unstored",
		Parts: []ContentPart{
			ReasoningContent{ProviderID: "rs_unstored", Content: "private reasoning summary", ProviderState: "encrypted_reasoning_blob"},
			TextContent{ProviderID: "msg_unstored", Content: "assistant value answer"},
			functionCall,
			customCall,
		},
		Usage:        usage,
		FinishReason: FinishReasonToolUse,
	}
	event := Event{Type: EventTypeCompletedSuccess, Turn: &originalTurn}

	prepared := openAIResponsesPrepareCompletedSuccessEvent(event, &SendOptions{NoStore: true})

	require.NotNil(t, prepared.Turn)
	assertOpenAINoStoreTurnScrubbed(t, prepared.Turn)
	assert.Equal(t, RoleAssistant, prepared.Turn.Role)
	assert.Equal(t, usage, prepared.Turn.Usage)
	assert.Equal(t, FinishReasonToolUse, prepared.Turn.FinishReason)
	assert.Equal(t, "assistant value answer", prepared.Turn.TextContent())
	assert.Equal(t, []ToolCall{
		{CallID: "call_function_value", Name: "lookup_weather", Type: "function_call", Input: `{"city":"Paris"}`},
		{CallID: "call_custom_value", Name: "structured_answer", Type: "custom_tool_call", Input: "answer:7"},
	}, prepared.Turn.ToolCalls())
	assert.Equal(t, []string{"encrypted_reasoning_blob"}, openAIReasoningProviderStates(prepared.Turn.Parts))

	assert.Equal(t, "resp_unstored", originalTurn.ProviderID)
	require.Len(t, originalTurn.Parts, 4)
}

func TestOpenAIResponsesPrepareCompletedSuccessEvent_OnlyScrubsNoStoreCompletedEvents(t *testing.T) {
	completedTurn := Turn{
		Role:       RoleAssistant,
		ProviderID: "resp_stored",
		Parts: []ContentPart{
			ReasoningContent{ProviderID: "rs_stored", Content: "reasoning summary"},
			TextContent{ProviderID: "msg_stored", Content: "assistant answer"},
			ToolCall{ProviderID: "fc_stored", CallID: "call_value", Name: "lookup_weather", Type: "function_call", Input: `{"city":"Paris"}`},
		},
	}

	stored := openAIResponsesPrepareCompletedSuccessEvent(Event{Type: EventTypeCompletedSuccess, Turn: &completedTurn}, nil)
	require.NotNil(t, stored.Turn)
	assert.Equal(t, "resp_stored", stored.Turn.ProviderID)
	assert.Len(t, stored.Turn.Parts, 3)

	toolUseEvent := Event{
		Type:     EventTypeToolUse,
		ToolCall: &ToolCall{ProviderID: "fc_streaming", CallID: "call_streaming", Name: "lookup_weather", Type: "function_call", Input: `{"city":"Paris"}`},
	}

	notCompleted := openAIResponsesPrepareCompletedSuccessEvent(toolUseEvent, &SendOptions{NoStore: true})
	require.NotNil(t, notCompleted.ToolCall)
	assert.Equal(t, "fc_streaming", notCompleted.ToolCall.ProviderID)
}

func TestBuildOpenAIResponsesRequestParams_StoredReplayAfterNoStoreScrubOmitsNoStoreProviderIDs(t *testing.T) {
	sc := NewConversation(llmmodel.ModelID("gpt-4o-mini"), "system instructions").(*streamingConversation)
	require.NoError(t, sc.AddUserTurn("first question"))

	functionCall := ToolCall{
		ProviderID: "fc_unstored",
		CallID:     "call_function_value",
		Name:       "lookup_weather",
		Type:       "function_call",
		Input:      `{"city":"Paris"}`,
	}
	customCall := ToolCall{
		ProviderID: "ct_unstored",
		CallID:     "call_custom_value",
		Name:       "structured_answer",
		Type:       "custom_tool_call",
		Input:      "answer:7",
	}
	completedEventTurn := Turn{
		Role:       RoleAssistant,
		ProviderID: "resp_unstored",
		Parts: []ContentPart{
			ReasoningContent{ProviderID: "rs_unstored", Content: "private reasoning summary"},
			TextContent{ProviderID: "msg_unstored", Content: "assistant value answer"},
			functionCall,
			customCall,
		},
		Usage: TokenUsage{
			TotalInputTokens:  11,
			ReasoningTokens:   2,
			TotalOutputTokens: 7,
		},
		FinishReason: FinishReasonToolUse,
	}

	retainedTurn := openAIResponsesScrubNoStoreTurn(completedEventTurn)
	sc.turns = append(sc.turns, retainedTurn)
	for _, call := range retainedTurn.ToolCalls() {
		sc.toolCalls[call.CallID] = toolCallResult{call: call}
	}

	assert.Equal(t, "resp_unstored", completedEventTurn.ProviderID)
	assert.Empty(t, retainedTurn.ProviderID)
	assert.Equal(t, completedEventTurn.Usage, retainedTurn.Usage)
	assert.Equal(t, completedEventTurn.FinishReason, retainedTurn.FinishReason)
	assert.Equal(t, "assistant value answer", retainedTurn.TextContent())
	require.Len(t, retainedTurn.Parts, 3)
	for _, part := range retainedTurn.Parts {
		switch part := part.(type) {
		case TextContent:
			assert.Empty(t, part.ProviderID)
		case ToolCall:
			assert.Empty(t, part.ProviderID)
			assert.NotEmpty(t, part.CallID)
			assert.NotEmpty(t, part.Name)
			assert.NotEmpty(t, part.Type)
			assert.NotEmpty(t, part.Input)
		case ReasoningContent:
			t.Fatalf("reasoning content should not be retained")
		}
	}

	functionResult := ToolResult{
		CallID: functionCall.CallID,
		Name:   functionCall.Name,
		Type:   functionCall.Type,
		Result: "72 F",
	}
	customResult := ToolResult{
		CallID: customCall.CallID,
		Name:   customCall.Name,
		Type:   customCall.Type,
		Result: "acknowledged 7",
	}
	require.NoError(t, sc.AddToolResults([]ToolResult{functionResult, customResult}))

	params, err := sc.buildOpenAIResponsesRequestParams(openAIRequestShapeModelInfo(), nil)
	require.NoError(t, err)

	req, reqJSON := mustMarshalOpenAIResponsesRequest(t, params)
	assert.Equal(t, true, req["store"])
	assert.NotContains(t, req, "previous_response_id")
	assert.Contains(t, reqJSON, "system instructions")
	assert.Contains(t, reqJSON, "first question")
	assert.Contains(t, reqJSON, "assistant value answer")
	assert.Contains(t, reqJSON, "lookup_weather")
	assert.Contains(t, reqJSON, "structured_answer")
	assert.Contains(t, reqJSON, "call_function_value")
	assert.Contains(t, reqJSON, "call_custom_value")
	assert.Contains(t, reqJSON, "Paris")
	assert.Contains(t, reqJSON, "answer:7")
	assert.Contains(t, reqJSON, "72 F")
	assert.Contains(t, reqJSON, "acknowledged 7")
	assert.NotContains(t, reqJSON, "resp_unstored")
	assert.NotContains(t, reqJSON, "rs_unstored")
	assert.NotContains(t, reqJSON, "private reasoning summary")
	assert.NotContains(t, reqJSON, "msg_unstored")
	assert.NotContains(t, reqJSON, "fc_unstored")
	assert.NotContains(t, reqJSON, "ct_unstored")
	assert.NotContains(t, reqJSON, `"type":"reasoning"`)
}

func TestRecordOpenAIResponseLink_NoStoreClearsRetainedLink(t *testing.T) {
	sc := openAIRequestShapeConversation(t)
	require.NotEmpty(t, sc.providerConversationID)

	sc.recordOpenAIResponseLink(Turn{ProviderID: "resp_no_store"}, &SendOptions{NoStore: true})

	assert.Empty(t, sc.providerConversationID)

	params, err := sc.buildOpenAIResponsesRequestParams(openAIRequestShapeModelInfo(), nil)
	require.NoError(t, err)

	req, reqJSON := mustMarshalOpenAIResponsesRequest(t, params)
	assert.NotContains(t, req, "previous_response_id")
	assert.Contains(t, reqJSON, "system instructions")
	assert.Contains(t, reqJSON, "first question")
	assert.Contains(t, reqJSON, "first answer")
	assert.Contains(t, reqJSON, "second question")
}

func openAIRequestShapeConversation(t *testing.T) *streamingConversation {
	t.Helper()

	sc := NewConversation(llmmodel.ModelID("gpt-4o-mini"), "system instructions").(*streamingConversation)
	require.NoError(t, sc.AddUserTurn("first question"))
	sc.turns = append(sc.turns, Turn{
		Role:       RoleAssistant,
		ProviderID: "resp_first",
		Parts: []ContentPart{
			TextContent{ProviderID: "msg_first", Content: "first answer"},
		},
	})
	require.NoError(t, sc.AddUserTurn("second question"))
	sc.providerConversationID = "resp_first"
	return sc
}

func newOpenAIResponsesContentBuildersForTest() *openAIResponsesContentBuilders {
	return &openAIResponsesContentBuilders{
		idToTextBuilder:      make(map[string]*strings.Builder),
		idToReasoningBuilder: make(map[string]*strings.Builder),
		idToTextDone:         make(map[string]bool),
		idToReasoningDone:    make(map[string]bool),
	}
}

func mustOpenAIResponseStreamEvent(t *testing.T, raw string) responses.ResponseStreamEventUnion {
	t.Helper()
	var event responses.ResponseStreamEventUnion
	require.NoError(t, json.Unmarshal([]byte(raw), &event))
	return event
}

func openAIProviderItemReplayConversation(t *testing.T) *streamingConversation {
	t.Helper()

	sc := NewConversation(llmmodel.ModelID("gpt-4o-mini"), "system instructions").(*streamingConversation)
	require.NoError(t, sc.AddUserTurn("first question"))

	functionCall := ToolCall{
		ProviderID: "fc_persisted",
		CallID:     "call_function_value",
		Name:       "lookup_weather",
		Type:       "function_call",
		Input:      `{"city":"Paris"}`,
	}
	customCall := ToolCall{
		ProviderID: "ct_persisted",
		CallID:     "call_custom_value",
		Name:       "structured_answer",
		Type:       "custom_tool_call",
		Input:      "answer:7",
	}
	sc.turns = append(sc.turns, Turn{
		Role:       RoleAssistant,
		ProviderID: "resp_first",
		Parts: []ContentPart{
			ReasoningContent{ProviderID: "rs_persisted", Content: "private reasoning summary"},
			TextContent{ProviderID: "msg_persisted", Content: "assistant value answer"},
			functionCall,
			customCall,
		},
	})

	functionResult := ToolResult{
		CallID: functionCall.CallID,
		Name:   functionCall.Name,
		Type:   functionCall.Type,
		Result: "72 F",
	}
	customResult := ToolResult{
		CallID: customCall.CallID,
		Name:   customCall.Name,
		Type:   customCall.Type,
		Result: "acknowledged 7",
	}
	sc.toolCalls[functionCall.CallID] = toolCallResult{call: functionCall, result: &functionResult}
	sc.toolCalls[customCall.CallID] = toolCallResult{call: customCall, result: &customResult}
	sc.turns = append(sc.turns, Turn{
		Role:  RoleUser,
		Parts: []ContentPart{functionResult, customResult},
	})
	sc.providerConversationID = "resp_first"
	return sc
}

func assertOpenAINoStoreTurnScrubbed(t *testing.T, turn *Turn) {
	t.Helper()

	require.NotNil(t, turn)
	assert.Empty(t, turn.ProviderID)
	for _, part := range turn.Parts {
		switch part := part.(type) {
		case TextContent:
			assert.Empty(t, part.ProviderID)
		case ToolCall:
			assert.Empty(t, part.ProviderID)
		case ReasoningContent:
			assert.Empty(t, part.ProviderID)
			assert.Empty(t, part.Content)
			assert.NotEmpty(t, part.ProviderState)
		}
	}
}

func openAIRequestShapeModelInfo() llmmodel.ModelInfo {
	return llmmodel.ModelInfo{
		ID:              llmmodel.ModelID("gpt-4o-mini"),
		ProviderID:      llmmodel.ProviderIDOpenAI,
		ProviderModelID: "gpt-4o-mini",
	}
}

func openAIReasoningRequestShapeModelInfo() llmmodel.ModelInfo {
	info := openAIRequestShapeModelInfo()
	info.ID = llmmodel.ModelID("gpt-5-mini")
	info.ProviderModelID = "gpt-5-mini"
	info.CanReason = true
	info.HasReasoningEffort = true
	return info
}

func mustMarshalOpenAIResponsesRequest(t *testing.T, params responses.ResponseNewParams) (map[string]any, string) {
	t.Helper()

	data, err := json.Marshal(params)
	require.NoError(t, err)

	var object map[string]any
	require.NoError(t, json.Unmarshal(data, &object))
	return object, string(data)
}

func openAIResponsesRequestInput(t *testing.T, req map[string]any) []any {
	t.Helper()

	input, ok := req["input"].([]any)
	require.True(t, ok)
	return input
}

func assertOpenAIRequestIncludesEncryptedReasoning(t *testing.T, req map[string]any) {
	t.Helper()

	include, ok := req["include"].([]any)
	require.True(t, ok)
	assert.Contains(t, include, "reasoning.encrypted_content")
}

func openAIResponsesRequestReasoningInputItems(t *testing.T, req map[string]any) []map[string]any {
	t.Helper()

	var reasoningItems []map[string]any
	for _, raw := range openAIResponsesRequestInput(t, req) {
		item, ok := raw.(map[string]any)
		require.True(t, ok)
		if item["type"] == "reasoning" {
			reasoningItems = append(reasoningItems, item)
		}
	}
	return reasoningItems
}

func openAIReasoningProviderStates(parts []ContentPart) []string {
	var states []string
	for _, part := range parts {
		reasoning, ok := part.(ReasoningContent)
		if ok && reasoning.ProviderState != "" {
			states = append(states, reasoning.ProviderState)
		}
	}
	return states
}
