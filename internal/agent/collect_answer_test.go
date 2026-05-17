package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectFinalAssistantText_SelectsFinalAnswer(t *testing.T) {
	testCases := []struct {
		name string
		in   []Event
		want string
	}{
		{
			name: "prefers completed turn text",
			in: []Event{
				{
					Type:        EventTypeAssistantText,
					TextContent: llmstream.TextContent{Content: "intermediate"},
				},
				finalizingTextEvent("streamed answer"),
				assistantTurnCompleteEvent("completed answer"),
				{Type: EventTypeDoneSuccess},
			},
			want: "completed answer",
		},
		{
			name: "falls back to completed turn text",
			in: []Event{
				assistantTurnCompleteEvent("completed answer"),
				{Type: EventTypeDoneSuccess},
			},
			want: "completed answer",
		},
		{
			name: "ignores non-final assistant text events",
			in: []Event{
				{
					Type:        EventTypeAssistantText,
					TextContent: llmstream.TextContent{Content: "first"},
				},
				{Type: EventTypeDoneSuccess},
			},
		},
		{
			name: "clears stale top-level finalizing text on later turn without assistant text",
			in: []Event{
				finalizingTextEvent("first answer"),
				assistantTurnCompleteEvent("first answer"),
				{
					Agent:       AgentMeta{ID: "root", Depth: 0},
					Type:        EventTypeQueuedUserMessageSent,
					UserMessage: "follow up",
				},
				{
					Agent: AgentMeta{ID: "root", Depth: 0},
					Type:  EventTypeAssistantTurnComplete,
					Turn: &llmstream.Turn{
						Role:         llmstream.RoleAssistant,
						Parts:        []llmstream.ContentPart{llmstream.ReasoningContent{Content: "thinking"}},
						FinishReason: llmstream.FinishReasonEndTurn,
					},
				},
				{
					Agent: AgentMeta{ID: "root", Depth: 0},
					Type:  EventTypeDoneSuccess,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			answer, err := collectFinalAssistantText(t, tc.in...)
			require.NoError(t, err)
			assert.Equal(t, tc.want, answer)
		})
	}
}

func TestCollectFinalAssistantText_IgnoresDescendantTerminalEvents(t *testing.T) {
	rootMeta := AgentMeta{ID: "root", Depth: 0}
	childMeta := AgentMeta{ID: "child", Depth: 1}

	testCases := []struct {
		name           string
		descendantDone Event
	}{
		{
			name:           "done success",
			descendantDone: Event{Agent: childMeta, Type: EventTypeDoneSuccess},
		},
		{
			name:           "canceled",
			descendantDone: Event{Agent: childMeta, Type: EventTypeCanceled, Error: context.Canceled},
		},
		{
			name:           "error",
			descendantDone: Event{Agent: childMeta, Type: EventTypeError, Error: errors.New("child boom")},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			answer, err := collectFinalAssistantText(t,
				Event{Agent: rootMeta, Type: EventTypeToolCall},
				Event{
					Agent:                   childMeta,
					Type:                    EventTypeAssistantText,
					AssistantTextFinalizing: true,
					TextContent:             llmstream.TextContent{Content: "child answer"},
				},
				tc.descendantDone,
				Event{
					Agent:                   rootMeta,
					Type:                    EventTypeAssistantText,
					AssistantTextFinalizing: true,
					TextContent:             llmstream.TextContent{Content: "root answer"},
				},
				Event{Agent: rootMeta, Type: EventTypeDoneSuccess},
			)

			require.NoError(t, err)
			assert.Equal(t, "root answer", answer)
		})
	}
}

func TestCollectFinalAssistantText_ReturnsErrors(t *testing.T) {
	testCases := []struct {
		name    string
		in      []Event
		wantErr string
	}{
		{
			name:    "propagates error event",
			in:      []Event{{Type: EventTypeError, Error: errors.New("boom")}},
			wantErr: "boom",
		},
		{
			name:    "generic error event without error",
			in:      []Event{{Type: EventTypeError}},
			wantErr: "agent failed",
		},
		{
			name:    "generic error when no answer",
			wantErr: "agent did not return an answer",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			answer, err := collectFinalAssistantText(t, tc.in...)
			assert.Empty(t, answer)
			assert.EqualError(t, err, tc.wantErr)
		})
	}
}

func TestCollectFinalAssistantText_ReturnsCancellationErrors(t *testing.T) {
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	testCases := []struct {
		name    string
		ctx     context.Context
		in      []Event
		wantErr error
	}{
		{
			name:    "canceled event propagates event error",
			ctx:     context.Background(),
			in:      []Event{{Type: EventTypeCanceled, Error: context.DeadlineExceeded}},
			wantErr: context.DeadlineExceeded,
		},
		{
			name:    "canceled event uses context error",
			ctx:     canceledCtx,
			in:      []Event{{Type: EventTypeCanceled}},
			wantErr: context.Canceled,
		},
		{
			name:    "canceled event defaults to canceled",
			ctx:     context.Background(),
			in:      []Event{{Type: EventTypeCanceled}},
			wantErr: context.Canceled,
		},
		{
			name:    "closed stream returns context error",
			ctx:     canceledCtx,
			wantErr: context.Canceled,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ch := make(chan Event, len(tc.in))
			for _, event := range tc.in {
				ch <- event
			}
			close(ch)

			answer, err := CollectFinalAssistantText(tc.ctx, ch)
			require.Empty(t, answer)
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func collectFinalAssistantText(t *testing.T, events ...Event) (string, error) {
	t.Helper()

	ch := make(chan Event, len(events))
	for _, event := range events {
		ch <- event
	}
	close(ch)

	return CollectFinalAssistantText(context.Background(), ch)
}

func finalizingTextEvent(content string) Event {
	return Event{
		Agent:                   AgentMeta{ID: "root", Depth: 0},
		Type:                    EventTypeAssistantText,
		AssistantTextFinalizing: true,
		TextContent:             llmstream.TextContent{Content: content},
	}
}

func assistantTurnCompleteEvent(content string) Event {
	return Event{
		Agent: AgentMeta{ID: "root", Depth: 0},
		Type:  EventTypeAssistantTurnComplete,
		Turn: &llmstream.Turn{
			Role:         llmstream.RoleAssistant,
			Parts:        []llmstream.ContentPart{llmstream.TextContent{Content: content}},
			FinishReason: llmstream.FinishReasonEndTurn,
		},
	}
}
