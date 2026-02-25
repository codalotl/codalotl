package anthropic

import (
	"context"
	"errors"
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
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	stream, err := client.StreamMessages(ctx, MessageRequest{
		Model:     model,
		MaxTokens: 64,
		System:    "You are a precise assistant. Follow the user's instructions exactly.",
		Messages: []MessageParam{
			{
				Role: "user",
				Content: []ContentBlockParam{
					{Type: "text", Text: "Reply with only the numeral 4."},
				},
			},
		},
	})
	require.NoError(t, err)
	defer stream.Close()

	require.NotNil(t, stream.Response())
	assert.Equal(t, http.StatusOK, stream.Response().StatusCode)
	assert.NotEmpty(t, stream.RequestID())

	var (
		gotMessageStart bool
		gotMessageDelta bool
		gotMessageStop  bool
		textDeltas      strings.Builder
	)

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
			gotMessageStart = true
			require.NotNil(t, event.Message)
			assert.NotEmpty(t, event.Message.ID)
			assert.Equal(t, "assistant", event.Message.Role)
		case EventTypeContentBlockDelta:
			require.NotNil(t, event.Delta)
			if event.Delta.Type == "text_delta" {
				textDeltas.WriteString(event.Delta.Text)
			}
		case EventTypeMessageDelta:
			gotMessageDelta = true
			require.NotNil(t, event.MessageDelta)
		case EventTypeMessageStop:
			gotMessageStop = true
		}
	}

	assert.True(t, gotMessageStart)
	assert.True(t, gotMessageDelta)
	assert.True(t, gotMessageStop)
	assert.NotEmpty(t, textDeltas.String())
	assert.Contains(t, textDeltas.String(), "4")
}
