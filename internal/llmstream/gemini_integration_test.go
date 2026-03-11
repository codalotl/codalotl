package llmstream

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codalotl/codalotl/internal/llmmodel"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runGeminiIntegrationTest(t *testing.T) bool {
	if os.Getenv("GEMINI_API_KEY") == "" && os.Getenv("GOOGLE_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY or GOOGLE_API_KEY is required to run Gemini integration tests")
		return false
	}
	if os.Getenv("INTEGRATION_TEST") != "1" {
		t.Skip("INTEGRATION_TEST=1 is required to run these tests")
		return false
	}
	return true
}

func TestGeminiProvider_ToolUsage(t *testing.T) {
	if !runGeminiIntegrationTest(t) {
		return
	}

	modelID := llmmodel.ProviderIDGemini.DefaultModel()
	if modelID == llmmodel.ModelIDUnknown {
		modelID = llmmodel.ModelID("gemini-2.5-pro")
		require.NoError(t, llmmodel.AddCustomModel(modelID, llmmodel.ProviderIDGemini, string(modelID), llmmodel.ModelOverrides{}))
	}

	const (
		toolName  = "get_weather"
		toolReply = "72 F"
	)

	system := `You are a precise assistant.
In your first response, call the tool named "get_weather" exactly once with {"location":"San Francisco"}.
You may include brief reasoning, but do not ask questions.
After the tool result is supplied, reply with exactly 72 F and nothing else.`

	conv := NewConversation(modelID, system)
	require.NoError(t, conv.AddUserTurn("Use the tool now."))
	require.NoError(t, conv.AddTools([]Tool{
		getWeatherTestTool{name: toolName, fixedTemp: toolReply},
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	events := conv.SendAsync(ctx, SendOptions{ReasoningEffort: "low"})

	var (
		firstTurn *Turn
		firstCall *ToolCall
	)
	for event := range events {
		switch event.Type {
		case EventTypeError:
			t.Fatalf("unexpected error: %v", event.Error)
		case EventTypeWarning:
			t.Fatalf("unexpected warning: %v", event.Error)
		case EventTypeToolUse:
			require.NotNil(t, event.ToolCall)
			callCopy := *event.ToolCall
			firstCall = &callCopy
		case EventTypeCompletedSuccess:
			require.NotNil(t, event.Turn)
			firstTurn = event.Turn
		}
	}

	require.NotNil(t, firstTurn)
	require.NotNil(t, firstCall)
	assert.Equal(t, FinishReasonToolUse, firstTurn.FinishReason)
	require.NotEmpty(t, firstTurn.ToolCalls())
	assert.Equal(t, toolName, firstTurn.ToolCalls()[0].Name)

	var args map[string]any
	require.NoError(t, json.Unmarshal([]byte(firstTurn.ToolCalls()[0].Input), &args))
	assert.Equal(t, "San Francisco", args["location"])

	result := getWeatherTestTool{name: toolName, fixedTemp: toolReply}.Run(ctx, *firstCall)
	require.NoError(t, conv.AddToolResults([]ToolResult{result}))

	ctx2, cancel2 := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel2()

	events = conv.SendAsync(ctx2, SendOptions{ReasoningEffort: "low"})

	var finalTurn *Turn
	for event := range events {
		switch event.Type {
		case EventTypeError:
			t.Fatalf("unexpected error after tool results: %v", event.Error)
		case EventTypeWarning:
			t.Fatalf("unexpected warning after tool results: %v", event.Error)
		case EventTypeCompletedSuccess:
			require.NotNil(t, event.Turn)
			finalTurn = event.Turn
		}
	}

	require.NotNil(t, finalTurn)
	assert.Equal(t, FinishReasonEndTurn, finalTurn.FinishReason)
	assert.Empty(t, finalTurn.ToolCalls())

	foundToolReply := false
	for _, part := range finalTurn.Parts {
		if text, ok := part.(TextContent); ok && strings.Contains(text.Content, toolReply) {
			foundToolReply = true
			break
		}
	}
	assert.True(t, foundToolReply)
	assert.Contains(t, conv.LastTurn().TextContent(), toolReply)
}
