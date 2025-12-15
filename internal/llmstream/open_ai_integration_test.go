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

const structuredAnswerGrammar = `
start: "answer:" SIGNED_NUMBER
%import common.SIGNED_NUMBER
%import common.WS
%ignore WS
`

func runIntegrationTest(t *testing.T, apiKeyEnvVar string) bool {
	if os.Getenv(apiKeyEnvVar) == "" {
		t.Skipf("%s is required to run these tests", apiKeyEnvVar)
		return false
	}
	if os.Getenv("INTEGRATION_TEST") != "1" {
		t.Skip("INTEGRATION_TEST=1 is required to run these tests")
		return false
	}
	return true
}

func TestOpenAIResponsesProvider_400Error(t *testing.T) {
	if !runIntegrationTest(t, "OPENAI_API_KEY") {
		return
	}

	mini4o := llmmodel.ModelID("gpt-4o-mini")
	err := llmmodel.AddCustomModel(mini4o, llmmodel.ProviderIDOpenAI, string(mini4o), llmmodel.ModelOverrides{})
	require.NoError(t, err)

	conv := NewConversation(mini4o, "You are a precise assistant. Follow the user's instructions exactly.")
	require.NoError(t, conv.AddUserTurn("Reply with only the numeral 4. Do not add words or punctuation."))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	events := conv.SendAsync(ctx, SendOptions{ReasoningSummary: "invalid"})
	for event := range events {
		switch event.Type {
		case EventTypeError:
			assert.Error(t, event.Error)
			assert.Contains(t, event.Error.Error(), "Bad Request")
		default:
			t.Fatalf("expected an error")
		}
	}
}

func TestOpenAIResponsesProvider_SimpleResponse(t *testing.T) {
	if !runIntegrationTest(t, "OPENAI_API_KEY") {
		return
	}

	conv := NewConversation(llmmodel.ModelID("gpt-4o-mini"), "You are a precise assistant. Follow the user's instructions exactly.")
	require.NoError(t, conv.AddUserTurn("Reply with only the numeral 4. Do not add words or punctuation."))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	events := conv.SendAsync(ctx, SendOptions{TemperaturePresent: true, Temperature: 0.0})

	var completeResp *Turn
	var gotCreated bool
	for event := range events {
		switch event.Type {
		case EventTypeError:
			require.Error(t, event.Error)
			t.Fatalf("unexpectedly got an error: %v", event.Error)
		case EventTypeWarning:
			t.Fatalf("unexpected warning event: %v", event.Error)
		case EventTypeCompletedSuccess:
			require.NotNil(t, event.Turn)
			completeResp = event.Turn
		case EventTypeCreated:
			require.NotNil(t, event.Turn)
			gotCreated = true
		}
	}

	assert.True(t, gotCreated)
	require.NotNil(t, completeResp)
	assert.NotEqual(t, "", completeResp.ProviderID)

	// Expect exactly one text part with "4" and no tool calls
	if assert.NotEmpty(t, completeResp.Parts) {
		// Find first text content part
		found := false
		for _, p := range completeResp.Parts {
			if tc, ok := p.(TextContent); ok {
				found = true
				assert.Equal(t, "4", tc.Content)
				assert.NotEmpty(t, tc.ProviderID)
				break
			}
		}
		assert.True(t, found)
	}
	assert.Len(t, completeResp.ToolCalls(), 0)
	assert.True(t, completeResp.Usage.TotalInputTokens > 0)
	assert.True(t, completeResp.Usage.TotalOutputTokens > 0)
	assert.Equal(t, FinishReasonEndTurn, completeResp.FinishReason)

	// Assert that the last message is an assistant message with one text part containing "4"
	m := conv.LastTurn()
	assert.Equal(t, RoleAssistant, m.Role)
	assert.Len(t, m.Parts, 1)
	tc, ok := m.Parts[0].(TextContent)
	require.True(t, ok)
	assert.Equal(t, "4", tc.Content)
	assert.Equal(t, "4", m.TextContent())
	assert.NotEmpty(t, tc.ProviderID)
}

func TestOpenAIResponsesProvider_ToolUsage(t *testing.T) {
	if !runIntegrationTest(t, "OPENAI_API_KEY") {
		return
	}

	const (
		toolName     = "store_message"
		requestedMsg = "integration tool message"
	)

	// Instruct the model to greet and then make a tool call.

	instructions := `You are a test automation assistant.

In a **single response**, do **both** of the following in this exact order:
1) Emit a normal user-facing greeting as plain text (no tools).
2) Immediately invoke the tool named 'store_message' with exactly:
   {"message": "integration tool message"}

Rules:
- The text greeting MUST appear before any tool call.
- Call exactly one tool (store_message) once.
- Do not ask questions or add extra text after the tool call.`

	conv := NewConversation(llmmodel.DefaultModel, instructions)
	require.NoError(t, conv.AddUserTurn("Hello Mr. Automaton."))

	// Register tool with the conversation so the provider can call it
	tool := integrationTestTool{name: toolName}
	require.NoError(t, conv.AddTools([]Tool{tool}))

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	events := conv.SendAsync(ctx, SendOptions{ReasoningEffort: "minimal"})

	var (
		gotToolUse   bool
		completeResp *Turn
	)

	for event := range events {
		switch event.Type {
		case EventTypeError:
			require.Error(t, event.Error)
			t.Fatalf("unexpectedly got an error: %v", event.Error)
		case EventTypeWarning:
			t.Fatalf("unexpected warning event: %v", event.Error)
		case EventTypeToolUse:
			gotToolUse = true
			require.NotNil(t, event.ToolCall)
			// Accept either function_call or custom_tool_call
			require.Equal(t, toolName, event.ToolCall.Name)
		case EventTypeCompletedSuccess:
			require.NotNil(t, event.Turn)
			completeResp = event.Turn
		}
	}

	require.True(t, gotToolUse)
	require.NotNil(t, completeResp)
	require.Equal(t, FinishReasonToolUse, completeResp.FinishReason)

	// Validate tool call presence in the response metadata
	calls := completeResp.ToolCalls()
	require.NotEmpty(t, calls)
	call := calls[0]
	require.Equal(t, toolName, call.Name)
	require.NotEmpty(t, call.Input)

	var payload struct {
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal([]byte(call.Input), &payload))
	require.Equal(t, requestedMsg, payload.Message)

	// Optionally assert text greeting appears somewhere in parts
	foundGreeting := false
	for _, p := range completeResp.Parts {
		if tc, ok := p.(TextContent); ok {
			if strings.Contains(strings.ToLower(tc.Content), "hello") {
				foundGreeting = true
				break
			}
		}
	}
	assert.True(t, foundGreeting)

	// Assert that one assistant message was added with two parts: a text part and a function call
	messages := conv.Turns()
	assert.Len(t, messages, 3)
	assistantMsg := messages[2]
	assert.Equal(t, RoleAssistant, assistantMsg.Role)
	assert.Len(t, assistantMsg.Parts, 2)

	// First part should be text content (the greeting)
	tc, ok := assistantMsg.Parts[0].(TextContent)
	require.True(t, ok)
	assert.True(t, strings.Contains(strings.ToLower(tc.Content), "hello"))

	// Second part should be a tool call
	toolCall, ok := assistantMsg.Parts[1].(ToolCall)
	require.True(t, ok)
	assert.Equal(t, toolName, toolCall.Name)

	//
	// Supply tool results and send to the LLM again
	//
	debugPrint(debugMisc, "--------- STARTING SECOND GO AROUND -----------", nil)

	// Build a tool result matching the tool call, using the tool's Run implementation
	ctx2, cancel2 := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel2()

	tr := tool.Run(ctx2, ToolCall{CallID: call.CallID, Name: call.Name, Input: call.Input, Type: call.Type})
	require.NoError(t, conv.AddToolResults([]ToolResult{tr}))

	// Send the follow-up turn with tool results
	events2 := conv.SendAsync(ctx2)

	var finalResp *Turn
	for ev := range events2 {
		switch ev.Type {
		case EventTypeError:
			require.Error(t, ev.Error)
			t.Fatalf("unexpected error after tool results: %v", ev.Error)
		case EventTypeWarning:
			t.Fatalf("unexpected warning event after tool results: %v", ev.Error)
		case EventTypeCompletedSuccess:
			require.NotNil(t, ev.Turn)
			finalResp = ev.Turn
		}
	}

	require.NotNil(t, finalResp)
	assert.Equal(t, FinishReasonEndTurn, finalResp.FinishReason)
	assert.Empty(t, finalResp.ToolCalls())

	// Expect some assistant text that acknowledges the provided message
	if assert.NotEmpty(t, finalResp.Parts) {
		foundMsg := false
		for _, p := range finalResp.Parts {
			if tc, ok := p.(TextContent); ok {
				if tc.Content != "" {
					foundMsg = true
					break
				}
			}
		}
		assert.True(t, foundMsg)
	}

	// Assert that the last assistant message has just one text part
	messages = conv.Turns()
	assert.Len(t, messages, 5)
	finalAssistantMsg := messages[4]
	assert.Equal(t, RoleAssistant, finalAssistantMsg.Role)
	assert.Len(t, finalAssistantMsg.Parts, 1)
	finalTc, ok := finalAssistantMsg.Parts[0].(TextContent)
	require.True(t, ok)
	assert.NotEmpty(t, finalTc.Content)
	assert.Equal(t, finalTc.Content, finalAssistantMsg.TextContent())
}

func TestOpenAIResponsesProvider_CustomToolGrammar(t *testing.T) {
	if !runIntegrationTest(t, "OPENAI_API_KEY") {
		return
	}

	const (
		toolName        = "structured_answer"
		requestedNumber = "7"
	)

	systemPrompt := `You are a precise assistant. You MUST call the tool named "structured_answer" exactly once in your first response.
Provide the number requested by the user through the tool input, and avoid additional assistant text in the same turn.
After the tool result is supplied, reply with "Final answer: <value>" where <value> is exactly the number returned by the tool result.`

	conv := NewConversation(llmmodel.DefaultModel, systemPrompt)
	require.NoError(t, conv.AddUserTurn("Use the structured tool to report the integer 7."))

	tool := grammarTestTool{name: toolName}
	require.NoError(t, conv.AddTools([]Tool{tool}))

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	events := conv.SendAsync(ctx, SendOptions{ReasoningEffort: "minimal"})

	var (
		firstResp *Turn
		call      *ToolCall
	)

	for event := range events {
		switch event.Type {
		case EventTypeError:
			require.Error(t, event.Error)
			t.Fatalf("unexpected error: %v", event.Error)
		case EventTypeWarning:
			t.Fatalf("unexpected warning event: %v", event.Error)
		case EventTypeToolUse:
			require.NotNil(t, event.ToolCall)
			copyCall := *event.ToolCall
			call = &copyCall
		case EventTypeCompletedSuccess:
			require.NotNil(t, event.Turn)
			firstResp = event.Turn
		}
	}

	require.NotNil(t, firstResp)
	require.NotNil(t, call)
	assert.Equal(t, FinishReasonToolUse, firstResp.FinishReason)
	assert.Equal(t, toolName, call.Name)
	assert.Equal(t, "custom_tool_call", call.Type)
	require.NotEmpty(t, call.CallID)

	rawInput := strings.TrimSpace(call.Input)
	require.True(t, strings.HasPrefix(rawInput, "answer:"), "tool input should start with answer:")
	value := strings.TrimSpace(strings.TrimPrefix(rawInput, "answer:"))
	assert.Equal(t, requestedNumber, value)

	calls := firstResp.ToolCalls()
	require.NotEmpty(t, calls)
	assert.Equal(t, toolName, calls[0].Name)
	assert.Equal(t, call.CallID, calls[0].CallID)

	ctx2, cancel2 := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel2()

	result := tool.Run(ctx2, *call)
	require.NoError(t, conv.AddToolResults([]ToolResult{result}))

	events2 := conv.SendAsync(ctx2)

	var secondResp *Turn
	for ev := range events2 {
		switch ev.Type {
		case EventTypeError:
			require.Error(t, ev.Error)
			t.Fatalf("unexpected error after tool results: %v", ev.Error)
		case EventTypeWarning:
			t.Fatalf("unexpected warning after tool results: %v", ev.Error)
		case EventTypeCompletedSuccess:
			require.NotNil(t, ev.Turn)
			secondResp = ev.Turn
		}
	}

	require.NotNil(t, secondResp)
	assert.Equal(t, FinishReasonEndTurn, secondResp.FinishReason)
	assert.Empty(t, secondResp.ToolCalls())

	foundNumber := false
	for _, part := range secondResp.Parts {
		if tc, ok := part.(TextContent); ok {
			if strings.Contains(tc.Content, requestedNumber) {
				foundNumber = true
				break
			}
		}
	}
	assert.True(t, foundNumber)

	msgs := conv.Turns()
	assert.True(t, len(msgs) >= 5)
	last := conv.LastTurn()
	assert.Equal(t, RoleAssistant, last.Role)
	assert.Len(t, last.Parts, 1)
	if tc, ok := last.Parts[0].(TextContent); assert.True(t, ok) {
		assert.Contains(t, tc.Content, requestedNumber)
		assert.Contains(t, last.TextContent(), requestedNumber)
	}
}

func TestOpenAIResponsesProvider_Reasoning(t *testing.T) {
	if !runIntegrationTest(t, "OPENAI_API_KEY") {
		return
	}

	conv := NewConversation(llmmodel.DefaultModel, "You are a precise assistant. Follow the user's instructions exactly.")
	require.NoError(t, conv.AddUserTurn("Reply with only the numeric answer to (393 + 16 / 8). Do not add words, punctuation, whitespace, or newlines."))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	events := conv.SendAsync(ctx, SendOptions{ReasoningEffort: "low"})

	var completeResp *Turn
	gotCreated := false
	gotReasoningDelta := false
	gotReasoningDeltaDone := false
	gotContentDelta := false
	gotContentDeltaDone := false
	for event := range events {
		switch event.Type {
		case EventTypeError:
			require.Error(t, event.Error)
			t.Fatalf("unexpectedly got an error: %v", event.Error)
		case EventTypeWarning:
			t.Fatalf("unexpected warning event: %v", event.Error)
		case EventTypeCompletedSuccess:
			require.NotNil(t, event.Turn)
			completeResp = event.Turn
		case EventTypeReasoningDelta:
			require.NotEmpty(t, event.ReasoningContent())
			gotReasoningDelta = true
			if event.Done {
				gotReasoningDeltaDone = true
			}
		case EventTypeTextDelta:
			require.NotEmpty(t, event.TextContent())
			gotContentDelta = true
			if event.Done {
				gotContentDeltaDone = true
			}
		case EventTypeCreated:
			require.NotNil(t, event.Turn)
			gotCreated = true
		}
	}

	assert.True(t, gotCreated)
	require.NotNil(t, completeResp)
	require.True(t, gotReasoningDelta)
	require.True(t, gotReasoningDeltaDone)
	require.True(t, gotContentDelta)
	require.True(t, gotContentDeltaDone)
	assert.NotEqual(t, "", completeResp.ProviderID)

	// Reasoning and content parts
	if assert.Len(t, completeResp.Parts, 2) {
		// Find first text content part
		foundContent := false
		for _, p := range completeResp.Parts {
			if tc, ok := p.(TextContent); ok {
				foundContent = true
				assert.Equal(t, "395", tc.Content)
				break
			}
		}
		assert.True(t, foundContent)

		foundReasoning := false
		for _, p := range completeResp.Parts {
			if tc, ok := p.(ReasoningContent); ok {
				foundReasoning = true
				assert.NotEmpty(t, tc.Content)
				break
			}
		}
		assert.True(t, foundReasoning)
	}
	assert.Len(t, completeResp.ToolCalls(), 0)
	assert.True(t, completeResp.Usage.TotalInputTokens > 0)
	assert.True(t, completeResp.Usage.TotalOutputTokens > 0)
	assert.Equal(t, FinishReasonEndTurn, completeResp.FinishReason)

	// Assert that the last message is an assistant message with reasoning and text parts
	m := conv.LastTurn()
	assert.Equal(t, RoleAssistant, m.Role)
	assert.Len(t, m.Parts, 2) // NOTE: in theory, the LLM may emit multiple reasoning parts

	// First part should be reasoning content
	reasoningPart, ok := m.Parts[0].(ReasoningContent)
	require.True(t, ok)
	assert.NotEmpty(t, reasoningPart.Content)
	assert.NotEmpty(t, reasoningPart.ProviderID)

	// Second part should be text content
	textPart, ok := m.Parts[1].(TextContent)
	require.True(t, ok)
	assert.Equal(t, "395", textPart.Content)
	assert.Equal(t, "395", m.TextContent())
}

func TestOpenAIResponsesProvider_NoLinkReasoningTool(t *testing.T) {
	if !runIntegrationTest(t, "OPENAI_API_KEY") {
		return
	}

	const (
		toolName  = "get_weather"
		location  = "San Francisco"
		toolReply = "72 F"
	)

	// Instruct the model to call the tool and then reply with only the tool result
	conv := NewConversation(llmmodel.DefaultModel, "You are a precise assistant. Use the available tool to answer and then reply ONLY with the tool result string.")
	require.NoError(t, conv.AddUserTurn("Call the tool named get_weather with the JSON {\"location\":\"San Francisco\"}. After you receive the result, reply with exactly the function call result and nothing else."))

	// Register the weather tool
	tool := getWeatherTestTool{name: toolName, fixedTemp: toolReply}
	require.NoError(t, conv.AddTools([]Tool{tool}))

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// First send: expect a tool call, with reasoning enabled and NoLink=true
	events := conv.SendAsync(ctx, SendOptions{ReasoningEffort: "low", NoLink: true})

	var (
		gotToolUse bool
		firstResp  *Turn
		firstCall  *ToolCall
	)

	for event := range events {
		switch event.Type {
		case EventTypeError:
			require.Error(t, event.Error)
			t.Fatalf("unexpected error in first turn: %v", event.Error)
		case EventTypeWarning:
			t.Fatalf("unexpected warning event: %v", event.Error)
		case EventTypeReasoningDelta:
			// ignore
		case EventTypeToolUse:
			gotToolUse = true
			require.NotNil(t, event.ToolCall)
			require.Equal(t, toolName, event.ToolCall.Name)
			firstCall = event.ToolCall
		case EventTypeCompletedSuccess:
			require.NotNil(t, event.Turn)
			firstResp = event.Turn
		}
	}

	require.True(t, gotToolUse)
	require.NotNil(t, firstResp)
	require.NotNil(t, firstCall)
	require.Equal(t, FinishReasonToolUse, firstResp.FinishReason)

	// Provide tool result matching the emitted tool call
	ctx2, cancel2 := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel2()

	tr := tool.Run(ctx2, ToolCall{CallID: firstCall.CallID, Name: firstCall.Name, Input: firstCall.Input, Type: firstCall.Type})
	require.NoError(t, conv.AddToolResults([]ToolResult{tr}))

	// Second send: expect a plain text answer echoing the tool result, with NoLink=true again
	events2 := conv.SendAsync(ctx2, SendOptions{ReasoningEffort: "low", NoLink: true})

	var finalResp *Turn
	for ev := range events2 {
		switch ev.Type {
		case EventTypeError:
			require.Error(t, ev.Error)
			t.Fatalf("unexpected error in second turn: %v", ev.Error)
		case EventTypeWarning:
			t.Fatalf("unexpected warning event in second turn: %v", ev.Error)
		case EventTypeCompletedSuccess:
			require.NotNil(t, ev.Turn)
			finalResp = ev.Turn
		}
	}

	require.NotNil(t, finalResp)
	assert.Equal(t, FinishReasonEndTurn, finalResp.FinishReason)
	assert.Empty(t, finalResp.ToolCalls())

	// Reply back with the function call result; keep assertion tolerant to formatting
	if assert.NotEmpty(t, finalResp.Parts) {
		found := false
		for _, p := range finalResp.Parts {
			if tc, ok := p.(TextContent); ok {
				if strings.Contains(tc.Content, toolReply) {
					found = true
					break
				}
			}
		}
		assert.True(t, found)
	}

	// Assert that the last assistant message has just one text part containing the tool result
	msgs := conv.Turns()
	assert.True(t, len(msgs) >= 5)
	last := conv.LastTurn()
	assert.Equal(t, RoleAssistant, last.Role)
	assert.Len(t, last.Parts, 1)
	if tc, ok := last.Parts[0].(TextContent); assert.True(t, ok) {
		assert.Contains(t, tc.Content, toolReply)
		assert.Contains(t, last.TextContent(), toolReply)
	}
}

// integrationTestTool implements Tool for integration testing.
type integrationTestTool struct{ name string }

func (integrationTestTool) Run(ctx context.Context, params ToolCall) ToolResult {
	return ToolResult{CallID: params.CallID, Name: params.Name, Type: params.Type, Result: "success"}
}

func (itt integrationTestTool) Name() string { return itt.name }

func (itt integrationTestTool) Info() ToolInfo {
	return ToolInfo{
		Name:        itt.name,
		Description: "Stores a message for testing tool streaming.",
		Parameters: map[string]any{
			"message": map[string]any{
				"type":        "string",
				"description": "The message that should be stored.",
			},
		},
		Required: []string{"message"},
	}
}

// getWeatherTestTool implements Tool and returns a fixed temperature string.
type getWeatherTestTool struct {
	name      string
	fixedTemp string
}

func (g getWeatherTestTool) Run(ctx context.Context, params ToolCall) ToolResult {
	return ToolResult{CallID: params.CallID, Name: params.Name, Type: params.Type, Result: g.fixedTemp}
}

func (g getWeatherTestTool) Name() string { return g.name }

func (g getWeatherTestTool) Info() ToolInfo {
	return ToolInfo{
		Name:        g.name,
		Description: "Gets the current temperature for a given location.",
		Parameters: map[string]any{
			"location": map[string]any{
				"type":        "string",
				"description": "The city and/or region to check.",
			},
		},
		Required: []string{"location"},
	}
}

type grammarTestTool struct{ name string }

func (grammarTestTool) Run(ctx context.Context, call ToolCall) ToolResult {
	raw := strings.TrimSpace(call.Input)
	if !strings.HasPrefix(raw, "answer:") {
		return NewErrorToolResult(`input must start with "answer:"`, call)
	}
	value := strings.TrimSpace(strings.TrimPrefix(raw, "answer:"))
	if value == "" {
		return NewErrorToolResult("missing answer value", call)
	}
	return ToolResult{
		CallID: call.CallID,
		Name:   call.Name,
		Type:   call.Type,
		Result: "acknowledged " + value,
	}
}

func (g grammarTestTool) Name() string { return g.name }

func (g grammarTestTool) Info() ToolInfo {
	return ToolInfo{
		Name:        g.name,
		Description: "Provides a structured answer using a Lark grammar.",
		Kind:        ToolKindCustom,
		Grammar: &ToolGrammar{
			Syntax:     ToolGrammarSyntaxLark,
			Definition: structuredAnswerGrammar,
		},
	}
}

// twoParamTool has two string parameters where only one is required.
// This exercises strict=true behavior with an optional nullable param.
type twoParamTool struct{ name string }

func (t twoParamTool) Run(ctx context.Context, call ToolCall) ToolResult {
	return ToolResult{CallID: call.CallID, Name: call.Name, Type: call.Type, Result: "ok"}
}

func (t twoParamTool) Name() string { return t.name }

func (t twoParamTool) Info() ToolInfo {
	return ToolInfo{
		Name:        t.name,
		Description: "Tool with two string params; only one required.",
		Parameters: map[string]any{
			"first": map[string]any{
				"type":        "string",
				"description": "Required string.",
			},
			"second": map[string]any{
				"type":        "string",
				"description": "Optional string.",
			},
		},
		Required: []string{"first"},
	}
}

func TestOpenAIResponsesProvider_ToolWithOptionalString_No400(t *testing.T) {

	// This test makes sure we can use two-parameter tools, where only one param is required.
	// The llmstream library will convert this schema into a strict schema where the non-required param's type becomes "type": ["string", null].

	if !runIntegrationTest(t, "OPENAI_API_KEY") {
		return
	}

	const toolName = "two_param_tool"

	system := `You are a precise assistant.
In your FIRST response, call the tool 'two_param_tool' exactly once with this JSON:
{"first":"hello"}  // do NOT include "second".
Do not include any assistant text outside of the tool call.`

	conv := NewConversation(llmmodel.ModelID("gpt-5.1-codex-low"), system)
	require.NoError(t, conv.AddUserTurn("Proceed."))
	require.NoError(t, conv.AddTools([]Tool{twoParamTool{name: toolName}}))

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	events := conv.SendAsync(ctx)

	var (
		gotToolUse bool
		done       *Turn
	)
	for ev := range events {
		switch ev.Type {
		case EventTypeError:
			t.Fatalf("unexpected error (possible 400): %v", ev.Error)
		case EventTypeWarning:
			t.Fatalf("unexpected warning: %v", ev.Error)
		case EventTypeToolUse:
			require.NotNil(t, ev.ToolCall)
			assert.Equal(t, toolName, ev.ToolCall.Name)
			// Optional: tolerate either absence or explicit null for "second"
			var args map[string]any
			_ = json.Unmarshal([]byte(ev.ToolCall.Input), &args)
			_, hasSecond := args["second"]
			// It's sufficient that we reached a tool use without server errors.
			_ = hasSecond
			gotToolUse = true
		case EventTypeCompletedSuccess:
			require.NotNil(t, ev.Turn)
			done = ev.Turn
		}
	}

	require.True(t, gotToolUse)
	require.NotNil(t, done)
	assert.Equal(t, FinishReasonToolUse, done.FinishReason)
}

// noParamTool exposes a function tool with no parameters.
type noParamTool struct{ name string }

func (n noParamTool) Run(ctx context.Context, call ToolCall) ToolResult {
	return ToolResult{CallID: call.CallID, Name: call.Name, Type: call.Type, Result: "ok"}
}
func (n noParamTool) Name() string { return n.name }
func (n noParamTool) Info() ToolInfo {
	return ToolInfo{
		Name:        n.name,
		Description: "A tool that takes no parameters.",
		// Parameters and Required intentionally empty/nil
	}
}

// TestOpenAIResponsesProvider_ToolWithNoParams ensures we can register and invoke a function tool
// that accepts no parameters (empty object schema) without 400 errors.
func TestOpenAIResponsesProvider_ToolWithNoParams(t *testing.T) {
	if !runIntegrationTest(t, "OPENAI_API_KEY") {
		return
	}

	const toolName = "noop_tool"

	system := `You are a precise assistant.
In your FIRST response, invoke the tool 'noop_tool' exactly once with no arguments.
Do not include any assistant text outside of the tool call.`

	conv := NewConversation(llmmodel.ModelID("gpt-5.1-codex-low"), system)
	require.NoError(t, conv.AddUserTurn("Proceed."))
	require.NoError(t, conv.AddTools([]Tool{noParamTool{name: toolName}}))

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	events := conv.SendAsync(ctx)

	var (
		gotToolUse bool
		firstTurn  *Turn
		firstCall  *ToolCall
	)
	for ev := range events {
		switch ev.Type {
		case EventTypeError:
			t.Fatalf("unexpected error (possible 400): %v", ev.Error)
		case EventTypeWarning:
			t.Fatalf("unexpected warning: %v", ev.Error)
		case EventTypeToolUse:
			require.NotNil(t, ev.ToolCall)
			assert.Equal(t, toolName, ev.ToolCall.Name)
			// Accept either empty string, "{}", or a JSON object with zero keys for no-arg tools
			in := strings.TrimSpace(ev.ToolCall.Input)
			if !(in == "") {
				var obj map[string]any
				if assert.NoError(t, json.Unmarshal([]byte(in), &obj), "tool input must be JSON object or empty") {
					assert.Len(t, obj, 0, "no-arg tool should have 0 keys in arguments")
				}
			}
			firstCall = ev.ToolCall
			gotToolUse = true
		case EventTypeCompletedSuccess:
			require.NotNil(t, ev.Turn)
			firstTurn = ev.Turn
		}
	}
	require.True(t, gotToolUse)
	require.NotNil(t, firstTurn)
	require.Equal(t, FinishReasonToolUse, firstTurn.FinishReason)

	// Provide the tool result and expect normal completion with assistant text
	ctx2, cancel2 := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel2()
	result := (noParamTool{name: toolName}).Run(ctx2, *firstCall)
	require.NoError(t, conv.AddToolResults([]ToolResult{result}))

	events2 := conv.SendAsync(ctx2)
	var finalTurn *Turn
	for ev := range events2 {
		switch ev.Type {
		case EventTypeError:
			t.Fatalf("unexpected error in second turn: %v", ev.Error)
		case EventTypeWarning:
			t.Fatalf("unexpected warning in second turn: %v", ev.Error)
		case EventTypeCompletedSuccess:
			require.NotNil(t, ev.Turn)
			finalTurn = ev.Turn
		}
	}
	require.NotNil(t, finalTurn)
	assert.Equal(t, FinishReasonEndTurn, finalTurn.FinishReason)
	assert.Empty(t, finalTurn.ToolCalls())
	// Should include some assistant text acknowledging success
	foundText := false
	for _, p := range finalTurn.Parts {
		if tc, ok := p.(TextContent); ok && strings.TrimSpace(tc.Content) != "" {
			foundText = true
			break
		}
	}
	assert.True(t, foundText)
}
