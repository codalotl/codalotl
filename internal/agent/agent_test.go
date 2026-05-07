package agent

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/stretchr/testify/require"
)

func TestSendUserMessageSimple(t *testing.T) {
	systemPrompt := "You are helpful."

	textPartial := llmstream.TextContent{ProviderID: "text-1", Content: "Hel"}
	textContent := llmstream.TextContent{ProviderID: "text-1", Content: "Hello"}
	assistantTurn := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{textContent},
		FinishReason: llmstream.FinishReasonEndTurn,
		Usage: llmstream.TokenUsage{
			TotalInputTokens:  10,
			TotalOutputTokens: 5,
		},
	}

	script := &sendScript{
		events: []llmstream.Event{
			{Type: llmstream.EventTypeTextDelta, Text: &textPartial, Delta: "Hel", Done: false},
			{Type: llmstream.EventTypeTextDelta, Text: &textContent, Delta: "lo", Done: true},
			{Type: llmstream.EventTypeCompletedSuccess, Turn: &assistantTurn},
		},
	}

	conv := newScriptedConversation(systemPrompt, script)
	overrideConversation(t, conv)

	a, err := New(systemPrompt, nil, NewOptions{Model: llmmodel.ModelID("model")})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventsCh := a.SendUserMessage(ctx, "Say hello")

	var events []Event
	for ev := range eventsCh {
		events = append(events, ev)
	}

	if got, want := len(events), 3; got != want {
		t.Fatalf("unexpected event count: got %d want %d", got, want)
	}

	if events[0].Type != EventTypeAssistantText {
		t.Fatalf("first event type = %s, want %s", events[0].Type, EventTypeAssistantText)
	}
	if events[0].TextContent.Content != "Hello" {
		t.Fatalf("unexpected text event payload: %+v", events[0])
	}
	if !events[0].AssistantTextFinalizing {
		t.Fatalf("expected text event to be finalizing: %+v", events[0])
	}

	if events[1].Type != EventTypeAssistantTurnComplete {
		t.Fatalf("second event type = %s, want %s", events[1].Type, EventTypeAssistantTurnComplete)
	}
	if events[1].Turn == nil || events[1].Turn.FinishReason != llmstream.FinishReasonEndTurn {
		t.Fatalf("assistant turn complete event missing turn data: %+v", events[1])
	}

	if events[2].Type != EventTypeDoneSuccess {
		t.Fatalf("third event type = %s, want %s", events[2].Type, EventTypeDoneSuccess)
	}

	if status := a.Status(); status != StatusIdle {
		t.Fatalf("status = %v, want %v", status, StatusIdle)
	}

	if usage := a.TokenUsage(); usage.TotalOutputTokens != assistantTurn.Usage.TotalOutputTokens || usage.TotalInputTokens != assistantTurn.Usage.TotalInputTokens {
		t.Fatalf("unexpected token usage: %+v", usage)
	}

	turns := a.Turns()
	if got, want := len(turns), 3; got != want {
		t.Fatalf("turn count = %d want %d", got, want)
	}
	if turns[0].Role != llmstream.RoleSystem {
		t.Fatalf("turn[0] role = %v, want system", turns[0].Role)
	}
	if turns[1].Role != llmstream.RoleUser {
		t.Fatalf("turn[1] role = %v, want user", turns[1].Role)
	}
	if turns[2].Role != llmstream.RoleAssistant {
		t.Fatalf("turn[2] role = %v, want assistant", turns[2].Role)
	}
}

func TestSendUserMessageFlushesAssistantTextBeforeReasoning(t *testing.T) {
	systemPrompt := "You are helpful."

	textContent := llmstream.TextContent{ProviderID: "text-1", Content: "draft answer"}
	reasoningContent := llmstream.ReasoningContent{ProviderID: "reasoning-1", Content: "thinking"}
	assistantTurn := llmstream.Turn{
		Role: llmstream.RoleAssistant,
		Parts: []llmstream.ContentPart{
			textContent,
			reasoningContent,
		},
		FinishReason: llmstream.FinishReasonEndTurn,
	}

	conv := newScriptedConversation(systemPrompt, &sendScript{
		events: []llmstream.Event{
			{Type: llmstream.EventTypeTextDelta, Text: &textContent, Delta: textContent.Content, Done: true},
			{Type: llmstream.EventTypeReasoningDelta, Reasoning: &reasoningContent, Delta: reasoningContent.Content, Done: true},
			{Type: llmstream.EventTypeCompletedSuccess, Turn: &assistantTurn},
		},
	})
	overrideConversation(t, conv)

	a, err := New(systemPrompt, nil, NewOptions{Model: llmmodel.ModelID("model")})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var events []Event
	for ev := range a.SendUserMessage(ctx, "Say hello") {
		events = append(events, ev)
	}

	require.Len(t, events, 4)
	require.Equal(t, EventTypeAssistantText, events[0].Type)
	require.Equal(t, textContent.Content, events[0].TextContent.Content)
	require.False(t, events[0].AssistantTextFinalizing)
	require.Equal(t, EventTypeAssistantReasoning, events[1].Type)
	require.Equal(t, reasoningContent.Content, events[1].ReasoningContent.Content)
	require.Equal(t, EventTypeAssistantTurnComplete, events[2].Type)
	require.Equal(t, EventTypeDoneSuccess, events[3].Type)
}

func TestSendUserMessageFlushesAssistantTextBeforeToolUse(t *testing.T) {
	systemPrompt := "You are helpful."

	preToolText := llmstream.TextContent{ProviderID: "text-1", Content: "Need a tool"}
	toolCall := llmstream.ToolCall{
		ProviderID: "tool-1",
		CallID:     "call_123",
		Name:       "stub_tool",
		Type:       "function_call",
		Input:      `{"query":"hi"}`,
	}
	turnTool := llmstream.Turn{
		Role: llmstream.RoleAssistant,
		Parts: []llmstream.ContentPart{
			preToolText,
			toolCall,
		},
		FinishReason: llmstream.FinishReasonToolUse,
	}

	finalText := llmstream.TextContent{ProviderID: "text-2", Content: "Done"}
	turnFinal := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{finalText},
		FinishReason: llmstream.FinishReasonEndTurn,
	}

	conv := newScriptedConversation(systemPrompt,
		&sendScript{
			events: []llmstream.Event{
				{Type: llmstream.EventTypeTextDelta, Text: &preToolText, Delta: preToolText.Content, Done: true},
				{Type: llmstream.EventTypeToolUse, ToolCall: &toolCall},
				{Type: llmstream.EventTypeCompletedSuccess, Turn: &turnTool},
			},
		},
		&sendScript{
			events: []llmstream.Event{
				{Type: llmstream.EventTypeTextDelta, Text: &finalText, Delta: finalText.Content, Done: true},
				{Type: llmstream.EventTypeCompletedSuccess, Turn: &turnFinal},
			},
		},
	)
	overrideConversation(t, conv)

	tool := newStubTool("stub_tool", llmstream.ToolResult{Result: "OK"})

	a, err := New(systemPrompt, []llmstream.Tool{tool}, NewOptions{Model: llmmodel.ModelID("model")})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var events []Event
	for ev := range a.SendUserMessage(ctx, "Use the tool") {
		events = append(events, ev)
	}

	require.GreaterOrEqual(t, len(events), 7)
	require.Equal(t, EventTypeAssistantText, events[0].Type)
	require.Equal(t, preToolText.Content, events[0].TextContent.Content)
	require.False(t, events[0].AssistantTextFinalizing)
	require.Equal(t, EventTypeToolCall, events[1].Type)

	finalTextIndex := -1
	for i, ev := range events {
		if ev.Type == EventTypeAssistantText && ev.TextContent.Content == finalText.Content {
			finalTextIndex = i
			require.True(t, ev.AssistantTextFinalizing)
		}
	}
	require.NotEqual(t, -1, finalTextIndex)
}

func TestSendUserMessageMergesTrailingAssistantTextPartsIntoOneFinalizingEvent(t *testing.T) {
	systemPrompt := "You are helpful."

	textOne := llmstream.TextContent{ProviderID: "text-1", Content: `{"answer":`}
	textTwo := llmstream.TextContent{ProviderID: "text-1", Content: `"ok"}`}
	assistantTurn := llmstream.Turn{
		Role: llmstream.RoleAssistant,
		Parts: []llmstream.ContentPart{
			textOne,
			textTwo,
		},
		FinishReason: llmstream.FinishReasonEndTurn,
	}

	conv := newScriptedConversation(systemPrompt, &sendScript{
		events: []llmstream.Event{
			{Type: llmstream.EventTypeTextDelta, Text: &textOne, Delta: textOne.Content, Done: true},
			{Type: llmstream.EventTypeTextDelta, Text: &textTwo, Delta: textTwo.Content, Done: true},
			{Type: llmstream.EventTypeCompletedSuccess, Turn: &assistantTurn},
		},
	})
	overrideConversation(t, conv)

	a, err := New(systemPrompt, nil, NewOptions{Model: llmmodel.ModelID("model")})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var events []Event
	for ev := range a.SendUserMessage(ctx, "Say hello") {
		events = append(events, ev)
	}

	require.Len(t, events, 3)
	require.Equal(t, EventTypeAssistantText, events[0].Type)
	require.True(t, events[0].AssistantTextFinalizing)
	require.Equal(t, textOne.Content+textTwo.Content, events[0].TextContent.Content)
	require.Equal(t, textOne.ProviderID, events[0].TextContent.ProviderID)
}

func TestSendUserMessageCompletedTurnWinsFinalizingAssistantText(t *testing.T) {
	systemPrompt := "You are helpful."

	streamedText := llmstream.TextContent{ProviderID: "text-1", Content: "answer"}
	finalTextOne := llmstream.TextContent{ProviderID: "text-1", Content: "draft "}
	finalTextTwo := llmstream.TextContent{ProviderID: "text-1", Content: "answer"}
	assistantTurn := llmstream.Turn{
		Role: llmstream.RoleAssistant,
		Parts: []llmstream.ContentPart{
			finalTextOne,
			finalTextTwo,
		},
		FinishReason: llmstream.FinishReasonEndTurn,
	}

	conv := newScriptedConversation(systemPrompt, &sendScript{
		events: []llmstream.Event{
			{Type: llmstream.EventTypeTextDelta, Text: &streamedText, Delta: streamedText.Content, Done: true},
			{Type: llmstream.EventTypeCompletedSuccess, Turn: &assistantTurn},
		},
	})
	overrideConversation(t, conv)

	a, err := New(systemPrompt, nil, NewOptions{Model: llmmodel.ModelID("model")})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var events []Event
	for ev := range a.SendUserMessage(ctx, "Say hello") {
		events = append(events, ev)
	}

	require.Len(t, events, 3)
	require.Equal(t, EventTypeAssistantText, events[0].Type)
	require.True(t, events[0].AssistantTextFinalizing)
	require.Equal(t, finalTextOne.Content+finalTextTwo.Content, events[0].TextContent.Content)
}

func TestSendUserMessageFlushesBufferedAssistantTextOnMissingCompletion(t *testing.T) {
	systemPrompt := "You are helpful."

	textContent := llmstream.TextContent{ProviderID: "text-1", Content: "draft answer"}
	conv := newScriptedConversation(systemPrompt, &sendScript{
		events: []llmstream.Event{
			{Type: llmstream.EventTypeTextDelta, Text: &textContent, Delta: textContent.Content, Done: true},
		},
	})
	overrideConversation(t, conv)

	a, err := New(systemPrompt, nil, NewOptions{Model: llmmodel.ModelID("model")})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var events []Event
	for ev := range a.SendUserMessage(ctx, "Say hello") {
		events = append(events, ev)
	}

	require.Len(t, events, 2)
	require.Equal(t, EventTypeAssistantText, events[0].Type)
	require.False(t, events[0].AssistantTextFinalizing)
	require.Equal(t, textContent.Content, events[0].TextContent.Content)
	require.Equal(t, EventTypeError, events[1].Type)
	require.ErrorIs(t, events[1].Error, errMissingCompletion)
}

func TestSendUserMessageFlushesBufferedAssistantTextOnCancellation(t *testing.T) {
	systemPrompt := "You are helpful."

	textContent := llmstream.TextContent{ProviderID: "text-1", Content: "draft answer"}
	completedTurn := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{textContent},
		FinishReason: llmstream.FinishReasonEndTurn,
	}
	firstEventSent := make(chan struct{})
	blockCompletion := make(chan struct{})

	conv := newScriptedConversation(systemPrompt, &sendScript{
		events: []llmstream.Event{
			{Type: llmstream.EventTypeTextDelta, Text: &textContent, Delta: textContent.Content, Done: true},
			{Type: llmstream.EventTypeCompletedSuccess, Turn: &completedTurn},
		},
		waitBefore: []<-chan struct{}{nil, blockCompletion},
		afterSend:  []chan struct{}{firstEventSent, nil},
	})
	overrideConversation(t, conv)

	a, err := New(systemPrompt, nil, NewOptions{Model: llmmodel.ModelID("model")})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	out := a.SendUserMessage(ctx, "Say hello")

	select {
	case <-firstEventSent:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for first llmstream event")
	}
	cancel()

	var events []Event
	for ev := range out {
		events = append(events, ev)
	}

	require.Len(t, events, 2)
	require.Equal(t, EventTypeAssistantText, events[0].Type)
	require.False(t, events[0].AssistantTextFinalizing)
	require.Equal(t, textContent.Content, events[0].TextContent.Content)
	require.Equal(t, EventTypeCanceled, events[1].Type)
	require.ErrorIs(t, events[1].Error, context.Canceled)
}

func TestSendUserMessageRetryResetsCompletedTextRunBookkeeping(t *testing.T) {
	systemPrompt := "You are helpful."

	draftText := llmstream.TextContent{ProviderID: "text-draft", Content: "draft"}
	finalText := llmstream.TextContent{ProviderID: "text-final", Content: "final"}
	assistantTurn := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{finalText},
		FinishReason: llmstream.FinishReasonEndTurn,
	}

	conv := newScriptedConversation(systemPrompt, &sendScript{
		events: []llmstream.Event{
			{Type: llmstream.EventTypeTextDelta, Text: &draftText, Delta: draftText.Content, Done: true},
			{Type: llmstream.EventTypeRetry, Error: errors.New("retrying")},
			{Type: llmstream.EventTypeCompletedSuccess, Turn: &assistantTurn},
		},
	})
	overrideConversation(t, conv)

	a, err := New(systemPrompt, nil, NewOptions{Model: llmmodel.ModelID("model")})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var events []Event
	for ev := range a.SendUserMessage(ctx, "Say hello") {
		events = append(events, ev)
	}

	require.Len(t, events, 5)
	require.Equal(t, EventTypeAssistantText, events[0].Type)
	require.Equal(t, draftText.Content, events[0].TextContent.Content)
	require.False(t, events[0].AssistantTextFinalizing)

	require.Equal(t, EventTypeRetry, events[1].Type)
	require.EqualError(t, events[1].Error, "retrying")

	require.Equal(t, EventTypeAssistantText, events[2].Type)
	require.Equal(t, finalText.Content, events[2].TextContent.Content)
	require.True(t, events[2].AssistantTextFinalizing)

	require.Equal(t, EventTypeAssistantTurnComplete, events[3].Type)
	require.Equal(t, EventTypeDoneSuccess, events[4].Type)
}

func TestSendUserMessageCompletedSuccessEmitsNonFinalAssistantTextWhenTurnEndsWithReasoning(t *testing.T) {
	systemPrompt := "You are helpful."

	textContent := llmstream.TextContent{ProviderID: "text-1", Content: "draft answer"}
	reasoningContent := llmstream.ReasoningContent{ProviderID: "reasoning-1", Content: "thinking"}
	assistantTurn := llmstream.Turn{
		Role: llmstream.RoleAssistant,
		Parts: []llmstream.ContentPart{
			textContent,
			reasoningContent,
		},
		FinishReason: llmstream.FinishReasonEndTurn,
	}

	testCases := []struct {
		name   string
		events []llmstream.Event
	}{
		{
			name: "buffered text delta",
			events: []llmstream.Event{
				{Type: llmstream.EventTypeTextDelta, Text: &textContent, Delta: textContent.Content, Done: true},
				{Type: llmstream.EventTypeCompletedSuccess, Turn: &assistantTurn},
			},
		},
		{
			name: "text only present in completed turn",
			events: []llmstream.Event{
				{Type: llmstream.EventTypeCompletedSuccess, Turn: &assistantTurn},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			conv := newScriptedConversation(systemPrompt, &sendScript{events: tc.events})
			overrideConversation(t, conv)

			a, err := New(systemPrompt, nil, NewOptions{Model: llmmodel.ModelID("model")})
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			var events []Event
			for ev := range a.SendUserMessage(ctx, "Say hello") {
				events = append(events, ev)
			}

			require.Len(t, events, 3)
			require.Equal(t, EventTypeAssistantText, events[0].Type)
			require.False(t, events[0].AssistantTextFinalizing)
			require.Equal(t, textContent.Content, events[0].TextContent.Content)
			require.Equal(t, EventTypeAssistantTurnComplete, events[1].Type)
			require.Equal(t, EventTypeDoneSuccess, events[2].Type)
		})
	}
}

func TestSendUserMessageWithToolUse(t *testing.T) {
	systemPrompt := "You are helpful."

	toolCall := llmstream.ToolCall{
		ProviderID: "tool-1",
		CallID:     "call_123",
		Name:       "stub_tool",
		Type:       "function_call",
		Input:      `{"query":"hi"}`,
	}

	turnTool := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{toolCall},
		FinishReason: llmstream.FinishReasonToolUse,
		Usage: llmstream.TokenUsage{
			TotalInputTokens:  3,
			TotalOutputTokens: 1,
		},
	}

	finalText := llmstream.TextContent{ProviderID: "text-2", Content: "Done"}
	turnFinal := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{finalText},
		FinishReason: llmstream.FinishReasonEndTurn,
		Usage: llmstream.TokenUsage{
			TotalInputTokens:  4,
			TotalOutputTokens: 2,
		},
	}

	script1 := &sendScript{
		events: []llmstream.Event{
			{Type: llmstream.EventTypeToolUse, ToolCall: &toolCall},
			{Type: llmstream.EventTypeCompletedSuccess, Turn: &turnTool},
		},
	}
	script2 := &sendScript{
		events: []llmstream.Event{
			{Type: llmstream.EventTypeTextDelta, Text: &finalText, Delta: "Done", Done: true},
			{Type: llmstream.EventTypeCompletedSuccess, Turn: &turnFinal},
		},
	}

	conv := newScriptedConversation(systemPrompt, script1, script2)
	overrideConversation(t, conv)

	tool := newStubTool("stub_tool", llmstream.ToolResult{Result: "OK"})

	a, err := New(systemPrompt, []llmstream.Tool{tool}, NewOptions{Model: llmmodel.ModelID("model")})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventsCh := a.SendUserMessage(ctx, "Use the tool")

	var events []Event
	for ev := range eventsCh {
		events = append(events, ev)
	}

	var sawToolCall, sawToolComplete bool
	for _, ev := range events {
		switch ev.Type {
		case EventTypeToolCall:
			sawToolCall = true
			require.Same(t, tool, ev.Tool)
			require.NotNil(t, ev.ToolCall)
			require.Equal(t, toolCall.CallID, ev.ToolCall.CallID)
		case EventTypeToolComplete:
			sawToolComplete = true
			require.Same(t, tool, ev.Tool)
			if ev.ToolResult == nil || ev.ToolResult.Result != "OK" {
				t.Fatalf("unexpected tool result event: %+v", ev)
			}
		}
	}

	if !sawToolCall {
		t.Fatalf("expected to observe tool call event")
	}
	if !sawToolComplete {
		t.Fatalf("expected to observe tool completion event")
	}

	if len(tool.calls) != 1 {
		t.Fatalf("tool run count = %d, want 1", len(tool.calls))
	}
	if tool.calls[0].CallID != toolCall.CallID {
		t.Fatalf("tool called with wrong call id: got %s want %s", tool.calls[0].CallID, toolCall.CallID)
	}

	if len(conv.toolResults) != 1 {
		t.Fatalf("tool results appended %d times, want 1", len(conv.toolResults))
	}
	if conv.toolResults[0][0].Result != "OK" {
		t.Fatalf("tool result stored incorrectly: %+v", conv.toolResults[0][0])
	}

	usage := a.TokenUsage()
	if usage.TotalOutputTokens != (turnTool.Usage.TotalOutputTokens + turnFinal.Usage.TotalOutputTokens) {
		t.Fatalf("unexpected total output tokens: %+v", usage)
	}

	turns := a.Turns()
	if got, want := len(turns), 5; got != want {
		t.Fatalf("turn count = %d want %d", got, want)
	}
	// Turns: system, user (message), assistant tool call, user tool result, assistant final.
	if turns[2].FinishReason != llmstream.FinishReasonToolUse {
		t.Fatalf("turn[2] finish reason = %s, want tool_use", turns[2].FinishReason)
	}
	if turns[4].FinishReason != llmstream.FinishReasonEndTurn {
		t.Fatalf("turn[4] finish reason = %s, want end_turn", turns[4].FinishReason)
	}
}

func TestEmitToolOutputNoopsWithoutActiveToolRun(t *testing.T) {
	var nilCtx context.Context
	require.NotPanics(t, func() {
		EmitToolOutput(nilCtx, "ignored")
		EmitToolOutput(context.Background(), "ignored")
	})
}

func TestEmitToolOutputFromToolRunEmitsDisplayOnlyEvent(t *testing.T) {
	systemPrompt := "You are helpful."

	toolCall := llmstream.ToolCall{
		ProviderID: "tool-1",
		CallID:     "call_output",
		Name:       "stream_tool",
		Type:       "function_call",
		Input:      `{}`,
	}
	turnTool := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{toolCall},
		FinishReason: llmstream.FinishReasonToolUse,
	}
	finalText := llmstream.TextContent{ProviderID: "text-2", Content: "Done"}
	turnFinal := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{finalText},
		FinishReason: llmstream.FinishReasonEndTurn,
	}

	conv := newScriptedConversation(systemPrompt,
		&sendScript{
			events: []llmstream.Event{
				{Type: llmstream.EventTypeToolUse, ToolCall: &toolCall},
				{Type: llmstream.EventTypeCompletedSuccess, Turn: &turnTool},
			},
		},
		&sendScript{
			events: []llmstream.Event{
				{Type: llmstream.EventTypeTextDelta, Text: &finalText, Delta: finalText.Content, Done: true},
				{Type: llmstream.EventTypeCompletedSuccess, Turn: &turnFinal},
			},
		},
	)
	overrideConversation(t, conv)

	var capturedCtx context.Context
	tool := &funcTool{name: "stream_tool"}
	tool.runFn = func(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
		capturedCtx = ctx
		EmitToolOutput(context.Background(), "ignored")
		EmitToolOutput(ctx, "visible output")
		return llmstream.ToolResult{
			CallID: call.CallID,
			Name:   call.Name,
			Type:   call.Type,
			Result: "llm output",
		}
	}

	a, err := New(systemPrompt, []llmstream.Tool{tool}, NewOptions{Model: llmmodel.ModelID("model")})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var events []Event
	for ev := range a.SendUserMessage(ctx, "Use the tool") {
		events = append(events, ev)
	}

	toolCallIndex := -1
	toolOutputIndex := -1
	toolCompleteIndex := -1
	var outputEvents []Event
	for i, ev := range events {
		switch ev.Type {
		case EventTypeToolCall:
			toolCallIndex = i
		case EventTypeToolOutput:
			toolOutputIndex = i
			outputEvents = append(outputEvents, ev)
		case EventTypeToolComplete:
			toolCompleteIndex = i
		}
	}

	require.NotEqual(t, -1, toolCallIndex)
	require.NotEqual(t, -1, toolOutputIndex)
	require.NotEqual(t, -1, toolCompleteIndex)
	require.Less(t, toolCallIndex, toolOutputIndex)
	require.Less(t, toolOutputIndex, toolCompleteIndex)
	require.Len(t, outputEvents, 1)

	output := outputEvents[0]
	require.Same(t, tool, output.Tool)
	require.NotNil(t, output.ToolCall)
	require.Equal(t, toolCall.CallID, output.ToolCall.CallID)
	require.Equal(t, toolCall.Name, output.ToolCall.Name)
	require.Equal(t, "visible output", output.ToolOutput.Content)
	require.Equal(t, a.agentID, output.Agent.ID)
	require.Equal(t, 0, output.Agent.Depth)
	require.Empty(t, output.Agent.Parent)

	turns := a.Turns()
	require.Len(t, turns, 5)
	require.Equal(t, llmstream.RoleUser, turns[3].Role)
	require.Len(t, turns[3].Parts, 1)
	result, ok := turns[3].Parts[0].(llmstream.ToolResult)
	require.True(t, ok)
	require.Equal(t, "llm output", result.Result)
	require.NotContains(t, result.Result, "visible output")

	require.NotPanics(t, func() {
		EmitToolOutput(capturedCtx, "late ignored")
	})
}

func TestSendUserMessageUnknownToolEventsHaveNilTool(t *testing.T) {
	systemPrompt := "You are helpful."

	toolCall := llmstream.ToolCall{
		ProviderID: "tool-1",
		CallID:     "call_unknown",
		Name:       "missing_tool",
		Type:       "function_call",
		Input:      `{}`,
	}
	turnTool := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{toolCall},
		FinishReason: llmstream.FinishReasonToolUse,
	}
	finalText := llmstream.TextContent{ProviderID: "text-2", Content: "Done"}
	turnFinal := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{finalText},
		FinishReason: llmstream.FinishReasonEndTurn,
	}

	conv := newScriptedConversation(systemPrompt,
		&sendScript{
			events: []llmstream.Event{
				{Type: llmstream.EventTypeToolUse, ToolCall: &toolCall},
				{Type: llmstream.EventTypeCompletedSuccess, Turn: &turnTool},
			},
		},
		&sendScript{
			events: []llmstream.Event{
				{Type: llmstream.EventTypeTextDelta, Text: &finalText, Delta: finalText.Content, Done: true},
				{Type: llmstream.EventTypeCompletedSuccess, Turn: &turnFinal},
			},
		},
	)
	overrideConversation(t, conv)

	a, err := New(systemPrompt, nil, NewOptions{Model: llmmodel.ModelID("model")})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var toolEvents []Event
	for ev := range a.SendUserMessage(ctx, "Use the tool") {
		if ev.Type == EventTypeToolCall || ev.Type == EventTypeToolComplete {
			toolEvents = append(toolEvents, ev)
		}
	}

	require.Len(t, toolEvents, 2)
	require.Nil(t, toolEvents[0].Tool)
	require.Equal(t, EventTypeToolCall, toolEvents[0].Type)
	require.NotNil(t, toolEvents[0].ToolCall)
	require.Equal(t, toolCall.Name, toolEvents[0].ToolCall.Name)

	require.Nil(t, toolEvents[1].Tool)
	require.Equal(t, EventTypeToolComplete, toolEvents[1].Type)
	require.NotNil(t, toolEvents[1].ToolResult)
	require.True(t, toolEvents[1].ToolResult.IsError)
	require.Equal(t, "unknown tool", toolEvents[1].ToolResult.Result)
}

func TestSendUserMessageRootEventMetadata(t *testing.T) {
	systemPrompt := "You are helpful."

	textContent := llmstream.TextContent{ProviderID: "text-1", Content: "Hello"}
	assistantTurn := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{textContent},
		FinishReason: llmstream.FinishReasonEndTurn,
	}

	conv := newScriptedConversation(systemPrompt, &sendScript{
		events: []llmstream.Event{
			{Type: llmstream.EventTypeTextDelta, Text: &textContent, Delta: textContent.Content, Done: true},
			{Type: llmstream.EventTypeCompletedSuccess, Turn: &assistantTurn},
		},
	})
	overrideConversation(t, conv)

	a, err := New(systemPrompt, nil, NewOptions{Model: llmmodel.ModelID("model")})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for ev := range a.SendUserMessage(ctx, "Say hello") {
		require.NotEqual(t, EventTypeStartSubagent, ev.Type)
		require.Equal(t, a.agentID, ev.Agent.ID)
		require.Equal(t, 0, ev.Agent.Depth)
		require.Empty(t, ev.Agent.Parent)
	}
}

func TestNewDefaultsToPackageDefaultModel(t *testing.T) {
	systemPrompt := "You are helpful."
	conv := newScriptedConversation(systemPrompt)
	overrideConversation(t, conv)

	a, err := New(systemPrompt, nil)
	require.NoError(t, err)
	require.Equal(t, llmmodel.ModelIDOrFallback(llmmodel.ModelIDUnknown), a.model)
}

func TestContextUsagePercentTracksTurnUsage(t *testing.T) {
	model := llmmodel.DefaultModel
	info := llmmodel.GetModelInfo(model)
	if info.ContextWindow <= 0 {
		t.Fatalf("model %q missing context window", model)
	}

	used := info.ContextWindow / 2
	if used == 0 {
		t.Fatalf("context window too small for test: %d", info.ContextWindow)
	}

	usage := llmstream.TokenUsage{TotalInputTokens: used}
	agent := runContextUsageAgent(t, model, usage)

	got := agent.ContextUsagePercent()
	want := roundPercentFloat(float64(used), float64(info.ContextWindow))
	if got != want {
		t.Fatalf("ContextUsagePercent = %d, want %d", got, want)
	}
}

func TestContextUsagePercentIncludesCachedTokens(t *testing.T) {
	model := llmmodel.DefaultModel
	info := llmmodel.GetModelInfo(model)
	if info.ContextWindow <= 0 {
		t.Fatalf("model %q missing context window", model)
	}

	total := info.ContextWindow / 4
	if total == 0 {
		total = 1
	}
	cached := info.ContextWindow / 2
	if cached <= total {
		cached = total + 1
	}

	usage := llmstream.TokenUsage{
		TotalInputTokens:  total,
		CachedInputTokens: cached,
	}
	agent := runContextUsageAgent(t, model, usage)

	got := agent.ContextUsagePercent()
	used := total + cached
	want := roundPercentFloat(float64(used), float64(info.ContextWindow))
	if got != want {
		t.Fatalf("ContextUsagePercent = %d, want %d (total=%d cached=%d)", got, want, total, cached)
	}
}

func TestContextUsagePercentClampsToFull(t *testing.T) {
	model := llmmodel.DefaultModel
	info := llmmodel.GetModelInfo(model)
	if info.ContextWindow <= 0 {
		t.Fatalf("model %q missing context window", model)
	}

	usage := llmstream.TokenUsage{TotalInputTokens: info.ContextWindow * 2}
	agent := runContextUsageAgent(t, model, usage)

	if got := agent.ContextUsagePercent(); got != 100 {
		t.Fatalf("ContextUsagePercent = %d, want 100", got)
	}
}

func TestContextUsagePercentUnknownModel(t *testing.T) {
	usage := llmstream.TokenUsage{TotalInputTokens: 50_000}
	agent := runContextUsageAgent(t, llmmodel.ModelID("unknown-model"), usage)

	if got := agent.ContextUsagePercent(); got != 0 {
		t.Fatalf("ContextUsagePercent = %d, want 0 for unknown model", got)
	}
}

func TestConcurrentSendRejected(t *testing.T) {
	systemPrompt := "system"

	wait := make(chan struct{})
	script := &sendScript{wait: wait}
	conv := newScriptedConversation(systemPrompt, script)
	overrideConversation(t, conv)

	a, err := New(systemPrompt, nil, NewOptions{Model: llmmodel.ModelID("model")})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	events1 := a.SendUserMessage(ctx, "First")

	if err := a.AddUserTurn("should fail"); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("AddUserTurn error = %v, want ErrAlreadyRunning", err)
	}

	events2 := a.SendUserMessage(context.Background(), "Second")
	ev, ok := <-events2
	if !ok {
		t.Fatalf("second send channel closed unexpectedly")
	}
	if ev.Type != EventTypeError || !errors.Is(ev.Error, ErrAlreadyRunning) {
		t.Fatalf("second send event = %+v, want error ErrAlreadyRunning", ev)
	}
	if _, more := <-events2; more {
		t.Fatalf("second send channel not closed after error")
	}

	cancel()

	select {
	case ev, ok := <-events1:
		if ok && ev.Type != EventTypeCanceled {
			t.Fatalf("unexpected first send event after cancel: %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for first send cancellation")
	}
	// Drain any remaining events.
	for range events1 {
	}

	if status := a.Status(); status != StatusIdle {
		t.Fatalf("status after cancellation = %v, want idle", status)
	}
}

func TestQueueUserMessageAfterEndTurnContinuesConversation(t *testing.T) {
	systemPrompt := "system"
	injected := "follow up"
	wait := make(chan struct{})

	turn1Text := llmstream.TextContent{ProviderID: "a1", Content: "first"}
	turn1 := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{turn1Text},
		FinishReason: llmstream.FinishReasonEndTurn,
	}
	turn2Text := llmstream.TextContent{ProviderID: "a2", Content: "second"}
	turn2 := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{turn2Text},
		FinishReason: llmstream.FinishReasonEndTurn,
	}

	conv := newScriptedConversation(systemPrompt,
		&sendScript{
			wait: wait,
			events: []llmstream.Event{
				{Type: llmstream.EventTypeTextDelta, Text: &turn1Text, Delta: turn1Text.Content, Done: true},
				{Type: llmstream.EventTypeCompletedSuccess, Turn: &turn1},
			},
		},
		&sendScript{
			events: []llmstream.Event{
				{Type: llmstream.EventTypeTextDelta, Text: &turn2Text, Delta: turn2Text.Content, Done: true},
				{Type: llmstream.EventTypeCompletedSuccess, Turn: &turn2},
			},
		},
	)
	overrideConversation(t, conv)

	a, err := New(systemPrompt, nil, NewOptions{Model: llmmodel.ModelID("model")})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := a.SendUserMessage(ctx, "start")
	require.Equal(t, StatusRunning, a.Status())
	require.NoError(t, a.QueueUserMessage(injected))
	close(wait)

	var events []Event
	for ev := range out {
		events = append(events, ev)
	}

	var doneCount int
	var turnCompleteCount int
	var queuedCount int
	var sentCount int
	queuedIndex := -1
	sentIndex := -1
	for i, ev := range events {
		switch ev.Type {
		case EventTypeDoneSuccess:
			doneCount++
		case EventTypeUserMessageQueued:
			queuedCount++
			if queuedIndex == -1 {
				queuedIndex = i
			}
			require.Equal(t, injected, ev.UserMessage)
		case EventTypeQueuedUserMessageSent:
			sentCount++
			if sentIndex == -1 {
				sentIndex = i
			}
			require.Equal(t, injected, ev.UserMessage)
		case EventTypeAssistantTurnComplete:
			turnCompleteCount++
		}
	}
	require.Equal(t, 1, doneCount)
	require.Equal(t, 2, turnCompleteCount)
	require.Equal(t, 1, queuedCount)
	require.Equal(t, 1, sentCount)
	require.Less(t, queuedIndex, sentIndex)

	turns := a.Turns()
	require.Len(t, turns, 5) // system, user(start), assistant(first), user(injected), assistant(second)
	require.Equal(t, llmstream.RoleUser, turns[3].Role)
	require.Len(t, turns[3].Parts, 1)
	userText, ok := turns[3].Parts[0].(llmstream.TextContent)
	require.True(t, ok)
	require.Equal(t, injected, userText.Content)
	require.Equal(t, StatusIdle, a.Status())
}

func TestQueueUserMessageAfterToolResults(t *testing.T) {
	systemPrompt := "system"
	toolStarted := make(chan struct{})
	releaseTool := make(chan struct{})

	toolCall := llmstream.ToolCall{
		ProviderID: "tool-1",
		CallID:     "call_123",
		Name:       "slow_tool",
		Type:       "function_call",
		Input:      `{}`,
	}
	turnTool := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{toolCall},
		FinishReason: llmstream.FinishReasonToolUse,
	}
	finalText := llmstream.TextContent{ProviderID: "final", Content: "done"}
	finalTurn := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{finalText},
		FinishReason: llmstream.FinishReasonEndTurn,
	}
	conv := newScriptedConversation(systemPrompt,
		&sendScript{
			events: []llmstream.Event{
				{Type: llmstream.EventTypeToolUse, ToolCall: &toolCall},
				{Type: llmstream.EventTypeCompletedSuccess, Turn: &turnTool},
			},
		},
		&sendScript{
			events: []llmstream.Event{
				{Type: llmstream.EventTypeTextDelta, Text: &finalText, Delta: finalText.Content, Done: true},
				{Type: llmstream.EventTypeCompletedSuccess, Turn: &finalTurn},
			},
		},
	)
	overrideConversation(t, conv)

	tool := &funcTool{name: "slow_tool"}
	tool.runFn = func(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
		close(toolStarted)
		select {
		case <-ctx.Done():
			return llmstream.NewErrorToolResult(ctx.Err().Error(), call)
		case <-releaseTool:
		}
		return llmstream.ToolResult{
			CallID: call.CallID,
			Name:   call.Name,
			Type:   call.Type,
			Result: "ok",
		}
	}

	a, err := New(systemPrompt, []llmstream.Tool{tool}, NewOptions{Model: llmmodel.ModelID("model")})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := a.SendUserMessage(ctx, "start tool flow")

	select {
	case <-toolStarted:
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for tool to start")
	}
	require.NoError(t, a.QueueUserMessage("after tool"))
	close(releaseTool)

	var events []Event
	for ev := range out {
		events = append(events, ev)
	}

	var doneCount int
	var queuedCount int
	var sentCount int
	for _, ev := range events {
		switch ev.Type {
		case EventTypeDoneSuccess:
			doneCount++
		case EventTypeUserMessageQueued:
			queuedCount++
			require.Equal(t, "after tool", ev.UserMessage)
		case EventTypeQueuedUserMessageSent:
			sentCount++
			require.Equal(t, "after tool", ev.UserMessage)
		}
	}
	require.Equal(t, 1, doneCount)
	require.Equal(t, 1, queuedCount)
	require.Equal(t, 1, sentCount)

	turns := a.Turns()
	require.Len(t, turns, 6)
	require.Equal(t, llmstream.RoleAssistant, turns[2].Role)
	require.Equal(t, llmstream.FinishReasonToolUse, turns[2].FinishReason)

	// Ensure the injected user message comes after the tool results turn.
	require.Equal(t, llmstream.RoleUser, turns[3].Role)
	require.Len(t, turns[3].Parts, 1)
	_, ok := turns[3].Parts[0].(llmstream.ToolResult)
	require.True(t, ok)

	require.Equal(t, llmstream.RoleUser, turns[4].Role)
	require.Len(t, turns[4].Parts, 1)
	userText, ok := turns[4].Parts[0].(llmstream.TextContent)
	require.True(t, ok)
	require.Equal(t, "after tool", userText.Content)
	require.Equal(t, StatusIdle, a.Status())
}

func TestTurnsReturnsCopy(t *testing.T) {
	systemPrompt := "sys"
	conv := newScriptedConversation(systemPrompt)
	overrideConversation(t, conv)

	a, err := New(systemPrompt, nil, NewOptions{Model: llmmodel.ModelID("model")})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	turns := a.Turns()
	if len(turns) != 1 {
		t.Fatalf("expected one turn, got %d", len(turns))
	}
	if tc, ok := turns[0].Parts[0].(llmstream.TextContent); ok {
		tc.Content = "mutated"
		turns[0].Parts[0] = tc
	}

	turns2 := a.Turns()
	if tc, ok := turns2[0].Parts[0].(llmstream.TextContent); ok {
		if tc.Content != systemPrompt {
			t.Fatalf("turns array shares state with agent (content=%q)", tc.Content)
		}
	}
}

func TestSubAgentMirrorsEventsAndUsage(t *testing.T) {
	systemPrompt := "Root system"
	subPrompt := "Sub system"
	subLabel := "Explore package metadata"
	baseModel := llmmodel.ModelIDOrFallback(llmmodel.ModelIDUnknown)

	toolCall := llmstream.ToolCall{
		ProviderID: "tool-1",
		CallID:     "call_123",
		Name:       "explore",
		Type:       "function_call",
		Input:      `{"query":"investigate"}`,
	}
	turnTool := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{toolCall},
		FinishReason: llmstream.FinishReasonToolUse,
		Usage: llmstream.TokenUsage{
			TotalInputTokens:  3,
			TotalOutputTokens: 1,
		},
	}
	finalText := llmstream.TextContent{ProviderID: "root-text", Content: "Completed exploration"}
	turnFinal := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{finalText},
		FinishReason: llmstream.FinishReasonEndTurn,
		Usage: llmstream.TokenUsage{
			TotalInputTokens:  4,
			TotalOutputTokens: 2,
		},
	}
	rootScriptTool := &sendScript{
		events: []llmstream.Event{
			{Type: llmstream.EventTypeToolUse, ToolCall: &toolCall},
			{Type: llmstream.EventTypeCompletedSuccess, Turn: &turnTool},
		},
	}
	rootScriptFinal := &sendScript{
		events: []llmstream.Event{
			{Type: llmstream.EventTypeTextDelta, Text: &finalText, Delta: finalText.Content, Done: true},
			{Type: llmstream.EventTypeCompletedSuccess, Turn: &turnFinal},
		},
	}
	rootConv := newScriptedConversation(systemPrompt, rootScriptTool, rootScriptFinal)

	subText := llmstream.TextContent{ProviderID: "sub-text", Content: "Found additional details"}
	subTurn := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{subText},
		FinishReason: llmstream.FinishReasonEndTurn,
		Usage: llmstream.TokenUsage{
			TotalInputTokens:  6,
			TotalOutputTokens: 5,
		},
	}
	subScript := &sendScript{
		events: []llmstream.Event{
			{Type: llmstream.EventTypeTextDelta, Text: &subText, Delta: subText.Content, Done: true},
			{Type: llmstream.EventTypeCompletedSuccess, Turn: &subTurn},
		},
	}
	subConv := newScriptedConversation(subPrompt, subScript)

	prev := newConversation
	convs := []llmstream.StreamingConversation{rootConv, subConv}
	newConversation = func(model llmmodel.ModelID, systemPrompt string) llmstream.StreamingConversation {
		if len(convs) == 0 {
			return nil
		}
		conv := convs[0]
		convs = convs[1:]
		return conv
	}
	t.Cleanup(func() {
		newConversation = prev
	})

	var agentRef *Agent
	var subEvents []Event
	var createdSubAgent *Agent
	var toolsCopy []llmstream.Tool

	tool := &funcTool{name: "explore"}
	tool.runFn = func(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
		if got := SubAgentDepth(ctx); got != 0 {
			t.Fatalf("root SubAgentDepth = %d, want 0", got)
		}
		toolsCopy = AgentToolsFromContext(ctx)
		if len(toolsCopy) != 1 || toolsCopy[0] == nil || toolsCopy[0].Name() != "explore" {
			t.Fatalf("AgentToolsFromContext returned unexpected tools: %+v", toolsCopy)
		}
		toolsCopy[0] = nil

		creator := SubAgentCreatorFromContext(ctx)
		subAgent, err := creator.New(subPrompt, nil, NewOptions{SubagentLabel: subLabel})
		if err != nil {
			t.Fatalf("creating sub agent: %v", err)
		}
		createdSubAgent = subAgent

		if agentRef == nil {
			t.Fatalf("agent reference not set")
		}
		if subAgent.parent != agentRef {
			t.Fatalf("sub agent parent mismatch")
		}
		if subAgent.depth != agentRef.depth+1 {
			t.Fatalf("sub agent depth = %d, want %d", subAgent.depth, agentRef.depth+1)
		}
		if subAgent.sessionID != agentRef.sessionID {
			t.Fatalf("sub agent session id = %q, want %q", subAgent.sessionID, agentRef.sessionID)
		}
		if subAgent.model != agentRef.model {
			t.Fatalf("sub agent model mismatch")
		}

		subCh := subAgent.SendUserMessage(ctx, "Run deeper search")
		for ev := range subCh {
			subEvents = append(subEvents, ev)
		}

		return llmstream.ToolResult{
			CallID: call.CallID,
			Name:   call.Name,
			Type:   call.Type,
			Result: "ok",
		}
	}

	a, err := New(systemPrompt, []llmstream.Tool{tool}, NewOptions{Model: baseModel})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	agentRef = a

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventsCh := a.SendUserMessage(ctx, "Trigger sub agent")

	var events []Event
	for ev := range eventsCh {
		events = append(events, ev)
	}

	if createdSubAgent == nil {
		t.Fatalf("sub agent was not created")
	}

	if len(subEvents) == 0 {
		t.Fatalf("sub agent produced no events")
	}
	require.Equal(t, EventTypeStartSubagent, subEvents[0].Type)
	require.Equal(t, StartSubagent{
		CallingAgentID: a.agentID,
		ToolCallID:     toolCall.CallID,
		Label:          subLabel,
	}, subEvents[0].StartSubagent)

	for _, ev := range subEvents {
		if ev.Agent.Depth != 1 {
			t.Fatalf("sub event depth = %d, want 1", ev.Agent.Depth)
		}
		if ev.Agent.ID == "" {
			t.Fatalf("sub event missing agent id")
		}
		if ev.Agent.Parent != a.agentID {
			t.Fatalf("sub event parent = %q, want %q", ev.Agent.Parent, a.agentID)
		}
	}

	var mirrored int
	firstMirroredSubEvent := -1
	for i, ev := range events {
		switch ev.Agent.Depth {
		case 0:
			if ev.Agent.ID == "" {
				t.Fatalf("root event missing agent id")
			}
			if ev.Agent.Parent != "" {
				t.Fatalf("root event parent = %q, want empty", ev.Agent.Parent)
			}
		case 1:
			if ev.Agent.Parent != a.agentID {
				t.Fatalf("mirrored sub event parent = %q, want %q", ev.Agent.Parent, a.agentID)
			}
			if firstMirroredSubEvent == -1 {
				firstMirroredSubEvent = i
			}
			mirrored++
		default:
			t.Fatalf("unexpected agent depth %d in root stream", ev.Agent.Depth)
		}
	}
	if mirrored != len(subEvents) {
		t.Fatalf("mirrored events = %d, want %d", mirrored, len(subEvents))
	}
	require.NotEqual(t, -1, firstMirroredSubEvent)
	require.Equal(t, EventTypeStartSubagent, events[firstMirroredSubEvent].Type)
	require.Equal(t, StartSubagent{
		CallingAgentID: a.agentID,
		ToolCallID:     toolCall.CallID,
		Label:          subLabel,
	}, events[firstMirroredSubEvent].StartSubagent)

	expectedInput := turnTool.Usage.TotalInputTokens + turnFinal.Usage.TotalInputTokens + subTurn.Usage.TotalInputTokens
	expectedOutput := turnTool.Usage.TotalOutputTokens + turnFinal.Usage.TotalOutputTokens + subTurn.Usage.TotalOutputTokens

	usage := a.TokenUsage()
	if usage.TotalInputTokens != expectedInput || usage.TotalOutputTokens != expectedOutput {
		t.Fatalf("root usage = %+v, want input %d output %d", usage, expectedInput, expectedOutput)
	}

	subUsage := createdSubAgent.TokenUsage()
	if subUsage.TotalInputTokens != subTurn.Usage.TotalInputTokens || subUsage.TotalOutputTokens != subTurn.Usage.TotalOutputTokens {
		t.Fatalf("sub agent usage = %+v, want %+v", subUsage, subTurn.Usage)
	}

	if createdSubAgent.Status() != StatusIdle {
		t.Fatalf("sub agent status = %v, want idle", createdSubAgent.Status())
	}

	rootTurns := a.Turns()
	if len(rootTurns) != 5 {
		t.Fatalf("root turn count = %d, want 5", len(rootTurns))
	}
	subTurns := createdSubAgent.Turns()
	if len(subTurns) != 3 {
		t.Fatalf("sub agent turn count = %d, want 3", len(subTurns))
	}

	if len(a.toolList) != 1 || a.toolList[0] == nil {
		t.Fatalf("parent tool list unexpectedly mutated: %+v", a.toolList)
	}
}

func TestSubAgentStartEventOnlyOncePerSubagent(t *testing.T) {
	rootPrompt := "Root system"
	subPrompt := "Sub system"
	rootModel := llmmodel.ModelIDOrFallback(llmmodel.ModelIDUnknown)

	toolCall := llmstream.ToolCall{
		ProviderID: "tool-1",
		CallID:     "call_789",
		Name:       "explore",
		Type:       "function_call",
		Input:      "{}",
	}
	rootToolTurn := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{toolCall},
		FinishReason: llmstream.FinishReasonToolUse,
	}
	rootFinalText := llmstream.TextContent{ProviderID: "root-final", Content: "done"}
	rootFinalTurn := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{rootFinalText},
		FinishReason: llmstream.FinishReasonEndTurn,
	}
	rootConv := newScriptedConversation(rootPrompt,
		&sendScript{
			events: []llmstream.Event{
				{Type: llmstream.EventTypeToolUse, ToolCall: &toolCall},
				{Type: llmstream.EventTypeCompletedSuccess, Turn: &rootToolTurn},
			},
		},
		&sendScript{
			events: []llmstream.Event{
				{Type: llmstream.EventTypeTextDelta, Text: &rootFinalText, Delta: rootFinalText.Content, Done: true},
				{Type: llmstream.EventTypeCompletedSuccess, Turn: &rootFinalTurn},
			},
		},
	)

	firstSubText := llmstream.TextContent{ProviderID: "sub-1", Content: "first"}
	firstSubTurn := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{firstSubText},
		FinishReason: llmstream.FinishReasonEndTurn,
	}
	secondSubText := llmstream.TextContent{ProviderID: "sub-2", Content: "second"}
	secondSubTurn := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{secondSubText},
		FinishReason: llmstream.FinishReasonEndTurn,
	}
	subConv := newScriptedConversation(subPrompt,
		&sendScript{
			events: []llmstream.Event{
				{Type: llmstream.EventTypeTextDelta, Text: &firstSubText, Delta: firstSubText.Content, Done: true},
				{Type: llmstream.EventTypeCompletedSuccess, Turn: &firstSubTurn},
			},
		},
		&sendScript{
			events: []llmstream.Event{
				{Type: llmstream.EventTypeTextDelta, Text: &secondSubText, Delta: secondSubText.Content, Done: true},
				{Type: llmstream.EventTypeCompletedSuccess, Turn: &secondSubTurn},
			},
		},
	)

	prev := newConversation
	convs := []llmstream.StreamingConversation{rootConv, subConv}
	newConversation = func(model llmmodel.ModelID, systemPrompt string) llmstream.StreamingConversation {
		if len(convs) == 0 {
			return nil
		}
		conv := convs[0]
		convs = convs[1:]
		return conv
	}
	t.Cleanup(func() {
		newConversation = prev
	})

	var firstSubEvents []Event
	var secondSubEvents []Event

	tool := &funcTool{name: "explore"}
	tool.runFn = func(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
		creator := SubAgentCreatorFromContext(ctx)
		subAgent, err := creator.New(subPrompt, nil)
		require.NoError(t, err)

		require.NoError(t, subAgent.AddUserTurn("context only"))

		for ev := range subAgent.SendUserMessage(ctx, "first request") {
			firstSubEvents = append(firstSubEvents, ev)
		}
		for ev := range subAgent.SendUserMessage(ctx, "second request") {
			secondSubEvents = append(secondSubEvents, ev)
		}

		return llmstream.ToolResult{
			CallID: call.CallID,
			Name:   call.Name,
			Type:   call.Type,
			Result: "ok",
		}
	}

	rootAgent, err := New(rootPrompt, []llmstream.Tool{tool}, NewOptions{Model: rootModel})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var rootEvents []Event
	for ev := range rootAgent.SendUserMessage(ctx, "start") {
		rootEvents = append(rootEvents, ev)
	}

	require.NotEmpty(t, firstSubEvents)
	require.Equal(t, EventTypeStartSubagent, firstSubEvents[0].Type)
	for _, ev := range secondSubEvents {
		require.NotEqual(t, EventTypeStartSubagent, ev.Type)
	}

	var startEvents []Event
	for _, ev := range rootEvents {
		if ev.Type == EventTypeStartSubagent {
			startEvents = append(startEvents, ev)
		}
	}
	require.Len(t, startEvents, 1)
	require.Equal(t, toolCall.CallID, startEvents[0].StartSubagent.ToolCallID)
	require.Equal(t, rootAgent.agentID, startEvents[0].StartSubagent.CallingAgentID)
}

func TestSubAgentConstructionWithoutSendDoesNotEmitStartEvent(t *testing.T) {
	rootPrompt := "Root system"
	subPrompt := "Sub system"
	rootModel := llmmodel.ModelIDOrFallback(llmmodel.ModelIDUnknown)

	toolCall := llmstream.ToolCall{
		ProviderID: "tool-1",
		CallID:     "call_unused",
		Name:       "explore",
		Type:       "function_call",
		Input:      "{}",
	}
	rootToolTurn := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{toolCall},
		FinishReason: llmstream.FinishReasonToolUse,
	}
	rootFinalText := llmstream.TextContent{ProviderID: "root-final", Content: "done"}
	rootFinalTurn := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{rootFinalText},
		FinishReason: llmstream.FinishReasonEndTurn,
	}
	rootConv := newScriptedConversation(rootPrompt,
		&sendScript{
			events: []llmstream.Event{
				{Type: llmstream.EventTypeToolUse, ToolCall: &toolCall},
				{Type: llmstream.EventTypeCompletedSuccess, Turn: &rootToolTurn},
			},
		},
		&sendScript{
			events: []llmstream.Event{
				{Type: llmstream.EventTypeTextDelta, Text: &rootFinalText, Delta: rootFinalText.Content, Done: true},
				{Type: llmstream.EventTypeCompletedSuccess, Turn: &rootFinalTurn},
			},
		},
	)
	subConv := newScriptedConversation(subPrompt)

	prev := newConversation
	convs := []llmstream.StreamingConversation{rootConv, subConv}
	newConversation = func(model llmmodel.ModelID, systemPrompt string) llmstream.StreamingConversation {
		if len(convs) == 0 {
			return nil
		}
		conv := convs[0]
		convs = convs[1:]
		return conv
	}
	t.Cleanup(func() {
		newConversation = prev
	})

	tool := &funcTool{name: "explore"}
	tool.runFn = func(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
		creator := SubAgentCreatorFromContext(ctx)
		subAgent, err := creator.New(subPrompt, nil, NewOptions{SubagentLabel: "unused"})
		require.NoError(t, err)
		require.NoError(t, subAgent.AddUserTurn("prefill only"))

		return llmstream.ToolResult{
			CallID: call.CallID,
			Name:   call.Name,
			Type:   call.Type,
			Result: "ok",
		}
	}

	rootAgent, err := New(rootPrompt, []llmstream.Tool{tool}, NewOptions{Model: rootModel})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for ev := range rootAgent.SendUserMessage(ctx, "start") {
		require.NotEqual(t, EventTypeStartSubagent, ev.Type)
	}
}

func TestSubAgentCanceledBeforeToolCompleteWhenToolReturns(t *testing.T) {
	rootPrompt := "Root system"
	subPrompt := "Sub system"
	rootModel := llmmodel.ModelIDOrFallback(llmmodel.ModelIDUnknown)

	toolCall := llmstream.ToolCall{
		ProviderID: "tool-1",
		CallID:     "call_cancel_subagent",
		Name:       "explore",
		Type:       "function_call",
		Input:      "{}",
	}
	rootToolTurn := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{toolCall},
		FinishReason: llmstream.FinishReasonToolUse,
	}
	rootFinalText := llmstream.TextContent{ProviderID: "root-final", Content: "done"}
	rootFinalTurn := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{rootFinalText},
		FinishReason: llmstream.FinishReasonEndTurn,
	}
	rootConv := newScriptedConversation(rootPrompt,
		&sendScript{
			events: []llmstream.Event{
				{Type: llmstream.EventTypeToolUse, ToolCall: &toolCall},
				{Type: llmstream.EventTypeCompletedSuccess, Turn: &rootToolTurn},
			},
		},
		&sendScript{
			events: []llmstream.Event{
				{Type: llmstream.EventTypeTextDelta, Text: &rootFinalText, Delta: rootFinalText.Content, Done: true},
				{Type: llmstream.EventTypeCompletedSuccess, Turn: &rootFinalTurn},
			},
		},
	)

	subCanceled := make(chan struct{})
	allowSubExit := make(chan struct{})
	subConv := newScriptedConversation(subPrompt,
		&sendScript{
			wait:             make(chan struct{}),
			afterCancel:      subCanceled,
			blockAfterCancel: allowSubExit,
		},
	)

	prev := newConversation
	convs := []llmstream.StreamingConversation{rootConv, subConv}
	newConversation = func(model llmmodel.ModelID, systemPrompt string) llmstream.StreamingConversation {
		if len(convs) == 0 {
			return nil
		}
		conv := convs[0]
		convs = convs[1:]
		return conv
	}
	t.Cleanup(func() {
		newConversation = prev
	})

	var createdSubAgent *Agent
	tool := &funcTool{name: "explore"}
	tool.runFn = func(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
		creator := SubAgentCreatorFromContext(ctx)
		subAgent, err := creator.New(subPrompt, nil)
		require.NoError(t, err)
		createdSubAgent = subAgent

		_ = subAgent.SendUserMessage(ctx, "slow abandoned work")
		return llmstream.ToolResult{
			CallID: call.CallID,
			Name:   call.Name,
			Type:   call.Type,
			Result: "ok",
		}
	}

	rootAgent, err := New(rootPrompt, []llmstream.Tool{tool}, NewOptions{Model: rootModel})
	require.NoError(t, err)

	eventsCh := rootAgent.SendUserMessage(context.Background(), "start")
	var events []Event

	waitForEvent := func(match func(Event) bool) Event {
		t.Helper()
		timeout := time.After(time.Second)
		for {
			select {
			case ev, ok := <-eventsCh:
				require.True(t, ok)
				events = append(events, ev)
				if match(ev) {
					return ev
				}
			case <-timeout:
				t.Fatal("timeout waiting for event")
			}
		}
	}

	waitForEvent(func(ev Event) bool {
		return ev.Type == EventTypeStartSubagent
	})

	select {
	case <-subCanceled:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for subagent cancellation")
	}

	noToolComplete := time.NewTimer(50 * time.Millisecond)
	defer noToolComplete.Stop()
	for {
		select {
		case ev, ok := <-eventsCh:
			require.True(t, ok)
			events = append(events, ev)
			if ev.Type == EventTypeToolComplete {
				t.Fatalf("tool completed before subagent exited")
			}
		case <-noToolComplete.C:
			goto allowExit
		}
	}

allowExit:
	close(allowSubExit)

	for {
		select {
		case ev, ok := <-eventsCh:
			if !ok {
				goto done
			}
			events = append(events, ev)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for root stream to close")
		}
	}

done:
	require.NotNil(t, createdSubAgent)
	require.Equal(t, StatusIdle, createdSubAgent.Status())

	canceledIndex := -1
	toolCompleteIndex := -1
	for i, ev := range events {
		if ev.Agent.Depth == 1 && ev.Type == EventTypeCanceled {
			canceledIndex = i
		}
		if ev.Agent.Depth == 0 && ev.Type == EventTypeToolComplete {
			toolCompleteIndex = i
		}
	}
	require.NotEqual(t, -1, canceledIndex)
	require.NotEqual(t, -1, toolCompleteIndex)
	require.Less(t, canceledIndex, toolCompleteIndex)
}

func TestSubAgentCreatorPanicsAfterRun(t *testing.T) {
	systemPrompt := "Root system"

	toolCall := llmstream.ToolCall{
		ProviderID: "tool-1",
		CallID:     "call_456",
		Name:       "explore",
		Type:       "function_call",
		Input:      "{}",
	}
	turnTool := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{toolCall},
		FinishReason: llmstream.FinishReasonToolUse,
		Usage: llmstream.TokenUsage{
			TotalInputTokens:  2,
			TotalOutputTokens: 1,
		},
	}
	finalText := llmstream.TextContent{ProviderID: "root-final", Content: "done"}
	turnFinal := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{finalText},
		FinishReason: llmstream.FinishReasonEndTurn,
		Usage: llmstream.TokenUsage{
			TotalInputTokens:  1,
			TotalOutputTokens: 1,
		},
	}

	scriptTool := &sendScript{
		events: []llmstream.Event{
			{Type: llmstream.EventTypeToolUse, ToolCall: &toolCall},
			{Type: llmstream.EventTypeCompletedSuccess, Turn: &turnTool},
		},
	}
	scriptFinal := &sendScript{
		events: []llmstream.Event{
			{Type: llmstream.EventTypeTextDelta, Text: &finalText, Delta: finalText.Content, Done: true},
			{Type: llmstream.EventTypeCompletedSuccess, Turn: &turnFinal},
		},
	}

	overrideConversation(t, newScriptedConversation(systemPrompt, scriptTool, scriptFinal))

	var captured SubAgentCreator
	tool := &funcTool{name: "explore"}
	tool.runFn = func(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
		if got := SubAgentDepth(ctx); got != 0 {
			t.Fatalf("root SubAgentDepth = %d, want 0", got)
		}
		captured = SubAgentCreatorFromContext(ctx)
		return llmstream.ToolResult{
			CallID: call.CallID,
			Name:   call.Name,
			Type:   call.Type,
			Result: "ok",
		}
	}

	a, err := New(systemPrompt, []llmstream.Tool{tool}, NewOptions{Model: llmmodel.ModelID("model")})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for range a.SendUserMessage(ctx, "call tool") {
	}

	if captured == nil {
		t.Fatalf("SubAgentCreator was not captured")
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic when using sub agent creator after run")
		}
	}()
	_, _ = captured.New("should panic", nil)
}

func TestSubAgentNestedDepth(t *testing.T) {
	rootPrompt := "Root system"
	childPrompt := "Child system"
	grandPrompt := "Grand system"
	rootModel := llmmodel.ModelIDOrFallback(llmmodel.ModelIDUnknown)
	childModel := rootModel

	outerCall := llmstream.ToolCall{
		ProviderID: "outer-tool",
		CallID:     "outer_1",
		Name:       "outer",
		Type:       "function_call",
		Input:      "{}",
	}
	outerTurn := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{outerCall},
		FinishReason: llmstream.FinishReasonToolUse,
		Usage: llmstream.TokenUsage{
			TotalInputTokens:  2,
			TotalOutputTokens: 1,
		},
	}
	rootFinalText := llmstream.TextContent{ProviderID: "root-final", Content: "root done"}
	rootFinalTurn := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{rootFinalText},
		FinishReason: llmstream.FinishReasonEndTurn,
	}
	rootConv := newScriptedConversation(rootPrompt,
		&sendScript{
			events: []llmstream.Event{
				{Type: llmstream.EventTypeToolUse, ToolCall: &outerCall},
				{Type: llmstream.EventTypeCompletedSuccess, Turn: &outerTurn},
			},
		},
		&sendScript{
			events: []llmstream.Event{
				{Type: llmstream.EventTypeTextDelta, Text: &rootFinalText, Delta: rootFinalText.Content, Done: true},
				{Type: llmstream.EventTypeCompletedSuccess, Turn: &rootFinalTurn},
			},
		},
	)

	innerCall := llmstream.ToolCall{
		ProviderID: "inner-tool",
		CallID:     "inner_1",
		Name:       "inner",
		Type:       "function_call",
		Input:      "{}",
	}
	innerTurn := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{innerCall},
		FinishReason: llmstream.FinishReasonToolUse,
	}
	childFinalText := llmstream.TextContent{ProviderID: "child-final", Content: "child done"}
	childFinalTurn := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{childFinalText},
		FinishReason: llmstream.FinishReasonEndTurn,
	}
	childConv := newScriptedConversation(childPrompt,
		&sendScript{
			events: []llmstream.Event{
				{Type: llmstream.EventTypeToolUse, ToolCall: &innerCall},
				{Type: llmstream.EventTypeCompletedSuccess, Turn: &innerTurn},
			},
		},
		&sendScript{
			events: []llmstream.Event{
				{Type: llmstream.EventTypeTextDelta, Text: &childFinalText, Delta: childFinalText.Content, Done: true},
				{Type: llmstream.EventTypeCompletedSuccess, Turn: &childFinalTurn},
			},
		},
	)

	grandText := llmstream.TextContent{ProviderID: "grand-final", Content: "grand done"}
	grandTurn := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{grandText},
		FinishReason: llmstream.FinishReasonEndTurn,
	}
	grandConv := newScriptedConversation(grandPrompt,
		&sendScript{
			events: []llmstream.Event{
				{Type: llmstream.EventTypeTextDelta, Text: &grandText, Delta: grandText.Content, Done: true},
				{Type: llmstream.EventTypeCompletedSuccess, Turn: &grandTurn},
			},
		},
	)

	prev := newConversation
	convs := []llmstream.StreamingConversation{rootConv, childConv, grandConv}
	newConversation = func(model llmmodel.ModelID, systemPrompt string) llmstream.StreamingConversation {
		if len(convs) == 0 {
			return nil
		}
		conv := convs[0]
		convs = convs[1:]
		return conv
	}
	t.Cleanup(func() {
		newConversation = prev
	})

	outerTool := &funcTool{name: "outer"}
	var rootAgent *Agent
	var childAgent *Agent
	var grandAgent *Agent
	var childEvents []Event
	var grandEvents []Event

	outerTool.runFn = func(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
		if got := SubAgentDepth(ctx); got != 0 {
			t.Fatalf("outer depth = %d, want 0", got)
		}
		creator := SubAgentCreatorFromContext(ctx)
		innerTool := &funcTool{name: "inner"}
		child, err := creator.New(childPrompt, []llmstream.Tool{innerTool}, NewOptions{Model: childModel})
		if err != nil {
			t.Fatalf("create child agent: %v", err)
		}
		childAgent = child

		innerTool.runFn = func(innerCtx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
			if got := SubAgentDepth(innerCtx); got != 1 {
				t.Fatalf("inner depth = %d, want 1", got)
			}
			tools := AgentToolsFromContext(innerCtx)
			if len(tools) != 1 || tools[0] == nil || tools[0].Name() != "inner" {
				t.Fatalf("inner AgentToolsFromContext: %+v", tools)
			}
			subCreator := SubAgentCreatorFromContext(innerCtx)
			grand, err := subCreator.New(grandPrompt, nil)
			if err != nil {
				t.Fatalf("create grand agent: %v", err)
			}
			grandAgent = grand
			if grand.depth != child.depth+1 {
				t.Fatalf("grand depth = %d, want %d", grand.depth, child.depth+1)
			}
			if grand.parent != child {
				t.Fatalf("grand parent mismatch")
			}
			if grand.model != child.model {
				t.Fatalf("grand model %q, want %q", grand.model, child.model)
			}
			if grand.sessionID != rootAgent.sessionID {
				t.Fatalf("grand session %q, want %q", grand.sessionID, rootAgent.sessionID)
			}

			grandCh := grand.SendUserMessage(innerCtx, "complete")
			for ev := range grandCh {
				grandEvents = append(grandEvents, ev)
			}

			return llmstream.ToolResult{
				CallID: call.CallID,
				Name:   call.Name,
				Type:   call.Type,
				Result: "inner done",
			}
		}

		childCh := child.SendUserMessage(ctx, "invoke inner")
		for ev := range childCh {
			childEvents = append(childEvents, ev)
		}

		return llmstream.ToolResult{
			CallID: call.CallID,
			Name:   call.Name,
			Type:   call.Type,
			Result: "outer done",
		}
	}

	agent, err := New(rootPrompt, []llmstream.Tool{outerTool}, NewOptions{Model: rootModel})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	rootAgent = agent

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventsCh := agent.SendUserMessage(ctx, "start nested flow")

	var events []Event
	for ev := range eventsCh {
		events = append(events, ev)
	}

	if childAgent == nil {
		t.Fatalf("child agent not created")
	}
	if grandAgent == nil {
		t.Fatalf("grand agent not created")
	}

	if childAgent.depth != 1 {
		t.Fatalf("child depth = %d, want 1", childAgent.depth)
	}
	if childAgent.model != childModel {
		t.Fatalf("child model = %q, want %q", childAgent.model, childModel)
	}
	if grandAgent.depth != 2 {
		t.Fatalf("grand depth = %d, want 2", grandAgent.depth)
	}
	if grandAgent.parent != childAgent {
		t.Fatalf("grand parent mismatch after run")
	}
	if grandAgent.Status() != StatusIdle {
		t.Fatalf("grand status = %v, want idle", grandAgent.Status())
	}
	if len(childEvents) == 0 {
		t.Fatalf("child agent produced no events")
	}
	if len(grandEvents) == 0 {
		t.Fatalf("grand agent produced no events")
	}

	var childDepth1, childDepth2 int
	for _, ev := range childEvents {
		switch ev.Agent.Depth {
		case 1:
			childDepth1++
			if ev.Agent.ID != childAgent.agentID {
				t.Fatalf("child event id = %q, want %q", ev.Agent.ID, childAgent.agentID)
			}
			if ev.Agent.Parent != rootAgent.agentID {
				t.Fatalf("child event parent = %q, want %q", ev.Agent.Parent, rootAgent.agentID)
			}
		case 2:
			childDepth2++
			if ev.Agent.ID != grandAgent.agentID {
				t.Fatalf("mirrored grand event in child stream id = %q, want %q", ev.Agent.ID, grandAgent.agentID)
			}
			if ev.Agent.Parent != childAgent.agentID {
				t.Fatalf("mirrored grand event in child stream parent = %q, want %q", ev.Agent.Parent, childAgent.agentID)
			}
		default:
			t.Fatalf("unexpected depth %d in child stream", ev.Agent.Depth)
		}
	}
	if childDepth1 == 0 {
		t.Fatalf("child stream missing depth-1 events")
	}
	if childDepth2 == 0 {
		t.Fatalf("child stream missing mirrored depth-2 events")
	}
	for _, ev := range grandEvents {
		if ev.Agent.ID != grandAgent.agentID {
			t.Fatalf("grand event id = %q, want %q", ev.Agent.ID, grandAgent.agentID)
		}
		if ev.Agent.Depth != 2 {
			t.Fatalf("grand event depth = %d, want 2", ev.Agent.Depth)
		}
		if ev.Agent.Parent != childAgent.agentID {
			t.Fatalf("grand event parent = %q, want %q", ev.Agent.Parent, childAgent.agentID)
		}
	}

	var depth1, depth2 int
	for _, ev := range events {
		switch ev.Agent.Depth {
		case 0:
			if ev.Agent.Parent != "" {
				t.Fatalf("root event parent = %q, want empty", ev.Agent.Parent)
			}
		case 1:
			if ev.Agent.ID != childAgent.agentID {
				t.Fatalf("mirrored child event id = %q, want %q", ev.Agent.ID, childAgent.agentID)
			}
			if ev.Agent.Parent != rootAgent.agentID {
				t.Fatalf("mirrored child event parent = %q, want %q", ev.Agent.Parent, rootAgent.agentID)
			}
			depth1++
		case 2:
			if ev.Agent.ID != grandAgent.agentID {
				t.Fatalf("mirrored grand event id = %q, want %q", ev.Agent.ID, grandAgent.agentID)
			}
			if ev.Agent.Parent != childAgent.agentID {
				t.Fatalf("mirrored grand event parent = %q, want %q", ev.Agent.Parent, childAgent.agentID)
			}
			depth2++
		default:
			t.Fatalf("unexpected depth %d in events", ev.Agent.Depth)
		}
	}
	if depth1 == 0 {
		t.Fatalf("no depth-1 events mirrored to root")
	}
	if depth2 == 0 {
		t.Fatalf("no depth-2 events mirrored to root")
	}
}

// --- Test helpers ---

func runContextUsageAgent(t *testing.T, model llmmodel.ModelID, usage llmstream.TokenUsage) *Agent {
	t.Helper()
	systemPrompt := "You are helpful."

	turn := llmstream.Turn{
		Role:         llmstream.RoleAssistant,
		Parts:        []llmstream.ContentPart{llmstream.TextContent{Content: "done"}},
		FinishReason: llmstream.FinishReasonEndTurn,
		Usage:        usage,
	}
	script := &sendScript{
		events: []llmstream.Event{
			{Type: llmstream.EventTypeCompletedSuccess, Turn: &turn},
		},
	}
	conv := newScriptedConversation(systemPrompt, script)
	overrideConversation(t, conv)

	agent, err := New(systemPrompt, nil, NewOptions{Model: model})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := agent.SendUserMessage(ctx, "ping")
	for range out {
	}

	return agent
}

func roundPercentFloat(used, capacity float64) int {
	if used <= 0 || capacity <= 0 {
		return 0
	}
	percent := int(math.Round((used / capacity) * 100))
	if percent < 0 {
		return 0
	}
	if percent > 100 {
		return 100
	}
	return percent
}

type scriptedConversation struct {
	mu          sync.Mutex
	systemTurn  llmstream.Turn
	turns       []llmstream.Turn
	scripts     []*sendScript
	toolResults [][]llmstream.ToolResult
}

type sendScript struct {
	events           []llmstream.Event
	wait             <-chan struct{}
	waitBefore       []<-chan struct{}
	afterSend        []chan struct{}
	afterCancel      chan struct{}
	blockAfterCancel <-chan struct{}
}

func newScriptedConversation(systemPrompt string, scripts ...*sendScript) *scriptedConversation {
	sysTurn := newTextTurn(llmstream.RoleSystem, systemPrompt)
	turns := []llmstream.Turn{cloneTurn(sysTurn)}
	return &scriptedConversation{
		systemTurn: sysTurn,
		turns:      turns,
		scripts:    scripts,
	}
}

func (c *scriptedConversation) LastTurn() llmstream.Turn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return cloneTurn(c.turns[len(c.turns)-1])
}

func (c *scriptedConversation) Turns() []llmstream.Turn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return cloneTurns(c.turns)
}

func (c *scriptedConversation) AddTools(tools []llmstream.Tool) error {
	return nil
}

func (c *scriptedConversation) AddUserTurn(text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.turns = append(c.turns, newTextTurn(llmstream.RoleUser, text))
	return nil
}

func (c *scriptedConversation) AddToolResults(results []llmstream.ToolResult) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	copied := make([]llmstream.ToolResult, len(results))
	copy(copied, results)
	c.toolResults = append(c.toolResults, copied)

	c.turns = append(c.turns, toolResultTurn(results))
	return nil
}

func (c *scriptedConversation) SendAsync(ctx context.Context, _ ...llmstream.SendOptions) <-chan llmstream.Event {
	c.mu.Lock()
	if len(c.scripts) == 0 {
		c.mu.Unlock()
		ch := make(chan llmstream.Event)
		close(ch)
		return ch
	}
	script := c.scripts[0]
	c.scripts = c.scripts[1:]
	c.mu.Unlock()

	out := make(chan llmstream.Event, len(script.events))

	go func() {
		defer close(out)
		cancelNotified := false
		handleCancel := func() {
			if script.afterCancel != nil && !cancelNotified {
				close(script.afterCancel)
				cancelNotified = true
			}
			if script.blockAfterCancel != nil {
				<-script.blockAfterCancel
			}
		}

		if script.wait != nil {
			select {
			case <-ctx.Done():
				handleCancel()
				return
			case <-script.wait:
			}
		}

		for i, ev := range script.events {
			if i < len(script.waitBefore) && script.waitBefore[i] != nil {
				select {
				case <-ctx.Done():
					handleCancel()
					return
				case <-script.waitBefore[i]:
				}
			}

			select {
			case <-ctx.Done():
				handleCancel()
				return
			case out <- ev:
			}
			if i < len(script.afterSend) && script.afterSend[i] != nil {
				close(script.afterSend[i])
			}
			if ev.Type == llmstream.EventTypeCompletedSuccess && ev.Turn != nil {
				c.mu.Lock()
				c.turns = append(c.turns, cloneTurn(*ev.Turn))
				c.mu.Unlock()
			}
		}
	}()

	return out
}

// stubTool satisfies llmstream.Tool for tests.
type stubTool struct {
	name   string
	result llmstream.ToolResult
	runErr error
	calls  []llmstream.ToolCall
	info   llmstream.ToolInfo
}

func newStubTool(name string, result llmstream.ToolResult) *stubTool {
	return &stubTool{
		name:   name,
		result: result,
		info: llmstream.ToolInfo{
			Name: name,
		},
	}
}

func (s *stubTool) Info() llmstream.ToolInfo { return s.info }

func (s *stubTool) Name() string { return s.name }

func (s *stubTool) Presenter() llmstream.Presenter { return nil }

func (s *stubTool) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	if s.runErr != nil {
		res := llmstream.NewErrorToolResult(s.runErr.Error(), call)
		res.SourceErr = s.runErr
		return res
	}
	s.calls = append(s.calls, call)
	res := s.result
	return normalizeToolResult(res, call)
}

type funcTool struct {
	name  string
	runFn func(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult
}

func (t *funcTool) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{Name: t.name}
}

func (t *funcTool) Name() string { return t.name }

func (t *funcTool) Presenter() llmstream.Presenter { return nil }

func (t *funcTool) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	if t.runFn == nil {
		res := llmstream.NewErrorToolResult("no run function provided", call)
		return res
	}
	res := t.runFn(ctx, call)
	return normalizeToolResult(res, call)
}

func overrideConversation(t *testing.T, conv llmstream.StreamingConversation) {
	t.Helper()
	prev := newConversation
	newConversation = func(model llmmodel.ModelID, systemPrompt string) llmstream.StreamingConversation {
		return conv
	}
	t.Cleanup(func() {
		newConversation = prev
	})
}

var _ llmstream.Tool = (*stubTool)(nil)
var _ llmstream.Tool = (*funcTool)(nil)
