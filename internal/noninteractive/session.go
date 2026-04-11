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

	for ev := range s.agent.SendUserMessage(ctx, userPrompt) {
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
