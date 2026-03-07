package gemini

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

func TestClientCreateInteraction_IntegrationRealAPI(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") == "" {
		t.Skip("INTEGRATION_TEST is required to run these tests")
	}

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY is required to run these tests")
	}

	model := os.Getenv("GEMINI_MODEL")
	if model == "" {
		model = "gemini-2.5-flash"
	}

	client := New(apiKey)
	maxOutput := int64(32)
	temperature := 0.1
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stream, err := client.CreateInteraction(ctx, InteractionRequest{
		Model:             model,
		SystemInstruction: "You are a precise assistant. Follow the user's instructions exactly.",
		Input: []Turn{
			{
				Role: "user",
				Content: []Content{
					{Type: "text", Text: "Reply with only the single word PING."},
				},
			},
		},
		GenerationConfig: &GenerationConfig{
			Temperature:       &temperature,
			MaxOutputTokens:   &maxOutput,
			ThinkingLevel:     "minimal",
			ThinkingSummaries: "none",
		},
	})
	require.NoError(t, err)
	defer stream.Close()

	require.NotNil(t, stream.Response())
	assert.Equal(t, http.StatusOK, stream.Response().StatusCode)

	var (
		gotStart    bool
		gotComplete bool
		textBuilder strings.Builder
		usage       *Usage
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
			t.Fatalf("gemini stream error event: %v", event.Error)
		case EventTypeInteractionStart:
			gotStart = true
			require.NotNil(t, event.Interaction)
			assert.NotEmpty(t, event.Interaction.ID)
		case EventTypeContentDelta:
			require.NotNil(t, event.Delta)
			if event.Delta.Type == "text" {
				textBuilder.WriteString(event.Delta.Text)
			}
		case EventTypeInteractionComplete:
			gotComplete = true
			require.NotNil(t, event.Interaction)
			usage = event.Interaction.Usage
		}
	}

	assert.True(t, gotStart)
	assert.True(t, gotComplete)
	assert.Contains(t, strings.ToUpper(textBuilder.String()), "PING")
	require.NotNil(t, usage)
	assert.Greater(t, usage.TotalTokens, int64(0))
}
