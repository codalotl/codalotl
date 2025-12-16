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

	a, err := NewAgent(llmmodel.ModelID("model"), systemPrompt, nil)
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

	a, err := NewAgent(llmmodel.ModelID("model"), systemPrompt, []llmstream.Tool{tool})
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
		case EventTypeToolComplete:
			sawToolComplete = true
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

	a, err := NewAgent(llmmodel.ModelID("model"), systemPrompt, nil)
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

func TestTurnsReturnsCopy(t *testing.T) {
	systemPrompt := "sys"
	conv := newScriptedConversation(systemPrompt)
	overrideConversation(t, conv)

	a, err := NewAgent(llmmodel.ModelID("model"), systemPrompt, nil)
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
		subAgent, err := creator.NewWithDefaultModel(subPrompt, nil)
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

	a, err := NewAgent(baseModel, systemPrompt, []llmstream.Tool{tool})
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

	for _, ev := range subEvents {
		if ev.Agent.Depth != 1 {
			t.Fatalf("sub event depth = %d, want 1", ev.Agent.Depth)
		}
		if ev.Agent.ID == "" {
			t.Fatalf("sub event missing agent id")
		}
	}

	var mirrored int
	for _, ev := range events {
		switch ev.Agent.Depth {
		case 0:
			if ev.Agent.ID == "" {
				t.Fatalf("root event missing agent id")
			}
		case 1:
			mirrored++
		default:
			t.Fatalf("unexpected agent depth %d in root stream", ev.Agent.Depth)
		}
	}
	if mirrored != len(subEvents) {
		t.Fatalf("mirrored events = %d, want %d", mirrored, len(subEvents))
	}

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

	a, err := NewAgent(llmmodel.ModelID("model"), systemPrompt, []llmstream.Tool{tool})
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
	_, _ = captured.NewWithDefaultModel("should panic", nil)
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

	outerTool.runFn = func(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
		if got := SubAgentDepth(ctx); got != 0 {
			t.Fatalf("outer depth = %d, want 0", got)
		}
		creator := SubAgentCreatorFromContext(ctx)
		innerTool := &funcTool{name: "inner"}
		child, err := creator.New(childModel, childPrompt, []llmstream.Tool{innerTool})
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
			grand, err := subCreator.NewWithDefaultModel(grandPrompt, nil)
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
			for range grandCh {
			}

			return llmstream.ToolResult{
				CallID: call.CallID,
				Name:   call.Name,
				Type:   call.Type,
				Result: "inner done",
			}
		}

		childCh := child.SendUserMessage(ctx, "invoke inner")
		for range childCh {
		}

		return llmstream.ToolResult{
			CallID: call.CallID,
			Name:   call.Name,
			Type:   call.Type,
			Result: "outer done",
		}
	}

	agent, err := NewAgent(rootModel, rootPrompt, []llmstream.Tool{outerTool})
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

	var depth1, depth2 int
	for _, ev := range events {
		switch ev.Agent.Depth {
		case 0:
		case 1:
			depth1++
		case 2:
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

	agent, err := NewAgent(model, systemPrompt, nil)
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
	events []llmstream.Event
	wait   <-chan struct{}
}

func newScriptedConversation(systemPrompt string, scripts ...*sendScript) *scriptedConversation {
	sysTurn := llmstream.Turn{
		Role:  llmstream.RoleSystem,
		Parts: []llmstream.ContentPart{llmstream.TextContent{Content: systemPrompt}},
	}
	turns := []llmstream.Turn{cloneTurnTest(sysTurn)}
	return &scriptedConversation{
		systemTurn: sysTurn,
		turns:      turns,
		scripts:    scripts,
	}
}

func (c *scriptedConversation) LastTurn() llmstream.Turn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return cloneTurnTest(c.turns[len(c.turns)-1])
}

func (c *scriptedConversation) Turns() []llmstream.Turn {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]llmstream.Turn, len(c.turns))
	for i, t := range c.turns {
		out[i] = cloneTurnTest(t)
	}
	return out
}

func (c *scriptedConversation) AddTools(tools []llmstream.Tool) error {
	return nil
}

func (c *scriptedConversation) AddUserTurn(text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.turns = append(c.turns, llmstream.Turn{
		Role:  llmstream.RoleUser,
		Parts: []llmstream.ContentPart{llmstream.TextContent{Content: text}},
	})
	return nil
}

func (c *scriptedConversation) AddToolResults(results []llmstream.ToolResult) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	copied := make([]llmstream.ToolResult, len(results))
	copy(copied, results)
	c.toolResults = append(c.toolResults, copied)

	parts := make([]llmstream.ContentPart, len(results))
	for i, r := range results {
		parts[i] = r
	}
	c.turns = append(c.turns, llmstream.Turn{Role: llmstream.RoleUser, Parts: parts})
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

		if script.wait != nil {
			select {
			case <-ctx.Done():
				return
			case <-script.wait:
			}
		}

		for _, ev := range script.events {
			select {
			case <-ctx.Done():
				return
			case out <- ev:
			}
			if ev.Type == llmstream.EventTypeCompletedSuccess && ev.Turn != nil {
				c.mu.Lock()
				c.turns = append(c.turns, cloneTurnTest(*ev.Turn))
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

func (s *stubTool) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	if s.runErr != nil {
		res := llmstream.NewErrorToolResult(s.runErr.Error(), call)
		res.SourceErr = s.runErr
		return res
	}
	s.calls = append(s.calls, call)
	res := s.result
	if res.CallID == "" {
		res.CallID = call.CallID
	}
	if res.Name == "" {
		res.Name = call.Name
	}
	if res.Type == "" {
		res.Type = call.Type
	}
	return res
}

type funcTool struct {
	name  string
	runFn func(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult
}

func (t *funcTool) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{Name: t.name}
}

func (t *funcTool) Name() string { return t.name }

func (t *funcTool) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	if t.runFn == nil {
		res := llmstream.NewErrorToolResult("no run function provided", call)
		return res
	}
	res := t.runFn(ctx, call)
	if res.CallID == "" {
		res.CallID = call.CallID
	}
	if res.Name == "" {
		res.Name = call.Name
	}
	if res.Type == "" {
		res.Type = call.Type
	}
	return res
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

func cloneTurnTest(t llmstream.Turn) llmstream.Turn {
	cp := t
	if len(t.Parts) > 0 {
		parts := make([]llmstream.ContentPart, len(t.Parts))
		copy(parts, t.Parts)
		cp.Parts = parts
	}
	return cp
}
