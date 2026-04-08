package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/iterate"
	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/noninteractive"
	qcli "github.com/codalotl/codalotl/internal/q/cli"
	"github.com/codalotl/codalotl/internal/q/remotemonitor"
)

const iterateResumePrompt = "Please continue your work."
const iterateDecisionPromptUnset = "__codalotl_iterate_decision_prompt_unset__"

type runWithConfigFunc func(string, func(*qcli.Context, Config, *remotemonitor.Monitor) error) qcli.RunFunc

type iterateSession interface {
	SendUserMessage(ctx context.Context, userPrompt string) (noninteractive.Result, error)
	Close() error
}

var newNoninteractiveSession = func(opts noninteractive.Options) (iterateSession, error) {
	return noninteractive.NewSession(opts)
}

var noninteractiveIsPrinted = noninteractive.IsPrinted

var runIterateLoop = iterate.Run

var newIterateInterruptContext = func(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt)
}

func newIterateCommand(runWithConfig runWithConfigFunc) *qcli.Command {
	iterateCmd := &qcli.Command{
		Name:  "iterate",
		Short: "Run repeated noninteractive agent steps until iteration policy stops.",
	}

	flags := iterateCmd.Flags()
	promptFile := flags.String("prompt-file", 0, "", "Load the initial prompt from a file.")
	orchestrate := flags.Bool("orchestrate", 0, false, "Start the built-in orchestrator flow.")
	maxSteps := flags.Int("max-steps", 0, 0, "Stop before starting a new prompt step after this many iterations (0 = unlimited).")
	maxMinutes := flags.Int("max-minutes", 0, 0, "Stop before starting a new prompt step after this many minutes (0 = unlimited).")
	decisionPrompt := flags.String("decision-prompt", 0, iterateDecisionPromptUnset, "Override the decision prompt. Use --decision-prompt='' to disable it.")
	continueMode := flags.String("continue-mode", 0, string(iterate.ContinueModeAuto), "How to continue between iterations: fresh, resume, or auto.")
	yes := flags.Bool("yes", 'y', false, "Auto-approve any permission checks (noninteractive).")
	noColor := flags.Bool("no-color", 0, false, "Disable ANSI colors and formatting.")
	outputJSON := flags.Bool("json", 0, false, "Output newline-delimited JSON.")
	model := flags.String("model", 0, "", "LLM model ID to use (overrides config preferredmodel; empty = default).")
	slashCommand := flags.String("slash-command", 0, "", "Apply a TUI-style slash command at session start (supported: orchestrate, /orchestrate).")

	iterateCmd.Args = func(args []string) error {
		normalizedSlashCommand, err := normalizeIterateSlashCommand(*orchestrate, *slashCommand)
		if err != nil {
			return err
		}
		_, err = resolveIteratePrompt(args, *promptFile, slashCommandAllowsEmptyInitialPrompt(normalizedSlashCommand))
		return err
	}

	iterateCmd.Run = runWithConfig("iterate", func(c *qcli.Context, cfg Config, _ *remotemonitor.Monitor) error {
		if *maxSteps < 0 {
			return qcli.UsageError{Message: fmt.Sprintf("invalid --max-steps: must be >= 0 (got %d)", *maxSteps)}
		}
		if *maxMinutes < 0 {
			return qcli.UsageError{Message: fmt.Sprintf("invalid --max-minutes: must be >= 0 (got %d)", *maxMinutes)}
		}

		mode, err := parseIterateContinueMode(*continueMode)
		if err != nil {
			return err
		}

		normalizedSlashCommand, err := normalizeIterateSlashCommand(*orchestrate, *slashCommand)
		if err != nil {
			return err
		}

		prompt, err := resolveIteratePrompt(c.Args, *promptFile, slashCommandAllowsEmptyInitialPrompt(normalizedSlashCommand))
		if err != nil {
			return err
		}

		modelID := llmmodel.ModelID(strings.TrimSpace(*model))
		if modelID == "" {
			modelID = llmmodel.ModelID(strings.TrimSpace(cfg.PreferredModel))
		}

		steps, err := lints.ResolveSteps(&cfg.Lints, cfg.ReflowWidth)
		if err != nil {
			return qcli.ExitError{Code: 1, Err: fmt.Errorf("invalid configuration: lints: %w", err)}
		}

		runner := &iterateSessionRunner{
			sessionOpts: noninteractive.Options{
				SlashCommand: normalizedSlashCommand,
				ModelID:      modelID,
				LintSteps:    steps,
				ReflowWidth:  cfg.ReflowWidth,
				AutoYes:      cfg.AutoYes || *yes,
				NoFormatting: *noColor,
				OutputJSON:   *outputJSON,
				Out:          c.Out,
			},
			lifecycle: iterateLifecycleWriter{
				out:        c.Out,
				outputJSON: *outputJSON,
			},
		}

		runCtx, stop := newIterateInterruptContext(c.Context)
		defer stop()

		var decisionPromptValue *string
		if *decisionPrompt != iterateDecisionPromptUnset {
			dp := *decisionPrompt
			decisionPromptValue = &dp
		}

		result, err := runIterateLoop(runCtx, runner, iterate.Options{
			Prompt:         prompt,
			MaxSteps:       *maxSteps,
			MaxElapsed:     time.Duration(*maxMinutes) * time.Minute,
			DecisionPrompt: decisionPromptValue,
			ContinueMode:   mode,
		})
		err = errors.Join(err, runner.Close())
		if metaErr := runner.lifecycle.Complete(result, err); metaErr != nil {
			err = errors.Join(err, metaErr)
		}
		if err == nil && result.StopReason == iterate.StopReasonRetryExhausted {
			return qcli.ExitError{Code: 1, Err: errors.New("iteration stopped after retry exhaustion")}
		}
		if err == nil {
			return nil
		}
		if noninteractiveIsPrinted(err) {
			return qcli.ExitError{Code: 1, Err: errors.New("")}
		}
		if errors.Is(err, context.Canceled) {
			return qcli.ExitError{Code: 1, Err: errors.New("interrupted")}
		}
		return err
	})

	return iterateCmd
}

func parseIterateContinueMode(mode string) (iterate.ContinueMode, error) {
	switch iterate.ContinueMode(strings.TrimSpace(mode)) {
	case "", iterate.ContinueModeAuto:
		return iterate.ContinueModeAuto, nil
	case iterate.ContinueModeFresh:
		return iterate.ContinueModeFresh, nil
	case iterate.ContinueModeResume:
		return iterate.ContinueModeResume, nil
	default:
		return "", qcli.UsageError{Message: fmt.Sprintf("invalid --continue-mode: %q (allowed: fresh, resume, auto)", mode)}
	}
}

func normalizeIterateSlashCommand(orchestrate bool, slashCommand string) (string, error) {
	slashCommand = strings.TrimSpace(slashCommand)
	if !orchestrate {
		return slashCommand, nil
	}
	switch slashCommand {
	case "", "orchestrate", "/orchestrate":
		if slashCommand == "" {
			return "orchestrate", nil
		}
		return slashCommand, nil
	default:
		return "", qcli.UsageError{Message: fmt.Sprintf("cannot combine --orchestrate with --slash-command=%q", slashCommand)}
	}
}

func resolveIteratePrompt(args []string, promptFile string, allowEmpty bool) (string, error) {
	argPrompt := strings.TrimSpace(strings.Join(args, " "))
	promptFile = strings.TrimSpace(promptFile)

	hasArgPrompt := argPrompt != ""
	hasPromptFile := promptFile != ""
	if hasArgPrompt && hasPromptFile {
		return "", qcli.UsageError{Message: "provide either <prompt> or --prompt-file, not both"}
	}

	if hasPromptFile {
		b, err := os.ReadFile(promptFile)
		if err != nil {
			return "", err
		}
		filePrompt := string(b)
		if strings.TrimSpace(filePrompt) == "" && !allowEmpty {
			return "", qcli.UsageError{Message: "prompt is required unless --orchestrate or --slash-command starts a session without an initial message"}
		}
		return filePrompt, nil
	}

	if hasArgPrompt {
		return argPrompt, nil
	}
	if allowEmpty {
		return "", nil
	}
	return "", qcli.UsageError{Message: "prompt is required unless --orchestrate or --slash-command starts a session without an initial message"}
}

func slashCommandAllowsEmptyInitialPrompt(slashCommand string) bool {
	switch strings.TrimSpace(slashCommand) {
	case "orchestrate", "/orchestrate":
		return true
	default:
		return false
	}
}

type iterateSessionRunner struct {
	sessionOpts noninteractive.Options
	session     iterateSession
	lifecycle   iterateLifecycleWriter
}

func (r *iterateSessionRunner) RunStep(ctx context.Context, step iterate.Step) (iterate.StepResult, error) {
	reuseSession := step.Mode == iterate.ContinueModeResume && r.session != nil
	if !reuseSession && (step.Mode == iterate.ContinueModeFresh || r.session == nil) {
		session, err := newNoninteractiveSession(r.sessionOpts)
		if err != nil {
			return iterate.StepResult{}, err
		}
		if err := r.replaceSession(session); err != nil {
			return iterate.StepResult{}, err
		}
	}

	prompt := step.Prompt
	if step.Kind == iterate.StepKindPrompt && reuseSession {
		prompt = iterateResumePrompt
	}

	if err := r.lifecycle.StepStart(step); err != nil {
		return iterate.StepResult{}, err
	}

	res, err := r.session.SendUserMessage(ctx, prompt)
	stepResult := iterate.StepResult{
		TerminalEventType:   res.TerminalEventType,
		FinalAssistantText:  res.FinalAssistantText,
		ContextUsagePercent: res.ContextUsagePercent,
	}
	if err != nil && noninteractiveIsPrinted(err) {
		switch stepResult.TerminalEventType {
		case agent.EventTypeError, agent.EventTypeCanceled:
			err = nil
		}
	}
	if err == nil || stepResult.TerminalEventType != "" {
		if finishErr := r.lifecycle.StepFinish(step, stepResult); finishErr != nil {
			if err != nil {
				return stepResult, errors.Join(err, finishErr)
			}
			return stepResult, finishErr
		}
	}
	return stepResult, err
}

func (r *iterateSessionRunner) replaceSession(session iterateSession) error {
	if r.session == nil {
		r.session = session
		return nil
	}

	oldSession := r.session
	r.session = session
	if err := oldSession.Close(); err != nil {
		r.session = nil
		return errors.Join(err, session.Close())
	}
	return nil
}

func (r *iterateSessionRunner) Close() error {
	if r.session == nil {
		return nil
	}
	session := r.session
	r.session = nil
	return session.Close()
}

type iterateLifecycleWriter struct {
	out        io.Writer
	outputJSON bool
}

func (w iterateLifecycleWriter) StepStart(step iterate.Step) error {
	if w.outputJSON {
		return w.writeJSON(iterateLifecycleEvent{
			Type:      "iterate_step_start",
			Iteration: step.Iteration,
			StepKind:  string(step.Kind),
			Mode:      string(step.Mode),
		})
	}
	return writeStringln(w.out, fmt.Sprintf("iterate: iteration %d %s starting (mode=%s)", step.Iteration, step.Kind, step.Mode))
}

func (w iterateLifecycleWriter) StepFinish(step iterate.Step, result iterate.StepResult) error {
	if w.outputJSON {
		return w.writeJSON(iterateLifecycleEvent{
			Type:                "iterate_step_finish",
			Iteration:           step.Iteration,
			StepKind:            string(step.Kind),
			Mode:                string(step.Mode),
			TerminalEventType:   string(result.TerminalEventType),
			ContextUsagePercent: result.ContextUsagePercent,
		})
	}

	return writeStringln(
		w.out,
		fmt.Sprintf(
			"iterate: iteration %d %s finished (event=%s, context=%d%%)",
			step.Iteration,
			step.Kind,
			result.TerminalEventType,
			result.ContextUsagePercent,
		),
	)
}

func (w iterateLifecycleWriter) Complete(result iterate.Result, runErr error) error {
	if w.outputJSON {
		event := iterateLifecycleEvent{
			Type:                "iterate_complete",
			Iterations:          result.Iterations,
			StopReason:          string(result.StopReason),
			TerminalEventType:   string(result.LastStep.TerminalEventType),
			ContextUsagePercent: result.LastStep.ContextUsagePercent,
		}
		if runErr != nil {
			if errors.Is(runErr, context.Canceled) {
				event.Error = "interrupted"
			} else {
				event.Error = runErr.Error()
			}
		}
		return w.writeJSON(event)
	}

	parts := []string{fmt.Sprintf("iterate: stopped after %d iteration(s)", result.Iterations)}
	details := []string{}
	if result.StopReason != "" {
		details = append(details, fmt.Sprintf("reason=%s", result.StopReason))
	}
	if result.LastStep.TerminalEventType != "" {
		details = append(details, fmt.Sprintf("event=%s", result.LastStep.TerminalEventType))
	}
	if runErr != nil {
		if errors.Is(runErr, context.Canceled) {
			details = append(details, "error=interrupted")
		} else {
			details = append(details, fmt.Sprintf("error=%s", runErr.Error()))
		}
	}
	if len(details) > 0 {
		parts = append(parts, "("+strings.Join(details, ", ")+")")
	}
	return writeStringln(w.out, strings.Join(parts, " "))
}

func (w iterateLifecycleWriter) writeJSON(v iterateLifecycleEvent) error {
	enc := json.NewEncoder(w.out)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

type iterateLifecycleEvent struct {
	Type                string `json:"type"`
	Iteration           int    `json:"iteration,omitempty"`
	StepKind            string `json:"step_kind,omitempty"`
	Mode                string `json:"mode,omitempty"`
	TerminalEventType   string `json:"terminal_event_type,omitempty"`
	ContextUsagePercent int    `json:"context_usage_percent,omitempty"`
	Iterations          int    `json:"iterations,omitempty"`
	StopReason          string `json:"stop_reason,omitempty"`
	Error               string `json:"error,omitempty"`
}

var _ iterate.Runner = (*iterateSessionRunner)(nil)
var _ iterateSession = (*noninteractive.Session)(nil)
