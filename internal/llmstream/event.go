package llmstream

import "strings"

type EventType string

const (
	// Indicates request is accepted but queued. Event will only have this status. Optional.
	EventTypeQueued EventType = "queued"

	EventTypeCreated          EventType = "created"           // Turn has been created and is in-progress. Event has Turn.
	EventTypeCompletedSuccess EventType = "completed_success" // Stream has ended successfully. Event has final Turn.
	EventTypeError            EventType = "error"             // Some error occurred. Event has Error set.

	// A retryable error occurred (ex: network blip) and the request will be retried. Event has Error set.
	EventTypeRetry EventType = "retry"

	// EventTypeTextDelta may be emitted at various times as the LLM outputs text. The event will contain Delta and Text.
	EventTypeTextDelta EventType = "text_delta"

	// EventTypeReasoningDelta may be emitted at various times as the LLM reasons. The event will contain Delta and Reasoning.
	EventTypeReasoningDelta EventType = "reasoning_delta"

	EventTypeToolUse EventType = "tool_use"

	// A way to communicate warnings, invariant violations, or likely bugs, without stopping program execution or crashing. Event has Error set.
	EventTypeWarning EventType = "warning"
)

type Event struct {
	Type  EventType
	Turn  *Turn
	Error error

	// Delta is new content added to Text or Reasoning. The suffix of Text.Content or Reasoning.Content should be Delta. May be blank. Only sent
	// in EventTypeTextDelta and EventTypeReasoningDelta.
	Delta string

	// Text is the cumulative text content so far for the given Text.ProviderID. Only sent in EventTypeTextDelta.
	Text *TextContent

	// Reasoning is the cumulative reasoning content so far for the given Reasoning.ProviderID. Only sent in EventTypeReasoningDelta. NOTE: some
	// providers have multiple items per reasoning ProviderID. In those cases, Reasoning is the "current" item. Callers can keep track of this by
	// looking at Done.
	Reasoning *ReasoningContent

	// Done is true if Text or Reasoning is done. Only sent in EventTypeTextDelta and EventTypeReasoningDelta. Note that for Reasoning, there may
	// be multiple Done events per ProviderID, denoting multiple reasoning items in the same overall reasoning ProviderID.
	Done bool

	// ToolCall is the tool call for EventTypeToolUse event.
	ToolCall *ToolCall
}

// TextContent returns e.Text.Content or "".
func (e Event) TextContent() string {
	if e.Text != nil {
		return e.Text.Content
	}
	return ""
}

// ReasoningContent returns e.Reasoning.Content or "".
func (e Event) ReasoningContent() string {
	if e.Reasoning != nil {
		return e.Reasoning.Content
	}
	return ""
}

func newErrorEvent(err error) Event {
	return Event{Type: EventTypeError, Error: err}
}

type TokenUsage struct {
	TotalInputTokens  int64 // Total input tokens for this turn (must include CachedInputTokens).
	CachedInputTokens int64
	ReasoningTokens   int64
	TotalOutputTokens int64 // Total output tokens for this turn (may exclude ReasoningTokens depending on provider semantics).
}

type FinishReason string

const (
	FinishReasonUnknown          FinishReason = ""
	FinishReasonInProgress       FinishReason = "in_progress"
	FinishReasonEndTurn          FinishReason = "end_turn"
	FinishReasonMaxTokens        FinishReason = "max_tokens"
	FinishReasonToolUse          FinishReason = "tool_use"
	FinishReasonCanceled         FinishReason = "canceled"
	FinishReasonError            FinishReason = "error"
	FinishReasonPermissionDenied FinishReason = "permission_denied" // Ex: content rejection.
)

// Turn represents a conversational turn: one party (system/user or assistant) conveying data.
//
// Turns don't map perfectly to OpenAI's Responses API. We model both local inputs (system/user/tool results) and provider outputs (assistant responses)
// as Turns.
//
// Notes:
//   - Locally-created turns (system/user/tool results) have ProviderID == "" and Usage is zero.
//   - Provider-created turns (assistant responses) have ProviderID set (ex: "resp_1234") and include Usage/FinishReason.
//   - Tool calls are ToolCall parts inside an assistant turn; tool results are ToolResult parts in a subsequent RoleUser turn.
//
// A conversation might look like:
//   - Turn{Role: RoleSystem, ProviderID: ""}
//   - Turn{Role: RoleUser, ProviderID: ""}
//   - Turn{Role: RoleAssistant, ProviderID: "resp_aaaa"} // Parts may include ToolCall(s)
//   - Turn{Role: RoleUser, ProviderID: ""}              // Parts contain ToolResult(s)
//   - Turn{Role: RoleAssistant, ProviderID: "resp_bbbb", FinishReason: FinishReasonEndTurn}
type Turn struct {
	Role         Role          // Role taking this turn (user, system, assistant).
	ProviderID   string        // ID of the turn from the LLM provider (ex: "resp_1234")
	Parts        []ContentPart // All parts of the turn.
	Usage        TokenUsage    // Provider-reported token usage for this provider response; currently only populated on assistant turns.
	FinishReason FinishReason  // Reason the turn is finished (unfinished turns: FinishReasonInProgress).
}

func (r Turn) ToolCalls() []ToolCall {
	if len(r.Parts) == 0 {
		return nil
	}
	var toolCalls []ToolCall
	for _, part := range r.Parts {
		if tc, ok := part.(ToolCall); ok {
			toolCalls = append(toolCalls, tc)
		}
	}
	return toolCalls
}

func (r Turn) ToolResults() []ToolResult {
	if len(r.Parts) == 0 {
		return nil
	}
	var toolResults []ToolResult
	for _, part := range r.Parts {
		if tr, ok := part.(ToolResult); ok {
			toolResults = append(toolResults, tr)
		}
	}
	return toolResults
}

// TextContent returns the concatenation of all TextContent parts in r, separated by blank lines if needed.
func (r Turn) TextContent() string {
	var b strings.Builder
	endsWithNewline := false
	for _, p := range r.Parts {
		if tc, ok := p.(TextContent); ok {
			if b.Len() > 0 && !endsWithNewline {
				b.WriteString("\n\n")
			}
			b.WriteString(tc.Content)
			endsWithNewline = len(tc.Content) > 0 && tc.Content[len(tc.Content)-1] == '\n'
		}
	}
	return b.String()
}
