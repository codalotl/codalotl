package llmstream

import (
	"encoding/json"
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

	input := openAIResponsesRequestInput(t, req)
	require.Len(t, input, 4)
	assert.Contains(t, reqJSON, "system instructions")
	assert.Contains(t, reqJSON, "first question")
	assert.Contains(t, reqJSON, "first answer")
	assert.Contains(t, reqJSON, "second question")
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

func openAIRequestShapeModelInfo() llmmodel.ModelInfo {
	return llmmodel.ModelInfo{
		ID:              llmmodel.ModelID("gpt-4o-mini"),
		ProviderID:      llmmodel.ProviderIDOpenAI,
		ProviderModelID: "gpt-4o-mini",
	}
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
