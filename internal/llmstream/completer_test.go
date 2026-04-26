package llmstream

import (
	"context"
	"errors"
	"testing"

	"github.com/codalotl/codalotl/internal/llmmodel"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompleterComplete_ReturnsCompletedTurn(t *testing.T) {
	t.Parallel()

	wantTurn := Turn{
		Role: RoleAssistant,
		Parts: []ContentPart{
			TextContent{ProviderID: "text_1", Content: "hello"},
		},
		FinishReason: FinishReasonEndTurn,
	}
	events := make(chan Event, 3)
	events <- Event{Type: EventTypeCreated}
	events <- Event{Type: EventTypeCompletedSuccess, Turn: &wantTurn}
	close(events)

	conv := &fakeCompleterConversation{events: events}
	completer := completer{newConversation: func(modelID llmmodel.ModelID, systemMessage string) StreamingConversation {
		conv.modelID = modelID
		conv.systemMessage = systemMessage
		return conv
	}}

	opt := SendOptions{NoStore: true, ServiceTier: "flex"}
	got, err := completer.Complete(context.Background(), llmmodel.ModelID("test-model"), "system", "user", opt)

	require.NoError(t, err)
	assert.Equal(t, wantTurn, got)
	assert.Equal(t, llmmodel.ModelID("test-model"), conv.modelID)
	assert.Equal(t, "system", conv.systemMessage)
	assert.Equal(t, "user", conv.userMessage)
	require.Len(t, conv.options, 1)
	assert.Equal(t, opt, conv.options[0])
}

func TestCompleterComplete_ReturnsAddUserTurnError(t *testing.T) {
	t.Parallel()

	addErr := errors.New("add user failed")
	conv := &fakeCompleterConversation{addUserErr: addErr}
	completer := completer{newConversation: func(llmmodel.ModelID, string) StreamingConversation {
		return conv
	}}

	_, err := completer.Complete(context.Background(), llmmodel.ModelID("test-model"), "system", "user")

	require.ErrorIs(t, err, addErr)
	assert.False(t, conv.sendCalled)
}

func TestCompleterComplete_PreservesFirstStreamError(t *testing.T) {
	t.Parallel()

	retryErr := errors.New("temporary stream failure")
	finalErr := errors.New("final stream failure")
	events := make(chan Event, 2)
	events <- Event{Type: EventTypeRetry, Error: retryErr}
	events <- Event{Type: EventTypeError, Error: finalErr}
	close(events)

	completer := completer{newConversation: func(llmmodel.ModelID, string) StreamingConversation {
		return &fakeCompleterConversation{events: events}
	}}

	_, err := completer.Complete(context.Background(), llmmodel.ModelID("test-model"), "system", "user")

	require.ErrorIs(t, err, retryErr)
}

func TestCompleterComplete_ReturnsContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	completer := completer{newConversation: func(llmmodel.ModelID, string) StreamingConversation {
		return &fakeCompleterConversation{events: make(chan Event)}
	}}

	_, err := completer.Complete(ctx, llmmodel.ModelID("test-model"), "system", "user")

	require.ErrorIs(t, err, context.Canceled)
}

func TestCompleterComplete_ReturnsErrorWhenStreamClosesWithoutSuccess(t *testing.T) {
	t.Parallel()

	events := make(chan Event)
	close(events)
	completer := completer{newConversation: func(llmmodel.ModelID, string) StreamingConversation {
		return &fakeCompleterConversation{events: events}
	}}

	_, err := completer.Complete(context.Background(), llmmodel.ModelID("test-model"), "system", "user")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed without successful completion")
}

type fakeCompleterConversation struct {
	modelID       llmmodel.ModelID
	systemMessage string
	userMessage   string
	addUserErr    error
	events        <-chan Event
	options       []SendOptions
	sendCalled    bool
}

func (c *fakeCompleterConversation) LastTurn() Turn {
	return Turn{}
}

func (c *fakeCompleterConversation) Turns() []Turn {
	return nil
}

func (c *fakeCompleterConversation) AddTools([]Tool) error {
	return nil
}

func (c *fakeCompleterConversation) AddUserTurn(text string) error {
	c.userMessage = text
	return c.addUserErr
}

func (c *fakeCompleterConversation) AddToolResults([]ToolResult) error {
	return nil
}

func (c *fakeCompleterConversation) SendAsync(ctx context.Context, options ...SendOptions) <-chan Event {
	c.sendCalled = true
	c.options = append(c.options, options...)
	return c.events
}
