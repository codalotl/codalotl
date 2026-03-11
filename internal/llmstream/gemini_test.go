package llmstream

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/codalotl/codalotl/internal/llmmodel"
	geminiapi "github.com/codalotl/codalotl/internal/llmstream/gemini"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeminiBuildContentFromTurn_PreservesThoughtSignature(t *testing.T) {
	signature := geminiEncodeThoughtSignature([]byte("sig-123"))
	turn := Turn{
		Role: RoleAssistant,
		Parts: []ContentPart{
			ReasoningContent{
				ProviderID:    "reasoning-1",
				Content:       "thinking",
				ProviderState: signature,
			},
			TextContent{
				ProviderID: "text-1",
				Content:    "hello",
			},
		},
	}

	content, include, err := geminiBuildContentFromTurn(turn)

	require.NoError(t, err)
	require.True(t, include)
	require.NotNil(t, content)
	require.Len(t, content.Parts, 2)
	require.True(t, content.Parts[0].Thought)
	assert.Equal(t, []byte("sig-123"), content.Parts[0].ThoughtSignature)
	assert.Equal(t, "thinking", content.Parts[0].Text)
	assert.Equal(t, "hello", content.Parts[1].Text)
}

func TestGeminiStreamState_AccumulatesThoughtTextAndToolCalls(t *testing.T) {
	state := newGeminiStreamState()

	events, err := state.processResponse(&geminiapi.GenerateContentResponse{
		ResponseID: "resp_1",
		UsageMetadata: &geminiapi.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     10,
			CandidatesTokenCount: 4,
			ThoughtsTokenCount:   2,
		},
		Candidates: []*geminiapi.Candidate{{
			Content: &geminiapi.Content{
				Role: string(geminiapi.RoleModel),
				Parts: []*geminiapi.Part{
					{Thought: true, Text: "alpha", ThoughtSignature: []byte("sigA")},
				},
			},
		}},
	})
	require.NoError(t, err)
	require.Len(t, events, 2)
	assert.Equal(t, EventTypeCreated, events[0].Type)
	assert.Equal(t, EventTypeReasoningDelta, events[1].Type)
	require.NotNil(t, events[1].Reasoning)
	assert.Equal(t, "alpha", events[1].Reasoning.Content)

	events, err = state.processResponse(&geminiapi.GenerateContentResponse{
		Candidates: []*geminiapi.Candidate{{
			Content: &geminiapi.Content{
				Role: string(geminiapi.RoleModel),
				Parts: []*geminiapi.Part{
					{Thought: true, Text: " beta", ThoughtSignature: []byte("sigB")},
					{Text: "hello"},
					{FunctionCall: &geminiapi.FunctionCall{
						ID:   "call_1",
						Name: "get_weather",
						Args: map[string]any{"location": "San Francisco"},
					}},
				},
			},
			FinishReason: geminiapi.FinishReasonStop,
		}},
	})
	require.NoError(t, err)
	require.Len(t, events, 5)
	assert.Equal(t, EventTypeReasoningDelta, events[0].Type)
	assert.Equal(t, EventTypeReasoningDelta, events[1].Type)
	assert.True(t, events[1].Done)
	assert.Equal(t, EventTypeTextDelta, events[2].Type)
	assert.Equal(t, EventTypeTextDelta, events[3].Type)
	assert.True(t, events[3].Done)
	assert.Equal(t, EventTypeToolUse, events[4].Type)

	finalEvents, turn, exactContent, err := state.finalize()
	require.NoError(t, err)
	assert.Empty(t, finalEvents)

	require.Len(t, turn.Parts, 3)
	reasoning, ok := turn.Parts[0].(ReasoningContent)
	require.True(t, ok)
	assert.Equal(t, "alpha beta", reasoning.Content)
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("sigAsigB")), reasoning.ProviderState)

	text, ok := turn.Parts[1].(TextContent)
	require.True(t, ok)
	assert.Equal(t, "hello", text.Content)

	toolCall, ok := turn.Parts[2].(ToolCall)
	require.True(t, ok)
	assert.Equal(t, "call_1", toolCall.CallID)
	assert.Equal(t, "get_weather", toolCall.Name)
	assert.Equal(t, FinishReasonToolUse, turn.FinishReason)
	assert.EqualValues(t, 10, turn.Usage.TotalInputTokens)
	assert.EqualValues(t, 6, turn.Usage.TotalOutputTokens)
	assert.EqualValues(t, 2, turn.Usage.ReasoningTokens)

	require.NotNil(t, exactContent)
	require.Len(t, exactContent.Parts, 3)
	assert.Equal(t, string(geminiapi.RoleModel), exactContent.Role)
}

func TestSendAsyncGeminiWithAttempt_RetriesEmptyStopThreeTimes(t *testing.T) {
	sc := NewConversation(llmmodel.ModelIDUnknown, "system").(*streamingConversation)
	out := make(chan Event, 16)
	attempts := 0

	turn, err := sc.sendAsyncGeminiWithAttempt(context.Background(), out, nil, llmmodel.ModelInfo{}, func(ctx context.Context, out chan Event, opt *SendOptions, modelInfo llmmodel.ModelInfo) (Turn, *geminiapi.Content, error) {
		attempts++
		if attempts <= geminiEmptyStopMaxRetries {
			return Turn{Role: RoleAssistant, FinishReason: FinishReasonEndTurn}, &geminiapi.Content{Role: string(geminiapi.RoleModel)}, nil
		}
		return Turn{
				Role:         RoleAssistant,
				FinishReason: FinishReasonEndTurn,
				Parts: []ContentPart{
					TextContent{ProviderID: "text_1", Content: "done"},
				},
			},
			&geminiapi.Content{
				Role:  string(geminiapi.RoleModel),
				Parts: []*geminiapi.Part{{Text: "done"}},
			},
			nil
	})

	require.NoError(t, err)
	assert.Equal(t, geminiEmptyStopMaxRetries+1, attempts)
	require.Len(t, sc.geminiContents, 1)
	assert.Equal(t, FinishReasonEndTurn, turn.FinishReason)
	require.Len(t, turn.Parts, 1)

	events := drainEvents(out)
	require.Len(t, events, geminiEmptyStopMaxRetries)
	for _, event := range events {
		assert.Equal(t, EventTypeRetry, event.Type)
		require.Error(t, event.Error)
	}
}

func TestSendAsyncGeminiWithAttempt_ExhaustsEmptyStopRetries(t *testing.T) {
	sc := NewConversation(llmmodel.ModelIDUnknown, "system").(*streamingConversation)
	out := make(chan Event, 16)
	attempts := 0

	_, err := sc.sendAsyncGeminiWithAttempt(context.Background(), out, nil, llmmodel.ModelInfo{}, func(ctx context.Context, out chan Event, opt *SendOptions, modelInfo llmmodel.ModelInfo) (Turn, *geminiapi.Content, error) {
		attempts++
		return Turn{Role: RoleAssistant, FinishReason: FinishReasonEndTurn}, &geminiapi.Content{Role: string(geminiapi.RoleModel)}, nil
	})

	require.Error(t, err)
	assert.Equal(t, geminiEmptyStopMaxRetries+1, attempts)
	assert.Empty(t, sc.geminiContents)

	events := drainEvents(out)
	require.Len(t, events, geminiEmptyStopMaxRetries)
	for _, event := range events {
		assert.Equal(t, EventTypeRetry, event.Type)
	}
}

func drainEvents(ch <-chan Event) []Event {
	var events []Event
	for {
		select {
		case event := <-ch:
			events = append(events, event)
		default:
			return events
		}
	}
}
