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

type sessionConfig struct {
	agentName             string
	pkgMode               bool
	allowEmptyInitialUser bool
}

type sessionAgent interface {
	SendUserMessage(ctx context.Context, message string) <-chan agent.Event
	TokenUsage() llmstream.TokenUsage
	ContextUsagePercent() int
	Turns() []llmstream.Turn
}

type stepStartOutput struct {
	sandboxDir string
	pkgRelPath string
	modelID    llmmodel.ModelID
}

// Result reports structured metadata for one top-level noninteractive step.
type Result struct {
	TerminalEventType   agent.EventType      // Terminal event for this step's run.
	FinalAssistantText  string               // Final top-level assistant text emitted for this step.
	TokenUsage          llmstream.TokenUsage // Cumulative session token usage after this step, not a per-step delta.
	ContextUsagePercent int                  // Overall session context usage after this step, based on the latest assistant turn.
}

// Session holds a reusable noninteractive agent conversation.
type Session struct {
	opts                           Options
	config                         sessionConfig
	startInfo                      stepStartOutput
	out                            io.Writer
	jsonWriter                     *jsonEventWriter
	formatter                      agentformatter.Formatter
	modelID                        llmmodel.ModelID
	terminalWidth                  int
	agent                          sessionAgent
	authorizer                     authdomain.Authorizer
	addGrants                      grantsAdder
	completedAssistantTurnsByAgent map[string][]llmstream.Turn
	stepsSent                      int
	mu                             sync.Mutex
	closeOnce                      sync.Once
	requestLoopWG                  sync.WaitGroup
	closed                         bool
}

type activeToolDisplayState struct {
	callID string
	policy llmstream.SubagentEventPolicy
}

type agentDisplayScope struct {
	policy           llmstream.SubagentEventPolicy
	launcherAgentID  string
	launcherToolCall string
}

type pendingAssistantEvents struct {
	events       []agent.Event
	turnComplete bool
}

type subagentDisplayFilter struct {
	activeTools      map[string]activeToolDisplayState
	agentScopes      map[string]agentDisplayScope
	pendingAssistant map[string]*pendingAssistantEvents
}

func newSubagentDisplayFilter() *subagentDisplayFilter {
	return &subagentDisplayFilter{
		activeTools:      make(map[string]activeToolDisplayState),
		agentScopes:      make(map[string]agentDisplayScope),
		pendingAssistant: make(map[string]*pendingAssistantEvents),
	}
}

func (f *subagentDisplayFilter) Prepare(ev agent.Event) ([]agent.Event, bool) {
	if f == nil {
		return nil, false
	}

	scope := f.scopeForAgent(ev.Agent)
	if scope.policy != llmstream.SubagentEventPolicyHideFinalMessage {
		f.updateToolState(ev)
		return nil, false
	}

	if ev.Agent.Depth > 0 && isAgentTerminalEvent(ev.Type) {
		delete(f.pendingAssistant, ev.Agent.ID)
		f.updateToolState(ev)
		return nil, false
	}

	flush := f.pendingEventsToFlushBefore(ev)
	if ev.Type == agent.EventTypeAssistantText {
		f.bufferAssistantEvent(ev)
		return flush, true
	}
	if ev.Type == agent.EventTypeAssistantTurnComplete {
		f.markAssistantTurnComplete(ev.Agent.ID)
		return flush, true
	}

	f.updateToolState(ev)
	return flush, false
}

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
				policy:           active.policy,
				launcherAgentID:  parentID,
				launcherToolCall: active.callID,
			}
		}
	}

	f.agentScopes[meta.ID] = scope
	return scope
}

func (f *subagentDisplayFilter) pendingEventsToFlushBefore(ev agent.Event) []agent.Event {
	if f == nil {
		return nil
	}

	pending := f.pendingAssistant[ev.Agent.ID]
	if pending == nil || len(pending.events) == 0 {
		return nil
	}

	switch ev.Type {
	case agent.EventTypeAssistantText:
		if !pending.turnComplete {
			return nil
		}
	case agent.EventTypeAssistantTurnComplete:
		return nil
	}

	events := pending.events
	delete(f.pendingAssistant, ev.Agent.ID)
	return events
}

func (f *subagentDisplayFilter) bufferAssistantEvent(ev agent.Event) {
	if f == nil {
		return
	}
	pending := f.pendingAssistant[ev.Agent.ID]
	if pending == nil {
		pending = &pendingAssistantEvents{}
		f.pendingAssistant[ev.Agent.ID] = pending
	}
	pending.events = append(pending.events, ev)
}

func (f *subagentDisplayFilter) markAssistantTurnComplete(agentID string) {
	if f == nil {
		return
	}
	pending := f.pendingAssistant[agentID]
	if pending == nil {
		return
	}
	pending.turnComplete = true
}

func (f *subagentDisplayFilter) updateToolState(ev agent.Event) {
	if f == nil {
		return
	}

	switch ev.Type {
	case agent.EventTypeToolCall:
		f.activeTools[ev.Agent.ID] = activeToolDisplayState{
			callID: toolCallIDFromEvent(ev),
			policy: subagentEventPolicyFromToolEvent(ev),
		}
	case agent.EventTypeToolComplete:
		callID := toolCallIDFromEvent(ev)
		if callID == "" {
			callID = f.activeTools[ev.Agent.ID].callID
		}
		delete(f.activeTools, ev.Agent.ID)
		f.discardFinalAssistantEventsForScope(ev.Agent.ID, callID)
	}
}

func (f *subagentDisplayFilter) discardFinalAssistantEventsForScope(agentID string, callID string) {
	if f == nil || strings.TrimSpace(agentID) == "" {
		return
	}

	for childID, scope := range f.agentScopes {
		if scope.launcherAgentID != agentID {
			continue
		}
		if callID != "" && scope.launcherToolCall != "" && scope.launcherToolCall != callID {
			continue
		}
		delete(f.pendingAssistant, childID)
	}
}

func subagentEventPolicyFromToolEvent(ev agent.Event) llmstream.SubagentEventPolicy {
	if ev.Tool == nil || ev.Tool.Presenter() == nil {
		return llmstream.SubagentEventPolicyDefault
	}

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

	return ev.Tool.Presenter().SubagentEventPolicy(call)
}

func toolCallIDFromEvent(ev agent.Event) string {
	if ev.ToolCall != nil && strings.TrimSpace(ev.ToolCall.CallID) != "" {
		return ev.ToolCall.CallID
	}
	if ev.ToolResult != nil {
		return strings.TrimSpace(ev.ToolResult.CallID)
	}
	return ""
}

func isAgentTerminalEvent(eventType agent.EventType) bool {
	return eventType == agent.EventTypeDoneSuccess || eventType == agent.EventTypeError || eventType == agent.EventTypeCanceled
}

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
	var partialAssistantText strings.Builder
	displayFilter := newSubagentDisplayFilter()

	for ev := range s.agent.SendUserMessage(ctx, userPrompt) {
		flush, hide := displayFilter.Prepare(ev)
		if err := s.writeFilteredEvents(flush); err != nil {
			return result, err
		}

		switch ev.Type {
		case agent.EventTypeAssistantText:
			if ev.Agent.Depth == 0 {
				appendAssistantText(&partialAssistantText, ev.TextContent.Content)
			}
		case agent.EventTypeAssistantTurnComplete:
			if ev.Turn != nil {
				s.completedAssistantTurnsByAgent[ev.Agent.ID] = append(s.completedAssistantTurnsByAgent[ev.Agent.ID], *ev.Turn)
				if ev.Agent.Depth == 0 {
					result.FinalAssistantText = ev.Turn.TextContent()
					partialAssistantText.Reset()
				}
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
			formatted := s.formatter.FormatEvent(legacyFormattedToolEvent(ev), s.terminalWidth)
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
			formatted := s.formatter.FormatEvent(legacyFormattedToolEvent(ev), s.terminalWidth)
			if shouldSuppressFormattedOutput(formatted) || formatted == "" {
				continue
			}
			if err := writeOutputLine(s.out, formatted); err != nil {
				return result, err
			}
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

	if partialAssistantText.Len() > 0 {
		result.FinalAssistantText = partialAssistantText.String()
	}
	result.TokenUsage = s.agent.TokenUsage()
	result.ContextUsagePercent = s.agent.ContextUsagePercent()

	if terminalErr != nil {
		return result, &printedError{err: terminalErr}
	}
	return result, nil
}

func (s *Session) writeFilteredEvents(events []agent.Event) error {
	for _, ev := range events {
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

func appendAssistantText(b *strings.Builder, content string) {
	if b == nil || content == "" {
		return
	}
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	b.WriteString(content)
}
