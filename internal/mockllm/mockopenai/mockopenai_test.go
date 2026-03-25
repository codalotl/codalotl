package mockopenai

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

	req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.4","input":"Tell me a story about a unicorn."}`))
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

func doResponsesRequest(t *testing.T, baseURL string, headers map[string]string, body string) string {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/responses", bytes.NewBufferString(body))
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
