package llmstream

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/q/health"
)

type Role int

const (
	RoleUser Role = iota
	RoleSystem
	RoleAssistant
)

func newTextTurn(role Role, content string) Turn {
	parts := []ContentPart{TextContent{Content: content}}
	return Turn{
		Role:  role,
		Parts: parts,
	}
}

type SendOptions struct {
	ReasoningEffort    string // ex: "minimal", "low", "medium", "high"
	ReasoningSummary   string // ex: "auto", "concise", "detailed"
	TemperaturePresent bool
	Temperature        float64 // ex: 1.0

	// ServiceTier may be "", "auto", "priority", or "flex". Provider behavior:
	//   - OpenAI Responses: when set to "priority" or "flex", sets service_tier in the request.
	ServiceTier string

	// If true, we do not link turns via server persistence. The ONLY context the LLM will have is what is provided in Turns. Note that this still stores.
	NoLink bool

	// For ZDR (zero data retention). Sends store=false so the provider does not store the turn. We'd need to implement reasoning.encrypted_content.
	NoStore bool
}

type StreamingConversation interface {
	LastTurn() Turn
	Turns() []Turn
	AddTools(tools []Tool) error
	AddUserTurn(text string) error
	AddToolResults(toolResults []ToolResult) error

	// SendSync(ctx context.Context) (Turn, error)
	SendAsync(ctx context.Context, options ...SendOptions) <-chan Event
}

type toolCallResult struct {
	call   ToolCall
	result *ToolResult
}

type streamingConversation struct {
	modelID   llmmodel.ModelID
	turns     []Turn
	tools     []Tool
	toolCalls map[string]toolCallResult // toolCalls maps call_id to paired call/result. An entry can have a nil result (indicating it hasn't happened yet).
	health.Ctx

	// conversationID is provider-specific. In the case of OpenAI's responses API, it's the ID received using the Conversations API.
	providerConversationID string

	// promptCacheKey is a stable identifier used by providers (ex: OpenAI Responses) to reuse cached prompt prefixes across requests.
	promptCacheKey string
}

func NewConversation(modelID llmmodel.ModelID, systemMessage string) StreamingConversation {
	sc := streamingConversation{
		modelID:   modelID,
		turns:     []Turn{newTextTurn(RoleSystem, systemMessage)},
		toolCalls: make(map[string]toolCallResult),
	}
	sc.promptCacheKey = computePromptCacheKey(modelID, systemMessage)

	return &sc
}

func (sc *streamingConversation) LastTurn() Turn {
	if len(sc.turns) == 0 {
		return Turn{}
	}
	return sc.turns[len(sc.turns)-1]
}

func (sc *streamingConversation) Turns() []Turn {
	return sc.turns
}

func (sc *streamingConversation) AddTools(tools []Tool) error {
	if len(tools) == 0 {
		return errors.New("tools cannot be empty")
	}
	// Deduplicate by tool name; later additions override earlier ones with same name
	existing := make(map[string]int, len(sc.tools))
	for i, t := range sc.tools {
		existing[t.Name()] = i
	}
	for _, t := range tools {
		name := strings.TrimSpace(t.Name())
		if name == "" {
			return fmt.Errorf("tool name is required")
		}
		if idx, ok := existing[name]; ok {
			sc.tools[idx] = t
		} else {
			sc.tools = append(sc.tools, t)
			existing[name] = len(sc.tools) - 1
		}
	}
	return nil
}

func (sc *streamingConversation) AddUserTurn(text string) error {
	lastTurn := sc.LastTurn()
	if len(lastTurn.ToolCalls()) > 0 {
		return errors.New("previous message had tool calls - cannot add new user message. Use AddToolResults first")
	}
	sc.turns = append(sc.turns, newTextTurn(RoleUser, text))
	return nil
}

// AddToolResults appends a message with toolResults to the conversation. It returns an error if the previous message is not an assistant message or if that assistant
// message contains no tool calls. Each provided tool result must match a prior tool call by call ID, name, and type. Duplicate results for the same call ID are
// errors.
func (sc *streamingConversation) AddToolResults(toolResults []ToolResult) error {
	if len(toolResults) == 0 {
		return errors.New("tool results cannot be empty")
	}
	if len(sc.turns) == 0 {
		return errors.New("no previous message to attach tool results")
	}

	prev := sc.LastTurn()

	if prev.Role != RoleAssistant {
		return fmt.Errorf("previous message was not an assistant message")
	}

	// Collect tool call IDs from the previous message parts
	callIDs := make(map[string]ToolCall)
	priorIDs := make([]string, 0)
	for _, tc := range prev.ToolCalls() {
		if tc.CallID != "" {
			callIDs[tc.CallID] = tc
			priorIDs = append(priorIDs, tc.CallID)
		}
	}
	if len(callIDs) == 0 {
		return fmt.Errorf("previous message does not contain tool calls; found %d part(s)", len(prev.Parts))
	}

	// Validate each tool result maps to a prior tool call id and name
	parts := make([]ContentPart, 0, len(toolResults))
	matched := make(map[string]bool, len(toolResults))
	for _, tr := range toolResults {
		if tr.CallID == "" {
			return fmt.Errorf("tool result missing call_id (name=%q)", tr.Name)
		}
		tc, ok := callIDs[tr.CallID]
		if !ok {
			return fmt.Errorf("tool result %s does not match prior tool call IDs (%s)", tr.CallID, strings.Join(priorIDs, ", "))
		}
		if matched[tr.CallID] {
			return fmt.Errorf("duplicate tool result for call %s", tr.CallID)
		}
		if tr.Name != tc.Name {
			return fmt.Errorf("tool result %s has name %q which does not match tool call name %q", tr.CallID, tr.Name, tc.Name)
		}
		if tr.Type != tc.Type {
			return fmt.Errorf("tool result %s has type %q which does not match tool call type %q", tr.CallID, tr.Type, tc.Type)
		}
		matched[tr.CallID] = true
		parts = append(parts, tr)
	}

	// Ensure all prior calls were matched by a result
	unmatched := make([]string, 0)
	for id := range callIDs {
		if !matched[id] {
			unmatched = append(unmatched, id)
		}
	}
	if len(unmatched) > 0 {
		return fmt.Errorf("missing tool results for call IDs (%s)", strings.Join(unmatched, ", "))
	}

	// Add the results to sc.toolCalls:
	for _, tr := range toolResults {
		tcr, ok := sc.toolCalls[tr.CallID]
		if !ok {
			return fmt.Errorf("conversation does not contain this call_id (invariant violation). prev calls=%v; tr=%v", sc.toolCalls, tr)
		}
		trCopy := tr
		tcr.result = &trCopy
		sc.toolCalls[tr.CallID] = tcr
	}

	// Append a new message carrying the tool results
	sc.turns = append(sc.turns, Turn{Role: RoleUser, Parts: parts})
	return nil
}

// SendAsync returns immediately. The returned channel can be read to obtain events. Any preflight validation errors (ex: turns with invalid roles) will be sent
// on the channel before any network work.
//
// If options are supplied, only the first option is used.
//
// In addition to sending a stream of events, we also update the conversation with any new turns.
//
// Errors may be retried. If a network issue (or other retryable error) happens mid-stream, an EventTypeRetry event will be sent and the same output channel will
// be used to try again.
func (sc *streamingConversation) SendAsync(ctx context.Context, options ...SendOptions) <-chan Event {
	out := make(chan Event, 1024) // NOTE: I am somewhat concerned about this. If each letter in the output gets a delta event, that's a lot of events
	go func() {
		defer close(out)

		var opt *SendOptions
		if len(options) > 0 {
			opt = &options[0]
		}

		if len(sc.turns) < 2 {
			out <- newErrorEvent(sc.LogNewErr("in order to send, the Conversation must contain a system and user message"))
			return
		}
		if sc.turns[0].Role != RoleSystem {
			out <- newErrorEvent(sc.LogNewErr("in order to send, the first message in the Conversation must be a system message"))
			return
		}
		lastUserTurn := sc.LastTurn()
		if lastUserTurn.Role != RoleUser {
			out <- newErrorEvent(sc.LogNewErr("in order to send, the last message in the Conversation must be a user message"))
			return
		}

		// Print tool calls and results:
		for _, tr := range lastUserTurn.ToolResults() {
			tcr, ok := sc.toolCalls[tr.CallID]
			if ok && tcr.result != nil { // it would be an invariant violation if this conditional is false (but this is only for debugging, so don't error)
				debugPrintToolCallResult(tcr.call, *tcr.result)
			}
		}

		modelInfo := llmmodel.GetModelInfo(sc.modelID)
		if modelInfo.ID == llmmodel.ModelIDUnknown {
			out <- newErrorEvent(sc.LogNewErr("conversation.model.invalid", "model_id", string(sc.modelID)))
			return
		}
		if modelInfo.ProviderID == llmmodel.ProviderIDUnknown {
			out <- newErrorEvent(sc.LogNewErr("conversation.provider.unknown", "model_id", string(sc.modelID)))
			return
		}
		if !modelSupportsAPIType(modelInfo, llmmodel.ProviderTypeOpenAIResponses) {
			out <- newErrorEvent(sc.LogNewErr("conversation.model.unsupported_api", "model_id", string(sc.modelID), "provider", modelInfo.ProviderID, "required_api", llmmodel.ProviderTypeOpenAIResponses))
			return
		}

		var newTurn Turn
		var err error

		const retryMaxAttempts = 3

		for attempt := 1; attempt <= retryMaxAttempts; attempt++ {
			newTurn, err = sc.sendAsyncOpenAIResponses(ctx, out, opt, modelInfo)

			if err == nil {
				break
			}

			if isRetryable(err) && attempt < retryMaxAttempts {
				sleep := 0 * time.Millisecond
				if (attempt - 1) < len(retrySleepDurations) {
					sleep = retrySleepDurations[attempt-1]
				} else {
					sleep = retrySleepDurations[len(retrySleepDurations)-1]
				}
				sc.Log("conversation.retry", "attempt", attempt, "max", retryMaxAttempts, "sleep", sleep, "err", err.Error())

				timer := time.NewTimer(sleep)
				ctxCancelled := false
				select {
				case <-timer.C:
				case <-ctx.Done():
					timer.Stop()
					err = sc.LogWrappedErr("conversation.retry", ctx.Err())
					ctxCancelled = true
				}

				// NOTE: a break inside the above select breaks out of the select. So we need to re-detect the ctx err and break out of loop.
				if ctxCancelled {
					break
				}

				trySendEvent(ctx, out, Event{Type: EventTypeRetry, Error: err})
				sc.Log("conversation.retry", "attempt", attempt, "max", retryMaxAttempts, "sleep", sleep, "err", err.Error())
				continue
			}

			// Not retryable or out of attempts
			break
		}

		if err != nil {
			trySendEvent(ctx, out, newErrorEvent(err)) // err should already be logged
			return
		}

		debugPrint(debugParsedResponses, "PARSED RESPONSE: assistant response", newTurn)

		sc.turns = append(sc.turns, newTurn)
		for _, tc := range newTurn.ToolCalls() {
			if _, ok := sc.toolCalls[tc.CallID]; ok {
				trySendEvent(ctx, out, Event{Type: EventTypeWarning, Error: fmt.Errorf("existing tool call already in cache: %s", tc.CallID)})
				sc.Log("conversation.add_tool_call", "call_id", tc.CallID)
			} else {
				sc.toolCalls[tc.CallID] = toolCallResult{call: tc}
			}
		}

	}()
	return out
}

func modelSupportsAPIType(info llmmodel.ModelInfo, apiType llmmodel.ProviderAPIType) bool {
	for _, t := range info.SupportedTypes {
		if t == apiType {
			return true
		}
	}
	return false
}

// trySendEvent sends ev on out, but fast-fails if ctx is done. Reports if the event was sent.
func trySendEvent(ctx context.Context, out chan<- Event, ev Event) bool {
	select {
	case out <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

// debounceDeltaInterval controls the minimum time between successive non-done delta events (by kind and ID) that get forwarded to the output channel. Keep modest
// to maintain a responsive UX while reducing chattiness.
const debounceDeltaInterval = 500 * time.Millisecond

// debounceEvents reads events from in and forwards them to out, applying a per-(event kind, content ID) throttle for Text/Reasoning delta events.
//
// Policy:
//   - Only EventTypeTextDelta and EventTypeReasoningDelta are rate-limited.
//   - Each ID is debounced independently within its event kind.
//   - Non-done deltas are sent at most once every debounceDeltaInterval per key. If multiple arrive in that window, aggregate their deltas and forward the combined
//     delta once the window elapses (trailing throttle).
//   - Done variants are forwarded immediately; any pending non-done deltas for the same key are purged (not flushed). The Done event's delta is computed to include
//     any unsent characters since the last forwarded event.
//   - All other event types bypass debouncing and are forwarded immediately.
//
// The function terminates when ctx is done or when the input channel is closed. On input close (normal end), any pending non-done deltas are flushed before exit.
func debounceEvents(ctx context.Context, in <-chan Event, out chan<- Event) {
	type key struct {
		kind EventType
		id   string
	}
	type state struct {
		lastSent  time.Time // last time we forwarded for this key
		latest    string    // latest full content for this key
		sentBytes int       // bytes already forwarded downstream
		// pending state
		hasPending bool
		template   Event     // event template used to carry shape when flushing
		dueAt      time.Time // when the pending flush should fire
	}

	states := make(map[key]*state)

	var timer *time.Timer
	var timerC <-chan time.Time

	stopTimer := func() {
		if timer == nil {
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer, timerC = nil, nil
	}

	armTimer := func() {
		// Find earliest dueAt among keys with a pending flush.
		var earliest time.Time
		have := false
		for _, s := range states {
			if !s.hasPending {
				continue
			}
			if !have || s.dueAt.Before(earliest) {
				earliest, have = s.dueAt, true
			}
		}
		if !have {
			stopTimer()
			return
		}
		d := time.Until(earliest)
		if d < 0 {
			d = 0
		}
		if timer == nil {
			timer = time.NewTimer(d)
			timerC = timer.C
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(d)
	}

	// Returns (key, true) for debouncable delta kinds.
	getKey := func(ev Event) (key, bool) {
		switch ev.Type {
		case EventTypeTextDelta:
			if ev.Text != nil {
				return key{kind: ev.Type, id: ev.Text.ProviderID}, true
			}
		case EventTypeReasoningDelta:
			if ev.Reasoning != nil {
				return key{kind: ev.Type, id: ev.Reasoning.ProviderID}, true
			}
		}
		return key{}, false
	}

	setLatest := func(s *state, k key, ev Event) {
		switch k.kind {
		case EventTypeTextDelta:
			if ev.Text != nil {
				s.latest = ev.Text.Content
			}
		case EventTypeReasoningDelta:
			if ev.Reasoning != nil {
				s.latest = ev.Reasoning.Content
			}
		}
	}

	// Build aggregated delta from s.latest and s.sentBytes, fill it into template, send.
	sendAggregated := func(now time.Time, k key, s *state, template Event) bool {
		content := s.latest
		start := s.sentBytes
		if start < 0 || start > len(content) {
			start = 0
		}
		delta := content[start:]

		ev := template // copy
		switch k.kind {
		case EventTypeTextDelta:
			if ev.Text != nil {
				tc := *ev.Text
				tc.Content = content
				ev.Text = &tc
			}
			ev.Delta = delta
		case EventTypeReasoningDelta:
			if ev.Reasoning != nil {
				rc := *ev.Reasoning
				rc.Content = content
				ev.Reasoning = &rc
			}
			ev.Delta = delta
		}

		if !trySendEvent(ctx, out, ev) {
			return false
		}

		// Update state after a successful send.
		s.lastSent = now
		s.sentBytes = len(content)
		s.hasPending = false
		s.dueAt = time.Time{} // IMPORTANT: clear stale dueAt so next queue gets a fresh schedule
		return true
	}

	for {
		select {
		case <-ctx.Done():
			// Context cancellation: exit promptly without flushing.
			stopTimer()
			return

		case ev, ok := <-in:
			if !ok {
				// Input closed: flush any pending debounced events, then exit.
				now := time.Now()
				for k, s := range states {
					if s.hasPending {
						_ = sendAggregated(now, k, s, s.template)
					}
				}
				stopTimer()
				return
			}

			k, deb := getKey(ev)
			if !deb {
				// Not a debounced kind: pass straight through.
				if !trySendEvent(ctx, out, ev) {
					stopTimer()
					return
				}
				continue
			}

			now := time.Now()
			s := states[k]
			if s == nil {
				s = &state{}
				states[k] = s
			}

			if ev.Done {
				// Final event: include any unsent tail in the Done delta; purge pending.
				setLatest(s, k, ev)
				if !sendAggregated(now, k, s, ev) {
					stopTimer()
					return
				}
				// No further scheduling for this key.
				continue
			}

			// Non-done delta.
			setLatest(s, k, ev)

			// If outside the interval, send immediately.
			if s.lastSent.IsZero() || now.Sub(s.lastSent) >= debounceDeltaInterval {
				if !sendAggregated(now, k, s, ev) {
					stopTimer()
					return
				}
				continue
			}

			// Too soon: queue trailing send and (re)arm timer.
			s.template = ev
			s.hasPending = true
			s.dueAt = s.lastSent.Add(debounceDeltaInterval) // ALWAYS recompute
			armTimer()

		case <-timerC:
			// Fire all keys whose dueAt has arrived.
			now := time.Now()
			for k, s := range states {
				if s.hasPending && !now.Before(s.dueAt) {
					if !sendAggregated(now, k, s, s.template) {
						stopTimer()
						return
					}
				}
			}
			armTimer()
		}
	}
}

// ErrRetryable marks an error as retryable by the caller.
var ErrRetryable = errors.New("retryable")

func makeRetryable(err error) error { return fmt.Errorf("%w: %w", ErrRetryable, err) }
func isRetryable(err error) bool    { return errors.Is(err, ErrRetryable) }

// retrySleepDurations' i'th index is the sleep duration for the i'th retry. Any retry after that would use the last value.
//
// This is meant to mix exponential backoff, an eager initial retry, keeping sleep times long enough that things might recover but short enough that the user doesn't
// think things hung.
var retrySleepDurations = []time.Duration{
	10 * time.Millisecond,
	500 * time.Millisecond,
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
	10 * time.Second,
}
