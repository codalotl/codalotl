package llmstream

import (
	"context"
	"encoding/json"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestAnthropicProvider_ToolUsage(t *testing.T) {
	if !runIntegrationTest(t, "ANTHROPIC_API_KEY") {
		return
	}
	modelID := llmmodel.ProviderIDAnthropic.DefaultModel()
	if modelID == llmmodel.ModelIDUnknown {
		t.Skip("no default anthropic model is registered")
	}
	const (
		toolName  = "get_weather"
		toolReply = "72 F"
	)
	system := `You are a precise assistant.
In your first response, call the tool named "get_weather" exactly once with {"location":"San Francisco"}.
Do not add any plain text before or after the tool call.`
	conv := NewConversation(modelID, system)
	require.NoError(t, conv.AddUserTurn("Use the tool now."))
	require.NoError(t, conv.AddTools([]Tool{
		getWeatherTestTool{name: toolName, fixedTemp: toolReply},
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	events := conv.SendAsync(ctx)
	var (
		gotToolUse bool
		complete   *Turn
		toolCall   *ToolCall
	)
	for event := range events {
		switch event.Type {
		case EventTypeError:
			t.Fatalf("unexpected error: %v", event.Error)
		case EventTypeWarning:
			t.Fatalf("unexpected warning: %v", event.Error)
		case EventTypeToolUse:
			gotToolUse = true
			require.NotNil(t, event.ToolCall)
			callCopy := *event.ToolCall
			toolCall = &callCopy
		case EventTypeCompletedSuccess:
			require.NotNil(t, event.Turn)
			complete = event.Turn
		}
	}
	require.True(t, gotToolUse)
	require.NotNil(t, toolCall)
	require.NotNil(t, complete)
	assert.Equal(t, FinishReasonToolUse, complete.FinishReason)
	calls := complete.ToolCalls()
	require.NotEmpty(t, calls)
	assert.Equal(t, toolName, calls[0].Name)
	assert.Equal(t, "function_call", calls[0].Type)
	var args map[string]any
	require.NoError(t, json.Unmarshal([]byte(calls[0].Input), &args))
	assert.Equal(t, RoleAssistant, conv.LastTurn().Role)
}
