package llmstream

import (
	"github.com/codalotl/codalotl/internal/llmmodel"
	anthropicapi "github.com/codalotl/codalotl/internal/llmstream/anthropic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAnthropicBuildMessageParam_ReasoningContent(t *testing.T) {
	t.Run("encodes thinking with provider state", func(t *testing.T) {
		turn := Turn{
			Role: RoleAssistant,
			Parts: []ContentPart{
				ReasoningContent{
					ProviderID:    "rs_1",
					Content:       "step by step",
					ProviderState: "sig_123",
				},
			},
		}
		msg, include, err := anthropicBuildMessageParam(turn)
		require.NoError(t, err)
		require.True(t, include)
		require.Equal(t, "assistant", msg.Role)
		require.Len(t, msg.Content, 1)
		block := msg.Content[0]
		assert.Equal(t, "thinking", block.Type)
		assert.Equal(t, "step by step", block.Thinking)
		assert.Equal(t, "sig_123", block.Signature)
	})
	t.Run("errors when provider state missing", func(t *testing.T) {
		turn := Turn{
			Role: RoleAssistant,
			Parts: []ContentPart{
				ReasoningContent{
					ProviderID: "rs_1",
					Content:    "step by step",
				},
			},
		}
		_, _, err := anthropicBuildMessageParam(turn)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "provider_state")
	})
}
func TestAnthropicStreamState_ThinkingProviderStateRoundTrip(t *testing.T) {
	state := newAnthropicStreamState()
	created, _, err := state.processEvent(anthropicapi.Event{
		Type: anthropicapi.EventTypeMessageStart,
		Message: &anthropicapi.Message{
			ID: "msg_123",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, created)
	assert.Equal(t, EventTypeCreated, created.Type)
	startEvt, _, err := state.processEvent(anthropicapi.Event{
		Type:  anthropicapi.EventTypeContentBlockStart,
		Index: 0,
		ContentBlock: &anthropicapi.ContentBlock{
			Type:      "thinking",
			Thinking:  "alpha",
			Signature: "sigA",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, startEvt)
	require.NotNil(t, startEvt.Reasoning)
	assert.Equal(t, "alpha", startEvt.Reasoning.Content)
	assert.Equal(t, "sigA", startEvt.Reasoning.ProviderState)
	_, _, err = state.processEvent(anthropicapi.Event{
		Type:  anthropicapi.EventTypeContentBlockDelta,
		Index: 0,
		Delta: &anthropicapi.ContentBlockDelta{
			Type:     "thinking_delta",
			Thinking: " beta",
		},
	})
	require.NoError(t, err)
	_, _, err = state.processEvent(anthropicapi.Event{
		Type:  anthropicapi.EventTypeContentBlockDelta,
		Index: 0,
		Delta: &anthropicapi.ContentBlockDelta{
			Type:      "signature_delta",
			Signature: "sigB",
		},
	})
	require.NoError(t, err)
	doneEvt, _, err := state.processEvent(anthropicapi.Event{
		Type:  anthropicapi.EventTypeContentBlockStop,
		Index: 0,
	})
	require.NoError(t, err)
	require.NotNil(t, doneEvt)
	assert.Equal(t, EventTypeReasoningDelta, doneEvt.Type)
	require.NotNil(t, doneEvt.Reasoning)
	assert.Equal(t, "alpha beta", doneEvt.Reasoning.Content)
	assert.Equal(t, "sigAsigB", doneEvt.Reasoning.ProviderState)
	assert.True(t, doneEvt.Done)
	_, _, err = state.processEvent(anthropicapi.Event{
		Type: anthropicapi.EventTypeMessageDelta,
		MessageDelta: &anthropicapi.MessageDelta{
			StopReason: "end_turn",
		},
	})
	require.NoError(t, err)
	completed, done, err := state.processEvent(anthropicapi.Event{
		Type: anthropicapi.EventTypeMessageStop,
	})
	require.NoError(t, err)
	require.True(t, done)
	require.NotNil(t, completed)
	require.NotNil(t, completed.Turn)
	assert.Equal(t, EventTypeCompletedSuccess, completed.Type)
	assert.Equal(t, FinishReasonEndTurn, completed.Turn.FinishReason)
	require.Len(t, completed.Turn.Parts, 1)
	reasoning, ok := completed.Turn.Parts[0].(ReasoningContent)
	require.True(t, ok)
	assert.Equal(t, "alpha beta", reasoning.Content)
	assert.Equal(t, "sigAsigB", reasoning.ProviderState)
}

func TestAnthropicConvertUsage_SeparatesCacheReadAndCreation(t *testing.T) {
	usage := anthropicapi.Usage{
		InputTokens:              100,
		CacheReadInputTokens:     50,
		CacheCreationInputTokens: 30,
		OutputTokens:             25,
	}

	got := anthropicConvertUsage(usage)

	assert.EqualValues(t, 180, got.TotalInputTokens)
	assert.EqualValues(t, 50, got.CachedInputTokens)
	assert.EqualValues(t, 30, got.CacheCreationInputTokens)
	assert.EqualValues(t, 0, got.ReasoningTokens)
	assert.EqualValues(t, 25, got.TotalOutputTokens)
}

func TestAnthropicConvertUsage_UsesTTLBreakdownWhenTopLevelCreationMissing(t *testing.T) {
	usage := anthropicapi.Usage{
		InputTokens:          20,
		CacheReadInputTokens: 7,
		OutputTokens:         9,
		CacheCreation: anthropicapi.CacheCreationUsage{
			Ephemeral5mInputTokens: 3,
			Ephemeral1hInputTokens: 2,
		},
	}

	got := anthropicConvertUsage(usage)

	assert.EqualValues(t, 32, got.TotalInputTokens)
	assert.EqualValues(t, 7, got.CachedInputTokens)
	assert.EqualValues(t, 5, got.CacheCreationInputTokens)
	assert.EqualValues(t, 0, got.ReasoningTokens)
	assert.EqualValues(t, 9, got.TotalOutputTokens)
}

func TestBuildAnthropicMessageRequest_SetsTopLevelCacheControl(t *testing.T) {
	sc := NewConversation(llmmodel.ModelIDUnknown, "system").(*streamingConversation)
	require.NoError(t, sc.AddUserTurn("hello"))

	req, err := sc.buildAnthropicMessageRequest(llmmodel.ModelInfo{ProviderModelID: "claude-sonnet-4-6"}, nil)
	require.NoError(t, err)
	require.NotNil(t, req.CacheControl)
	assert.Equal(t, "ephemeral", req.CacheControl.Type)
	assert.Equal(t, "", req.CacheControl.TTL)
}

func TestBuildAnthropicMessageRequest_UsesModelMaxOutput(t *testing.T) {
	sc := NewConversation(llmmodel.ModelIDUnknown, "system").(*streamingConversation)
	require.NoError(t, sc.AddUserTurn("hello"))

	req, err := sc.buildAnthropicMessageRequest(llmmodel.ModelInfo{
		ProviderModelID: "claude-sonnet-4-6",
		MaxOutput:       64000,
	}, nil)
	require.NoError(t, err)
	assert.EqualValues(t, 64000, req.MaxTokens)
}

func TestBuildAnthropicMessageRequest_FallsBackToDefaultMaxOutput(t *testing.T) {
	sc := NewConversation(llmmodel.ModelIDUnknown, "system").(*streamingConversation)
	require.NoError(t, sc.AddUserTurn("hello"))

	req, err := sc.buildAnthropicMessageRequest(llmmodel.ModelInfo{ProviderModelID: "claude-sonnet-4-6"}, nil)
	require.NoError(t, err)
	assert.EqualValues(t, defaultAnthropicMaxTokens, req.MaxTokens)
}
