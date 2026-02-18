package cmdrunner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"text/template"
	"time"
)

// InputType represents the type of value that a command expects for a given input key.
type InputType string

// Supported input types.
//
// Path types can be absolute or relative to the root. The Any/Dir/File types are checked for existence (or Run will return an error). InputTypePathUnchecked is
// not checked for existence. All paths are converted to absolute paths before being passed to templates.
const (
	InputTypePathAny       InputType = "path_any"
	InputTypePathDir       InputType = "path_dir"
	InputTypePathFile      InputType = "path_file"
	InputTypePathUnchecked InputType = "path_unchecked"
	InputTypeBool          InputType = "bool"
	InputTypeString        InputType = "string"
	InputTypeInt           InputType = "int"
)

// Runner coordinates templating and execution for a collection of commands.
type Runner struct {
	inputSchema    map[string]InputType
	requiredInputs []string
	commands       []Command
}

// NewRunner constructs a Runner with the provided schema and required inputs. Defensive copies are taken to ensure subsequent callers cannot mutate the Runner's
// internal state by modifying the arguments.
func NewRunner(inputSchema map[string]InputType, requiredInputs []string) *Runner {
	cloneSchema := make(map[string]InputType, len(inputSchema))
	for k, v := range inputSchema {
		cloneSchema[k] = v
	}

	cloneRequired := append([]string(nil), requiredInputs...)

	return &Runner{
		inputSchema:    cloneSchema,
		requiredInputs: cloneRequired,
	}
}

// Run executes all configured commands. An error is returned if inputs are invalid or templating fails.
func (r *Runner) Run(ctx context.Context, rootDir string, inputs map[string]any) (Result, error) {
	if r == nil {
		return Result{}, errors.New("cmdrunner: runner is nil")
	}
	if ctx == nil {
		return Result{}, errors.New("cmdrunner: context is nil")
	}
	if rootDir == "" {
		return Result{}, errors.New("cmdrunner: rootDir must not be empty")
	}

	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return Result{}, fmt.Errorf("cmdrunner: resolve rootDir: %w", err)
	}

	rootInfo, err := os.Stat(absRoot)
	if err != nil {
		return Result{}, fmt.Errorf("cmdrunner: rootDir: %w", err)
	}
	if !rootInfo.IsDir() {
		return Result{}, fmt.Errorf("cmdrunner: rootDir %q is not a directory", absRoot)
	}

	normalizedInputs, err := r.normalizeInputs(absRoot, inputs)
	if err != nil {
		return Result{}, err
	}

	templateData := make(map[string]any, len(normalizedInputs)+2)
	for k, v := range normalizedInputs {
		templateData[k] = v
	}
	templateData["RootDir"] = absRoot
	templateData["DevNull"] = os.DevNull

	helpers := newTemplateHelperProvider(absRoot, normalizedInputs)
	funcs := helpers.funcMap()

	results := make([]CommandResult, len(r.commands))
	for i, cmd := range r.commands {
		if len(cmd.Attrs)%2 != 0 {
			return Result{}, fmt.Errorf("cmdrunner: command[%d]: attrs must have even length, got %d", i, len(cmd.Attrs))
		}

		renderedCommand := cmd.Command
		if cmd.Command != "" {
			renderedCommand, err = renderTemplate(fmt.Sprintf("command_%d", i), cmd.Command, funcs, templateData)
			if err != nil {
				return Result{}, fmt.Errorf("cmdrunner: command[%d] command template: %w", i, err)
			}
			renderedCommand = strings.TrimSpace(renderedCommand)
		}
		if renderedCommand == "" {
			return Result{}, fmt.Errorf("cmdrunner: command[%d]: command template rendered empty", i)
		}

		renderedArgs := make([]string, 0, len(cmd.Args))
		for argIdx, argTmpl := range cmd.Args {
			value := argTmpl
			if argTmpl != "" {
				value, err = renderTemplate(fmt.Sprintf("command_%d_arg_%d", i, argIdx), argTmpl, funcs, templateData)
				if err != nil {
					return Result{}, fmt.Errorf("cmdrunner: command[%d] arg[%d] template: %w", i, argIdx, err)
				}
			}
			trimmed := strings.TrimSpace(value)
			if trimmed == "" {
				continue
			}
			renderedArgs = append(renderedArgs, trimmed)
		}

		renderedCWD := absRoot
		if cmd.CWD != "" {
			renderedCWD, err = renderTemplate(fmt.Sprintf("command_%d_cwd", i), cmd.CWD, funcs, templateData)
			if err != nil {
				return Result{}, fmt.Errorf("cmdrunner: command[%d] cwd template: %w", i, err)
			}
			renderedCWD = strings.TrimSpace(renderedCWD)
		}
		if renderedCWD == "" {
			renderedCWD = absRoot
		}
		if !filepath.IsAbs(renderedCWD) {
			renderedCWD = filepath.Join(absRoot, renderedCWD)
		}

		initial := CommandResult{
			Command:           renderedCommand,
			Args:              renderedArgs,
			CWD:               renderedCWD,
			MessageIfNoOutput: cmd.MessageIfNoOutput,
			ShowCWD:           cmd.ShowCWD,
			Attrs:             append([]string(nil), cmd.Attrs...),
		}

		results[i] = executeCommand(ctx, cmd, initial)
	}

	return Result{Results: results}, nil
}

// AddCommand registers the provided command with the Runner.
func (r *Runner) AddCommand(c Command) {
	r.commands = append(r.commands, c)
}

// Command defines a templated command to run. Command, Args, and CWD fields support templating prior to execution.
type Command struct {
	Command                string   `json:"command"`
	Args                   []string `json:"args"`
	CWD                    string   `json:"cwd"`
	OutcomeFailIfAnyOutput bool     `json:"outcomefailifanyoutput"`
	MessageIfNoOutput      string   `json:"messageifnooutput"`
	ShowCWD                bool     `json:"showcwd"` // ShowCWD, when true, instructs XML rendering to include the command's CWD.

	// Attrs are pairs of keys/values that will be added to the corresponding command tag when rendering ToXML output. len(Attrs) must be a multiple of 2. Strings are
	// NOT validated or escaped.
	Attrs []string `json:"attrs"`
}

// Result aggregates all command executions performed by Run.
type Result struct {
	Results []CommandResult
}

// Success reports whether all individual command outcomes were successful.
func (r Result) Success() bool {
	for _, cr := range r.Results {
		if cr.Outcome != OutcomeSuccess {
			return false
		}
	}
	return true
}

// ExecStatus captures how process execution concluded.
type ExecStatus string

const (
	ExecStatusCompleted     ExecStatus = "completed"
	ExecStatusFailedToStart ExecStatus = "failed_to_start"
	ExecStatusTimedOut      ExecStatus = "timed_out"
	ExecStatusCanceled      ExecStatus = "canceled"
	ExecStatusTerminated    ExecStatus = "terminated"
)

// Outcome is a semantic status for the command result.
type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailed  Outcome = "failed"
)

// CommandResult captures the execution details for a single command.
type CommandResult struct {
	Command           string
	Args              []string
	CWD               string
	Output            string
	MessageIfNoOutput string
	ShowCWD           bool
	Attrs             []string
	ExecStatus        ExecStatus
	ExecError         error
	ExitCode          int
	Signal            string
	Outcome           Outcome
	Duration          time.Duration
}

func (r *Runner) normalizeInputs(rootDir string, inputs map[string]any) (map[string]any, error) {
	if inputs == nil {
		inputs = map[string]any{}
	}

	for _, key := range r.requiredInputs {
		if _, ok := r.inputSchema[key]; !ok {
			return nil, fmt.Errorf("cmdrunner: required input %q not defined in schema", key)
		}
		if _, ok := inputs[key]; !ok {
			return nil, fmt.Errorf("cmdrunner: missing required input %q", key)
		}
	}

	normalized := make(map[string]any, len(inputs))
	for key, raw := range inputs {
		inputType, ok := r.inputSchema[key]
		if !ok {
			return nil, fmt.Errorf("cmdrunner: unknown input %q", key)
		}

		value, err := normalizeInputValue(rootDir, raw, inputType)
		if err != nil {
			return nil, fmt.Errorf("cmdrunner: input %q: %w", key, err)
		}

		normalized[key] = value
	}

	return normalized, nil
}

func normalizeInputValue(rootDir string, raw any, inputType InputType) (any, error) {
	switch inputType {
	case InputTypeBool:
		val, ok := raw.(bool)
		if !ok {
			return nil, fmt.Errorf("expected bool, got %T", raw)
		}
		return val, nil
	case InputTypeString:
		val, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("expected string, got %T", raw)
		}
		return val, nil
	case InputTypeInt:
		val, ok := raw.(int)
		if !ok {
			return nil, fmt.Errorf("expected int, got %T", raw)
		}
		return val, nil
	case InputTypePathAny, InputTypePathDir, InputTypePathFile, InputTypePathUnchecked:
		return normalizePathInput(rootDir, raw, inputType)
	default:
		return nil, fmt.Errorf("unsupported input type %q", inputType)
	}
}

func normalizePathInput(rootDir string, raw any, inputType InputType) (string, error) {
	value, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("expected string for path, got %T", raw)
	}
	if value == "" {
		return "", errors.New("path is empty")
	}

	var absolute string
	if filepath.IsAbs(value) {
		absolute = filepath.Clean(value)
	} else {
		absolute = filepath.Join(rootDir, value)
	}

	if inputType == InputTypePathUnchecked {
		return absolute, nil
	}

	info, err := os.Stat(absolute)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("path does not exist: %s", absolute)
		}
		return "", fmt.Errorf("stat path: %w", err)
	}

	switch inputType {
	case InputTypePathAny:
		return absolute, nil
	case InputTypePathDir:
		if info.IsDir() {
			return absolute, nil
		}
		return filepath.Dir(absolute), nil
	case InputTypePathFile:
		if info.IsDir() {
			return "", fmt.Errorf("expected file, got directory: %s", absolute)
		}
		return absolute, nil
	default:
		return absolute, nil
	}
}

func renderTemplate(name, tmpl string, funcs template.FuncMap, data map[string]any) (string, error) {
	t, err := template.New(name).Funcs(funcs).Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func executeCommand(ctx context.Context, cmd Command, result CommandResult) CommandResult {
	start := time.Now()
	defer func() {
		if result.Duration == 0 {
			result.Duration = time.Since(start)
		}
	}()

	if err := ctx.Err(); err != nil {
		result.ExecError = err
		result.ExecStatus = statusFromContextError(err)
		result.ExitCode = -1
		result.Outcome = determineOutcome(result.ExitCode, result.ExecStatus, result.Output, cmd.OutcomeFailIfAnyOutput)
		return result
	}

	execCmd := exec.CommandContext(ctx, result.Command, result.Args...)
	execCmd.Dir = result.CWD

	var buf bytes.Buffer
	var bufMu sync.Mutex
	writer := &lockedBuffer{buf: &buf, mu: &bufMu}
	execCmd.Stdout = writer
	execCmd.Stderr = writer

	if err := execCmd.Start(); err != nil {
		result.ExecError = err
		result.ExecStatus = ExecStatusFailedToStart
		result.ExitCode = -1
		result.Output = writer.String()
		result.Outcome = determineOutcome(result.ExitCode, result.ExecStatus, result.Output, cmd.OutcomeFailIfAnyOutput)
		return result
	}

	waitErr := execCmd.Wait()
	result.Output = writer.String()
	result.Duration = time.Since(start)

	state := execCmd.ProcessState
	exitCode := -1
	if state != nil {
		exitCode = state.ExitCode()
		if ws, ok := state.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			result.Signal = ws.Signal().String()
		}
	}
	result.ExitCode = exitCode

	ctxErr := ctx.Err()
	result.ExecStatus = determineExecStatus(waitErr, state, ctxErr)
	switch result.ExecStatus {
	case ExecStatusTimedOut, ExecStatusCanceled:
		result.ExecError = ctxErr
	default:
		if waitErr != nil {
			result.ExecError = waitErr
		}
	}
	result.Outcome = determineOutcome(result.ExitCode, result.ExecStatus, result.Output, cmd.OutcomeFailIfAnyOutput)

	return result
}

func determineExecStatus(waitErr error, state *os.ProcessState, ctxErr error) ExecStatus {
	if ctxErr != nil {
		return statusFromContextError(ctxErr)
	}

	switch {
	case waitErr == nil:
		return ExecStatusCompleted
	case errors.Is(waitErr, context.DeadlineExceeded):
		return ExecStatusTimedOut
	case errors.Is(waitErr, context.Canceled):
		return ExecStatusCanceled
	default:
		if state != nil {
			if ws, ok := state.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
				return ExecStatusTerminated
			}
			return ExecStatusCompleted
		}
		return ExecStatusFailedToStart
	}
}

func statusFromContextError(err error) ExecStatus {
	if errors.Is(err, context.DeadlineExceeded) {
		return ExecStatusTimedOut
	}
	return ExecStatusCanceled
}

func determineOutcome(exitCode int, status ExecStatus, output string, failIfOutput bool) Outcome {
	if status != ExecStatusCompleted || exitCode != 0 {
		return OutcomeFailed
	}

	if failIfOutput && strings.TrimSpace(output) != "" {
		return OutcomeFailed
	}

	return OutcomeSuccess
}

type lockedBuffer struct {
	buf *bytes.Buffer
	mu  *sync.Mutex
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
