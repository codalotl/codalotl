package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectFinalAssistantText_PrefersCompletedTurnText(t *testing.T) {
	events := make(chan Event, 3)
	events <- Event{
		Type:        EventTypeAssistantText,
		TextContent: llmstream.TextContent{Content: "intermediate"},
	}
	events <- Event{
		Type:                    EventTypeAssistantText,
		AssistantTextFinalizing: true,
		TextContent:             llmstream.TextContent{Content: "final answer"},
	}
	events <- Event{Type: EventTypeDoneSuccess}
	close(events)

	answer, err := CollectFinalAssistantText(context.Background(), events)
	require.NoError(t, err)
	assert.Equal(t, "final answer", answer)
}

func TestCollectFinalAssistantText_IgnoresNonFinalAssistantTextEvents(t *testing.T) {
	events := make(chan Event, 2)
	events <- Event{
		Type:        EventTypeAssistantText,
		TextContent: llmstream.TextContent{Content: "first"},
	}
	events <- Event{Type: EventTypeDoneSuccess}
	close(events)

	answer, err := CollectFinalAssistantText(context.Background(), events)
	require.NoError(t, err)
	assert.Empty(t, answer)
}

func TestCollectFinalAssistantText_PropagatesErrorEvent(t *testing.T) {
	events := make(chan Event, 1)
	events <- Event{Type: EventTypeError, Error: errors.New("boom")}
	close(events)

	answer, err := CollectFinalAssistantText(context.Background(), events)
	assert.Empty(t, answer)
	assert.EqualError(t, err, "boom")
}

func TestCollectFinalAssistantText_IgnoresDescendantDoneSuccess(t *testing.T) {
	events := make(chan Event, 5)
	events <- Event{
		Agent: AgentMeta{ID: "root", Depth: 0},
		Type:  EventTypeToolCall,
	}
	events <- Event{
		Agent:                   AgentMeta{ID: "child", Depth: 1},
		Type:                    EventTypeAssistantText,
		AssistantTextFinalizing: true,
		TextContent:             llmstream.TextContent{Content: "child answer"},
	}
	events <- Event{
		Agent: AgentMeta{ID: "child", Depth: 1},
		Type:  EventTypeDoneSuccess,
	}
	events <- Event{
		Agent:                   AgentMeta{ID: "root", Depth: 0},
		Type:                    EventTypeAssistantText,
		AssistantTextFinalizing: true,
		TextContent:             llmstream.TextContent{Content: "root answer"},
	}
	events <- Event{
		Agent: AgentMeta{ID: "root", Depth: 0},
		Type:  EventTypeDoneSuccess,
	}
	close(events)

	answer, err := CollectFinalAssistantText(context.Background(), events)
	require.NoError(t, err)
	assert.Equal(t, "root answer", answer)
}

func TestCollectFinalAssistantText_IgnoresDescendantCanceled(t *testing.T) {
	events := make(chan Event, 4)
	events <- Event{
		Agent: AgentMeta{ID: "root", Depth: 0},
		Type:  EventTypeToolCall,
	}
	events <- Event{
		Agent: AgentMeta{ID: "child", Depth: 1},
		Type:  EventTypeCanceled,
		Error: context.Canceled,
	}
	events <- Event{
		Agent:                   AgentMeta{ID: "root", Depth: 0},
		Type:                    EventTypeAssistantText,
		AssistantTextFinalizing: true,
		TextContent:             llmstream.TextContent{Content: "root answer"},
	}
	events <- Event{
		Agent: AgentMeta{ID: "root", Depth: 0},
		Type:  EventTypeDoneSuccess,
	}
	close(events)

	answer, err := CollectFinalAssistantText(context.Background(), events)
	require.NoError(t, err)
	assert.Equal(t, "root answer", answer)
}

func TestCollectFinalAssistantText_IgnoresDescendantError(t *testing.T) {
	events := make(chan Event, 4)
	events <- Event{
		Agent: AgentMeta{ID: "root", Depth: 0},
		Type:  EventTypeToolCall,
	}
	events <- Event{
		Agent: AgentMeta{ID: "child", Depth: 1},
		Type:  EventTypeError,
		Error: errors.New("child boom"),
	}
	events <- Event{
		Agent:                   AgentMeta{ID: "root", Depth: 0},
		Type:                    EventTypeAssistantText,
		AssistantTextFinalizing: true,
		TextContent:             llmstream.TextContent{Content: "root answer"},
	}
	events <- Event{
		Agent: AgentMeta{ID: "root", Depth: 0},
		Type:  EventTypeDoneSuccess,
	}
	close(events)

	answer, err := CollectFinalAssistantText(context.Background(), events)
	require.NoError(t, err)
	assert.Equal(t, "root answer", answer)
}

func TestCollectFinalAssistantText_ReturnsGenericErrorWhenNoAnswer(t *testing.T) {
	events := make(chan Event)
	close(events)

	answer, err := CollectFinalAssistantText(context.Background(), events)
	assert.Empty(t, answer)
	assert.EqualError(t, err, "agent did not return an answer")
}
