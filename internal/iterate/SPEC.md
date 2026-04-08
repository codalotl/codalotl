# iterate

`iterate` runs a prompt-driven workflow step-by-step until policy says stop.

It owns iteration policy: stop limits, retry handling, decision-token parsing, default decision prompting, and fresh-vs-resume selection. It does not own CLI formatting or a specific execution backend.

## Behavior

- One iteration step is one top-level user message sent through a runner.
- Support fresh sessions, resumed sessions, and auto selection.
- Stop before starting a new prompt step when max-step or max-elapsed limits are reached.
- Parse simple substring decision tokens from final assistant text:
  - `STOP_ITERATION`
  - `CONTINUE_ITERATION`
  - `CONTINUE_FRESH`
  - `CONTINUE_RESUME`
- If a step does not end with `agent.EventTypeDoneSuccess`, continue while retry budget remains. Retry budget is shared across prompt steps and decision-prompt steps.
- If no explicit decision token is present and decision prompting is enabled, ask for a decision in the current session before choosing whether to continue.
- Auto mode prefers resume when context usage is 25% or less; otherwise fresh. Explicit continue mode wins over assistant hints.

## Runners

- Runner is stateful. The coordinator tells it whether a step should start fresh or resume the current conversation.
- Runner abstraction supports codalotl-native sessions and external backends.
- Caller can observe per-step lifecycle data without this package printing directly.

## Public API

```go
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

func Run(ctx context.Context, runner Runner, opts Options) (Result, error)
```
