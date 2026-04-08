package iterate

import (
	"context"
	"testing"
	"time"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/stretchr/testify/require"
)

func TestRunStopsOnExplicitStopToken(t *testing.T) {
	runner := &scriptedRunner{
		t: t,
		calls: []scriptedCall{
			{
				check: func(t *testing.T, step Step) {
					require.Equal(t, Step{
						Iteration: 1,
						Kind:      StepKindPrompt,
						Mode:      ContinueModeFresh,
						Prompt:    "work",
					}, step)
				},
				result: successResult("done "+tokenStop, 10),
			},
		},
	}

	result, err := Run(context.Background(), runner, Options{Prompt: "work"})
	require.NoError(t, err)
	require.Equal(t, 1, result.Iterations)
	require.Equal(t, StopReasonDone, result.StopReason)
	require.Equal(t, successResult("done "+tokenStop, 10), result.LastStep)
	runner.requireDone()
}

func TestRunContinuesOnExplicitContinueToken(t *testing.T) {
	runner := &scriptedRunner{
		t: t,
		calls: []scriptedCall{
			{
				check: func(t *testing.T, step Step) {
					require.Equal(t, Step{
						Iteration: 1,
						Kind:      StepKindPrompt,
						Mode:      ContinueModeFresh,
						Prompt:    "work",
					}, step)
				},
				result: successResult("keep going "+tokenContinue, 60),
			},
			{
				check: func(t *testing.T, step Step) {
					require.Equal(t, Step{
						Iteration: 2,
						Kind:      StepKindPrompt,
						Mode:      ContinueModeFresh,
						Prompt:    "work",
					}, step)
				},
				result: successResult(tokenStop, 5),
			},
		},
	}

	result, err := Run(context.Background(), runner, Options{Prompt: "work"})
	require.NoError(t, err)
	require.Equal(t, 2, result.Iterations)
	require.Equal(t, StopReasonDone, result.StopReason)
	require.Equal(t, successResult(tokenStop, 5), result.LastStep)
	runner.requireDone()
}

func TestRunFallsBackToDecisionPrompt(t *testing.T) {
	runner := &scriptedRunner{
		t: t,
		calls: []scriptedCall{
			{
				check: func(t *testing.T, step Step) {
					require.Equal(t, Step{
						Iteration: 1,
						Kind:      StepKindPrompt,
						Mode:      ContinueModeFresh,
						Prompt:    "work",
					}, step)
				},
				result: successResult("no token here", 20),
			},
			{
				check: func(t *testing.T, step Step) {
					require.Equal(t, Step{
						Iteration: 1,
						Kind:      StepKindDecision,
						Mode:      ContinueModeResume,
						Prompt:    defaultDecisionPrompt,
					}, step)
				},
				result: successResult(tokenStop, 20),
			},
		},
	}

	result, err := Run(context.Background(), runner, Options{Prompt: "work"})
	require.NoError(t, err)
	require.Equal(t, 1, result.Iterations)
	require.Equal(t, StopReasonDone, result.StopReason)
	require.Equal(t, successResult(tokenStop, 20), result.LastStep)
	runner.requireDone()
}

func TestRunSkipsDecisionPromptWhenDisabled(t *testing.T) {
	blank := "   "
	runner := &scriptedRunner{
		t: t,
		calls: []scriptedCall{
			{
				check: func(t *testing.T, step Step) {
					require.Equal(t, Step{
						Iteration: 1,
						Kind:      StepKindPrompt,
						Mode:      ContinueModeFresh,
						Prompt:    "work",
					}, step)
				},
				result: successResult("no token here", 10),
			},
		},
	}

	result, err := Run(context.Background(), runner, Options{
		Prompt:         "work",
		MaxSteps:       1,
		DecisionPrompt: &blank,
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.Iterations)
	require.Equal(t, StopReasonMaxSteps, result.StopReason)
	require.Equal(t, successResult("no token here", 10), result.LastStep)
	runner.requireDone()
}

func TestRunStopsAtMaxSteps(t *testing.T) {
	runner := &scriptedRunner{
		t: t,
		calls: []scriptedCall{
			{
				result: successResult(tokenContinue, 30),
			},
		},
	}

	result, err := Run(context.Background(), runner, Options{
		Prompt:   "work",
		MaxSteps: 1,
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.Iterations)
	require.Equal(t, StopReasonMaxSteps, result.StopReason)
	require.Equal(t, successResult(tokenContinue, 30), result.LastStep)
	runner.requireDone()
}

func TestRunStopsAtMaxElapsed(t *testing.T) {
	setTimeNowSequence(t,
		time.Unix(100, 0),
		time.Unix(100, 0),
		time.Unix(102, 0),
	)

	runner := &scriptedRunner{
		t: t,
		calls: []scriptedCall{
			{
				result: successResult(tokenContinue, 10),
			},
		},
	}

	result, err := Run(context.Background(), runner, Options{
		Prompt:     "work",
		MaxElapsed: time.Second,
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.Iterations)
	require.Equal(t, StopReasonMaxElapsed, result.StopReason)
	require.Equal(t, successResult(tokenContinue, 10), result.LastStep)
	runner.requireDone()
}

func TestRunStopsWhenRetryBudgetIsExhausted(t *testing.T) {
	runner := &scriptedRunner{
		t: t,
		calls: []scriptedCall{
			{result: failureResult(agent.EventTypeError, 0)},
			{result: failureResult(agent.EventTypeError, 0)},
			{result: failureResult(agent.EventTypeCanceled, 0)},
		},
	}

	result, err := Run(context.Background(), runner, Options{Prompt: "work"})
	require.NoError(t, err)
	require.Equal(t, 3, result.Iterations)
	require.Equal(t, StopReasonRetryExhausted, result.StopReason)
	require.Equal(t, failureResult(agent.EventTypeCanceled, 0), result.LastStep)
	runner.requireDone()
}

func TestRunAutoSelectsContinueMode(t *testing.T) {
	tests := []struct {
		name     string
		usage    int
		wantMode ContinueMode
	}{
		{
			name:     "resume at twenty five percent",
			usage:    25,
			wantMode: ContinueModeResume,
		},
		{
			name:     "fresh above twenty five percent",
			usage:    26,
			wantMode: ContinueModeFresh,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &scriptedRunner{
				t: t,
				calls: []scriptedCall{
					{
						result: successResult(tokenContinue, tt.usage),
					},
					{
						check: func(t *testing.T, step Step) {
							require.Equal(t, tt.wantMode, step.Mode)
						},
						result: successResult(tokenStop, 5),
					},
				},
			}

			result, err := Run(context.Background(), runner, Options{Prompt: "work"})
			require.NoError(t, err)
			require.Equal(t, 2, result.Iterations)
			require.Equal(t, StopReasonDone, result.StopReason)
			runner.requireDone()
		})
	}
}

func TestRunExplicitContinueModeOverridesAssistantHints(t *testing.T) {
	tests := []struct {
		name          string
		configured    ContinueMode
		assistantText string
		wantMode      ContinueMode
	}{
		{
			name:          "fresh overrides resume hint",
			configured:    ContinueModeFresh,
			assistantText: tokenContinueResume,
			wantMode:      ContinueModeFresh,
		},
		{
			name:          "resume overrides fresh hint",
			configured:    ContinueModeResume,
			assistantText: tokenContinueFresh,
			wantMode:      ContinueModeResume,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &scriptedRunner{
				t: t,
				calls: []scriptedCall{
					{
						result: successResult(tt.assistantText, 99),
					},
					{
						check: func(t *testing.T, step Step) {
							require.Equal(t, tt.wantMode, step.Mode)
						},
						result: successResult(tokenStop, 5),
					},
				},
			}

			result, err := Run(context.Background(), runner, Options{
				Prompt:       "work",
				ContinueMode: tt.configured,
			})
			require.NoError(t, err)
			require.Equal(t, 2, result.Iterations)
			require.Equal(t, StopReasonDone, result.StopReason)
			runner.requireDone()
		})
	}
}

func TestRunDecisionFailuresConsumeSharedRetryBudget(t *testing.T) {
	runner := &scriptedRunner{
		t: t,
		calls: []scriptedCall{
			{result: successResult("no token", 10)},
			{result: failureResult(agent.EventTypeError, 10)},
			{result: successResult("still no token", 10)},
			{result: failureResult(agent.EventTypeCanceled, 10)},
			{result: successResult("again no token", 10)},
			{result: failureResult(agent.EventTypeError, 10)},
		},
	}

	result, err := Run(context.Background(), runner, Options{Prompt: "work"})
	require.NoError(t, err)
	require.Equal(t, 3, result.Iterations)
	require.Equal(t, StopReasonRetryExhausted, result.StopReason)
	require.Equal(t, failureResult(agent.EventTypeError, 10), result.LastStep)
	require.Equal(t, []StepKind{
		StepKindPrompt,
		StepKindDecision,
		StepKindPrompt,
		StepKindDecision,
		StepKindPrompt,
		StepKindDecision,
	}, runner.stepKinds())
	runner.requireDone()
}

type scriptedRunner struct {
	t     *testing.T
	calls []scriptedCall
	seen  []Step
	index int
}

type scriptedCall struct {
	check  func(*testing.T, Step)
	result StepResult
	err    error
}

func (r *scriptedRunner) RunStep(_ context.Context, step Step) (StepResult, error) {
	r.t.Helper()

	require.Less(r.t, r.index, len(r.calls))

	call := r.calls[r.index]
	r.index++
	r.seen = append(r.seen, step)

	if call.check != nil {
		call.check(r.t, step)
	}

	return call.result, call.err
}

func (r *scriptedRunner) requireDone() {
	r.t.Helper()
	require.Equal(r.t, len(r.calls), r.index)
}

func (r *scriptedRunner) stepKinds() []StepKind {
	kinds := make([]StepKind, 0, len(r.seen))
	for _, step := range r.seen {
		kinds = append(kinds, step.Kind)
	}
	return kinds
}

func successResult(text string, usage int) StepResult {
	return StepResult{
		TerminalEventType:   agent.EventTypeDoneSuccess,
		FinalAssistantText:  text,
		ContextUsagePercent: usage,
	}
}

func failureResult(eventType agent.EventType, usage int) StepResult {
	return StepResult{
		TerminalEventType:   eventType,
		ContextUsagePercent: usage,
	}
}

func setTimeNowSequence(t *testing.T, values ...time.Time) {
	t.Helper()

	original := timeNow
	index := 0

	timeNow = func() time.Time {
		if index >= len(values) {
			return values[len(values)-1]
		}

		value := values[index]
		index++
		return value
	}

	t.Cleanup(func() {
		timeNow = original
	})
}
