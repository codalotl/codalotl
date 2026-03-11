package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/codalotl/codalotl/internal/q/sseclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient_UsesEnvironmentAPIKey(t *testing.T) {
	t.Parallel()

	client, err := NewClient(context.Background(), &ClientConfig{
		envVarProvider: func() map[string]string {
			return map[string]string{
				"GOOGLE_API_KEY": "google-key",
				"GEMINI_API_KEY": "gemini-key",
			}
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "google-key", client.apiKey)
}

func TestModelsGenerateContentStream_BasicStubbedFlow(t *testing.T) {
	t.Parallel()

	type seenRequest struct {
		Method  string
		Path    string
		Query   string
		Headers http.Header
		Body    []byte
	}
	seenCh := make(chan seenRequest, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seenCh <- seenRequest{
			Method:  r.Method,
			Path:    r.URL.Path,
			Query:   r.URL.RawQuery,
			Headers: r.Header.Clone(),
			Body:    body,
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"hel\"}]},\"index\":0}],\"responseId\":\"resp_1\"}\n\n")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"lo\"},{\"functionCall\":{\"id\":\"call_1\",\"name\":\"get_weather\",\"args\":{\"location\":\"San Francisco\"}}}]},\"finishReason\":\"STOP\",\"finishMessage\":\"Model generated function call(s).\",\"index\":0}],\"usageMetadata\":{\"promptTokenCount\":11,\"candidatesTokenCount\":7,\"totalTokenCount\":18},\"responseId\":\"resp_1\"}\n\n")
	}))
	defer srv.Close()

	client, err := NewClient(context.Background(), &ClientConfig{
		APIKey:     "test-key",
		HTTPClient: srv.Client(),
		HTTPOptions: HTTPOptions{
			BaseURL: srv.URL,
			Headers: http.Header{
				"X-Client-Header": []string{"client"},
			},
		},
	})
	require.NoError(t, err)

	stream := client.Models.GenerateContentStream(context.Background(), "gemini-test", []*Content{
		{
			Role: string(RoleUser),
			Parts: []*Part{
				{Text: "Say hello"},
			},
		},
		{
			Role: string(RoleUser),
			Parts: []*Part{
				{
					FunctionResponse: &FunctionResponse{
						ID:       "call_1",
						Name:     "get_weather",
						Response: map[string]any{"output": "72 F"},
					},
				},
			},
		},
	}, &GenerateContentConfig{
		SystemInstruction: &Content{
			Parts: []*Part{{Text: "Be precise."}},
		},
		Temperature:     Ptr(float32(0.25)),
		CandidateCount:  1,
		MaxOutputTokens: 64,
		StopSequences:   []string{"STOP"},
		ThinkingConfig: &ThinkingConfig{
			IncludeThoughts: true,
			ThinkingLevel:   ThinkingLevelLow,
		},
		Tools: []*Tool{{
			FunctionDeclarations: []*FunctionDeclaration{{
				Name:                 "get_weather",
				Description:          "Get the weather",
				ParametersJsonSchema: map[string]any{"type": "object"},
			}},
		}},
		ToolConfig: &ToolConfig{
			FunctionCallingConfig: &FunctionCallingConfig{
				Mode:                 FunctionCallingConfigModeAny,
				AllowedFunctionNames: []string{"get_weather"},
			},
		},
		HTTPOptions: &HTTPOptions{
			Headers: http.Header{
				"X-Request-Header": []string{"request"},
			},
		},
	})

	var responses []*GenerateContentResponse
	for resp, err := range stream {
		require.NoError(t, err)
		responses = append(responses, resp)
	}

	seen := <-seenCh
	assert.Equal(t, http.MethodPost, seen.Method)
	assert.Equal(t, "/v1beta/models/gemini-test:streamGenerateContent", seen.Path)
	assert.Equal(t, "alt=sse", seen.Query)
	assert.Equal(t, "test-key", seen.Headers.Get("x-goog-api-key"))
	assert.Equal(t, "text/event-stream", seen.Headers.Get("accept"))
	assert.Equal(t, "application/json", seen.Headers.Get("content-type"))
	assert.Equal(t, "client", seen.Headers.Get("X-Client-Header"))
	assert.Equal(t, "request", seen.Headers.Get("X-Request-Header"))

	var bodyPayload map[string]any
	require.NoError(t, json.Unmarshal(seen.Body, &bodyPayload))
	assert.Contains(t, bodyPayload, "contents")
	assert.Contains(t, bodyPayload, "systemInstruction")
	assert.Contains(t, bodyPayload, "tools")
	assert.Contains(t, bodyPayload, "toolConfig")
	generationConfig, ok := bodyPayload["generationConfig"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 0.25, generationConfig["temperature"])
	assert.Equal(t, float64(1), generationConfig["candidateCount"])
	assert.Equal(t, float64(64), generationConfig["maxOutputTokens"])
	assert.Equal(t, []any{"STOP"}, generationConfig["stopSequences"])

	require.Len(t, responses, 2)
	assert.Equal(t, "resp_1", responses[0].ResponseID)
	require.Len(t, responses[1].Candidates, 1)
	require.NotNil(t, responses[1].Candidates[0].Content)
	require.Len(t, responses[1].Candidates[0].Content.Parts, 2)
	require.NotNil(t, responses[1].Candidates[0].Content.Parts[1].FunctionCall)
	assert.Equal(t, "call_1", responses[1].Candidates[0].Content.Parts[1].FunctionCall.ID)
	assert.Equal(t, FinishReasonStop, responses[1].Candidates[0].FinishReason)
	require.NotNil(t, responses[1].UsageMetadata)
	assert.EqualValues(t, 11, responses[1].UsageMetadata.PromptTokenCount)
}

func TestModelsGenerateContentStream_PreservesModelPrefix(t *testing.T) {
	t.Parallel()

	seenCh := make(chan string, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenCh <- r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"responseId\":\"resp_1\"}\n\n")
	}))
	defer srv.Close()

	client, err := NewClient(context.Background(), &ClientConfig{
		APIKey:     "test-key",
		HTTPClient: srv.Client(),
		HTTPOptions: HTTPOptions{
			BaseURL: srv.URL,
		},
	})
	require.NoError(t, err)

	for _, model := range []string{"gemini-test", "models/gemini-test"} {
		stream := client.Models.GenerateContentStream(context.Background(), model, nil, nil)
		for _, err := range stream {
			require.NoError(t, err)
		}
	}

	assert.Equal(t, "/v1beta/models/gemini-test:streamGenerateContent", <-seenCh)
	assert.Equal(t, "/v1beta/models/gemini-test:streamGenerateContent", <-seenCh)
}

func TestModelsGenerateContentStream_Non200ReturnsOpenError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"code":400,"message":"bad request","status":"INVALID_ARGUMENT"}}`)
	}))
	defer srv.Close()

	client, err := NewClient(context.Background(), &ClientConfig{
		APIKey:     "test-key",
		HTTPClient: srv.Client(),
		HTTPOptions: HTTPOptions{
			BaseURL: srv.URL,
		},
	})
	require.NoError(t, err)

	stream := client.Models.GenerateContentStream(context.Background(), "gemini-test", nil, nil)
	var iterErr error
	for _, err := range stream {
		iterErr = err
	}

	require.Error(t, iterErr)
	assert.ErrorIs(t, iterErr, sseclient.ErrUnexpectedStatus)

	var openErr *sseclient.OpenError
	require.ErrorAs(t, iterErr, &openErr)
	require.NotNil(t, openErr.Response)
	assert.Equal(t, http.StatusBadRequest, openErr.Response.StatusCode)
}

func TestModelsGenerateContentStream_InvalidEventData(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: not-json\n\n")
	}))
	defer srv.Close()

	client, err := NewClient(context.Background(), &ClientConfig{
		APIKey:     "test-key",
		HTTPClient: srv.Client(),
		HTTPOptions: HTTPOptions{
			BaseURL: srv.URL,
		},
	})
	require.NoError(t, err)

	stream := client.Models.GenerateContentStream(context.Background(), "gemini-test", nil, nil)
	var iterErr error
	for _, err := range stream {
		iterErr = err
	}

	require.Error(t, iterErr)
	assert.False(t, errors.Is(iterErr, io.EOF))
}
