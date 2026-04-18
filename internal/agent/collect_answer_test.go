package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectFinalAssistantText_UsesFinalFlaggedTextAfterSuccess(t *testing.T) {
	events := make(chan Event, 4)
	events <- Event{
		Type:               EventTypeAssistantText,
		TextContent:        llmstream.TextContent{Content: "intermediate"},
		AssistantTextFinal: false,
	}
	events <- Event{
		Type: EventTypeAssistantTurnComplete,
		Turn: &llmstream.Turn{
			Role:  llmstream.RoleAssistant,
			Parts: []llmstream.ContentPart{llmstream.TextContent{Content: "ignored turn text"}},
		},
	}
	events <- Event{
		Type:               EventTypeAssistantText,
		TextContent:        llmstream.TextContent{Content: "final answer"},
		AssistantTextFinal: true,
	}
	events <- Event{Type: EventTypeDoneSuccess}
	close(events)

	answer, err := CollectFinalAssistantText(context.Background(), events)
	require.NoError(t, err)
	assert.Equal(t, "final answer", answer)
}

func TestCollectFinalAssistantText_RequiresTopLevelDoneSuccess(t *testing.T) {
	events := make(chan Event, 1)
	events <- Event{
		Type:               EventTypeAssistantText,
		TextContent:        llmstream.TextContent{Content: "final answer"},
		AssistantTextFinal: true,
	}
	close(events)

	answer, err := CollectFinalAssistantText(context.Background(), events)
	assert.Empty(t, answer)
	assert.EqualError(t, err, "agent did not return an answer")
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
		Agent:              AgentMeta{ID: "child", Depth: 1},
		Type:               EventTypeAssistantText,
		TextContent:        llmstream.TextContent{Content: "child answer"},
		AssistantTextFinal: true,
	}
	events <- Event{
		Agent: AgentMeta{ID: "child", Depth: 1},
		Type:  EventTypeDoneSuccess,
	}
	events <- Event{
		Agent:              AgentMeta{ID: "root", Depth: 0},
		Type:               EventTypeAssistantText,
		TextContent:        llmstream.TextContent{Content: "root answer"},
		AssistantTextFinal: true,
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
		Agent:              AgentMeta{ID: "root", Depth: 0},
		Type:               EventTypeAssistantText,
		TextContent:        llmstream.TextContent{Content: "root answer"},
		AssistantTextFinal: true,
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
		Agent:              AgentMeta{ID: "root", Depth: 0},
		Type:               EventTypeAssistantText,
		TextContent:        llmstream.TextContent{Content: "root answer"},
		AssistantTextFinal: true,
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
