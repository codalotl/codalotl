package llmstream

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/mockllm/mockopenai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingDiagnosticHook struct {
	turns []diagnosticRecordedTurn
}

type diagnosticRecordedTurn struct {
	Request  map[string]any
	Response map[string]any
}

func (h *recordingDiagnosticHook) AddTurn(request map[string]any, response map[string]any) {
	h.turns = append(h.turns, diagnosticRecordedTurn{
		Request:  request,
		Response: response,
	})
}

func TestAddDiagnosticHook_UnregisterStopsReceiving(t *testing.T) {
	handler, err := mockopenai.NewHandler([]byte(`{
		"responses": [
			{
				"name": "first",
				"consume": true,
				"request": {
					"model": "mock-model-diagnostic-hooks",
					"input": {"match": "partial", "text": "First prompt"}
				},
				"response": {
					"id": "resp_diag_1",
					"object": "response",
					"usage": {
						"input_tokens": 10,
						"output_tokens": 4
					},
					"output": [
						{
							"id": "msg_diag_1",
							"type": "message",
							"role": "assistant",
							"content": [
								{"type": "output_text", "text": "First reply"}
							]
						}
					]
				}
			},
			{
				"name": "second",
				"consume": true,
				"request": {
					"model": "mock-model-diagnostic-hooks",
					"previous_response_id": "resp_diag_1",
					"input": {"match": "partial", "text": "Second prompt"}
				},
				"response": {
					"id": "resp_diag_2",
					"object": "response",
					"usage": {
						"input_tokens": 12,
						"output_tokens": 5
					},
					"output": [
						{
							"id": "msg_diag_2",
							"type": "message",
							"role": "assistant",
							"content": [
								{"type": "output_text", "text": "Second reply"}
							]
						}
					]
				}
			}
		]
	}`))
	require.NoError(t, err)

	server := httptest.NewServer(handler)
	defer server.Close()

	modelID := registerDiagnosticHookTestModel(t, "hooks", "mock-model-diagnostic-hooks", server.URL)
	hookA := &recordingDiagnosticHook{}
	hookB := &recordingDiagnosticHook{}
	unregisterA := AddDiagnosticHook(hookA)
	unregisterB := AddDiagnosticHook(hookB)
	defer unregisterA()
	defer unregisterB()

	conv := NewConversation(modelID, "You are concise.")
	require.NoError(t, conv.AddUserTurn("First prompt"))
	drainEventsWithoutErrors(t, conv.SendAsync(newDiagnosticHookTestContext(t)))

	unregisterA()
	unregisterA()

	require.NoError(t, conv.AddUserTurn("Second prompt"))
	drainEventsWithoutErrors(t, conv.SendAsync(newDiagnosticHookTestContext(t)))

	require.Len(t, hookA.turns, 1)
	require.Len(t, hookB.turns, 2)

	assert.Equal(t, "mock-model-diagnostic-hooks", hookA.turns[0].Request["model"])
	assert.Equal(t, "resp_diag_1", hookA.turns[0].Response["id"])

	assert.Equal(t, "mock-model-diagnostic-hooks", hookB.turns[1].Request["model"])
	assert.Equal(t, "resp_diag_1", hookB.turns[1].Request["previous_response_id"])
	assert.Equal(t, "resp_diag_2", hookB.turns[1].Response["id"])

	require.NoError(t, mockopenai.AssertAllConsumed(handler))
}

func TestDiagnosticHook_RecordsOpenAICompletedTurn(t *testing.T) {
	handler, err := mockopenai.NewHandler([]byte(`{
		"responses": [
			{
				"name": "single",
				"consume": true,
				"request": {
					"model": "mock-model-diagnostic-single",
					"input": {"match": "partial", "text": "Prompt"}
				},
				"response": {
					"id": "resp_diag_single",
					"object": "response",
					"usage": {
						"input_tokens": 10,
						"output_tokens": 4
					},
					"output": [
						{
							"id": "msg_diag_single",
							"type": "message",
							"role": "assistant",
							"content": [
								{"type": "output_text", "text": "Reply"}
							]
						}
					]
				}
			}
		]
	}`))
	require.NoError(t, err)

	server := httptest.NewServer(handler)
	defer server.Close()

	modelID := registerDiagnosticHookTestModel(t, "single", "mock-model-diagnostic-single", server.URL)
	hook := &recordingDiagnosticHook{}
	unregister := AddDiagnosticHook(hook)
	defer unregister()

	conv := NewConversation(modelID, "You are concise.")
	require.NoError(t, conv.AddUserTurn("Prompt"))
	drainEventsWithoutErrors(t, conv.SendAsync(newDiagnosticHookTestContext(t)))

	require.Len(t, hook.turns, 1)
	assert.Equal(t, "mock-model-diagnostic-single", hook.turns[0].Request["model"])
	assert.Equal(t, true, hook.turns[0].Request["parallel_tool_calls"])
	assert.Contains(t, mustMarshalDiagnosticJSON(t, hook.turns[0].Request), "Prompt")

	assert.Equal(t, "resp_diag_single", hook.turns[0].Response["id"])
	assert.Equal(t, "completed", hook.turns[0].Response["status"])
	assert.Contains(t, mustMarshalDiagnosticJSON(t, hook.turns[0].Response), "Reply")

	require.NoError(t, mockopenai.AssertAllConsumed(handler))
}

func registerDiagnosticHookTestModel(t *testing.T, suffix string, providerModelID string, baseURL string) llmmodel.ModelID {
	t.Helper()

	modelID := llmmodel.ModelID("test-openai-diagnostic-" + sanitizeDiagnosticHookTestName(suffix))
	err := llmmodel.AddCustomModel(modelID, llmmodel.ProviderIDOpenAI, providerModelID, llmmodel.ModelOverrides{
		APIActualKey:   "test-openai-key",
		APIEndpointURL: baseURL,
	})
	require.NoError(t, err)
	return modelID
}

func sanitizeDiagnosticHookTestName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		b.WriteRune('-')
	}
	return strings.Trim(b.String(), "-")
}

func newDiagnosticHookTestContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func drainEventsWithoutErrors(t *testing.T, ch <-chan Event) {
	t.Helper()

	for event := range ch {
		require.NotEqual(t, EventTypeError, event.Type)
	}
}

func mustMarshalDiagnosticJSON(t *testing.T, value any) string {
	t.Helper()

	data, err := json.Marshal(value)
	require.NoError(t, err)
	return string(data)
}
