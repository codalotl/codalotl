package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"sync"
)

var (
	// ErrAlreadyRunning is returned when an agent operation cannot proceed because
	// a turn is currently being processed.
	ErrAlreadyRunning     = errors.New("agent: already running")
	errMissingCompletion  = errors.New("agent: llmstream completed without a final turn")
	errNoToolCallsPresent = errors.New("agent: finish reason requested tool use but no tool calls were present")
)

// newConversation is overridden in tests.
var newConversation = llmstream.NewConversation

// Agent orchestrates the conversation loop between llmstream and tools.
type Agent struct {
	sessionID string
	agentID   string
	model     llmmodel.ModelID
	conv      llmstream.StreamingConversation

	mu                 sync.Mutex
	status             Status
	turns              []llmstream.Turn
	tokenUsage         llmstream.TokenUsage
	contextUsageTokens int64

	tools    map[string]llmstream.Tool
	toolList []llmstream.Tool

	parent     *Agent
	depth      int
	parentOut  chan<- Event
	currentOut chan<- Event
}

// NewAgent constructs a new Agent for the supplied model, system prompt, and tools.
func NewAgent(model llmmodel.ModelID, systemPrompt string, tools []llmstream.Tool) (*Agent, error) {
	sessionID, err := generateSessionID()
	if err != nil {
		return nil, err
	}

	return newAgentInstance(model, systemPrompt, tools, sessionID, sessionID, nil, 0, nil)
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

// ContextUsagePercent estimates how much of the model's context window is consumed
// based on the latest assistant turn. Returns 0 when unknown.
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

// SendUserMessage appends a user turn and starts processing it. Events describing the
// assistant's behaviour are delivered on the returned channel.
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
	a.mu.Unlock()

	go a.run(ctx, out)
	return out
}

func (a *Agent) run(ctx context.Context, out chan<- Event) {
	defer func() {
		a.finishRun()
		close(out)
	}()

	for {
		turn, seenCalls, err := a.sendOnce(ctx, out)
		if err != nil {
			a.emitTerminalEvent(out, err)
			return
		}

		switch turn.FinishReason {
		case llmstream.FinishReasonEndTurn:
			a.dispatchEvent(out, Event{Type: EventTypeDoneSuccess})
			return
		case llmstream.FinishReasonToolUse:
			if err := a.handleToolUse(ctx, out, turn.ToolCalls(), seenCalls); err != nil {
				a.emitTerminalEvent(out, err)
				return
			}
			continue
		case llmstream.FinishReasonCanceled:
			a.dispatchEvent(out, Event{Type: EventTypeCanceled, Error: errors.New("agent: turn canceled by provider")})
			return
		case llmstream.FinishReasonError, llmstream.FinishReasonPermissionDenied:
			a.dispatchEvent(out, Event{Type: EventTypeError, Error: fmt.Errorf("agent: provider reported finish reason %s", turn.FinishReason)})
			return
		case llmstream.FinishReasonMaxTokens:
			a.dispatchEvent(out, Event{Type: EventTypeError, Error: errors.New("agent: turn stopped after hitting token limit")})
			return
		default:
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
	)

	for ev := range events {
		switch ev.Type {
		case llmstream.EventTypeError:
			sendErr = ev.Error
		case llmstream.EventTypeTextDelta:
			if ev.Text != nil && ev.Done {
				textCopy := *ev.Text
				a.dispatchEvent(out, Event{
					Type:        EventTypeAssistantText,
					TextContent: textCopy,
				})
			}
		case llmstream.EventTypeReasoningDelta:
			if ev.Reasoning != nil && ev.Done {
				reasoningCopy := *ev.Reasoning
				a.dispatchEvent(out, Event{
					Type:             EventTypeAssistantReasoning,
					ReasoningContent: reasoningCopy,
				})
			}
		case llmstream.EventTypeToolUse:
			if ev.ToolCall != nil {
				callCopy := *ev.ToolCall
				seenToolCallIDs[callCopy.CallID] = struct{}{}
				a.dispatchEvent(out, Event{Type: EventTypeToolCall, Tool: callCopy.Name, ToolCall: &callCopy})
			}
		case llmstream.EventTypeCompletedSuccess:
			completedTurn = ev.Turn
		case llmstream.EventTypeWarning:
			a.dispatchEvent(out, Event{Type: EventTypeWarning, Error: ev.Error})
		case llmstream.EventTypeRetry:
			a.dispatchEvent(out, Event{Type: EventTypeRetry, Error: ev.Error})
		}
	}

	if sendErr != nil {
		return nil, nil, sendErr
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, nil, ctxErr
	}

	if completedTurn == nil {
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
		if _, already := seen[call.CallID]; !already {
			a.dispatchEvent(out, Event{Type: EventTypeToolCall, Tool: callCopy.Name, ToolCall: &callCopy})
		}

		tool := a.tools[call.Name]
		var result llmstream.ToolResult

		if tool == nil {
			result = llmstream.NewErrorToolResult("unknown tool", call)
		} else {
			toolCtx, cancel := context.WithCancel(ctx)
			factory := newSubAgentFactory(a)
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
		a.dispatchEvent(out, Event{Type: EventTypeToolComplete, Tool: call.Name, ToolCall: &callCopy, ToolResult: &resultCopy})

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
	return AgentMeta{
		ID:    a.agentID,
		Depth: a.depth,
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
	a.mu.Unlock()
}

func (a *Agent) addUsage(usage llmstream.TokenUsage) {
	if usage.TotalInputTokens == 0 &&
		usage.TotalOutputTokens == 0 &&
		usage.CachedInputTokens == 0 &&
		usage.ReasoningTokens == 0 {
		return
	}

	a.mu.Lock()
	a.tokenUsage.TotalInputTokens += usage.TotalInputTokens
	a.tokenUsage.TotalOutputTokens += usage.TotalOutputTokens
	a.tokenUsage.CachedInputTokens += usage.CachedInputTokens
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

func newAgentInstance(model llmmodel.ModelID, systemPrompt string, tools []llmstream.Tool, sessionID, agentID string, parent *Agent, depth int, parentOut chan<- Event) (*Agent, error) {
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
		sessionID: sessionID,
		agentID:   agentID,
		model:     model,
		conv:      conv,
		status:    StatusIdle,
		turns:     []llmstream.Turn{systemTurn},
		tools:     toolMap,
		toolList:  toolList,
		parent:    parent,
		depth:     depth,
		parentOut: parentOut,
	}, nil
}

func generateSessionID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("agent: generate session id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
