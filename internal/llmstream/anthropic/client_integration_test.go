package anthropic

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientStreamMessages_IntegrationRealAPI(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") == "" {
		t.Skip("INTEGRATION_TEST is required to run these tests")
	}

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY is required to run these tests")
	}

	model := os.Getenv("ANTHROPIC_MODEL")
	if model == "" {
		model = "claude-sonnet-4-6"
	}

	client := New(apiKey)
	cachePayload := strings.Repeat("Cache this sentence. ", 800)
	userPrompt := fmt.Sprintf("%s Marker:%d Reply with only the numeral 4.", cachePayload, time.Now().UnixNano())
	req := MessageRequest{
		Model:     model,
		MaxTokens: 64,
		System:    "You are a precise assistant. Follow the user's instructions exactly.",
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
						Text: userPrompt,
						CacheControl: &CacheControlParam{
							Type: "ephemeral",
							TTL:  "5m",
						},
					},
				},
			},
		},
	}
	firstText, firstUsage := runIntegrationRequest(t, client, req)
	secondText, secondUsage := runIntegrationRequest(t, client, req)
	assert.Contains(t, firstText, "4")
	assert.Contains(t, secondText, "4")
	assert.Greater(t, firstUsage.CacheCreationInputTokens, int64(0))
	assert.Greater(t, secondUsage.CacheReadInputTokens, int64(0))
}

func runIntegrationRequest(t *testing.T, client *Client, req MessageRequest) (string, Usage) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	stream, err := client.StreamMessages(ctx, req)
	require.NoError(t, err)
	defer stream.Close()

	require.NotNil(t, stream.Response())
	assert.Equal(t, http.StatusOK, stream.Response().StatusCode)
	assert.NotEmpty(t, stream.RequestID())

	var gotMessageStop bool
	var textDeltas strings.Builder
	var usage Usage
	var sawUsage bool

	for {
		event, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		require.NoError(t, recvErr)

		switch event.Type {
		case EventTypeError:
			require.NotNil(t, event.Error)
			t.Fatalf("anthropic stream error event: %v", event.Error)
		case EventTypeMessageStart:
			require.NotNil(t, event.Message)
			assert.NotEmpty(t, event.Message.ID)
			assert.Equal(t, "assistant", event.Message.Role)
			usage = event.Message.Usage
			sawUsage = true
		case EventTypeContentBlockDelta:
			require.NotNil(t, event.Delta)
			if event.Delta.Type == "text_delta" {
				textDeltas.WriteString(event.Delta.Text)
			}
		case EventTypeMessageDelta:
			require.NotNil(t, event.MessageDelta)
			usage = event.MessageDelta.Usage
			sawUsage = true
		case EventTypeMessageStop:
			gotMessageStop = true
		}
	}

	assert.True(t, gotMessageStop)
	assert.NotEmpty(t, textDeltas.String())
	assert.True(t, sawUsage)
	return textDeltas.String(), usage
}
