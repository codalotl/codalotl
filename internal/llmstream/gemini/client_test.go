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

func TestModelsGenerateContentStream_YieldsIncrementalEventsNotCumulativeSnapshots(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"hel\"}]},\"index\":0}],\"responseId\":\"resp_1\"}\n\n")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"lo\"}]},\"index\":0}],\"responseId\":\"resp_1\"}\n\n")
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

	var responses []*GenerateContentResponse
	var iterErrs []error
	for resp, err := range client.Models.GenerateContentStream(context.Background(), "gemini-test", nil, nil) {
		responses = append(responses, resp)
		iterErrs = append(iterErrs, err)
	}

	require.Len(t, responses, 2)
	require.Len(t, iterErrs, 2)
	require.NoError(t, iterErrs[0])
	require.NoError(t, iterErrs[1])
	require.NotNil(t, responses[0])
	require.NotNil(t, responses[1])
	require.Len(t, responses[0].Candidates, 1)
	require.Len(t, responses[1].Candidates, 1)
	require.NotNil(t, responses[0].Candidates[0].Content)
	require.NotNil(t, responses[1].Candidates[0].Content)
	require.Len(t, responses[0].Candidates[0].Content.Parts, 1)
	require.Len(t, responses[1].Candidates[0].Content.Parts, 1)
	assert.Equal(t, "hel", responses[0].Candidates[0].Content.Parts[0].Text)
	assert.Equal(t, "lo", responses[1].Candidates[0].Content.Parts[0].Text)
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

func TestModelsGenerateContentStream_RequestHeadersOverrideClientHeaders(t *testing.T) {
	t.Parallel()

	seenCh := make(chan http.Header, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenCh <- r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"responseId\":\"resp_1\"}\n\n")
	}))
	defer srv.Close()

	client, err := NewClient(context.Background(), &ClientConfig{
		APIKey:     "test-key",
		HTTPClient: srv.Client(),
		HTTPOptions: HTTPOptions{
			BaseURL: srv.URL,
			Headers: http.Header{
				"X-Shared-Header": []string{"client-value"},
				"X-Client-Only":   []string{"client-only"},
			},
		},
	})
	require.NoError(t, err)

	stream := client.Models.GenerateContentStream(context.Background(), "gemini-test", nil, &GenerateContentConfig{
		HTTPOptions: &HTTPOptions{
			Headers: http.Header{
				"X-Shared-Header": []string{"request-value-1", "request-value-2"},
				"X-Request-Only":  []string{"request-only"},
			},
		},
	})
	for _, err := range stream {
		require.NoError(t, err)
	}

	headers := <-seenCh
	assert.Equal(t, []string{"request-value-1", "request-value-2"}, headers.Values("X-Shared-Header"))
	assert.Equal(t, "client-only", headers.Get("X-Client-Only"))
	assert.Equal(t, "request-only", headers.Get("X-Request-Only"))
}

func TestClient_streamGenerateContentURL_ComposesBaseURLAndVersion(t *testing.T) {
	t.Parallel()

	client, err := NewClient(context.Background(), &ClientConfig{
		APIKey: "test-key",
		HTTPOptions: HTTPOptions{
			BaseURL: "https://example.com/custom-prefix",
		},
	})
	require.NoError(t, err)

	endpoint, err := client.streamGenerateContentURL("gemini-test", nil)
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/custom-prefix/v1beta/models/gemini-test:streamGenerateContent?alt=sse", endpoint)

	overrideEndpoint, err := client.streamGenerateContentURL("gemini-test", &GenerateContentConfig{
		HTTPOptions: &HTTPOptions{
			BaseURL: "https://override.example.com/alt-prefix",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "https://override.example.com/alt-prefix/v1beta/models/gemini-test:streamGenerateContent?alt=sse", overrideEndpoint)
}

func TestModelsGenerateContentStream_OpenFailureYieldsNilErrAndStops(t *testing.T) {
	t.Parallel()

	transportErr := errors.New("dial failed")
	client, err := NewClient(context.Background(), &ClientConfig{
		APIKey: "test-key",
		HTTPClient: &http.Client{
			Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
				return nil, transportErr
			}),
		},
	})
	require.NoError(t, err)

	var responses []*GenerateContentResponse
	var iterErrs []error
	for resp, err := range client.Models.GenerateContentStream(context.Background(), "gemini-test", nil, nil) {
		responses = append(responses, resp)
		iterErrs = append(iterErrs, err)
	}

	require.Len(t, responses, 1)
	require.Len(t, iterErrs, 1)
	assert.Nil(t, responses[0])
	require.Error(t, iterErrs[0])
	assert.ErrorIs(t, iterErrs[0], transportErr)

	var openErr *sseclient.OpenError
	require.ErrorAs(t, iterErrs[0], &openErr)
	assert.Nil(t, openErr.Response)
}

func TestModelsGenerateContentStream_Non200YieldsNilErrAndStops(t *testing.T) {
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

	var responses []*GenerateContentResponse
	var iterErrs []error
	for resp, err := range client.Models.GenerateContentStream(context.Background(), "gemini-test", nil, nil) {
		responses = append(responses, resp)
		iterErrs = append(iterErrs, err)
	}

	require.Len(t, responses, 1)
	require.Len(t, iterErrs, 1)
	assert.Nil(t, responses[0])
	iterErr := iterErrs[0]
	require.Error(t, iterErr)
	assert.ErrorIs(t, iterErr, sseclient.ErrUnexpectedStatus)

	var openErr *sseclient.OpenError
	require.ErrorAs(t, iterErr, &openErr)
	require.NotNil(t, openErr.Response)
	assert.Equal(t, http.StatusBadRequest, openErr.Response.StatusCode)
}

func TestModelsGenerateContentStream_ReadFailureYieldsNilErrAndStops(t *testing.T) {
	t.Parallel()

	readErr := errors.New("stream dropped")
	bodyReader, bodyWriter := io.Pipe()
	go func() {
		_, _ = io.WriteString(bodyWriter, "data: {\"responseId\":\"resp_1\"}\n\n")
		_ = bodyWriter.CloseWithError(readErr)
	}()

	client, err := NewClient(context.Background(), &ClientConfig{
		APIKey: "test-key",
		HTTPClient: &http.Client{
			Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header: http.Header{
						"Content-Type": []string{"text/event-stream"},
					},
					Body:    bodyReader,
					Request: req,
				}, nil
			}),
		},
	})
	require.NoError(t, err)

	var responses []*GenerateContentResponse
	var iterErrs []error
	for resp, err := range client.Models.GenerateContentStream(context.Background(), "gemini-test", nil, nil) {
		responses = append(responses, resp)
		iterErrs = append(iterErrs, err)
	}

	require.Len(t, responses, 2)
	require.Len(t, iterErrs, 2)
	require.NotNil(t, responses[0])
	require.NoError(t, iterErrs[0])
	assert.Nil(t, responses[1])
	require.Error(t, iterErrs[1])
	assert.ErrorContains(t, iterErrs[1], "stream dropped")
}

func TestModelsGenerateContentStream_DecodeFailureYieldsNilErrAndStops(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"responseId\":\"resp_1\"}\n\n")
		_, _ = io.WriteString(w, "data: not-json\n\n")
		_, _ = io.WriteString(w, "data: {\"responseId\":\"resp_2\"}\n\n")
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

	var responses []*GenerateContentResponse
	var iterErrs []error
	for resp, err := range client.Models.GenerateContentStream(context.Background(), "gemini-test", nil, nil) {
		responses = append(responses, resp)
		iterErrs = append(iterErrs, err)
	}

	require.Len(t, responses, 2)
	require.Len(t, iterErrs, 2)
	require.NotNil(t, responses[0])
	require.NoError(t, iterErrs[0])
	assert.Nil(t, responses[1])
	require.Error(t, iterErrs[1])
	assert.ErrorContains(t, iterErrs[1], "decode stream event")
	assert.False(t, errors.Is(iterErrs[1], io.EOF))
}

func TestFunctionResponse_UnmarshalDropsUnsupportedFields(t *testing.T) {
	t.Parallel()

	var resp GenerateContentResponse
	err := json.Unmarshal([]byte(`{
		"candidates": [{
			"content": {
				"parts": [{
					"functionResponse": {
						"id": "call_1",
						"name": "get_weather",
						"response": {"output": "72 F"},
						"scheduling": "INTERRUPT",
						"willContinue": true,
						"parts": [{"text": "ignored"}]
					}
				}]
			}
		}]
	}`), &resp)
	require.NoError(t, err)

	require.Len(t, resp.Candidates, 1)
	require.NotNil(t, resp.Candidates[0].Content)
	require.Len(t, resp.Candidates[0].Content.Parts, 1)
	require.NotNil(t, resp.Candidates[0].Content.Parts[0].FunctionResponse)
	assert.Equal(t, "call_1", resp.Candidates[0].Content.Parts[0].FunctionResponse.ID)
	assert.Equal(t, "get_weather", resp.Candidates[0].Content.Parts[0].FunctionResponse.Name)
	assert.Equal(t, map[string]any{"output": "72 F"}, resp.Candidates[0].Content.Parts[0].FunctionResponse.Response)

	body, err := json.Marshal(resp.Candidates[0].Content.Parts[0].FunctionResponse)
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"call_1","name":"get_weather","response":{"output":"72 F"}}`, string(body))
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
