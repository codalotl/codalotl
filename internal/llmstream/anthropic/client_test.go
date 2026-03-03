package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/q/sseclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientStreamMessages_BasicStubbedFlow(t *testing.T) {
	t.Parallel()

	type seenRequest struct {
		Method  string
		Path    string
		Headers http.Header
		Body    []byte
	}
	seenCh := make(chan seenRequest, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seenCh <- seenRequest{
			Method:  r.Method,
			Path:    r.URL.Path,
			Headers: r.Header.Clone(),
			Body:    body,
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("request-id", "req_123")
		_, _ = io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"role\":\"assistant\",\"model\":\"claude-test\",\"content\":[],\"usage\":{\"input_tokens\":1,\"cache_creation_input_tokens\":10,\"cache_read_input_tokens\":0,\"output_tokens\":0,\"cache_creation\":{\"ephemeral_5m_input_tokens\":10}}}}\n\n")
		_, _ = io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n")
		_, _ = io.WriteString(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":\"\"},\"usage\":{\"input_tokens\":2,\"cache_creation_input_tokens\":10,\"cache_read_input_tokens\":7,\"output_tokens\":3,\"cache_creation\":{\"ephemeral_5m_input_tokens\":10}}}\n\n")
		_, _ = io.WriteString(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
		_, _ = io.WriteString(w, "event: ping\ndata: {\"type\":\"ping\"}\n\n")
	}))
	defer srv.Close()

	temperature := 0.5
	client := New(
		"test-key",
		WithBaseURL(srv.URL),
		WithHTTPClient(srv.Client()),
		WithBeta("beta-1"),
		WithBeta("beta-2"),
	)

	stream, err := client.StreamMessages(context.Background(), MessageRequest{
		Model:     "claude-test",
		MaxTokens: 128,
		OutputConfig: &OutputConfigParam{
			Effort: "high",
			Format: &OutputFormatParam{
				Type:   "json_schema",
				Schema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`),
			},
		},
		CacheControl: &CacheControlParam{
			Type: "ephemeral",
			TTL:  "5m",
		},
		Messages: []MessageParam{
			{
				Role: "user",
				Content: []ContentBlockParam{
					{
						Type: "text",
						Text: "hello",
						CacheControl: &CacheControlParam{
							Type: "ephemeral",
							TTL:  "5m",
						},
					},
					{Type: "tool_result", ToolUseID: "tool_1", Result: "done"},
				},
			},
		},
		Temperature: &temperature,
	})
	require.NoError(t, err)
	defer stream.Close()

	seen := <-seenCh
	assert.Equal(t, http.MethodPost, seen.Method)
	assert.Equal(t, "/v1/messages", seen.Path)
	assert.Equal(t, "test-key", seen.Headers.Get("x-api-key"))
	assert.Equal(t, DefaultVersion, seen.Headers.Get("anthropic-version"))
	assert.Equal(t, "application/json", seen.Headers.Get("content-type"))
	assert.Equal(t, []string{"beta-1", "beta-2"}, seen.Headers.Values("anthropic-beta"))

	var bodyPayload map[string]any
	require.NoError(t, json.Unmarshal(seen.Body, &bodyPayload))
	assert.Equal(t, true, bodyPayload["stream"])
	outputConfig, ok := bodyPayload["output_config"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "high", outputConfig["effort"])
	format, ok := outputConfig["format"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "json_schema", format["type"])
	schema, ok := format["schema"].(map[string]any)
	require.True(t, ok)
	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	city, ok := properties["city"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "string", city["type"])
	assert.Contains(t, string(seen.Body), "\"tool_use_id\":\"tool_1\"")
	assert.Contains(t, string(seen.Body), "\"content\":\"done\"")
	assert.Contains(t, string(seen.Body), "\"cache_control\":{\"type\":\"ephemeral\",\"ttl\":\"5m\"}")
	assert.Equal(t, "req_123", stream.RequestID())

	event, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, EventTypeMessageStart, event.Type)
	require.NotNil(t, event.Message)
	assert.Equal(t, "msg_1", event.Message.ID)
	assert.Equal(t, int64(10), event.Message.Usage.CacheCreationInputTokens)
	assert.Equal(t, int64(10), event.Message.Usage.CacheCreation.Ephemeral5mInputTokens)

	event, err = stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, EventTypeContentBlockDelta, event.Type)
	require.NotNil(t, event.Delta)
	assert.Equal(t, 0, event.Index)
	assert.Equal(t, "text_delta", event.Delta.Type)
	assert.Equal(t, "hello", event.Delta.Text)

	event, err = stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, EventTypeMessageDelta, event.Type)
	require.NotNil(t, event.MessageDelta)
	assert.Equal(t, "end_turn", event.MessageDelta.StopReason)
	assert.Equal(t, int64(3), event.MessageDelta.Usage.OutputTokens)
	assert.Equal(t, int64(7), event.MessageDelta.Usage.CacheReadInputTokens)

	event, err = stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, EventTypeMessageStop, event.Type)

	event, err = stream.Recv()
	assert.ErrorIs(t, err, io.EOF)
	assert.Equal(t, Event{}, event)
}

func TestClientStreamMessages_StreamErrorEvent(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"try again\"}}\n\n")
		_, _ = io.WriteString(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer srv.Close()

	client := New("test-key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	stream, err := client.StreamMessages(context.Background(), MessageRequest{
		Model:     "claude-test",
		MaxTokens: 8,
		Messages:  []MessageParam{{Role: "user", Content: []ContentBlockParam{{Type: "text", Text: "hi"}}}},
	})
	require.NoError(t, err)
	defer stream.Close()

	event, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, EventTypeError, event.Type)
	require.NotNil(t, event.Error)
	assert.Equal(t, "overloaded_error", event.Error.Type)
	assert.Equal(t, "try again", event.Error.Message)

	event, err = stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, EventTypeMessageStop, event.Type)

	event, err = stream.Recv()
	assert.ErrorIs(t, err, io.EOF)
}

func TestClientStreamMessages_Non200ReturnsOpenError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"bad request"}}`)
	}))
	defer srv.Close()

	client := New("test-key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	_, err := client.StreamMessages(context.Background(), MessageRequest{
		Model:     "claude-test",
		MaxTokens: 8,
		Messages:  []MessageParam{{Role: "user", Content: []ContentBlockParam{{Type: "text", Text: "hi"}}}},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, sseclient.ErrUnexpectedStatus)

	var openErr *sseclient.OpenError
	require.ErrorAs(t, err, &openErr)
	require.NotNil(t, openErr.Response)
	assert.Equal(t, http.StatusBadRequest, openErr.Response.StatusCode)
	assert.True(t, errors.Is(openErr, sseclient.ErrUnexpectedStatus))
}

func TestStream_DefaultMessageEventTypeFallback(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer srv.Close()

	client := New("test-key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	stream, err := client.StreamMessages(context.Background(), MessageRequest{
		Model:     "claude-test",
		MaxTokens: 8,
		Messages:  []MessageParam{{Role: "user", Content: []ContentBlockParam{{Type: "text", Text: "hi"}}}},
	})
	require.NoError(t, err)
	defer stream.Close()

	event, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, EventTypeMessageStop, event.Type)
	assert.True(t, strings.Contains(string(event.Raw), "\"type\":\"message_stop\""))

	event, err = stream.Recv()
	assert.ErrorIs(t, err, io.EOF)
}
