package mockopenai

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHandler_StreamResponseFromJSONC(t *testing.T) {
	handler, err := NewHandler([]byte(`{
		// json-with-comments is supported
		"responses": [
			{
				"name": "storybook",
				"request": {
					"model": "gpt-5.4",
					"input": {"match": "partial", "text": "unicorn"},
				},
				"headers": [
					{"name": "Authorization", "value": {"match": "partial", "text": "Bearer"}},
				],
				"response": {
					"id": "resp_story",
					"object": "response",
					"output": [
						{
							"id": "msg_story",
							"type": "message",
							"role": "assistant",
							"content": [
								{"type": "output_text", "text": "Once upon a time there was a unicorn."},
							],
						},
					],
				},
			},
		],
	}`))
	require.NoError(t, err)

	server := httptest.NewServer(handler)
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL+"/responses", bytes.NewBufferString(`{"model":"gpt-5.4","input":"Tell me a story about a unicorn."}`))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer test-token")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/event-stream")
	assert.Contains(t, string(body), `"type":"response.created"`)
	assert.Contains(t, string(body), `"type":"response.output_text.delta"`)
	assert.Contains(t, string(body), `Once upon a time there was a unicorn.`)
	assert.Contains(t, string(body), `"type":"response.completed"`)
	assert.Contains(t, string(body), `data: [DONE]`)
}

func TestHandler_ConsumeAndOrder(t *testing.T) {
	handler, err := NewHandler([]byte(`{
		"responses": [
			{
				"name": "first",
				"consume": true,
				"request": {
					"model": "gpt-5.4",
					"input": "Hello",
				},
				"response": {
					"id": "resp_first",
					"object": "response",
					"output": [
						{
							"id": "msg_first",
							"type": "message",
							"role": "assistant",
							"content": [
								{"type": "output_text", "text": "first response"},
							],
						},
					],
				},
			},
			{
				"name": "second",
				"request": {
					"model": "gpt-5.4",
					"input": "Hello",
				},
				"response": {
					"id": "resp_second",
					"object": "response",
					"output": [
						{
							"id": "msg_second",
							"type": "message",
							"role": "assistant",
							"content": [
								{"type": "output_text", "text": "second response"},
							],
						},
					],
				},
			}
		]
	}`))
	require.NoError(t, err)

	server := httptest.NewServer(handler)
	defer server.Close()

	firstBody := doResponsesRequest(t, server.URL, nil, `{"model":"gpt-5.4","input":"Hello"}`)
	secondBody := doResponsesRequest(t, server.URL, nil, `{"model":"gpt-5.4","input":"Hello"}`)

	assert.Contains(t, firstBody, `first response`)
	assert.Contains(t, secondBody, `second response`)
}

func TestHandler_HeaderMatching(t *testing.T) {
	handler, err := NewHandler([]byte(`{
		"responses": [
			{
				"name": "tenant-a",
				"request": {
					"model": "gpt-5.4",
					"input": {"match": "partial", "text": "bedtime"},
				},
				"headers": [
					{"name": "X-Tenant-ID", "value": "tenant-a"},
				],
				"response": {
					"id": "resp_tenant_a",
					"object": "response",
					"output": [
						{
							"id": "msg_tenant_a",
							"type": "message",
							"role": "assistant",
							"content": [
								{"type": "output_text", "text": "tenant a"},
							],
						},
					],
				},
			},
			{
				"name": "tenant-b",
				"request": {
					"model": "gpt-5.4",
					"input": {"match": "partial", "text": "bedtime"},
				},
				"headers": [
					{"name": "X-Tenant-ID", "value": "tenant-b"},
				],
				"response": {
					"id": "resp_tenant_b",
					"object": "response",
					"output": [
						{
							"id": "msg_tenant_b",
							"type": "message",
							"role": "assistant",
							"content": [
								{"type": "output_text", "text": "tenant b"},
							],
						},
					],
				},
			}
		]
	}`))
	require.NoError(t, err)

	server := httptest.NewServer(handler)
	defer server.Close()

	bodyA := doResponsesRequest(t, server.URL, map[string]string{"X-Tenant-ID": "tenant-a"}, `{"model":"gpt-5.4","input":"Tell me a bedtime story."}`)
	bodyB := doResponsesRequest(t, server.URL, map[string]string{"X-Tenant-ID": "tenant-b"}, `{"model":"gpt-5.4","input":"Tell me a bedtime story."}`)

	assert.Contains(t, bodyA, `tenant a`)
	assert.Contains(t, bodyB, `tenant b`)

	req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.4","input":"Tell me a bedtime story."}`))
	require.NoError(t, err)
	req.Header.Set("X-Tenant-ID", "tenant-c")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestNewHandlerFromFileAndRequestErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "responses.jsonc")
	err := os.WriteFile(path, []byte(`{
		"responses": [
			{
				"request": {
					"model": "gpt-5.4",
					"input": "Hello",
				},
				"response": {
					"id": "resp_file",
					"object": "response",
					"output": [
						{
							"id": "msg_file",
							"type": "message",
							"role": "assistant",
							"content": [
								{"type": "output_text", "text": "from file"},
							],
						},
					],
				},
			},
		],
	}`), 0o644)
	require.NoError(t, err)

	handler, err := NewHandlerFromFile(path)
	require.NoError(t, err)

	server := httptest.NewServer(handler)
	defer server.Close()

	t.Run("success", func(t *testing.T) {
		body := doResponsesRequest(t, server.URL, nil, `{"model":"gpt-5.4","input":"Hello"}`)
		assert.Contains(t, body, `from file`)
	})

	t.Run("invalid json request", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/responses", bytes.NewBufferString(`{`))
		require.NoError(t, err)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("wrong method", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/v1/responses")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
		assert.Equal(t, http.MethodPost, resp.Header.Get("Allow"))
	})

	t.Run("wrong path", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/not-responses")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}

func TestHandler_ResponseCompletedDefaultsStatusToCompleted(t *testing.T) {
	handler, err := NewHandler([]byte(`{
		"responses": [
			{
				"request": {
					"model": "gpt-5.4",
					"input": "Hello",
				},
				"response": {
					"id": "resp_status",
					"object": "response",
					"output": [
						{
							"id": "msg_status",
							"type": "message",
							"role": "assistant",
							"content": [
								{"type": "output_text", "text": "done"},
							],
						},
					],
				},
			},
		]
	}`))
	require.NoError(t, err)

	server := httptest.NewServer(handler)
	defer server.Close()

	body := doResponsesRequest(t, server.URL, nil, `{"model":"gpt-5.4","input":"Hello"}`)
	events := parseSSEEvents(t, body)

	completed := firstEventOfType(t, events, "response.completed")
	response, ok := completed["response"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "completed", response["status"])
}

func TestHandler_ResponseCreatedIsInProgressAndEmpty(t *testing.T) {
	handler, err := NewHandler([]byte(`{
		"responses": [
			{
				"request": {
					"model": "gpt-5.4",
					"input": "Hello",
				},
				"response": {
					"id": "resp_created",
					"object": "response",
					"status": "completed",
					"usage": {
						"input_tokens": 12,
						"output_tokens": 7
					},
					"output": [
						{
							"id": "msg_created",
							"type": "message",
							"role": "assistant",
							"content": [
								{"type": "output_text", "text": "hello from the mock"},
							],
						},
						{
							"id": "fc_created",
							"type": "function_call",
							"call_id": "call_created",
							"name": "lookup_weather",
							"arguments": "{\"city\":\"San Francisco\"}",
						}
					],
				},
			},
		]
	}`))
	require.NoError(t, err)

	server := httptest.NewServer(handler)
	defer server.Close()

	body := doResponsesRequest(t, server.URL, nil, `{"model":"gpt-5.4","input":"Hello"}`)
	events := parseSSEEvents(t, body)

	created := firstEventOfType(t, events, "response.created")
	response, ok := created["response"].(map[string]any)
	require.True(t, ok)

	assert.Equal(t, "in_progress", response["status"])
	assert.Nil(t, response["usage"])

	output, ok := response["output"].([]any)
	require.True(t, ok)
	assert.Empty(t, output)

	completed := firstEventOfType(t, events, "response.completed")
	completedResponse, ok := completed["response"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "completed", completedResponse["status"])

	completedOutput, ok := completedResponse["output"].([]any)
	require.True(t, ok)
	require.Len(t, completedOutput, 2)
}

func TestHandler_StreamsToolOutputItemDoneEvents(t *testing.T) {
	handler, err := NewHandler([]byte(`{
		"responses": [
			{
				"request": {
					"model": "gpt-5.4",
					"input": "Run tools",
				},
				"response": {
					"id": "resp_tools",
					"object": "response",
					"output": [
						{
							"id": "fc_123",
							"type": "function_call",
							"call_id": "call_fc_123",
							"name": "lookup_weather",
							"arguments": "{\"city\":\"San Francisco\"}",
						},
						{
							"id": "ct_456",
							"type": "custom_tool_call",
							"call_id": "call_ct_456",
							"name": "apply_patch",
							"input": "*** Begin Patch\n*** End Patch\n",
						},
					],
				},
			},
		]
	}`))
	require.NoError(t, err)

	server := httptest.NewServer(handler)
	defer server.Close()

	body := doResponsesRequest(t, server.URL, nil, `{"model":"gpt-5.4","input":"Run tools"}`)
	events := parseSSEEvents(t, body)

	assert.NotEmpty(t, eventsOfType(events, "response.function_call_arguments.delta"))
	functionDone := firstEventOfType(t, events, "response.function_call_arguments.done")
	assert.Equal(t, "lookup_weather", functionDone["name"])
	assert.Equal(t, `{"city":"San Francisco"}`, functionDone["arguments"])

	assert.NotEmpty(t, eventsOfType(events, "response.custom_tool_call_input.delta"))
	customDone := firstEventOfType(t, events, "response.custom_tool_call_input.done")
	assert.Equal(t, "*** Begin Patch\n*** End Patch\n", customDone["input"])

	outputItemDoneEvents := eventsOfType(events, "response.output_item.done")
	require.Len(t, outputItemDoneEvents, 2)

	functionItem, ok := outputItemDoneEvents[0]["item"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "function_call", functionItem["type"])
	assert.Equal(t, "call_fc_123", functionItem["call_id"])
	assert.Equal(t, "completed", functionItem["status"])

	customItem, ok := outputItemDoneEvents[1]["item"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "custom_tool_call", customItem["type"])
	assert.Equal(t, "call_ct_456", customItem["call_id"])
	assert.Equal(t, "*** Begin Patch\n*** End Patch\n", customItem["input"])
}

func TestHandler_PartialMatcherSupportsStructuredStringsWithAngleBrackets(t *testing.T) {
	handler, err := NewHandler([]byte(`{
		"responses": [
			{
				"request": {
					"model": "gpt-5.4",
					"input": {"match": "partial", "text": "<file name=\"hello.txt\""}
				},
				"response": {
					"id": "resp_structured_match",
					"object": "response",
					"output": [
						{
							"id": "msg_structured_match",
							"type": "message",
							"role": "assistant",
							"content": [
								{"type": "output_text", "text": "matched"}
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

	body := doResponsesRequest(t, server.URL, nil, `{
		"model":"gpt-5.4",
		"input":[
			{
				"type":"function_call_output",
				"call_id":"call_123",
				"output":"<file name=\"hello.txt\">hi</file>"
			}
		]
	}`)

	assert.Contains(t, body, `matched`)
}

func TestHandler_PartialMatcherSupportsMultipleRequiredTexts(t *testing.T) {
	handler, err := NewHandler([]byte(`{
		"responses": [
			{
				"request": {
					"model": "gpt-5.4",
					"input": {
						"match": "partial",
						"texts": [
							"<apply-patch ok=\"true\">",
							"$ golangci-lint run ./...",
							"$ go test ./..."
						]
					}
				},
				"response": {
					"id": "resp_multi_match",
					"object": "response",
					"output": [
						{
							"id": "msg_multi_match",
							"type": "message",
							"role": "assistant",
							"content": [
								{"type": "output_text", "text": "matched"}
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

	body := doResponsesRequest(t, server.URL, nil, `{
		"model":"gpt-5.4",
		"input":[
			{
				"type":"custom_tool_call_output",
				"call_id":"call_123",
				"output":"<apply-patch ok=\"true\">\n$ golangci-lint run ./...\n$ go test ./...\n</apply-patch>"
			}
		]
	}`)

	assert.Contains(t, body, `matched`)

	req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/responses", bytes.NewBufferString(`{
		"model":"gpt-5.4",
		"input":[
			{
				"type":"custom_tool_call_output",
				"call_id":"call_123",
				"output":"<apply-patch ok=\"true\">\n$ go test ./...\n</apply-patch>"
			}
		]
	}`))
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestAssertAllConsumed(t *testing.T) {
	handler, err := NewHandler([]byte(`{
		"responses": [
			{
				"name": "used",
				"consume": true,
				"request": {
					"model": "gpt-5.4",
					"input": "Hello"
				},
				"response": {
					"id": "resp_used",
					"object": "response",
					"output": [
						{
							"id": "msg_used",
							"type": "message",
							"role": "assistant",
							"content": [
								{"type": "output_text", "text": "used"}
							]
						}
					]
				}
			},
			{
				"name": "unused",
				"consume": true,
				"request": {
					"model": "gpt-5.4",
					"input": "Never sent"
				},
				"response": {
					"id": "resp_unused",
					"object": "response",
					"output": [
						{
							"id": "msg_unused",
							"type": "message",
							"role": "assistant",
							"content": [
								{"type": "output_text", "text": "unused"}
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

	body := doResponsesRequest(t, server.URL, nil, `{"model":"gpt-5.4","input":"Hello"}`)
	assert.Contains(t, body, `used`)

	err = AssertAllConsumed(handler)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unused")
}

func doResponsesRequest(t *testing.T, baseURL string, headers map[string]string, body string) string {
	return doResponsesRequestToPath(t, baseURL, pathV1Responses, headers, body)
}

func doResponsesRequestToPath(t *testing.T, baseURL string, path string, headers map[string]string, body string) string {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, baseURL+path, bytes.NewBufferString(body))
	require.NoError(t, err)
	for name, value := range headers {
		req.Header.Set(name, value)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	return string(responseBody)
}

func parseSSEEvents(t *testing.T, body string) []map[string]any {
	t.Helper()

	lines := strings.Split(body, "\n")
	events := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		payload, ok := strings.CutPrefix(line, "data: ")
		if !ok || payload == "[DONE]" || payload == "" {
			continue
		}

		var event map[string]any
		require.NoError(t, json.Unmarshal([]byte(payload), &event))
		events = append(events, event)
	}

	return events
}

func eventsOfType(events []map[string]any, eventType string) []map[string]any {
	filtered := make([]map[string]any, 0)
	for _, event := range events {
		if event["type"] == eventType {
			filtered = append(filtered, event)
		}
	}
	return filtered
}

func firstEventOfType(t *testing.T, events []map[string]any, eventType string) map[string]any {
	t.Helper()

	filtered := eventsOfType(events, eventType)
	require.NotEmpty(t, filtered)
	return filtered[0]
}
