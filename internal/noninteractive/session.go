package noninteractive

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/agentbuilder"
	"github.com/codalotl/codalotl/internal/agentformatter"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/prompt"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
)

// A sessionConfig selects the agent and initial-message rules for a session.
type sessionConfig struct {
	agentName             string // It is the registry name used to prepare the agent.
	pkgMode               bool   // It reports whether package-mode behavior is active.
	allowEmptyInitialUser bool   // It reports whether the first user message may be empty.
}

// sessionAgent is the agent contract required by a reusable noninteractive session.
type sessionAgent interface {
	// SendUserMessage sends message to the agent and streams events until the turn finishes.
	SendUserMessage(ctx context.Context, message string) <-chan agent.Event

	// TokenUsage returns the token usage accumulated by the agent conversation.
	TokenUsage() llmstream.TokenUsage

	// ContextUsagePercent returns the current context-window usage percentage.
	ContextUsagePercent() int

	// Turns returns the current conversation turns.
	Turns() []llmstream.Turn
}

// A stepStartOutput contains the values emitted at the start of a session step.
type stepStartOutput struct {
	sandboxDir string           // It is the normalized sandbox directory reported as the run CWD.
	pkgRelPath string           // It is the package path relative to the sandbox, or empty outside package mode.
	modelID    llmmodel.ModelID // It is the effective model ID used by the session.
}

// Result reports structured metadata for one top-level noninteractive step.
type Result struct {
	TerminalEventType   agent.EventType      // Terminal event for this step's run.
	FinalAssistantText  string               // Final top-level finalizing assistant text emitted for this step.
	TokenUsage          llmstream.TokenUsage // Cumulative session token usage after this step, not a per-step delta.
	ContextUsagePercent int                  // Overall session context usage after this step, based on the latest assistant turn.
}

// Session holds a reusable noninteractive agent conversation.
type Session struct {
	opts                           Options                     // opts stores the options used to create the session.
	config                         sessionConfig               // config stores the resolved agent and initial-message rules.
	startInfo                      stepStartOutput             // startInfo contains the values emitted at the start of each session step.
	out                            io.Writer                   // out receives all serialized session output.
	jsonWriter                     *jsonEventWriter            // jsonWriter emits newline-delimited JSON events when JSON output is enabled.
	formatter                      agentformatter.Formatter    // formatter formats events for human-readable output.
	modelID                        llmmodel.ModelID            // modelID is the effective model selected for the session.
	terminalWidth                  int                         // terminalWidth is the detected output width used for human-readable formatting.
	agent                          sessionAgent                // agent is the underlying reusable agent conversation.
	authorizer                     authdomain.Authorizer       // authorizer controls tool permissions and is closed with the session.
	addGrants                      grantsAdder                 // addGrants applies authorization grants derived from each user message.
	completedAssistantTurnsByAgent map[string][]llmstream.Turn // completedAssistantTurnsByAgent records completed assistant turns by agent ID for reporting.
	stepsSent                      int                         // stepsSent counts top-level user messages started on the session.
	mu                             sync.Mutex                  // mu serializes session steps and protects mutable session state.
	closeOnce                      sync.Once                   // closeOnce ensures session cleanup runs at most once.
	requestLoopWG                  sync.WaitGroup              // requestLoopWG waits for the permission request loop to exit.
	closed                         bool                        // closed reports whether Close has been called.
}

// activeToolDisplayState records display metadata for an active tool call.
type activeToolDisplayState struct {
	call                  llmstream.ToolCall                      // call is the active tool call.
	finalMessagePresenter llmstream.SubagentFinalMessagePresenter // finalMessagePresenter optionally customizes direct subagent final messages.
}

// agentDisplayScope describes how a descendant agent's output should be displayed.
type agentDisplayScope struct {
	call                  llmstream.ToolCall                      // call is the tool call that launched the scoped subagent.
	finalMessagePresenter llmstream.SubagentFinalMessagePresenter // finalMessagePresenter optionally customizes the subagent's final visible message.
	launcherAgentID       string                                  // launcherAgentID is the ID of the agent that launched the subagent.
	launcherToolCall      string                                  // launcherToolCall is the call ID of the launching tool call.
	subagentLabel         string                                  // subagentLabel is the optional label used for concurrent labeled subagent output.
}

// A labeledSubagentState records display state for an active labeled subagent.
type labeledSubagentState struct {
	agent     agent.AgentMeta   // It identifies the labeled subagent that owns the active scope.
	scope     agentDisplayScope // It describes the launching tool call and label presentation settings.
	finalText string            // It is the finalizing assistant text captured before the terminal event.
}

// subagentDisplayFilter tracks subagent display state while preparing agent events for output.
type subagentDisplayFilter struct {
	humanReadable bool                              // humanReadable enables human-readable-only labeled subagent presentation.
	activeTools   map[string]activeToolDisplayState // activeTools maps an agent ID to its currently active tool call display state.
	agentScopes   map[string]agentDisplayScope      // agentScopes maps a subagent ID to the display scope inherited from the tool call that launched it.
	agentParents  map[string]string                 // agentParents maps an agent ID to its parent agent ID.

	// activeLabeledSubagent maps a labeled subagent ID to the state used to hide descendant output until completion.
	activeLabeledSubagent map[string]labeledSubagentState
}

func newSubagentDisplayFilter(humanReadable bool) *subagentDisplayFilter {
	return &subagentDisplayFilter{
		humanReadable:         humanReadable,
		activeTools:           make(map[string]activeToolDisplayState),
		agentScopes:           make(map[string]agentDisplayScope),
		agentParents:          make(map[string]string),
		activeLabeledSubagent: make(map[string]labeledSubagentState),
	}
}

// Prepare updates f with ev, applies subagent display rules, and returns adjustments for the caller to apply before writing ev. The returned events are synthetic
// events to flush first, forceToolCallID names a delayed tool call that must be printed immediately, and hide reports whether ev itself should be suppressed. A
// nil receiver returns no adjustments.
func (f *subagentDisplayFilter) Prepare(ev agent.Event) ([]agent.Event, string, bool) {
	if f == nil {
		return nil, "", false
	}

	f.rememberAgent(ev.Agent)
	f.updateToolState(ev)

	if f.humanReadable {
		flush, forceToolCallID, hide, handled := f.prepareLabeledSubagentEvent(ev)
		if handled {
			return flush, forceToolCallID, hide
		}
	}

	if ev.Type == agent.EventTypeStartSubagent {
		return nil, "", true
	}

	if ev.Type != agent.EventTypeAssistantText || ev.Agent.Depth == 0 || !ev.AssistantTextFinalizing {
		return nil, "", false
	}

	scope := f.scopeForAgent(ev.Agent)
	if scope.finalMessagePresenter == nil {
		return nil, "", false
	}

	block := scope.finalMessagePresenter.SubagentFinalMessage(
		scope.call,
		scope.subagentLabel,
		ev.TextContent.Content,
	)
	if block == nil {
		return nil, "", true
	}

	content := agentformatter.RenderPlainTextBlock(block)
	if content == "" {
		return nil, "", true
	}

	return []agent.Event{{
		Agent: ev.Agent,
		Type:  agent.EventTypeAssistantText,
		TextContent: llmstream.TextContent{
			Content: content,
		},
	}}, "", true
}

// rememberAgent records meta's parent relationship for descendant display filtering.
func (f *subagentDisplayFilter) rememberAgent(meta agent.AgentMeta) {
	if f == nil || meta.ID == "" {
		return
	}
	f.agentParents[meta.ID] = meta.Parent
}

// scopeForAgent returns the cached or derived display scope for a non-root agent.
func (f *subagentDisplayFilter) scopeForAgent(meta agent.AgentMeta) agentDisplayScope {
	if f == nil || strings.TrimSpace(meta.ID) == "" || meta.Depth == 0 {
		return agentDisplayScope{}
	}

	if scope, ok := f.agentScopes[meta.ID]; ok {
		return scope
	}

	scope := agentDisplayScope{}
	parentID := strings.TrimSpace(meta.Parent)
	if parentID != "" {
		if active, ok := f.activeTools[parentID]; ok {
			scope = agentDisplayScope{
				call:                  active.call,
				finalMessagePresenter: active.finalMessagePresenter,
				launcherAgentID:       parentID,
				launcherToolCall:      active.call.CallID,
			}
		}
	}

	f.agentScopes[meta.ID] = scope
	return scope
}

// updateToolState records active tool and subagent display scope metadata from ev.
func (f *subagentDisplayFilter) updateToolState(ev agent.Event) {
	if f == nil {
		return
	}

	switch ev.Type {
	case agent.EventTypeToolCall:
		f.activeTools[ev.Agent.ID] = activeToolDisplayState{call: toolCallFromToolEvent(ev), finalMessagePresenter: subagentFinalMessagePresenterFromToolEvent(ev)}
	case agent.EventTypeToolComplete:
		delete(f.activeTools, ev.Agent.ID)
	case agent.EventTypeStartSubagent:
		if ev.Agent.ID == "" {
			return
		}
		f.agentScopes[ev.Agent.ID] = f.scopeFromStartSubagent(ev)
	}
}

// The prepareLabeledSubagentEvent method applies the human-readable display rules for labeled subagent events. It starts and finishes labeled scopes, captures the
// labeled subagent's finalizing assistant text, and hides descendant output while a labeled scope is active. It returns synthetic status events to flush, the launching
// tool call ID to force-print when a labeled subagent starts, whether ev should be hidden, and whether labeled-subagent handling consumed ev. A nil receiver returns
// no handling.
func (f *subagentDisplayFilter) prepareLabeledSubagentEvent(ev agent.Event) ([]agent.Event, string, bool, bool) {
	if f == nil {
		return nil, "", false, false
	}

	switch ev.Type {
	case agent.EventTypeStartSubagent:
		if strings.TrimSpace(ev.StartSubagent.Label) == "" {
			if _, _, ok := f.activeLabeledOwner(ev.Agent); ok {
				return nil, "", true, true
			}
			return nil, "", false, false
		}
		if _, _, ok := f.activeLabeledOwner(ev.Agent); ok {
			return nil, "", true, true
		}
		scope := f.scopeFromStartSubagent(ev)
		state := labeledSubagentState{
			agent: ev.Agent,
			scope: scope,
		}
		f.activeLabeledSubagent[ev.Agent.ID] = state
		return []agent.Event{f.syntheticAssistantText(ev.Agent, buildLabeledSubagentMessage(scope.subagentLabel, "started"))}, scope.launcherToolCall, true, true
	default:
		ownerID, state, ok := f.activeLabeledOwner(ev.Agent)
		if !ok {
			return nil, "", false, false
		}
		if ev.Agent.ID == ownerID && ev.Type == agent.EventTypeAssistantText && ev.AssistantTextFinalizing {
			state.finalText = ev.TextContent.Content
			f.activeLabeledSubagent[ownerID] = state
			return nil, "", true, true
		}
		if ev.Agent.ID == ownerID && isSubagentTerminalEvent(ev.Type) {
			delete(f.activeLabeledSubagent, ownerID)
			return []agent.Event{f.syntheticAssistantText(ev.Agent, f.labeledSubagentCompletionText(state, ev))}, "", true, true
		}
		return nil, "", true, true
	}
}

// activeLabeledOwner reports whether meta belongs to an active labeled subagent.
//
// It returns the active labeled subagent's agent ID and state when meta is the labeled subagent itself or a descendant of it. The boolean is false when there is
// no active labeled owner, f is nil, or meta has no ID.
func (f *subagentDisplayFilter) activeLabeledOwner(meta agent.AgentMeta) (string, labeledSubagentState, bool) {
	if f == nil || meta.ID == "" {
		return "", labeledSubagentState{}, false
	}
	if state, ok := f.activeLabeledSubagent[meta.ID]; ok {
		return meta.ID, state, true
	}

	parentID := meta.Parent
	for parentID != "" {
		if state, ok := f.activeLabeledSubagent[parentID]; ok {
			return parentID, state, true
		}
		parentID = f.agentParents[parentID]
	}
	return "", labeledSubagentState{}, false
}

// labeledSubagentCompletionText returns the single visible terminal message for a labeled subagent. Errors and cancellations are reported explicitly with any terminal
// error text; successful completion prefers presenter-customized text, then captured finalizing assistant text, and finally a finished fallback.
func (f *subagentDisplayFilter) labeledSubagentCompletionText(state labeledSubagentState, terminal agent.Event) string {
	switch terminal.Type {
	case agent.EventTypeError, agent.EventTypeCanceled:
		status := string(terminal.Type)
		msg := errorString(terminal.Error)
		if strings.TrimSpace(msg) == "" {
			return buildLabeledSubagentMessage(state.scope.subagentLabel, status)
		}
		return buildLabeledSubagentMessage(state.scope.subagentLabel, status+": "+msg)
	}

	content := ""
	if state.scope.finalMessagePresenter != nil {
		block := state.scope.finalMessagePresenter.SubagentFinalMessage(
			state.scope.call,
			state.scope.subagentLabel,
			state.finalText,
		)
		if block != nil {
			content = agentformatter.RenderPlainTextBlock(block)
		}
	}
	if strings.TrimSpace(content) == "" {
		content = state.finalText
	}
	if strings.TrimSpace(content) == "" {
		return buildLabeledSubagentMessage(state.scope.subagentLabel, "finished")
	}
	return prefixLabeledSubagentContent(state.scope.subagentLabel, content)
}

// syntheticAssistantText returns an assistant-text event for meta with content.
func (f *subagentDisplayFilter) syntheticAssistantText(meta agent.AgentMeta, content string) agent.Event {
	return agent.Event{
		Agent: meta,
		Type:  agent.EventTypeAssistantText,
		TextContent: llmstream.TextContent{
			Content: content,
		},
	}
}

func isSubagentTerminalEvent(eventType agent.EventType) bool {
	switch eventType {
	case agent.EventTypeDoneSuccess, agent.EventTypeError, agent.EventTypeCanceled:
		return true
	default:
		return false
	}
}

func buildLabeledSubagentMessage(label string, status string) string {
	return label + ": " + status
}

func prefixLabeledSubagentContent(label string, content string) string {
	firstLine, rest, found := strings.Cut(content, "\n")
	if !found {
		return buildLabeledSubagentMessage(label, firstLine)
	}
	if firstLine == "" {
		return label + ":\n" + rest
	}
	return buildLabeledSubagentMessage(label, firstLine) + "\n" + rest
}

func subagentFinalMessagePresenterFromToolEvent(ev agent.Event) llmstream.SubagentFinalMessagePresenter {
	if ev.Tool == nil || ev.Tool.Presenter() == nil {
		return nil
	}
	customizer, _ := ev.Tool.Presenter().(llmstream.SubagentFinalMessagePresenter)
	return customizer
}

func toolCallFromToolEvent(ev agent.Event) llmstream.ToolCall {
	call := llmstream.ToolCall{}
	if ev.ToolCall != nil {
		call = *ev.ToolCall
	} else if ev.ToolResult != nil {
		call.CallID = ev.ToolResult.CallID
		call.Name = ev.ToolResult.Name
		call.Type = ev.ToolResult.Type
	}

	if name := toolNameFromEvent(ev); name != "" {
		call.Name = name
	}
	return call
}

// scopeFromStartSubagent builds the display scope for a start-subagent event.
func (f *subagentDisplayFilter) scopeFromStartSubagent(ev agent.Event) agentDisplayScope {
	scope := agentDisplayScope{
		launcherAgentID:  ev.StartSubagent.CallingAgentID,
		launcherToolCall: ev.StartSubagent.ToolCallID,
		subagentLabel:    ev.StartSubagent.Label,
	}
	active, ok := f.activeTools[ev.StartSubagent.CallingAgentID]
	if !ok {
		return scope
	}
	scope.call = active.call
	scope.finalMessagePresenter = active.finalMessagePresenter
	if scope.launcherToolCall == "" {
		scope.launcherToolCall = active.call.CallID
	}
	return scope
}

// buildSessionConfig selects the session agent and initial-message rules from opts.
func buildSessionConfig(opts Options) (sessionConfig, error) {
	slashCommand, err := normalizeSlashCommand(opts.SlashCommand)
	if err != nil {
		return sessionConfig{}, err
	}

	config := sessionConfig{
		agentName: agentbuilder.AgentGeneric,
		pkgMode:   strings.TrimSpace(opts.PackagePath) != "",
	}
	if config.pkgMode {
		config.agentName = agentbuilder.AgentPackageModeNoContext
	}

	if slashCommand == slashCommandOrchestrate {
		config.agentName = orchestratorAgentName
		config.pkgMode = false
		config.allowEmptyInitialUser = true
	}

	return config, nil
}

// NewSession validates opts, prepares the underlying agent, and returns a reusable noninteractive session.
func NewSession(opts Options) (*Session, error) {
	config, err := buildSessionConfig(opts)
	if err != nil {
		return nil, err
	}

	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	rawOut := out
	out = &lockedWriter{w: out}
	jsonWriter := newJSONEventWriter(out)

	sandboxDir, err := normalizeSandboxDir(opts.CWD)
	if err != nil {
		return nil, err
	}

	var pkgRelPath string
	var pkgAbsPath string
	if config.pkgMode {
		pkgRelPath, pkgAbsPath, err = normalizePackagePath(opts.PackagePath, sandboxDir)
		if err != nil {
			return nil, err
		}
	}

	formatter := agentformatter.NewTUIFormatter(agentformatter.Config{
		PlainText: opts.NoFormatting,
	})
	terminalWidth := detectTerminalWidth(rawOut)
	modelID := effectiveModelID(opts)
	prompt.SetModel(modelID)

	sandboxAuthorizer, userRequests, err := authdomain.NewSessionAuthorizer(sandboxDir, nil, opts.AutoYes)
	if err != nil {
		return nil, err
	}
	authorizerForTools, err := buildAuthorizerForTools(config.pkgMode, pkgRelPath, pkgAbsPath, sandboxAuthorizer, "", authdomain.AddGrantsFromUserMessage)
	if err != nil {
		sandboxAuthorizer.Close()
		return nil, err
	}

	agentStart := sessionStart{
		agentName: config.agentName,
		pkgMode:   config.pkgMode,
	}
	agentInstance, err := buildAgent(agentStart, sandboxDir, pkgRelPath, pkgAbsPath, modelID, authorizerForTools, opts.LintSteps)
	if err != nil {
		authorizerForTools.Close()
		return nil, err
	}

	session := &Session{
		opts:   opts,
		config: config,
		startInfo: stepStartOutput{
			sandboxDir: sandboxDir,
			pkgRelPath: pkgRelPath,
			modelID:    modelID,
		},
		out:                            out,
		jsonWriter:                     jsonWriter,
		formatter:                      formatter,
		modelID:                        modelID,
		terminalWidth:                  terminalWidth,
		agent:                          agentInstance,
		authorizer:                     authorizerForTools,
		addGrants:                      authdomain.AddGrantsFromUserMessage,
		completedAssistantTurnsByAgent: make(map[string][]llmstream.Turn),
	}
	if userRequests != nil {
		session.requestLoopWG.Add(1)
		go func() {
			defer session.requestLoopWG.Done()
			autoRespondToUserRequests(userRequests, out, opts.AutoYes, jsonWriter, opts.OutputJSON)
		}()
	}

	return session, nil
}

// Close releases any resources owned by the session. It is safe to call multiple times.
func (s *Session) Close() error {
	if s == nil {
		return nil
	}

	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		authorizer := s.authorizer
		s.authorizer = nil
		s.addGrants = nil
		s.agent = nil
		s.formatter = nil
		s.jsonWriter = nil
		s.out = nil
		s.completedAssistantTurnsByAgent = nil
		s.mu.Unlock()

		if authorizer != nil {
			authorizer.Close()
		}
		s.requestLoopWG.Wait()
	})
	return nil
}

// SendUserMessage runs one top-level user message on an existing session, writes output according to the session options, and returns structured step metadata.
func (s *Session) SendUserMessage(ctx context.Context, userPrompt string) (Result, error) {
	if s == nil {
		return Result{}, fmt.Errorf("nil session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Result{}, fmt.Errorf("session is closed")
	}
	if s.agent == nil {
		return Result{}, fmt.Errorf("nil agent")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	userPrompt = strings.TrimSpace(userPrompt)
	if userPrompt == "" && !(s.stepsSent == 0 && s.config.allowEmptyInitialUser) {
		return Result{}, fmt.Errorf("prompt is required")
	}

	if err := applyGrantsFromUserPrompt(s.authorizer, userPrompt, s.addGrants); err != nil {
		return Result{}, err
	}
	if err := writeStepStartOutput(s.out, s.jsonWriter, s.opts.OutputJSON, s.startInfo, userPrompt); err != nil {
		return Result{}, err
	}

	s.stepsSent++

	var toolCallPrinter *delayedToolCallPrinter
	if !s.opts.OutputJSON {
		toolCallPrinter = newDelayedToolCallPrinter(s.out, toolCallPrintDelay)
		defer toolCallPrinter.Close()
	}

	result := Result{}
	var terminalErr error
	displayFilter := newSubagentDisplayFilter(!s.opts.OutputJSON)

	for ev := range s.agent.SendUserMessage(ctx, userPrompt) {
		flush, forceToolCallID, hide := displayFilter.Prepare(ev)
		if toolCallPrinter != nil && forceToolCallID != "" {
			toolCallPrinter.Force(forceToolCallID)
		}
		if err := s.writeFilteredEvents(flush); err != nil {
			return result, err
		}

		switch ev.Type {
		case agent.EventTypeAssistantText:
			if ev.Agent.Depth == 0 && ev.AssistantTextFinalizing {
				result.FinalAssistantText = ev.TextContent.Content
			}
		case agent.EventTypeAssistantTurnComplete:
			if ev.Turn != nil {
				s.completedAssistantTurnsByAgent[ev.Agent.ID] = append(s.completedAssistantTurnsByAgent[ev.Agent.ID], *ev.Turn)
			}
			continue
		case agent.EventTypeDoneSuccess:
			if ev.Agent.Depth > 0 {
				continue
			}
			result.TerminalEventType = ev.Type
			if s.opts.OutputJSON {
				var idealUsage *llmstream.TokenUsage
				if reportIdealCachingEnabled() {
					ideal := idealCachingForCompletedTurnsByAgent(s.completedAssistantTurnsByAgent, s.agent.Turns())
					idealUsage = &ideal
				}
				if err := s.jsonWriter.WriteDone(s.agent.TokenUsage(), idealUsage); err != nil {
					return result, err
				}
				continue
			}

			report := buildDoneSuccessReport(s.modelID, s.agent.Turns(), s.completedAssistantTurnsByAgent, s.agent.TokenUsage(), reportIdealCachingEnabled())
			if err := writeDoneSuccessReport(s.out, report); err != nil {
				return result, err
			}
			continue
		}

		if hide {
			continue
		}

		if s.opts.OutputJSON {
			if err := s.jsonWriter.WriteAgentEvent(ev); err != nil {
				return result, err
			}
			if shouldTrackTerminalError(ev) {
				result.TerminalEventType = ev.Type
				terminalErr = ev.Error
			}
			continue
		}

		switch ev.Type {
		case agent.EventTypeToolCall:
			formatted := formatHumanToolEvent(s.formatter, s.terminalWidth, legacyFormattedToolEvent(ev))
			if shouldSuppressFormattedOutput(formatted) || formatted == "" {
				continue
			}

			callID := ""
			if ev.ToolCall != nil {
				callID = ev.ToolCall.CallID
			}
			if callID == "" || toolCallPrintDelay <= 0 {
				if err := writeOutputLine(s.out, formatted); err != nil {
					return result, err
				}
			} else {
				toolCallPrinter.Schedule(callID, formatted)
			}
		case agent.EventTypeToolComplete:
			if ev.ToolResult != nil && strings.TrimSpace(ev.ToolResult.CallID) != "" {
				toolCallPrinter.Cancel(ev.ToolResult.CallID)
			}
			formatted := formatHumanToolEvent(s.formatter, s.terminalWidth, legacyFormattedToolEvent(ev))
			if shouldSuppressFormattedOutput(formatted) || formatted == "" {
				continue
			}
			if err := writeOutputLine(s.out, formatted); err != nil {
				return result, err
			}
		case agent.EventTypeToolOutput:
			formatted := s.formatter.FormatEvent(ev, s.terminalWidth)
			if shouldSuppressFormattedOutput(formatted) || formatted == "" {
				continue
			}
			toolCallPrinter.Force(toolCallIDFromEvent(ev))
			if err := writeOutputLine(s.out, formatted); err != nil {
				return result, err
			}
		case agent.EventTypeStartSubagent:
			continue
		default:
			formatted := s.formatter.FormatEvent(ev, s.terminalWidth)
			if shouldSuppressFormattedOutput(formatted) || formatted == "" {
				continue
			}
			if err := writeOutputLine(s.out, formatted); err != nil {
				return result, err
			}
		}

		if shouldTrackTerminalError(ev) {
			result.TerminalEventType = ev.Type
			terminalErr = ev.Error
		}
	}
	result.TokenUsage = s.agent.TokenUsage()
	result.ContextUsagePercent = s.agent.ContextUsagePercent()

	if terminalErr != nil {
		return result, &printedError{err: terminalErr}
	}
	return result, nil
}

func formatHumanToolEvent(formatter agentformatter.Formatter, terminalWidth int, ev agent.Event) string {
	if formatter == nil {
		return presenterSummaryFallback(ev)
	}

	formatted := formatter.FormatEvent(ev, terminalWidth)
	if formatted != "" || ev.Type != agent.EventTypeToolCall {
		return formatted
	}

	return presenterSummaryFallback(ev)
}

func presenterSummaryFallback(ev agent.Event) string {
	if ev.Type != agent.EventTypeToolCall || ev.Tool == nil || ev.Tool.Presenter() == nil || ev.ToolCall == nil {
		return ""
	}

	summary := renderPresentationLine(ev.Tool.Presenter().Present(*ev.ToolCall, nil).Summary)
	if summary == "" {
		return ""
	}
	return "• " + summary
}

func renderPresentationLine(line llmstream.Line) string {
	if len(line.Segments) == 0 {
		return ""
	}

	var b strings.Builder
	for i, seg := range line.Segments {
		if line.JoinWithSpace && i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(seg.Text)
	}
	return b.String()
}

// writeFilteredEvents writes prepared display events according to the session output mode.
func (s *Session) writeFilteredEvents(events []agent.Event) error {
	for _, ev := range events {
		if ev.Type == agent.EventTypeStartSubagent {
			continue
		}
		if s.opts.OutputJSON {
			if err := s.jsonWriter.WriteAgentEvent(ev); err != nil {
				return err
			}
			continue
		}

		formatted := s.formatter.FormatEvent(ev, s.terminalWidth)
		if shouldSuppressFormattedOutput(formatted) || formatted == "" {
			continue
		}
		if err := writeOutputLine(s.out, formatted); err != nil {
			return err
		}
	}
	return nil
}

func writeStepStartOutput(out io.Writer, jsonWriter *jsonEventWriter, outputJSON bool, info stepStartOutput, visibleUserPrompt string) error {
	if outputJSON {
		if err := jsonWriter.WriteStart(info.sandboxDir, info.pkgRelPath, info.modelID); err != nil {
			return err
		}
		if strings.TrimSpace(visibleUserPrompt) == "" {
			return nil
		}
		return jsonWriter.WriteUserMessage(visibleUserPrompt)
	}

	if strings.TrimSpace(visibleUserPrompt) == "" {
		return nil
	}
	return printUserPrompt(out, visibleUserPrompt)
}

func writeDoneSuccessReport(out io.Writer, report doneSuccessReport) error {
	if strings.TrimSpace(report.UsageAndCaching) != "" {
		if err := writeOutputLine(out, report.UsageAndCaching); err != nil {
			return err
		}
	}

	for _, line := range report.Lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if err := writeOutputLine(out, line); err != nil {
			return err
		}
	}
	return nil
}

func writeOutputLine(out io.Writer, s string) error {
	if out == nil || s == "" {
		return nil
	}
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	_, err := io.WriteString(out, s)
	return err
}
