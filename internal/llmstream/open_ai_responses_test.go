package llmstream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func TestOpenAIResponesBuildResponse_CapturesCompactionState(t *testing.T) {
	var resp responses.Response
	require.NoError(t, json.Unmarshal([]byte(`{
		"id": "resp_123",
		"status": "completed",
		"output": [
			{
				"id": "cmp_123",
				"type": "compaction",
				"encrypted_content": "encrypted_compaction_blob",
				"created_by": "server"
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
	assert.Contains(t, turn.Parts, CompactionContent{ProviderID: "cmp_123", ProviderState: "encrypted_compaction_blob"})
	assert.Contains(t, turn.Parts, TextContent{ProviderID: "msg_123", Content: "answer"})
}

func TestOpenAIResponsesResolveAuth_UsesEligibleProviderSubscription(t *testing.T) {
	registerTestOpenAIProviderSubscription(t, "https://chatgpt.com/backend-api/codex", true)

	auth, err := openAIResponsesResolveAuth(llmmodel.ModelID("gpt-4o-mini"), openAIRequestShapeModelInfo())
	require.NoError(t, err)

	assert.Equal(t, openAIResponsesAuthModeProviderSubscription, auth.mode)
	assert.Equal(t, "sub-token", auth.apiKey)
	assert.Equal(t, "https://chatgpt.com/backend-api/codex", auth.baseURL)
	assert.Equal(t, "acct_123", auth.accountID)
	assert.True(t, auth.requiresNoStore)
}

func TestOpenAIResponsesSubscriptionEligible(t *testing.T) {
	sub := llmmodel.ProviderSubscription{ProviderID: llmmodel.ProviderIDOpenAI}

	tests := []struct {
		name      string
		info      llmmodel.ModelInfo
		wantAllow bool
	}{
		{
			name:      "no overrides",
			info:      openAIRequestShapeModelInfo(),
			wantAllow: true,
		},
		{
			name: "default endpoint is not an override",
			info: llmmodel.ModelInfo{
				ID:              llmmodel.ModelID("gpt-4o-mini"),
				ProviderID:      llmmodel.ProviderIDOpenAI,
				ProviderModelID: "gpt-4o-mini",
				APIEndpointURL:  "https://api.openai.com/v1",
			},
			wantAllow: true,
		},
		{
			name: "actual key override suppresses",
			info: llmmodel.ModelInfo{
				ProviderID: llmmodel.ProviderIDOpenAI,
				ModelOverrides: llmmodel.ModelOverrides{
					APIActualKey: "model-key",
				},
			},
		},
		{
			name: "env key override suppresses",
			info: llmmodel.ModelInfo{
				ProviderID: llmmodel.ProviderIDOpenAI,
				ModelOverrides: llmmodel.ModelOverrides{
					APIEnvKey: "CUSTOM_OPENAI_API_KEY",
				},
			},
		},
		{
			name: "endpoint override suppresses",
			info: llmmodel.ModelInfo{
				ProviderID: llmmodel.ProviderIDOpenAI,
				ModelOverrides: llmmodel.ModelOverrides{
					APIEndpointURL: "https://example.test/v1",
				},
			},
		},
		{
			name: "provider mismatch suppresses",
			info: llmmodel.ModelInfo{ProviderID: llmmodel.ProviderIDAnthropic},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantAllow, openAIResponsesSubscriptionEligible(tt.info, sub))
		})
	}
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

func TestSendAsyncOpenAIResponses_SubscriptionAuthUsesEndpointHeaderAndForcesNoStore(t *testing.T) {
	var (
		gotPath      string
		gotAuth      string
		gotAccountID string
		gotRequest   map[string]any
		requests     int
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		gotPath = r.URL.EscapedPath()
		gotAuth = r.Header.Get("Authorization")
		gotAccountID = r.Header.Get("ChatGPT-Account-ID")
		_ = json.NewDecoder(r.Body).Decode(&gotRequest)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		writeOpenAISSEEvent(t, w, "response.output_text.delta", `{
			"type":"response.output_text.delta",
			"sequence_number":0,
			"item_id":"msg_sub",
			"output_index":0,
			"content_index":0,
			"delta":"hello"
		}`)
		writeOpenAISSEEvent(t, w, "response.completed", `{
			"type":"response.completed",
			"sequence_number":1,
			"response":{
				"id":"resp_sub",
				"object":"response",
				"created_at":0,
				"model":"gpt-4o-mini",
				"status":"completed",
				"output":[],
				"usage":{
					"input_tokens":3,
					"input_tokens_details":{"cached_tokens":0},
					"output_tokens":1,
					"output_tokens_details":{"reasoning_tokens":0},
					"total_tokens":4
				}
			}
		}`)
	}))
	defer server.Close()
	registerTestOpenAIProviderSubscription(t, server.URL+"/backend-api/codex", true)

	modelID := llmmodel.ModelID("test-openai-subscription-stream")
	require.NoError(t, llmmodel.AddCustomModel(modelID, llmmodel.ProviderIDOpenAI, "gpt-4o-mini", llmmodel.ModelOverrides{}))
	conv := NewConversation(modelID, "system instructions")
	require.NoError(t, conv.AddUserTurn("say hello"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var finalTurn *Turn
	for ev := range conv.SendAsync(ctx) {
		switch ev.Type {
		case EventTypeError:
			require.NoError(t, ev.Error)
		case EventTypeCompletedSuccess:
			finalTurn = ev.Turn
		}
	}

	assert.Equal(t, 1, requests)
	assert.Equal(t, "/backend-api/codex/responses", gotPath)
	assert.Equal(t, "Bearer sub-token", gotAuth)
	assert.Equal(t, "acct_123", gotAccountID)
	require.NotNil(t, gotRequest)
	assert.Equal(t, false, gotRequest["store"])
	assert.NotContains(t, gotRequest, "previous_response_id")

	require.NotNil(t, finalTurn)
	assert.Empty(t, finalTurn.ProviderID)
	assert.Equal(t, "hello", finalTurn.TextContent())
	assert.Equal(t, FinishReasonEndTurn, finalTurn.FinishReason)

	sc := conv.(*streamingConversation)
	assert.Empty(t, sc.providerConversationID)
	assert.Empty(t, sc.LastTurn().ProviderID)
	assert.Equal(t, "hello", sc.LastTurn().TextContent())
}

func TestSendAsyncOpenAIResponses_NoStoreReplaysCompactionWithMockServer(t *testing.T) {
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var gotRequest map[string]any
		_ = json.NewDecoder(r.Body).Decode(&gotRequest)
		requests = append(requests, gotRequest)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if len(requests) == 1 {
			writeOpenAISSEEvent(t, w, "response.completed", `{
				"type":"response.completed",
				"sequence_number":0,
				"response":{
					"id":"resp_compacted",
					"object":"response",
					"created_at":0,
					"model":"gpt-5.5-high",
					"status":"completed",
					"output":[
						{
							"id":"msg_first",
							"type":"message",
							"role":"assistant",
							"status":"completed",
							"content":[{"type":"output_text","text":"first answer"}]
						},
						{
							"id":"cmp_first",
							"type":"compaction",
							"encrypted_content":"encrypted_compaction_blob",
							"created_by":"server"
						}
					],
					"usage":{
						"input_tokens":100,
						"input_tokens_details":{"cached_tokens":0},
						"output_tokens":10,
						"output_tokens_details":{"reasoning_tokens":0},
						"total_tokens":110
					}
				}
			}`)
			return
		}
		writeOpenAISSEEvent(t, w, "response.completed", `{
			"type":"response.completed",
			"sequence_number":0,
			"response":{
				"id":"resp_second",
				"object":"response",
				"created_at":0,
				"model":"gpt-5.5-high",
				"status":"completed",
				"output":[
					{
						"id":"msg_second",
						"type":"message",
						"role":"assistant",
						"status":"completed",
						"content":[{"type":"output_text","text":"second answer"}]
					}
				],
				"usage":{
					"input_tokens":20,
					"input_tokens_details":{"cached_tokens":0},
					"output_tokens":2,
					"output_tokens_details":{"reasoning_tokens":0},
					"total_tokens":22
				}
			}
		}`)
	}))
	defer server.Close()
	registerTestOpenAIProviderSubscription(t, server.URL+"/backend-api/codex", true)

	conv := NewConversation(llmmodel.DefaultModel, "system instructions")
	require.NoError(t, conv.AddUserTurn("first question"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var firstTurn *Turn
	for ev := range conv.SendAsync(ctx) {
		switch ev.Type {
		case EventTypeError:
			require.NoError(t, ev.Error)
		case EventTypeCompletedSuccess:
			firstTurn = ev.Turn
		}
	}

	require.NotNil(t, firstTurn)
	assert.Equal(t, "first answer", firstTurn.TextContent())
	assertOpenAINoStoreTurnScrubbed(t, firstTurn)
	assert.Equal(t, []string{"encrypted_compaction_blob"}, openAICompactionProviderStates(firstTurn.Parts))
	assert.Empty(t, conv.(*streamingConversation).providerConversationID)

	require.NoError(t, conv.AddUserTurn("second question"))

	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()

	var secondTurn *Turn
	for ev := range conv.SendAsync(ctx2) {
		switch ev.Type {
		case EventTypeError:
			require.NoError(t, ev.Error)
		case EventTypeCompletedSuccess:
			secondTurn = ev.Turn
		}
	}

	require.NotNil(t, secondTurn)
	assert.Equal(t, "second answer", secondTurn.TextContent())
	require.Len(t, requests, 2)

	secondRequest := requests[1]
	assert.Equal(t, false, secondRequest["store"])
	assert.NotContains(t, secondRequest, "previous_response_id")
	compactionItems := openAIResponsesRequestCompactionInputItems(t, secondRequest)
	require.Len(t, compactionItems, 1)
	assert.Equal(t, "encrypted_compaction_blob", compactionItems[0]["encrypted_content"])
	assert.NotContains(t, compactionItems[0], "id")

	secondRequestJSON := mustMarshalDiagnosticJSON(t, secondRequest)
	assert.Contains(t, secondRequestJSON, "second question")
	assert.NotContains(t, secondRequestJSON, "first question")
	assert.NotContains(t, secondRequestJSON, "first answer")
	assert.NotContains(t, secondRequestJSON, "resp_compacted")
	assert.NotContains(t, secondRequestJSON, "msg_first")
	assert.NotContains(t, secondRequestJSON, "cmp_first")
}

func TestBuildOpenAIResponsesRequestParams_DefaultLinksAndTrimsHistory(t *testing.T) {
	sc := openAIRequestShapeConversation(t)

	params, err := sc.buildOpenAIResponsesRequestParams(openAIRequestShapeModelInfo(), nil)
	require.NoError(t, err)

	req, reqJSON := mustMarshalOpenAIResponsesRequest(t, params)
	assert.Equal(t, true, req["store"])
	assert.Equal(t, true, req["parallel_tool_calls"])
	assert.Equal(t, "resp_first", req["previous_response_id"])
	assertOpenAIRequestInstructions(t, req, "system instructions")

	input := openAIResponsesRequestInput(t, req)
	require.Len(t, input, 1)
	inputJSON := openAIResponsesRequestInputJSON(t, req)
	assert.Contains(t, inputJSON, "second question")
	assert.NotContains(t, inputJSON, "system instructions")
	assert.NotContains(t, inputJSON, "first question")
	assert.NotContains(t, inputJSON, "first answer")
	assert.Contains(t, reqJSON, `"instructions":"system instructions"`)
}

func TestBuildOpenAIResponsesRequestParams_SubscriptionRequiresNoStore(t *testing.T) {
	registerTestOpenAIProviderSubscription(t, "https://chatgpt.com/backend-api/codex", true)
	sc := openAIRequestShapeConversation(t)

	params, err := sc.buildOpenAIResponsesRequestParams(openAIRequestShapeModelInfo(), nil)
	require.NoError(t, err)

	req, reqJSON := mustMarshalOpenAIResponsesRequest(t, params)
	assert.Equal(t, false, req["store"])
	assert.NotContains(t, req, "previous_response_id")
	assertOpenAIRequestInstructions(t, req, "system instructions")

	input := openAIResponsesRequestInput(t, req)
	require.Len(t, input, 3)
	inputJSON := openAIResponsesRequestInputJSON(t, req)
	assert.Contains(t, inputJSON, "first question")
	assert.Contains(t, inputJSON, "first answer")
	assert.Contains(t, inputJSON, "second question")
	assert.NotContains(t, inputJSON, "system instructions")
	assert.Contains(t, reqJSON, `"instructions":"system instructions"`)
	assert.NotContains(t, reqJSON, "resp_first")
}

func TestBuildOpenAIResponsesRequestParams_SubscriptionNoStoreSuppressedByModelEndpointOverride(t *testing.T) {
	registerTestOpenAIProviderSubscription(t, "https://chatgpt.com/backend-api/codex", true)
	sc := openAIRequestShapeConversation(t)
	modelInfo := openAIRequestShapeModelInfo()
	modelInfo.ModelOverrides.APIEndpointURL = "https://example.test/v1"

	params, err := sc.buildOpenAIResponsesRequestParams(modelInfo, nil)
	require.NoError(t, err)

	req, _ := mustMarshalOpenAIResponsesRequest(t, params)
	assert.Equal(t, true, req["store"])
	assert.Equal(t, "resp_first", req["previous_response_id"])
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
	assertOpenAIRequestInstructions(t, req, "system instructions")

	input := openAIResponsesRequestInput(t, req)
	require.Len(t, input, 3)
	inputJSON := openAIResponsesRequestInputJSON(t, req)
	assert.Contains(t, inputJSON, "first question")
	assert.Contains(t, inputJSON, "first answer")
	assert.Contains(t, inputJSON, "second question")
	assert.NotContains(t, inputJSON, "system instructions")
	assert.Contains(t, reqJSON, `"instructions":"system instructions"`)
}

func TestOpenAIResponsesProcessEvent_CompletedEmptyOutputUsesStreamedState(t *testing.T) {
	builders := newOpenAIResponsesContentBuilders()

	events := []string{
		`{
			"type":"response.output_text.delta",
			"sequence_number":0,
			"item_id":"msg_1",
			"output_index":0,
			"content_index":0,
			"delta":"hello"
		}`,
		`{
			"type":"response.output_item.done",
			"sequence_number":1,
			"output_index":1,
			"item":{
				"id":"fc_1",
				"type":"function_call",
				"status":"completed",
				"call_id":"call_1",
				"name":"lookup_weather",
				"arguments":"{\"city\":\"Paris\"}"
			}
		}`,
		`{
			"type":"response.output_item.done",
			"sequence_number":2,
			"output_index":2,
			"item":{
				"id":"rs_1",
				"type":"reasoning",
				"summary":[{"type":"summary_text","text":"reasoning summary"}],
				"encrypted_content":"encrypted_reasoning_blob"
			}
		}`,
		`{
			"type":"response.output_item.done",
			"sequence_number":3,
			"output_index":3,
			"item":{
				"id":"cmp_1",
				"type":"compaction",
				"encrypted_content":"encrypted_compaction_blob",
				"created_by":"server"
			}
		}`,
	}
	for _, raw := range events {
		processed, cont, err := openAIResponsesProcessEvent(mustUnmarshalOpenAIStreamEvent(t, raw), builders)
		require.NoError(t, err)
		assert.True(t, cont)
		_ = processed
	}

	completed, cont, err := openAIResponsesProcessEvent(mustUnmarshalOpenAIStreamEvent(t, `{
		"type":"response.completed",
		"sequence_number":4,
		"response":{
			"id":"resp_1",
			"object":"response",
			"created_at":0,
			"model":"gpt-4o-mini",
			"status":"completed",
			"output":[],
			"usage":{
				"input_tokens":10,
				"input_tokens_details":{"cached_tokens":2},
				"output_tokens":5,
				"output_tokens_details":{"reasoning_tokens":1},
				"total_tokens":15
			}
		}
	}`), builders)
	require.NoError(t, err)
	assert.False(t, cont)
	require.NotNil(t, completed)
	require.NotNil(t, completed.Turn)

	turn := completed.Turn
	assert.Equal(t, "resp_1", turn.ProviderID)
	assert.Equal(t, "hello", turn.TextContent())
	assert.Equal(t, FinishReasonToolUse, turn.FinishReason)
	assert.EqualValues(t, 10, turn.Usage.TotalInputTokens)
	assert.EqualValues(t, 2, turn.Usage.CachedInputTokens)
	assert.EqualValues(t, 1, turn.Usage.ReasoningTokens)
	assert.EqualValues(t, 5, turn.Usage.TotalOutputTokens)
	assert.Contains(t, turn.ToolCalls(), ToolCall{
		ProviderID: "fc_1",
		CallID:     "call_1",
		Name:       "lookup_weather",
		Type:       "function_call",
		Input:      `{"city":"Paris"}`,
	})
	assert.Contains(t, turn.Parts, ReasoningContent{ProviderID: "rs_1", Content: "reasoning summary"})
	assert.Contains(t, turn.Parts, ReasoningContent{ProviderID: "rs_1", ProviderState: "encrypted_reasoning_blob"})
	assert.Contains(t, turn.Parts, CompactionContent{ProviderID: "cmp_1", ProviderState: "encrypted_compaction_blob"})
}

func TestOpenAIResponsesProcessEvent_CompletedNonEmptyOutputKeepsStreamedCompaction(t *testing.T) {
	builders := newOpenAIResponsesContentBuilders()

	events := []string{
		`{
			"type":"response.output_text.delta",
			"sequence_number":0,
			"item_id":"msg_streamed",
			"output_index":0,
			"content_index":0,
			"delta":"streamed draft"
		}`,
		`{
			"type":"response.output_item.done",
			"sequence_number":1,
			"output_index":1,
			"item":{
				"id":"cmp_streamed",
				"type":"compaction",
				"encrypted_content":"encrypted_compaction_blob",
				"created_by":"server"
			}
		}`,
	}
	for _, raw := range events {
		processed, cont, err := openAIResponsesProcessEvent(mustUnmarshalOpenAIStreamEvent(t, raw), builders)
		require.NoError(t, err)
		assert.True(t, cont)
		_ = processed
	}

	completed, cont, err := openAIResponsesProcessEvent(mustUnmarshalOpenAIStreamEvent(t, `{
		"type":"response.completed",
		"sequence_number":2,
		"response":{
			"id":"resp_1",
			"object":"response",
			"created_at":0,
			"model":"gpt-4o-mini",
			"status":"completed",
			"output":[
				{
					"id":"msg_completed",
					"type":"message",
					"role":"assistant",
					"status":"completed",
					"content":[{"type":"output_text","text":"completed answer"}]
				}
			],
			"usage":{
				"input_tokens":10,
				"input_tokens_details":{"cached_tokens":2},
				"output_tokens":5,
				"output_tokens_details":{"reasoning_tokens":1},
				"total_tokens":15
			}
		}
	}`), builders)
	require.NoError(t, err)
	assert.False(t, cont)
	require.NotNil(t, completed)
	require.NotNil(t, completed.Turn)

	turn := completed.Turn
	assert.Equal(t, "resp_1", turn.ProviderID)
	assert.Equal(t, "completed answer", turn.TextContent())
	assert.Equal(t, FinishReasonEndTurn, turn.FinishReason)
	assert.Contains(t, turn.Parts, TextContent{ProviderID: "msg_completed", Content: "completed answer"})
	assert.NotContains(t, turn.Parts, TextContent{ProviderID: "msg_streamed", Content: "streamed draft"})
	assert.Contains(t, turn.Parts, CompactionContent{ProviderID: "cmp_streamed", ProviderState: "encrypted_compaction_blob"})
}

func TestOpenAIResponsesProcessEvent_CompletedNonEmptyOutputMergesStreamedCompactionBeforeLaterMessage(t *testing.T) {
	turn := openAIResponsesMergedStreamedCompactionBeforeCompletedMessageTurn(t)

	require.Len(t, turn.Parts, 2)
	assert.Equal(t, CompactionContent{ProviderID: "cmp_streamed", ProviderState: "encrypted_compaction_blob"}, turn.Parts[0])
	assert.Equal(t, TextContent{ProviderID: "msg_completed", Content: "completed answer after compaction"}, turn.Parts[1])
	assert.Equal(t, "completed answer after compaction", turn.TextContent())
	assert.NotContains(t, turn.Parts, TextContent{ProviderID: "msg_completed", Content: "streamed draft"})
}

func TestBuildOpenAIResponsesRequestParams_NoStoreReplayKeepsMessageAfterStreamedCompaction(t *testing.T) {
	turn := openAIResponsesMergedStreamedCompactionBeforeCompletedMessageTurn(t)
	sc := NewConversation(llmmodel.ModelID("gpt-4o-mini"), "system instructions").(*streamingConversation)
	require.NoError(t, sc.AddUserTurn("question before compaction"))
	sc.turns = append(sc.turns, openAIResponsesScrubNoStoreTurn(*turn))
	require.NoError(t, sc.AddUserTurn("latest question"))

	params, err := sc.buildOpenAIResponsesRequestParams(openAIRequestShapeModelInfo(), &SendOptions{NoStore: true})
	require.NoError(t, err)

	req, _ := mustMarshalOpenAIResponsesRequest(t, params)
	assert.Equal(t, false, req["store"])
	assert.NotContains(t, req, "previous_response_id")

	compactionItems := openAIResponsesRequestCompactionInputItems(t, req)
	require.Len(t, compactionItems, 1)
	assert.Equal(t, "encrypted_compaction_blob", compactionItems[0]["encrypted_content"])
	assert.NotContains(t, compactionItems[0], "id")

	inputJSON := openAIResponsesRequestInputJSON(t, req)
	assert.Contains(t, inputJSON, "completed answer after compaction")
	assert.Contains(t, inputJSON, "latest question")
	assert.NotContains(t, inputJSON, "question before compaction")
	assert.NotContains(t, inputJSON, "msg_completed")
	assert.NotContains(t, inputJSON, "cmp_streamed")
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
	assertOpenAIRequestInstructions(t, req, "system instructions")

	reasoningItems := openAIResponsesRequestReasoningInputItems(t, req)
	require.Len(t, reasoningItems, 1)
	assert.Equal(t, "reasoning", reasoningItems[0]["type"])
	assert.Equal(t, "encrypted_reasoning_blob", reasoningItems[0]["encrypted_content"])
	assert.NotContains(t, reasoningItems[0], "id")
	assert.Empty(t, reasoningItems[0]["summary"])
	assert.NotContains(t, reasoningItems[0], "content")

	inputJSON := openAIResponsesRequestInputJSON(t, req)
	assert.Contains(t, inputJSON, "first question")
	assert.Contains(t, inputJSON, "first answer")
	assert.Contains(t, inputJSON, "second question")
	assert.NotContains(t, inputJSON, "system instructions")
	assert.NotContains(t, reqJSON, "resp_unstored")
	assert.NotContains(t, reqJSON, "rs_unstored")
	assert.NotContains(t, reqJSON, "private reasoning summary")
	assert.NotContains(t, reqJSON, "msg_unstored")
}

func TestBuildOpenAIResponsesRequestParams_NoStoreReplaysLatestCompactionAndPrunesHistory(t *testing.T) {
	sc := NewConversation(llmmodel.ModelID("gpt-4o-mini"), "system instructions").(*streamingConversation)
	require.NoError(t, sc.AddUserTurn("old question before compaction"))
	sc.turns = append(sc.turns, Turn{
		Role:       RoleAssistant,
		ProviderID: "resp_compacted",
		Parts: []ContentPart{
			TextContent{ProviderID: "msg_old", Content: "old answer before compaction"},
			ReasoningContent{ProviderID: "rs_old", ProviderState: "encrypted_reasoning_before_compaction"},
			CompactionContent{ProviderID: "cmp_latest", ProviderState: "encrypted_compaction_blob"},
		},
	})
	require.NoError(t, sc.AddUserTurn("question after compaction"))
	sc.turns = append(sc.turns, Turn{
		Role:       RoleAssistant,
		ProviderID: "resp_after_compaction",
		Parts: []ContentPart{
			ReasoningContent{ProviderID: "rs_after", ProviderState: "encrypted_reasoning_after_compaction"},
			TextContent{ProviderID: "msg_after", Content: "answer after compaction"},
		},
	})
	require.NoError(t, sc.AddUserTurn("latest question"))
	sc.providerConversationID = "resp_after_compaction"

	params, err := sc.buildOpenAIResponsesRequestParams(openAIReasoningRequestShapeModelInfo(), &SendOptions{NoStore: true})
	require.NoError(t, err)

	req, reqJSON := mustMarshalOpenAIResponsesRequest(t, params)
	assert.Equal(t, false, req["store"])
	assert.NotContains(t, req, "previous_response_id")
	assertOpenAIRequestIncludesEncryptedReasoning(t, req)
	assertOpenAIRequestInstructions(t, req, "system instructions")

	compactionItems := openAIResponsesRequestCompactionInputItems(t, req)
	require.Len(t, compactionItems, 1)
	assert.Equal(t, "compaction", compactionItems[0]["type"])
	assert.Equal(t, "encrypted_compaction_blob", compactionItems[0]["encrypted_content"])
	assert.NotContains(t, compactionItems[0], "id")

	reasoningItems := openAIResponsesRequestReasoningInputItems(t, req)
	require.Len(t, reasoningItems, 1)
	assert.Equal(t, "encrypted_reasoning_after_compaction", reasoningItems[0]["encrypted_content"])

	inputJSON := openAIResponsesRequestInputJSON(t, req)
	assert.Contains(t, inputJSON, "question after compaction")
	assert.Contains(t, inputJSON, "answer after compaction")
	assert.Contains(t, inputJSON, "latest question")
	assert.NotContains(t, inputJSON, "system instructions")
	assert.NotContains(t, inputJSON, "old question before compaction")
	assert.NotContains(t, inputJSON, "old answer before compaction")
	assert.NotContains(t, reqJSON, "resp_compacted")
	assert.NotContains(t, reqJSON, "resp_after_compaction")
	assert.NotContains(t, reqJSON, "cmp_latest")
	assert.NotContains(t, reqJSON, "msg_old")
	assert.NotContains(t, reqJSON, "rs_old")
	assert.NotContains(t, reqJSON, "encrypted_reasoning_before_compaction")
	assert.NotContains(t, reqJSON, "rs_after")
	assert.NotContains(t, reqJSON, "msg_after")
}

func TestBuildOpenAIResponsesRequestParams_NoStoreOmitsPersistedProviderItemIDs(t *testing.T) {
	sc := openAIProviderItemReplayConversation(t)

	params, err := sc.buildOpenAIResponsesRequestParams(openAIRequestShapeModelInfo(), &SendOptions{NoStore: true})
	require.NoError(t, err)

	req, reqJSON := mustMarshalOpenAIResponsesRequest(t, params)
	assert.Equal(t, false, req["store"])
	assert.NotContains(t, req, "previous_response_id")
	assertOpenAIRequestInstructions(t, req, "system instructions")

	input := openAIResponsesRequestInput(t, req)
	require.NotEmpty(t, input)
	inputJSON := openAIResponsesRequestInputJSON(t, req)
	assert.Contains(t, inputJSON, "first question")
	assert.Contains(t, inputJSON, "assistant value answer")
	assert.Contains(t, inputJSON, "lookup_weather")
	assert.Contains(t, inputJSON, "structured_answer")
	assert.Contains(t, inputJSON, "call_function_value")
	assert.Contains(t, inputJSON, "call_custom_value")
	assert.Contains(t, inputJSON, "Paris")
	assert.Contains(t, inputJSON, "answer:7")
	assert.Contains(t, inputJSON, "72 F")
	assert.Contains(t, inputJSON, "acknowledged 7")
	assert.NotContains(t, inputJSON, "system instructions")
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
	assertOpenAIRequestInstructions(t, req, "system instructions")
	assert.NotContains(t, openAIResponsesRequestInputJSON(t, req), "system instructions")
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
			CompactionContent{ProviderID: "cmp_unstored", ProviderState: "encrypted_compaction_blob"},
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
	assert.Equal(t, []string{"encrypted_compaction_blob"}, openAICompactionProviderStates(prepared.Turn.Parts))

	assert.Equal(t, "resp_unstored", originalTurn.ProviderID)
	require.Len(t, originalTurn.Parts, 5)
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
	assertOpenAIRequestInstructions(t, req, "system instructions")
	inputJSON := openAIResponsesRequestInputJSON(t, req)
	assert.Contains(t, inputJSON, "first question")
	assert.Contains(t, inputJSON, "assistant value answer")
	assert.Contains(t, inputJSON, "lookup_weather")
	assert.Contains(t, inputJSON, "structured_answer")
	assert.Contains(t, inputJSON, "call_function_value")
	assert.Contains(t, inputJSON, "call_custom_value")
	assert.Contains(t, inputJSON, "Paris")
	assert.Contains(t, inputJSON, "answer:7")
	assert.Contains(t, inputJSON, "72 F")
	assert.Contains(t, inputJSON, "acknowledged 7")
	assert.NotContains(t, inputJSON, "system instructions")
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
	assertOpenAIRequestInstructions(t, req, "system instructions")
	inputJSON := openAIResponsesRequestInputJSON(t, req)
	assert.Contains(t, inputJSON, "first question")
	assert.Contains(t, inputJSON, "first answer")
	assert.Contains(t, inputJSON, "second question")
	assert.NotContains(t, inputJSON, "system instructions")
	assert.Contains(t, reqJSON, `"instructions":"system instructions"`)
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

func registerTestOpenAIProviderSubscription(t *testing.T, endpoint string, requiresNoStore bool) {
	t.Helper()

	oldSub, hadOldSub := llmmodel.GetProviderSubscription(llmmodel.ProviderIDOpenAI)
	llmmodel.SetProviderSubscription(llmmodel.ProviderIDOpenAI, llmmodel.ProviderSubscription{
		ProviderID:       llmmodel.ProviderIDOpenAI,
		AccessToken:      "sub-token",
		AccountID:        "acct_123",
		APIEndpointURL:   endpoint,
		ExpiresAt:        time.Now().Add(time.Hour),
		RequiresNoStore:  requiresNoStore,
		RootInstructions: true,
	})
	t.Cleanup(func() {
		if hadOldSub {
			llmmodel.SetProviderSubscription(llmmodel.ProviderIDOpenAI, oldSub)
			return
		}
		llmmodel.ClearProviderSubscription(llmmodel.ProviderIDOpenAI)
	})
}

func writeOpenAISSEEvent(t *testing.T, w http.ResponseWriter, event string, data string) {
	t.Helper()

	var compact bytes.Buffer
	require.NoError(t, json.Compact(&compact, []byte(data)))

	_, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, compact.String())
	require.NoError(t, err)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func mustUnmarshalOpenAIStreamEvent(t *testing.T, raw string) responses.ResponseStreamEventUnion {
	t.Helper()

	var evt responses.ResponseStreamEventUnion
	require.NoError(t, json.Unmarshal([]byte(raw), &evt))
	return evt
}

func openAIResponsesMergedStreamedCompactionBeforeCompletedMessageTurn(t *testing.T) *Turn {
	t.Helper()

	builders := newOpenAIResponsesContentBuilders()
	events := []string{
		`{
			"type":"response.output_item.done",
			"sequence_number":0,
			"output_index":0,
			"item":{
				"id":"cmp_streamed",
				"type":"compaction",
				"encrypted_content":"encrypted_compaction_blob",
				"created_by":"server"
			}
		}`,
		`{
			"type":"response.output_text.delta",
			"sequence_number":1,
			"item_id":"msg_completed",
			"output_index":1,
			"content_index":0,
			"delta":"streamed draft"
		}`,
	}
	for _, raw := range events {
		processed, cont, err := openAIResponsesProcessEvent(mustUnmarshalOpenAIStreamEvent(t, raw), builders)
		require.NoError(t, err)
		assert.True(t, cont)
		_ = processed
	}

	completed, cont, err := openAIResponsesProcessEvent(mustUnmarshalOpenAIStreamEvent(t, `{
		"type":"response.completed",
		"sequence_number":2,
		"response":{
			"id":"resp_1",
			"object":"response",
			"created_at":0,
			"model":"gpt-4o-mini",
			"status":"completed",
			"output":[
				{
					"id":"msg_completed",
					"type":"message",
					"role":"assistant",
					"status":"completed",
					"content":[{"type":"output_text","text":"completed answer after compaction"}]
				}
			],
			"usage":{
				"input_tokens":10,
				"input_tokens_details":{"cached_tokens":2},
				"output_tokens":5,
				"output_tokens_details":{"reasoning_tokens":1},
				"total_tokens":15
			}
		}
	}`), builders)
	require.NoError(t, err)
	assert.False(t, cont)
	require.NotNil(t, completed)
	require.NotNil(t, completed.Turn)
	return completed.Turn
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
		case CompactionContent:
			assert.Empty(t, part.ProviderID)
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

func openAIResponsesRequestInputJSON(t *testing.T, req map[string]any) string {
	t.Helper()

	data, err := json.Marshal(openAIResponsesRequestInput(t, req))
	require.NoError(t, err)
	return string(data)
}

func assertOpenAIRequestInstructions(t *testing.T, req map[string]any, want string) {
	t.Helper()

	assert.Equal(t, want, req["instructions"])
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

func openAIResponsesRequestCompactionInputItems(t *testing.T, req map[string]any) []map[string]any {
	t.Helper()

	var compactionItems []map[string]any
	for _, raw := range openAIResponsesRequestInput(t, req) {
		item, ok := raw.(map[string]any)
		require.True(t, ok)
		if item["type"] == "compaction" {
			compactionItems = append(compactionItems, item)
		}
	}
	return compactionItems
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

func openAICompactionProviderStates(parts []ContentPart) []string {
	var states []string
	for _, part := range parts {
		compaction, ok := part.(CompactionContent)
		if ok && compaction.ProviderState != "" {
			states = append(states, compaction.ProviderState)
		}
	}
	return states
}
