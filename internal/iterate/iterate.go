package iterate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/codalotl/codalotl/internal/agent"
)

// ContinueMode controls whether a prompt step starts fresh, resumes the current session, or is selected automatically.
type ContinueMode string

// Continue mode constants describe how Run should choose the session used for prompt steps.
const (
	ContinueModeAuto   ContinueMode = "auto"   // ContinueModeAuto lets Run choose fresh or resume for each prompt step.
	ContinueModeFresh  ContinueMode = "fresh"  // ContinueModeFresh starts each prompt step in a fresh session.
	ContinueModeResume ContinueMode = "resume" // ContinueModeResume runs each prompt step in the current session.
)

// StepKind identifies the role of a step executed by a Runner.
type StepKind string

// Step kind constants distinguish normal prompt work from continuation decisions.
const (
	StepKindPrompt   StepKind = "prompt"   // StepKindPrompt is a top-level iteration prompt.
	StepKindDecision StepKind = "decision" // StepKindDecision asks the current session whether iteration should continue.
)

// StopReason describes why Run stopped without returning an error.
type StopReason string

// Stop reason constants report the policy condition that ended iteration.
const (
	StopReasonDone           StopReason = "done"            // StopReasonDone means assistant output contained STOP_ITERATION.
	StopReasonMaxSteps       StopReason = "max_steps"       // StopReasonMaxSteps means MaxSteps was reached before starting another prompt step.
	StopReasonMaxElapsed     StopReason = "max_elapsed"     // StopReasonMaxElapsed means MaxElapsed was reached before starting another prompt step.
	StopReasonRetryExhausted StopReason = "retry_exhausted" // StopReasonRetryExhausted means the shared failed-step retry budget was exhausted.
)

// Step describes one message Run asks a Runner to send.
type Step struct {
	Iteration int          // Iteration is the 1-based prompt iteration associated with the step.
	Kind      StepKind     // Kind identifies whether the step is prompt work or a continuation decision.
	Mode      ContinueMode // Mode tells the Runner whether to start a fresh session or resume the current one.
	Prompt    string       // Prompt is the user message to send for the step.
}

// StepResult reports the terminal outcome and continuation signals from a completed step.
type StepResult struct {
	TerminalEventType   agent.EventType // TerminalEventType is the terminal agent event for the step.
	FinalAssistantText  string          // FinalAssistantText is the assistant text Run scans for iteration decision tokens.
	ContextUsagePercent int             // ContextUsagePercent is the current context usage percentage used by auto continuation mode.
}

// Runner executes prompt and decision steps for Run.
type Runner interface {
	// RunStep sends step and returns its terminal outcome.
	RunStep(ctx context.Context, step Step) (StepResult, error)
}

// Options configures an iteration run.
type Options struct {
	Prompt         string        // Prompt is the top-level user message sent for each prompt step.
	MaxSteps       int           // MaxSteps stops iteration before a new prompt step once this many prompt steps have completed.
	MaxElapsed     time.Duration // MaxElapsed stops iteration before a new prompt step once this duration has elapsed.
	DecisionPrompt *string       // nil uses built-in default; non-nil blank disables decision prompting.
	ContinueMode   ContinueMode  // ContinueMode selects the fresh, resume, or automatic continuation policy.
}

// Result summarizes a completed or partially completed iteration run.
type Result struct {
	Iterations int        // Iterations is the number of prompt steps completed.
	StopReason StopReason // StopReason is why iteration stopped when Run returns a nil error.
	LastStep   StepResult // LastStep is the most recent prompt or decision step result.
}

const (
	tokenStop           = "STOP_ITERATION"
	tokenContinue       = "CONTINUE_ITERATION"
	tokenContinueFresh  = "CONTINUE_FRESH"
	tokenContinueResume = "CONTINUE_RESUME"

	maxFailedStepAttempts = 3
)

const defaultDecisionPrompt = "Decide whether this workflow should continue. Output STOP_ITERATION if the workflow is done. Otherwise output CONTINUE_ITERATION. Output only the token."

var timeNow = time.Now

// Run executes prompt-driven iteration with runner until policy selects a stop condition or an error occurs.
func Run(ctx context.Context, runner Runner, opts Options) (Result, error) {
	if runner == nil {
		return Result{}, errors.New("iterate: runner is nil")
	}

	configuredMode, err := normalizeContinueMode(opts.ContinueMode)
	if err != nil {
		return Result{}, err
	}

	decisionPrompt, decisionPromptEnabled := resolveDecisionPrompt(opts.DecisionPrompt)

	result := Result{}
	failedSteps := 0
	nextPromptMode := initialPromptMode(configuredMode)
	startedAt := timeNow()

	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		if opts.MaxSteps > 0 && result.Iterations >= opts.MaxSteps {
			result.StopReason = StopReasonMaxSteps
			return result, nil
		}

		if opts.MaxElapsed > 0 && timeNow().Sub(startedAt) >= opts.MaxElapsed {
			result.StopReason = StopReasonMaxElapsed
			return result, nil
		}

		promptResult, err := runner.RunStep(ctx, Step{
			Iteration: result.Iterations + 1,
			Kind:      StepKindPrompt,
			Mode:      nextPromptMode,
			Prompt:    opts.Prompt,
		})
		if err != nil {
			return result, err
		}

		result.Iterations++
		result.LastStep = promptResult

		if err := ctx.Err(); err != nil {
			return result, err
		}

		if promptResult.TerminalEventType != agent.EventTypeDoneSuccess {
			failedSteps++
			if failedSteps >= maxFailedStepAttempts {
				result.StopReason = StopReasonRetryExhausted
				return result, nil
			}

			nextPromptMode = selectContinueMode(configuredMode, "", promptResult.ContextUsagePercent)
			continue
		}

		if hasStopToken(promptResult.FinalAssistantText) {
			result.StopReason = StopReasonDone
			return result, nil
		}

		if directive, ok := parseContinueDirective(promptResult.FinalAssistantText); ok {
			nextPromptMode = selectContinueMode(configuredMode, directive, promptResult.ContextUsagePercent)
			continue
		}

		if !decisionPromptEnabled {
			nextPromptMode = selectContinueMode(configuredMode, "", promptResult.ContextUsagePercent)
			continue
		}

		decisionResult, err := runner.RunStep(ctx, Step{
			Iteration: result.Iterations,
			Kind:      StepKindDecision,
			Mode:      ContinueModeResume,
			Prompt:    decisionPrompt,
		})
		if err != nil {
			return result, err
		}

		result.LastStep = decisionResult

		if err := ctx.Err(); err != nil {
			return result, err
		}

		if decisionResult.TerminalEventType != agent.EventTypeDoneSuccess {
			failedSteps++
			if failedSteps >= maxFailedStepAttempts {
				result.StopReason = StopReasonRetryExhausted
				return result, nil
			}

			nextPromptMode = selectContinueMode(configuredMode, "", decisionResult.ContextUsagePercent)
			continue
		}

		if hasStopToken(decisionResult.FinalAssistantText) {
			result.StopReason = StopReasonDone
			return result, nil
		}

		if directive, ok := parseContinueDirective(decisionResult.FinalAssistantText); ok {
			nextPromptMode = selectContinueMode(configuredMode, directive, decisionResult.ContextUsagePercent)
			continue
		}

		nextPromptMode = selectContinueMode(configuredMode, "", decisionResult.ContextUsagePercent)
	}
}

func normalizeContinueMode(mode ContinueMode) (ContinueMode, error) {
	switch mode {
	case "", ContinueModeAuto:
		return ContinueModeAuto, nil
	case ContinueModeFresh, ContinueModeResume:
		return mode, nil
	default:
		return "", fmt.Errorf("iterate: invalid continue mode %q", mode)
	}
}

func initialPromptMode(configuredMode ContinueMode) ContinueMode {
	switch configuredMode {
	case ContinueModeFresh, ContinueModeResume:
		return configuredMode
	default:
		return ContinueModeFresh
	}
}

func resolveDecisionPrompt(prompt *string) (string, bool) {
	if prompt == nil {
		return defaultDecisionPrompt, true
	}

	if strings.TrimSpace(*prompt) == "" {
		return "", false
	}

	return *prompt, true
}

func hasStopToken(text string) bool {
	return strings.Contains(text, tokenStop)
}

func parseContinueDirective(text string) (ContinueMode, bool) {
	switch {
	case strings.Contains(text, tokenContinueFresh):
		return ContinueModeFresh, true
	case strings.Contains(text, tokenContinueResume):
		return ContinueModeResume, true
	case strings.Contains(text, tokenContinue):
		return ContinueModeAuto, true
	default:
		return "", false
	}
}

func selectContinueMode(configuredMode ContinueMode, directive ContinueMode, contextUsagePercent int) ContinueMode {
	if configuredMode == ContinueModeFresh || configuredMode == ContinueModeResume {
		return configuredMode
	}

	if directive == ContinueModeFresh || directive == ContinueModeResume {
		return directive
	}

	if contextUsagePercent <= 25 {
		return ContinueModeResume
	}

	return ContinueModeFresh
}
