package iterate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/codalotl/codalotl/internal/agent"
)

type ContinueMode string

const (
	ContinueModeAuto   ContinueMode = "auto"
	ContinueModeFresh  ContinueMode = "fresh"
	ContinueModeResume ContinueMode = "resume"
)

type StepKind string

const (
	StepKindPrompt   StepKind = "prompt"
	StepKindDecision StepKind = "decision"
)

type StopReason string

const (
	StopReasonDone           StopReason = "done"
	StopReasonMaxSteps       StopReason = "max_steps"
	StopReasonMaxElapsed     StopReason = "max_elapsed"
	StopReasonRetryExhausted StopReason = "retry_exhausted"
)

type Step struct {
	Iteration int
	Kind      StepKind
	Mode      ContinueMode
	Prompt    string
}

type StepResult struct {
	TerminalEventType   agent.EventType
	FinalAssistantText  string
	ContextUsagePercent int
}

type Runner interface {
	RunStep(ctx context.Context, step Step) (StepResult, error)
}

type Options struct {
	Prompt         string
	MaxSteps       int
	MaxElapsed     time.Duration
	DecisionPrompt *string // nil uses built-in default; non-nil blank disables decision prompting.
	ContinueMode   ContinueMode
}

type Result struct {
	Iterations int
	StopReason StopReason
	LastStep   StepResult
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
