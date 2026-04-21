package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"strings"
	"sync"
)

var (
	// ErrAlreadyRunning is returned when an agent operation cannot proceed because a turn is currently being processed.
	ErrAlreadyRunning = errors.New("agent: already running")

	// ErrNotRunning is returned when an operation requires an active SendUserMessage loop, but the agent is idle (or is finishing and no longer accepting requests).
	ErrNotRunning = errors.New("agent: not running")

	errMissingCompletion  = errors.New("agent: llmstream completed without a final turn")
	errNoToolCallsPresent = errors.New("agent: finish reason requested tool use but no tool calls were present")
)

// newConversation is overridden in tests.
var newConversation = llmstream.NewConversation

// Agent orchestrates the conversation loop between llmstream and tools.
type Agent struct {
	sessionID           string
	agentID             string
	model               llmmodel.ModelID
	subagentLabel       string
	callingToolCallID   string
	conv                llmstream.StreamingConversation
	mu                  sync.Mutex
	status              Status
	turns               []llmstream.Turn
	tokenUsage          llmstream.TokenUsage
	contextUsageTokens  int64
	startSubagentSent   bool
	tools               map[string]llmstream.Tool
	toolList            []llmstream.Tool
	pendingUserMessages []string
	pendingQueuedEvents []string
	acceptingQueue      bool
	parent              *Agent
	depth               int
	parentOut           chan<- Event
	currentOut          chan<- Event
}

// NewOptions controls optional agent construction behavior.
type NewOptions struct {
	Model         llmmodel.ModelID
	SubagentLabel string
}

// New constructs a root Agent.
func New(systemPrompt string, tools []llmstream.Tool, options ...NewOptions) (*Agent, error) {
	sessionID, err := generateSessionID()
	if err != nil {
		return nil, err
	}

	resolved := mergeNewOptions(options)
	model := resolved.Model
	if model == "" {
		model = llmmodel.ModelIDOrFallback(llmmodel.ModelIDUnknown)
	}

	return newAgentInstance(model, systemPrompt, tools, sessionID, sessionID, nil, 0, nil, resolved.SubagentLabel, "")
}

// SessionID returns a globally unique identifier for this agent session.
func (a *Agent) SessionID() string {
	return a.sessionID
}

// Status reports whether the agent is currently processing a turn.
func (a *Agent) Status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status
}

// TokenUsage returns the cumulative token usage across assistant turns.
func (a *Agent) TokenUsage() llmstream.TokenUsage {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tokenUsage
}

// ContextUsagePercent estimates how much of the model's context window is consumed based on the latest assistant turn. Returns 0 when unknown.
func (a *Agent) ContextUsagePercent() int {
	info := llmmodel.GetModelInfo(a.model)
	if info.ContextWindow <= 0 {
		return 0
	}

	a.mu.Lock()
	used := a.contextUsageTokens
	a.mu.Unlock()

	if used <= 0 {
		return 0
	}

	return percentOfContext(used, info.ContextWindow)
}

// Turns returns a snapshot of the conversation turns so far.
func (a *Agent) Turns() []llmstream.Turn {
	a.mu.Lock()
	defer a.mu.Unlock()
	return cloneTurns(a.turns)
}

// AddUserTurn appends a user turn to the conversation without triggering the LLM send loop.
func (a *Agent) AddUserTurn(text string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.status == StatusRunning {
		return ErrAlreadyRunning
	}

	if err := a.conv.AddUserTurn(text); err != nil {
		return err
	}

	a.turns = append(a.turns, newTextTurn(llmstream.RoleUser, text))
	return nil
}

// QueueUserMessage queues a user message to be appended to the conversation the next time the agent reaches a safe boundary (after tool results are appended, or
// after an assistant end-of-turn completes).
//
// If the agent is currently executing a tool (including any subagents created by that tool), the message is queued and will not be appended until after the tool
// returns; messages are never injected into subagent tool calls/responses.
//
// When QueueUserMessage is accepted, the agent emits EventTypeUserMessageQueued with Event.UserMessage set. When the queued message is appended to the conversation
// (and will be included in the next provider send), the agent emits EventTypeQueuedUserMessageSent with Event.UserMessage set.
//
// Note: EventTypeUserMessageQueued is emitted asynchronously by the agent's run loop (it may not be emitted before QueueUserMessage returns). This avoids deadlocks
// when QueueUserMessage is called by the same goroutine that is draining the event stream.
//
// QueueUserMessage does not start a new run loop and does not return an event stream; it extends the currently running SendUserMessage call.
//
// It returns ErrNotRunning when the agent is idle, or when a run is finishing and no longer accepting queued messages (to avoid races where the message would be
// accepted but never delivered).
func (a *Agent) QueueUserMessage(message string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.status != StatusRunning || !a.acceptingQueue {
		return ErrNotRunning
	}
	a.pendingUserMessages = append(a.pendingUserMessages, message)
	a.pendingQueuedEvents = append(a.pendingQueuedEvents, message)
	return nil
}

// SendUserMessage appends message as a user turn and starts processing it asynchronously. It returns an event stream describing the agent's progress (assistant
// output, tool calls, and terminal status).
//
// Concurrency: Only one turn may be processed at a time. It is safe to call SendUserMessage from multiple goroutines, but if a turn is already running the returned
// channel will synchronously receive exactly one EventTypeError with ErrAlreadyRunning and then be closed. No background goroutine is started in that case.
//
// Note: QueueUserMessage may extend the lifetime of the returned channel by causing the agent to perform additional provider sends as it processes queued user turns.
// The terminal event (EventTypeDoneSuccess / EventTypeCanceled / EventTypeError) is emitted only when the agent stops and there are no queued messages.
//
// Channel lifecycle: The returned channel is always non-nil and is always closed when processing ends. Callers may safely range over it until closed. The channel
// is buffered (currently size 32); if the caller stops reading, the agent (and any subagents) may block while emitting events.
//
// Event ordering and invariants: For each provider send, zero or more intermediate events may be emitted, followed by EventTypeAssistantTurnComplete for the completed
// assistant turn (when a completed turn is available and has been appended to the agent's internal history).
//
// Typical events include:
//   - EventTypeAssistantText / EventTypeAssistantReasoning when a content block is complete (this implementation does not emit token-by-token deltas; it emits only
//     when the provider marks the block as done).
//   - EventTypeToolCall when the provider requests a tool.
//   - EventTypeToolComplete after each tool returns a ToolResult (and after any subagent activity performed by that tool).
//   - EventTypeWarning / EventTypeRetry as reported by the provider.
//
// The stream terminates with exactly one terminal event: EventTypeDoneSuccess on a normal end-of-turn, EventTypeCanceled when ctx is canceled / deadline exceeded
// (or the provider reports cancellation), or EventTypeError for all other errors. The channel is closed immediately after the terminal event is emitted.
//
// Note: tool execution may create subagents. Subagent events are mirrored into the same returned channel (distinguished by Event.Agent.Depth/ID) and may interleave
// with the parent agent's events; consumers should not assume a total ordering across different agents.
//
// Context cancellation: Cancellation is surfaced as an EventTypeCanceled terminal event. Depending on when ctx is canceled and when the underlying provider/tool
// stops, some non-terminal events may be delivered before the cancellation is observed.
func (a *Agent) SendUserMessage(ctx context.Context, message string) <-chan Event {
	out := make(chan Event, 32)

	a.mu.Lock()
	if a.status == StatusRunning {
		a.mu.Unlock()
		a.dispatchEvent(out, Event{Type: EventTypeError, Error: ErrAlreadyRunning})
		close(out)
		return out
	}

	if err := a.conv.AddUserTurn(message); err != nil {
		a.mu.Unlock()
		a.dispatchEvent(out, Event{Type: EventTypeError, Error: err})
		close(out)
		return out
	}

	a.turns = append(a.turns, newTextTurn(llmstream.RoleUser, message))
	a.status = StatusRunning
	a.currentOut = out
	a.pendingUserMessages = nil
	a.acceptingQueue = true
	a.mu.Unlock()

	a.maybeEmitStartSubagentEvent(out)
	go a.run(ctx, out)
	return out
}

func (a *Agent) run(ctx context.Context, out chan<- Event) {
	defer func() {
		a.finishRun()
		close(out)
	}()

	for {
		a.flushQueuedUserMessageEvents(out)
		turn, seenCalls, err := a.sendOnce(ctx, out)
		if err != nil {
			a.flushQueuedUserMessageEvents(out)
			a.stopAcceptingQueue()
			a.emitTerminalEvent(out, err)
			return
		}
		a.flushQueuedUserMessageEvents(out)

		switch turn.FinishReason {
		case llmstream.FinishReasonEndTurn:
			finish, err := a.injectOrStopAccepting(out)
			if err != nil {
				a.flushQueuedUserMessageEvents(out)
				a.stopAcceptingQueue()
				a.emitTerminalEvent(out, err)
				return
			}
			if !finish {
				continue
			}
			a.dispatchEvent(out, Event{Type: EventTypeDoneSuccess})
			return
		case llmstream.FinishReasonToolUse:
			if err := a.handleToolUse(ctx, out, turn.ToolCalls(), seenCalls); err != nil {
				a.flushQueuedUserMessageEvents(out)
				a.stopAcceptingQueue()
				a.emitTerminalEvent(out, err)
				return
			}
			if err := a.injectAllPending(out); err != nil {
				a.flushQueuedUserMessageEvents(out)
				a.stopAcceptingQueue()
				a.emitTerminalEvent(out, err)
				return
			}
			continue
		case llmstream.FinishReasonCanceled:
			a.flushQueuedUserMessageEvents(out)
			a.stopAcceptingQueue()
			a.dispatchEvent(out, Event{Type: EventTypeCanceled, Error: errors.New("agent: turn canceled by provider")})
			return
		case llmstream.FinishReasonError, llmstream.FinishReasonPermissionDenied:
			a.flushQueuedUserMessageEvents(out)
			a.stopAcceptingQueue()
			a.dispatchEvent(out, Event{Type: EventTypeError, Error: fmt.Errorf("agent: provider reported finish reason %s", turn.FinishReason)})
			return
		case llmstream.FinishReasonMaxTokens:
			a.flushQueuedUserMessageEvents(out)
			a.stopAcceptingQueue()
			a.dispatchEvent(out, Event{Type: EventTypeError, Error: errors.New("agent: turn stopped after hitting token limit")})
			return
		default:
			a.flushQueuedUserMessageEvents(out)
			a.stopAcceptingQueue()
			a.dispatchEvent(out, Event{Type: EventTypeError, Error: fmt.Errorf("agent: unsupported finish reason %s", turn.FinishReason)})
			return
		}
	}
}

// sendOnce sends the current conversation to the provider and streams events back to out.
func (a *Agent) sendOnce(ctx context.Context, out chan<- Event) (*llmstream.Turn, map[string]struct{}, error) {
	events := a.conv.SendAsync(ctx)

	var (
		sendErr         error
		completedTurn   *llmstream.Turn
		seenToolCallIDs = make(map[string]struct{})
		bufferedText    []llmstream.TextContent
		emittedTextRuns int
	)

	emitTextRun := func(parts []llmstream.TextContent, finalizing bool) {
		if len(parts) == 0 {
			return
		}

		a.dispatchEvent(out, Event{
			Type:                    EventTypeAssistantText,
			AssistantTextFinalizing: finalizing,
			TextContent:             coalesceTextContent(parts),
		})
		emittedTextRuns++
	}

	flushBufferedText := func(finalizing bool) {
		if len(bufferedText) == 0 {
			return
		}

		parts := bufferedText
		bufferedText = nil
		emitTextRun(parts, finalizing)
	}

	emitPendingCompletedText := func(turn llmstream.Turn) {
		runs, trailingRunIndex := assistantTextRuns(turn)
		bufferedText = nil
		if len(runs) == 0 {
			return
		}

		start := emittedTextRuns
		if start < 0 {
			start = 0
		}
		if start > len(runs) {
			start = len(runs)
		}

		for i := start; i < len(runs); i++ {
			emitTextRun(runs[i], i == trailingRunIndex)
		}
	}

	for ev := range events {
		switch ev.Type {
		case llmstream.EventTypeError:
			flushBufferedText(false)
			sendErr = ev.Error
		case llmstream.EventTypeTextDelta:
			if ev.Text != nil && ev.Done {
				bufferedText = append(bufferedText, *ev.Text)
			}
		case llmstream.EventTypeReasoningDelta:
			if ev.Reasoning != nil {
				flushBufferedText(false)
			}
			if ev.Reasoning != nil && ev.Done {
				reasoningCopy := *ev.Reasoning
				a.dispatchEvent(out, Event{
					Type:             EventTypeAssistantReasoning,
					ReasoningContent: reasoningCopy,
				})
			}
		case llmstream.EventTypeToolUse:
			flushBufferedText(false)
			if ev.ToolCall != nil {
				callCopy := *ev.ToolCall
				seenToolCallIDs[callCopy.CallID] = struct{}{}
				a.dispatchEvent(out, Event{Type: EventTypeToolCall, Tool: a.tools[callCopy.Name], ToolCall: &callCopy})
			}
		case llmstream.EventTypeCompletedSuccess:
			completedTurn = ev.Turn
			if completedTurn == nil {
				continue
			}
			emitPendingCompletedText(*completedTurn)
		case llmstream.EventTypeWarning:
			flushBufferedText(false)
			a.dispatchEvent(out, Event{Type: EventTypeWarning, Error: ev.Error})
		case llmstream.EventTypeRetry:
			flushBufferedText(false)
			a.dispatchEvent(out, Event{Type: EventTypeRetry, Error: ev.Error})
		}
	}

	if sendErr != nil {
		flushBufferedText(false)
		return nil, nil, sendErr
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		flushBufferedText(false)
		return nil, nil, ctxErr
	}

	if completedTurn == nil {
		flushBufferedText(false)
		return nil, nil, errMissingCompletion
	}

	cloned := cloneTurn(*completedTurn)

	a.mu.Lock()
	a.turns = append(a.turns, cloned)
	a.mu.Unlock()

	a.addUsage(completedTurn.Usage)
	a.updateContextUsage(completedTurn.Usage)

	turnCopy := cloned
	a.dispatchEvent(out, Event{Type: EventTypeAssistantTurnComplete, Turn: &turnCopy})

	return completedTurn, seenToolCallIDs, nil
}

func assistantTextRuns(turn llmstream.Turn) ([][]llmstream.TextContent, int) {
	var runs [][]llmstream.TextContent
	var current []llmstream.TextContent
	trailingRunIndex := -1

	for _, part := range turn.Parts {
		text, ok := part.(llmstream.TextContent)
		if ok {
			current = append(current, text)
			continue
		}

		if len(current) == 0 {
			continue
		}

		runs = append(runs, current)
		current = nil
	}

	if len(current) > 0 {
		trailingRunIndex = len(runs)
		runs = append(runs, current)
	}

	return runs, trailingRunIndex
}

func coalesceTextContent(parts []llmstream.TextContent) llmstream.TextContent {
	if len(parts) == 0 {
		return llmstream.TextContent{}
	}
	if len(parts) == 1 {
		return parts[0]
	}

	providerID := parts[0].ProviderID
	sameProviderID := true
	for _, part := range parts {
		if part.ProviderID != providerID {
			sameProviderID = false
		}
	}
	if !sameProviderID {
		providerID = ""
	}

	var builder strings.Builder
	totalLen := 0
	for _, part := range parts {
		totalLen += len(part.Content)
	}
	builder.Grow(totalLen)
	for _, part := range parts {
		builder.WriteString(part.Content)
	}

	return llmstream.TextContent{
		ProviderID: providerID,
		Content:    builder.String(),
	}
}

func (a *Agent) handleToolUse(ctx context.Context, out chan<- Event, calls []llmstream.ToolCall, seen map[string]struct{}) error {
	if len(calls) == 0 {
		return errNoToolCallsPresent
	}

	results := make([]llmstream.ToolResult, 0, len(calls))
	for _, call := range calls {
		if err := ctx.Err(); err != nil {
			return err
		}

		callCopy := call
		tool := a.tools[call.Name]
		if _, already := seen[call.CallID]; !already {
			a.dispatchEvent(out, Event{Type: EventTypeToolCall, Tool: tool, ToolCall: &callCopy})
		}

		var result llmstream.ToolResult

		if tool == nil {
			result = llmstream.NewErrorToolResult("unknown tool", call)
		} else {
			toolCtx, cancel := context.WithCancel(ctx)
			factory := newSubAgentFactory(a, callCopy.CallID)
			toolCtx = withSubAgentContext(toolCtx, factory, a.depth)

			func() {
				defer func() {
					factory.Close()
					cancel()
				}()
				result = tool.Run(toolCtx, call)
			}()

			if result.CallID == "" {
				result.CallID = call.CallID
			}
			if result.Name == "" {
				result.Name = call.Name
			}
			if result.Type == "" {
				result.Type = call.Type
			}
		}

		resultCopy := result
		a.dispatchEvent(out, Event{Type: EventTypeToolComplete, Tool: tool, ToolCall: &callCopy, ToolResult: &resultCopy})

		results = append(results, result)
	}

	a.mu.Lock()
	err := a.conv.AddToolResults(results)
	if err == nil {
		a.turns = append(a.turns, toolResultTurn(results))
	}
	a.mu.Unlock()
	if err != nil {
		return err
	}

	return nil
}

func (a *Agent) emitTerminalEvent(out chan<- Event, err error) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		a.dispatchEvent(out, Event{Type: EventTypeCanceled, Error: err})
		return
	}
	a.dispatchEvent(out, Event{Type: EventTypeError, Error: err})
}

func newTextTurn(role llmstream.Role, text string) llmstream.Turn {
	return llmstream.Turn{
		Role: role,
		Parts: []llmstream.ContentPart{
			llmstream.TextContent{Content: text},
		},
	}
}

func toolResultTurn(results []llmstream.ToolResult) llmstream.Turn {
	parts := make([]llmstream.ContentPart, len(results))
	for i, r := range results {
		parts[i] = r
	}
	return llmstream.Turn{Role: llmstream.RoleUser, Parts: parts}
}

func cloneTurns(src []llmstream.Turn) []llmstream.Turn {
	cloned := make([]llmstream.Turn, len(src))
	for i, t := range src {
		cloned[i] = cloneTurn(t)
	}
	return cloned
}

func cloneTurn(t llmstream.Turn) llmstream.Turn {
	cp := t
	if len(t.Parts) > 0 {
		parts := make([]llmstream.ContentPart, len(t.Parts))
		copy(parts, t.Parts)
		cp.Parts = parts
	}
	return cp
}

func (a *Agent) meta() AgentMeta {
	parentID := ""
	if a.parent != nil {
		parentID = a.parent.agentID
	}

	return AgentMeta{
		ID:     a.agentID,
		Depth:  a.depth,
		Parent: parentID,
	}
}

func (a *Agent) dispatchEvent(local chan<- Event, ev Event) {
	ev.Agent = a.meta()

	if local != nil {
		local <- ev
	}
	if a.parent != nil {
		a.parent.relayFromChild(ev, local)
		return
	}
	if a.parentOut != nil && a.parentOut != local {
		a.parentOut <- ev
	}
}

func (a *Agent) relayFromChild(ev Event, child chan<- Event) {
	a.mu.Lock()
	out := a.currentOut
	parent := a.parent
	parentOut := a.parentOut
	a.mu.Unlock()

	if out != nil && out != child {
		out <- ev
	}

	if parent != nil {
		parent.relayFromChild(ev, out)
		return
	}

	if parentOut != nil && parentOut != child && parentOut != out {
		parentOut <- ev
	}
}

func (a *Agent) finishRun() {
	a.mu.Lock()
	a.status = StatusIdle
	a.currentOut = nil
	a.acceptingQueue = false
	a.pendingUserMessages = nil
	a.pendingQueuedEvents = nil
	a.mu.Unlock()
}

func (a *Agent) addUsage(usage llmstream.TokenUsage) {
	if usage.TotalInputTokens == 0 &&
		usage.TotalOutputTokens == 0 &&
		usage.CachedInputTokens == 0 &&
		usage.CacheCreationInputTokens == 0 &&
		usage.ReasoningTokens == 0 {
		return
	}

	a.mu.Lock()
	a.tokenUsage.TotalInputTokens += usage.TotalInputTokens
	a.tokenUsage.TotalOutputTokens += usage.TotalOutputTokens
	a.tokenUsage.CachedInputTokens += usage.CachedInputTokens
	a.tokenUsage.CacheCreationInputTokens += usage.CacheCreationInputTokens
	a.tokenUsage.ReasoningTokens += usage.ReasoningTokens
	a.mu.Unlock()

	if a.parent != nil {
		a.parent.addUsage(usage)
	}
}

func (a *Agent) updateContextUsage(usage llmstream.TokenUsage) {
	tokens := contextTokensFromUsage(usage)
	if tokens <= 0 {
		return
	}

	a.mu.Lock()
	a.contextUsageTokens = tokens
	a.mu.Unlock()
}

func contextTokensFromUsage(usage llmstream.TokenUsage) int64 {
	nonCached := usage.TotalInputTokens - usage.CachedInputTokens
	if nonCached < 0 {
		nonCached = usage.TotalInputTokens
	}
	nonCached = clampNonNegative(nonCached)
	cached := clampNonNegative(usage.CachedInputTokens)

	return nonCached + cached
}

func clampNonNegative(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

func percentOfContext(used, capacity int64) int {
	if used <= 0 || capacity <= 0 {
		return 0
	}
	if used >= capacity {
		return 100
	}

	scaled := used * 100
	percent := int((scaled + capacity/2) / capacity)
	if percent < 0 {
		return 0
	}
	if percent > 100 {
		return 100
	}
	return percent
}

func buildToolRegistry(tools []llmstream.Tool) (map[string]llmstream.Tool, []llmstream.Tool) {
	if len(tools) == 0 {
		return make(map[string]llmstream.Tool), nil
	}

	toolMap := make(map[string]llmstream.Tool, len(tools))
	toolList := make([]llmstream.Tool, 0, len(tools))
	for _, t := range tools {
		if t == nil {
			continue
		}
		toolMap[t.Name()] = t
		toolList = append(toolList, t)
	}
	return toolMap, toolList
}

func cloneToolSlice(tools []llmstream.Tool) []llmstream.Tool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]llmstream.Tool, len(tools))
	copy(out, tools)
	return out
}

func mergeNewOptions(options []NewOptions) NewOptions {
	var merged NewOptions
	for _, opt := range options {
		if opt.Model != "" {
			merged.Model = opt.Model
		}
		if opt.SubagentLabel != "" {
			merged.SubagentLabel = opt.SubagentLabel
		}
	}
	return merged
}

func newAgentInstance(model llmmodel.ModelID, systemPrompt string, tools []llmstream.Tool, sessionID, agentID string, parent *Agent, depth int, parentOut chan<- Event, subagentLabel, callingToolCallID string) (*Agent, error) {
	conv := newConversation(model, systemPrompt)
	if conv == nil {
		return nil, errors.New("agent: failed to create conversation")
	}

	if len(tools) > 0 {
		if err := conv.AddTools(tools); err != nil {
			return nil, err
		}
	}

	toolMap, toolList := buildToolRegistry(tools)

	systemTurn := llmstream.Turn{
		Role: llmstream.RoleSystem,
		Parts: []llmstream.ContentPart{
			llmstream.TextContent{Content: systemPrompt},
		},
	}

	return &Agent{
		sessionID:         sessionID,
		agentID:           agentID,
		model:             model,
		subagentLabel:     subagentLabel,
		callingToolCallID: callingToolCallID,
		conv:              conv,
		status:            StatusIdle,
		turns:             []llmstream.Turn{systemTurn},
		tools:             toolMap,
		toolList:          toolList,
		parent:            parent,
		depth:             depth,
		parentOut:         parentOut,
	}, nil
}

func generateSessionID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("agent: generate session id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func (a *Agent) stopAcceptingQueue() {
	a.mu.Lock()
	a.acceptingQueue = false
	a.mu.Unlock()
}

func (a *Agent) maybeEmitStartSubagentEvent(out chan<- Event) {
	a.mu.Lock()
	if a.parent == nil || a.startSubagentSent {
		a.mu.Unlock()
		return
	}

	start := StartSubagent{
		CallingAgentID: a.parent.agentID,
		ToolCallID:     a.callingToolCallID,
		Label:          a.subagentLabel,
	}
	a.startSubagentSent = true
	a.mu.Unlock()

	a.dispatchEvent(out, Event{
		Type:          EventTypeStartSubagent,
		StartSubagent: start,
	})
}

// injectOrStopAccepting is called when the provider has produced an end-of-turn. If there are queued user messages, they are appended and the agent continues. Otherwise
// the agent stops accepting queued messages and the run may finish.
func (a *Agent) injectOrStopAccepting(out chan<- Event) (bool, error) {
	a.flushQueuedUserMessageEvents(out)
	a.mu.Lock()
	if len(a.pendingUserMessages) == 0 {
		a.acceptingQueue = false
		a.mu.Unlock()
		return true, nil
	}
	msgs := a.pendingUserMessages
	a.pendingUserMessages = nil
	a.mu.Unlock()
	for _, msg := range msgs {
		a.mu.Lock()
		err := a.conv.AddUserTurn(msg)
		if err == nil {
			a.turns = append(a.turns, newTextTurn(llmstream.RoleUser, msg))
		}
		a.mu.Unlock()
		if err != nil {
			return false, err
		}
		a.dispatchEvent(out, Event{Type: EventTypeQueuedUserMessageSent, UserMessage: msg})
	}
	return false, nil
}

// injectAllPending appends all currently queued user messages (if any). This is used after tool results are appended, before the next provider send.
func (a *Agent) injectAllPending(out chan<- Event) error {
	a.flushQueuedUserMessageEvents(out)
	a.mu.Lock()
	msgs := a.pendingUserMessages
	a.pendingUserMessages = nil
	a.mu.Unlock()
	if len(msgs) == 0 {
		return nil
	}
	for _, msg := range msgs {
		a.mu.Lock()
		err := a.conv.AddUserTurn(msg)
		if err == nil {
			a.turns = append(a.turns, newTextTurn(llmstream.RoleUser, msg))
		}
		a.mu.Unlock()
		if err != nil {
			return err
		}
		a.dispatchEvent(out, Event{Type: EventTypeQueuedUserMessageSent, UserMessage: msg})
	}
	return nil
}

func (a *Agent) flushQueuedUserMessageEvents(out chan<- Event) {
	a.mu.Lock()
	msgs := a.pendingQueuedEvents
	a.pendingQueuedEvents = nil
	a.mu.Unlock()
	for _, msg := range msgs {
		a.dispatchEvent(out, Event{Type: EventTypeUserMessageQueued, UserMessage: msg})
	}
}
