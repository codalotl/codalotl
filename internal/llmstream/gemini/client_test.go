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

func TestClientCreateInteraction_BasicStubbedFlow(t *testing.T) {
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
		_, _ = io.WriteString(w, "data: {\"event_id\":\"evt_1\",\"event_type\":\"interaction.start\",\"interaction\":{\"id\":\"int_1\",\"model\":\"gemini-2.5-flash\",\"object\":\"interaction\",\"status\":\"in_progress\"}}\n\n")
		_, _ = io.WriteString(w, "data: {\"event_id\":\"evt_2\",\"event_type\":\"content.start\",\"index\":0,\"content\":{\"type\":\"text\"}}\n\n")
		_, _ = io.WriteString(w, "data: {\"event_id\":\"evt_3\",\"event_type\":\"content.delta\",\"index\":0,\"delta\":{\"type\":\"text\",\"text\":\"hello\"}}\n\n")
		_, _ = io.WriteString(w, "data: {\"event_id\":\"evt_4\",\"event_type\":\"interaction.complete\",\"interaction\":{\"id\":\"int_1\",\"model\":\"gemini-2.5-flash\",\"object\":\"interaction\",\"role\":\"model\",\"status\":\"completed\",\"usage\":{\"total_input_tokens\":5,\"total_output_tokens\":1,\"total_tokens\":6}}}\n\n")
		_, _ = io.WriteString(w, "data: {\"event_id\":\"evt_5\",\"event_type\":\"content.delta\",\"index\":0,\"delta\":{\"type\":\"text\",\"text\":\"ignored\"}}\n\n")
	}))
	defer srv.Close()

	temperature := 0.5
	maxOutput := int64(128)
	store := false
	client := New(
		"test-key",
		WithBaseURL(srv.URL),
		WithHTTPClient(srv.Client()),
	)

	stream, err := client.CreateInteraction(context.Background(), InteractionRequest{
		Model:             "gemini-2.5-flash",
		SystemInstruction: "You are concise.",
		Input: []Turn{
			{
				Role: "user",
				Content: []Content{
					{Type: "text", Text: "Say hello"},
					{Type: "function_result", Name: "get_weather", CallID: "call_1", Result: "sunny"},
				},
			},
		},
		Tools: []Tool{
			{
				Type:        "function",
				Name:        "get_weather",
				Description: "Gets weather.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"location": map[string]any{"type": "string"},
					},
				},
			},
		},
		ResponseFormat:   map[string]any{"type": "object"},
		ResponseMIMEType: "application/json",
		Store:            &store,
		GenerationConfig: &GenerationConfig{
			Temperature:       &temperature,
			MaxOutputTokens:   &maxOutput,
			ThinkingLevel:     "low",
			ThinkingSummaries: "none",
			StopSequences:     []string{"STOP"},
		},
		PreviousInteractionID: "int_prev",
	})
	require.NoError(t, err)
	defer stream.Close()

	seen := <-seenCh
	assert.Equal(t, http.MethodPost, seen.Method)
	assert.Equal(t, "/v1beta/interactions", seen.Path)
	assert.Equal(t, "alt=sse", seen.Query)
	assert.Equal(t, "test-key", seen.Headers.Get("x-goog-api-key"))
	assert.Equal(t, "application/json", seen.Headers.Get("content-type"))
	assert.Equal(t, "text/event-stream", seen.Headers.Get("accept"))

	var bodyPayload map[string]any
	require.NoError(t, json.Unmarshal(seen.Body, &bodyPayload))
	assert.Equal(t, true, bodyPayload["stream"])
	assert.Equal(t, "int_prev", bodyPayload["previous_interaction_id"])
	assert.Equal(t, "application/json", bodyPayload["response_mime_type"])
	assert.Equal(t, false, bodyPayload["store"])
	generationConfig, ok := bodyPayload["generation_config"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "low", generationConfig["thinking_level"])
	assert.Equal(t, "none", generationConfig["thinking_summaries"])

	event, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, EventTypeInteractionStart, event.Type)
	require.NotNil(t, event.Interaction)
	assert.Equal(t, "int_1", event.Interaction.ID)
	assert.Equal(t, "evt_1", event.EventID)
	assert.Equal(t, "evt_1", stream.LastEventID())

	event, err = stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, EventTypeContentStart, event.Type)
	require.NotNil(t, event.Content)
	assert.Equal(t, "text", event.Content.Type)
	assert.Equal(t, 0, event.Index)

	event, err = stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, EventTypeContentDelta, event.Type)
	require.NotNil(t, event.Delta)
	assert.Equal(t, "text", event.Delta.Type)
	assert.Equal(t, "hello", event.Delta.Text)

	event, err = stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, EventTypeInteractionComplete, event.Type)
	require.NotNil(t, event.Interaction)
	require.NotNil(t, event.Interaction.Usage)
	assert.EqualValues(t, 6, event.Interaction.Usage.TotalTokens)
	assert.Equal(t, "evt_4", stream.LastEventID())

	event, err = stream.Recv()
	assert.ErrorIs(t, err, io.EOF)
	assert.Equal(t, Event{}, event)
}

func TestClientCreateInteraction_Non200ReturnsOpenError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"bad request","code":"invalid_argument"}}`)
	}))
	defer srv.Close()

	client := New("test-key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	_, err := client.CreateInteraction(context.Background(), InteractionRequest{
		Model: "gemini-2.5-flash",
		Input: []Turn{{Role: "user", Content: []Content{{Type: "text", Text: "hi"}}}},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, sseclient.ErrUnexpectedStatus)

	var openErr *sseclient.OpenError
	require.ErrorAs(t, err, &openErr)
	require.NotNil(t, openErr.Response)
	assert.Equal(t, http.StatusBadRequest, openErr.Response.StatusCode)
	assert.True(t, errors.Is(openErr, sseclient.ErrUnexpectedStatus))
}

func TestClientGetInteractionStream_UsesResumeParams(t *testing.T) {
	t.Parallel()

	seenCh := make(chan *http.Request, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenCh <- r.Clone(r.Context())
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"event_id\":\"evt_9\",\"event_type\":\"interaction.complete\",\"interaction\":{\"id\":\"int_123\",\"status\":\"completed\"}}\n\n")
	}))
	defer srv.Close()

	client := New("test-key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	stream, err := client.GetInteractionStream(context.Background(), "int_123", &GetInteractionOptions{
		LastEventID:  "evt_8",
		IncludeInput: true,
	})
	require.NoError(t, err)
	defer stream.Close()

	seen := <-seenCh
	assert.Equal(t, http.MethodGet, seen.Method)
	assert.Equal(t, "/v1beta/interactions/int_123", seen.URL.Path)
	assert.Equal(t, "true", seen.URL.Query().Get("stream"))
	assert.Equal(t, "evt_8", seen.URL.Query().Get("last_event_id"))
	assert.Equal(t, "true", seen.URL.Query().Get("include_input"))
	assert.Equal(t, "test-key", seen.Header.Get("x-goog-api-key"))

	event, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, EventTypeInteractionComplete, event.Type)

	_, err = stream.Recv()
	assert.ErrorIs(t, err, io.EOF)
}
