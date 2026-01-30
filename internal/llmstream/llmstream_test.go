package llmstream

import (
	"context"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/llmmodel"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddToolResults(t *testing.T) {
	t.Run("success single call/result", func(t *testing.T) {
		sc := NewConversation(llmmodel.ModelIDUnknown, "sys").(*streamingConversation)

		call := ToolCall{CallID: "call_A", Name: "tool1", Input: "{}", Type: "function_call"}
		// Assistant issues a tool call
		sc.turns = append(sc.turns, Turn{Role: RoleAssistant, Parts: []ContentPart{call}})
		sc.toolCalls[call.CallID] = toolCallResult{call: call}

		// Provide matching tool result
		res := ToolResult{CallID: "call_A", Name: "tool1", Type: "function_call", Result: "ok"}
		require.NoError(t, sc.AddToolResults([]ToolResult{res}))

		responses := sc.Turns()
		require.GreaterOrEqual(t, len(responses), 3)
		last := responses[len(responses)-1]
		assert.Equal(t, RoleUser, last.Role)
		require.Len(t, last.Parts, 1)
		tr, ok := last.Parts[0].(ToolResult)
		require.True(t, ok)
		assert.Equal(t, "call_A", tr.CallID)
		assert.Equal(t, "tool1", tr.Name)
		assert.Equal(t, "ok", tr.Result)
	})

	t.Run("error empty results", func(t *testing.T) {
		sc := NewConversation(llmmodel.ModelIDUnknown, "").(*streamingConversation)
		require.Error(t, sc.AddToolResults(nil))
		require.Error(t, sc.AddToolResults([]ToolResult{}))
	})

	t.Run("error previous message has no tool calls", func(t *testing.T) {
		sc := NewConversation(llmmodel.ModelIDUnknown, "sys").(*streamingConversation)
		sc.turns = append(sc.turns,
			newTextTurn(RoleUser, "user"),
			newTextTurn(RoleAssistant, "assistant"),
		)
		err := sc.AddToolResults([]ToolResult{{CallID: "call_X", Name: "tool1", Result: "ok"}})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "previous message does not contain tool calls")
	})

	t.Run("error missing call_id on result", func(t *testing.T) {
		sc := NewConversation(llmmodel.ModelIDUnknown, "sys").(*streamingConversation)
		call := ToolCall{CallID: "call_A", Name: "tool1"}
		sc.turns = append(sc.turns, Turn{Role: RoleAssistant, Parts: []ContentPart{call}})
		sc.toolCalls[call.CallID] = toolCallResult{call: call}
		err := sc.AddToolResults([]ToolResult{{CallID: "", Name: "tool1", Result: "ok"}})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing call_id")
	})

	t.Run("error unmatched call id", func(t *testing.T) {
		sc := NewConversation(llmmodel.ModelIDUnknown, "sys").(*streamingConversation)
		callA := ToolCall{CallID: "call_A", Name: "tool1"}
		callB := ToolCall{CallID: "call_B", Name: "tool2"}
		sc.turns = append(sc.turns, Turn{Role: RoleAssistant, Parts: []ContentPart{
			callA,
			callB,
		}})
		sc.toolCalls[callA.CallID] = toolCallResult{call: callA}
		sc.toolCalls[callB.CallID] = toolCallResult{call: callB}
		err := sc.AddToolResults([]ToolResult{{CallID: "call_C", Name: "tool1", Result: "ok"}})
		require.Error(t, err)
		msg := err.Error()
		assert.Contains(t, msg, "does not match prior tool call IDs")
		assert.True(t, strings.Contains(msg, "call_A") && strings.Contains(msg, "call_B"))
	})

	t.Run("error duplicate result for same call id", func(t *testing.T) {
		sc := NewConversation(llmmodel.ModelIDUnknown, "sys").(*streamingConversation)
		call := ToolCall{CallID: "call_A", Name: "tool1"}
		sc.turns = append(sc.turns, Turn{Role: RoleAssistant, Parts: []ContentPart{call}})
		sc.toolCalls[call.CallID] = toolCallResult{call: call}
		err := sc.AddToolResults([]ToolResult{
			{CallID: "call_A", Name: "tool1", Result: "ok1"},
			{CallID: "call_A", Name: "tool1", Result: "ok2"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate tool result for call call_A")
	})

	t.Run("error missing results for prior calls", func(t *testing.T) {
		sc := NewConversation(llmmodel.ModelIDUnknown, "sys").(*streamingConversation)
		callA := ToolCall{CallID: "call_A", Name: "tool1"}
		callB := ToolCall{CallID: "call_B", Name: "tool2"}
		sc.turns = append(sc.turns, Turn{Role: RoleAssistant, Parts: []ContentPart{
			callA,
			callB,
		}})
		sc.toolCalls[callA.CallID] = toolCallResult{call: callA}
		sc.toolCalls[callB.CallID] = toolCallResult{call: callB}
		err := sc.AddToolResults([]ToolResult{{CallID: "call_A", Name: "tool1", Result: "ok"}})
		require.Error(t, err)
		msg := err.Error()
		assert.Contains(t, msg, "missing tool results for call IDs")
		assert.Contains(t, msg, "call_B")
	})

	t.Run("error name mismatch", func(t *testing.T) {
		sc := NewConversation(llmmodel.ModelIDUnknown, "sys").(*streamingConversation)
		call := ToolCall{CallID: "call_A", Name: "tool1"}
		sc.turns = append(sc.turns, Turn{Role: RoleAssistant, Parts: []ContentPart{call}})
		sc.toolCalls[call.CallID] = toolCallResult{call: call}
		err := sc.AddToolResults([]ToolResult{{CallID: "call_A", Name: "wrong", Result: "ok"}})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not match tool call name")
	})
}

func TestSendAsyncInvalidModel(t *testing.T) {
	conv := NewConversation(llmmodel.ModelIDUnknown, "system prompt")
	require.NoError(t, conv.AddUserTurn("hello"))

	events := conv.SendAsync(context.Background())
	ev, ok := <-events
	require.True(t, ok, "expected error event")
	assert.Equal(t, EventTypeError, ev.Type)
	require.Error(t, ev.Error)
	assert.Contains(t, ev.Error.Error(), "conversation.model.invalid")

	_, ok = <-events
	assert.False(t, ok, "channel should be closed")
}

func TestSendAsyncUnsupportedModel(t *testing.T) {
	const unsupportedModel = "claude-sonnet-4-5"
	info := llmmodel.GetModelInfo(llmmodel.ModelID(unsupportedModel))
	if info.ID == llmmodel.ModelIDUnknown {
		t.Skipf("test requires llmmodel to register %q", unsupportedModel)
	}
	require.False(t, modelSupportsAPIType(info, llmmodel.ProviderTypeOpenAIResponses), "expected test model to lack openai_responses support")

	conv := NewConversation(llmmodel.ModelID(unsupportedModel), "system prompt")
	require.NoError(t, conv.AddUserTurn("hello"))

	events := conv.SendAsync(context.Background())
	ev, ok := <-events
	require.True(t, ok, "expected error event")
	assert.Equal(t, EventTypeError, ev.Type)
	require.Error(t, ev.Error)
	assert.Contains(t, ev.Error.Error(), "conversation.model.unsupported_api")
	assert.Contains(t, ev.Error.Error(), unsupportedModel)

	_, ok = <-events
	assert.False(t, ok, "channel should be closed")
}
