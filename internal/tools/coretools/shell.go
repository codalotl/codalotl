package coretools

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// Requirements:
//   - accept shell commands via a function_call.
//   - inputs: command (array of strings, argv style); timeout_ms; cwd (abs path)
//   - output: {"success": true|false, "content": content}, where content is like: "Command: go test .\nCode: 0\nDuration: 19ms\nOutput:\n<the output>"
//   - no security (allowlist, blacklist, etc)

const (
	ToolNameShell              = "shell" // ToolNameShell is the registered name of the shell tool.
	defaultShellTimeout        = 120 * time.Second
	defaultShellMaxOutputBytes = 40_000
	minShellMaxOutputBytes     = 1024
	maxShellMaxOutputBytes     = 1024 * 1024
)

//go:embed shell.md
var descriptionShell string

// The toolShell type implements the shell tool with sandbox-aware working directories and authorization.
type toolShell struct {
	sandboxAbsDir string                // This is the absolute sandbox root used as the default working directory and relative cwd base.
	authorizer    authdomain.Authorizer // This authorizes shell commands before they run.
}

// shellParams contains the JSON arguments for the shell tool.
type shellParams struct {
	Command   []string `json:"command"`    // Command is the argv-style command and arguments to execute. It must contain at least the program name.
	TimeoutMS int64    `json:"timeout_ms"` // TimeoutMS is the optional timeout in milliseconds; a non-positive value uses the default timeout.
	Cwd       string   `json:"cwd"`        // Cwd is the optional working directory, absolute or relative to the sandbox root. Empty Cwd uses the sandbox root.

	// MaxOutputBytes limits combined stdout and stderr returned in the result; non-positive uses the default.
	MaxOutputBytes int `json:"max_output_bytes"`

	// RequestPermission asks for approval to run the command when policy requires it.
	RequestPermission bool `json:"request_permission"`
}

// NewShellTool returns a shell tool that authorizes commands with authorizer and resolves working directories relative to authorizer's sandbox. The authorizer must
// be non-nil.
func NewShellTool(authorizer authdomain.Authorizer) llmstream.Tool {
	abs := authorizer.SandboxDir()
	return &toolShell{
		sandboxAbsDir: abs,
		authorizer:    authorizer,
	}
}

// Name returns the registered shell tool name.
func (t *toolShell) Name() string { return ToolNameShell }

// Presenter returns the semantic presenter for shell tool calls and results.
func (t *toolShell) Presenter() llmstream.Presenter { return shellPresenterInstance }

// Info returns the tool definition for shell, including its embedded description, required argv-style command parameter, and optional timeout, working-directory,
// output-limit, and permission-request parameters.
func (t *toolShell) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{
		Name:        ToolNameShell,
		Description: strings.TrimSpace(descriptionShell),
		Parameters: map[string]any{
			"command": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Command and args (argv style), e.g., ['go','test','./...']",
			},
			"timeout_ms": map[string]any{
				"type":        "integer",
				"description": "Optional timeout in milliseconds (default ~120s)",
			},
			"cwd": map[string]any{
				"type":        "string",
				"description": "Optional working directory (absolute, or relative to sandbox dir; defaults to sandbox dir itself)",
			},
			"max_output_bytes": map[string]any{
				"type":        "integer",
				"description": "Optional max bytes of combined stdout+stderr returned in the result (default 40000; clamped to 1024..1048576)",
			},
			"request_permission": map[string]any{
				"type":        "boolean",
				"description": "Optionally request the user's permission to run this command. Set to true for dangerous commands, and material access outside sandbox dir",
			},
		},
		Required: []string{"command"},
	}
}

// Result is JSON-serialized object: {"success": true|false, "content": content}, where content is a string like:
//
//	Command: go test .
//	Process State: exit status 0    // (os.ProcessState's String())
//	Timeout: false
//	Duration: 240ms
//	Output:
//	ok  	axi/codeai/applypatch	0.232s
//
// Error semantics:
//   - success (JSON field) indicates the shell command exited cleanly (non-error exit code and no timeout). It is the signal consumed by agents.
//   - IsError (ToolResult field) mirrors the success flag so callers that only inspect ToolResult still understand the command failed.
//   - SourceErr (ToolResult field) captures system-level failures in executing the tool (ex: JSON parsing or spawning the process). Normal command failures should
//     leave SourceErr nil.
func (t *toolShell) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	var params shellParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewToolErrorResult(call, fmt.Sprintf("error parsing parameters: %s", err), err)
	}

	if len(params.Command) == 0 {
		return llmstream.NewErrorToolResult("command is required", call)
	}

	timeout := defaultShellTimeout
	if params.TimeoutMS > 0 {
		timeout = time.Duration(params.TimeoutMS) * time.Millisecond
	}

	// Build context with timeout if set
	runCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(runCtx, params.Command[0], params.Command[1:]...)

	dir := t.sandboxAbsDir
	if strings.TrimSpace(params.Cwd) != "" {
		normalized, normalizeErr := t.normalizeCwd(params.Cwd)
		if normalizeErr != nil {
			return NewToolErrorResult(call, normalizeErr.Error(), normalizeErr)
		}
		dir = normalized
	}
	cmd.Dir = dir
	if t.authorizer != nil {
		if authErr := t.authorizer.IsShellAuthorized(params.RequestPermission, "", dir, params.Command); authErr != nil {
			return NewToolErrorResult(call, authErr.Error(), authErr)
		}
	}

	start := time.Now()
	output, err := cmd.CombinedOutput()
	dur := time.Since(start)
	output = limitShellOutputBytes(output, effectiveShellMaxOutputBytes(params.MaxOutputBytes))

	// Capture process state and exit code details
	procState := cmd.ProcessState
	var ee *exec.ExitError
	if procState == nil && errors.As(err, &ee) && ee.ProcessState != nil {
		procState = ee.ProcessState
	}
	processStateStr := "unavailable"
	if procState != nil {
		processStateStr = procState.String()
	}
	timedOut := errors.Is(err, context.DeadlineExceeded) || errors.Is(runCtx.Err(), context.DeadlineExceeded)

	// Build the content string
	var sb strings.Builder
	sb.WriteString("Command: ")
	sb.WriteString(strings.Join(params.Command, " "))
	sb.WriteString("\n")
	sb.WriteString("Process State: ")
	sb.WriteString(processStateStr)
	sb.WriteString("\n")
	sb.WriteString("Timeout: ")
	sb.WriteString(fmt.Sprintf("%t", timedOut))
	sb.WriteString("\n")
	sb.WriteString("Duration: ")
	sb.WriteString(dur.String())
	sb.WriteString("\n")
	sb.WriteString("Output:\n")
	if len(output) > 0 {
		sb.Write(output)
		if output[len(output)-1] != '\n' {
			sb.WriteByte('\n')
		}
	}
	// If there was a non-exit error and no output, include the error for debugging
	if err != nil && len(output) == 0 {
		sb.WriteString(err.Error())
		sb.WriteByte('\n')
	}

	payload := map[string]any{
		"success": err == nil,
		"content": sb.String(),
	}
	b, _ := json.Marshal(payload)

	result := llmstream.ToolResult{
		CallID:  call.CallID,
		Name:    call.Name,
		Type:    call.Type,
		Result:  string(b),
		IsError: err != nil,
	}
	if err != nil {
		result.SourceErr = err
	}

	return result
}

func effectiveShellMaxOutputBytes(value int) int {
	switch {
	case value <= 0:
		return defaultShellMaxOutputBytes
	case value < minShellMaxOutputBytes:
		return minShellMaxOutputBytes
	case value > maxShellMaxOutputBytes:
		return maxShellMaxOutputBytes
	default:
		return value
	}
}

// limitShellOutputBytes returns output unchanged when it is within maxBytes or maxBytes is non-positive. Otherwise, it preserves the head and tail around an elision
// marker and adjusts cut points so UTF-8 encodings are not split. If the marker cannot fit in maxBytes, the marker is returned by itself.
func limitShellOutputBytes(output []byte, maxBytes int) []byte {
	if maxBytes <= 0 || len(output) <= maxBytes {
		return output
	}

	budgetMarker := []byte(fmt.Sprintf("\n[... %d bytes elided ...]\n", len(output)))
	contentBudget := maxBytes - len(budgetMarker)
	if contentBudget <= 0 {
		return budgetMarker
	}

	headBudget := contentBudget / 2
	tailBudget := contentBudget - headBudget
	headEnd := shellUTF8PrefixLen(output, headBudget)
	tailStart := shellUTF8TailStart(output, len(output)-tailBudget)
	if tailStart < headEnd {
		tailStart = headEnd
	}

	elidedBytes := tailStart - headEnd
	marker := []byte(fmt.Sprintf("\n[... %d bytes elided ...]\n", elidedBytes))

	limited := make([]byte, 0, headEnd+len(marker)+len(output)-tailStart)
	limited = append(limited, output[:headEnd]...)
	limited = append(limited, marker...)
	limited = append(limited, output[tailStart:]...)
	return limited
}

func shellUTF8PrefixLen(b []byte, maxBytes int) int {
	if maxBytes <= 0 {
		return 0
	}
	if maxBytes >= len(b) {
		return len(b)
	}
	for maxBytes > 0 && !utf8.RuneStart(b[maxBytes]) {
		maxBytes--
	}
	return maxBytes
}

func shellUTF8TailStart(b []byte, start int) int {
	if start <= 0 {
		return 0
	}
	if start >= len(b) {
		return len(b)
	}
	for start < len(b) && !utf8.RuneStart(b[start]) {
		start++
	}
	return start
}

// The normalizeCwd method returns a cleaned absolute working directory for cwd. It resolves relative paths against the shell tool's sandbox root and cleans absolute
// paths as given. It validates that cwd and the sandbox root are set, but it does not stat or authorize the path.
func (t *toolShell) normalizeCwd(cwd string) (string, error) {
	if strings.TrimSpace(cwd) == "" {
		return "", fmt.Errorf("cwd is required")
	}

	sandboxBase := filepath.Clean(t.sandboxAbsDir)
	if sandboxBase == "" || sandboxBase == "." {
		return "", fmt.Errorf("sandbox directory is not configured")
	}

	var absPath string
	if filepath.IsAbs(cwd) {
		absPath = filepath.Clean(cwd)
	} else {
		absPath = filepath.Join(sandboxBase, cwd)
		absPath = filepath.Clean(absPath)
	}

	return absPath, nil
}

var shellPresenterInstance llmstream.Presenter = shellPresenter{}

// A shellPresenter presents shell tool calls as replacement summaries and includes completed command output as the body.
type shellPresenter struct{}

// Present returns a replacement presentation for a shell tool call. It shows "Running <command>" before a result is available and "Ran <command>" with summarized
// command output after completion.
func (p shellPresenter) Present(call llmstream.ToolCall, result *llmstream.ToolResult) llmstream.Presentation {
	action := "Running"
	if result != nil {
		action = "Ran"
	}

	presentation := llmstream.Presentation{
		Behavior: llmstream.CompletionBehaviorReplace,
		Summary: llmstream.Line{
			JoinWithSpace: true,
			Segments: []llmstream.Segment{
				{Text: action, Role: llmstream.RoleAction},
				{Text: shellPresenterCommand(call), Role: llmstream.RoleNormal},
			},
		},
	}
	if result != nil {
		presentation.Body = shellPresenterBody(*result)
	}

	return presentation
}

func shellPresenterCommand(call llmstream.ToolCall) string {
	var params shellParams
	if err := json.Unmarshal([]byte(call.Input), &params); err == nil {
		if command, ok := joinShellCommand(params.Command); ok {
			return command
		}
	}

	name := strings.TrimSpace(call.Name)
	if name == "" {
		name = ToolNameShell
	}
	return name
}

func joinShellCommand(argv []string) (string, bool) {
	if len(argv) == 0 {
		return "", false
	}
	if strings.TrimSpace(argv[0]) == "" {
		return "", false
	}
	return strings.Join(argv, " "), true
}

func shellPresenterBody(result llmstream.ToolResult) llmstream.Block {
	lines, omittedLineCount := summarizeShellPresenterResult(result)
	if len(lines) == 0 && omittedLineCount == 0 {
		return nil
	}

	return llmstream.Output{
		Lines:            lines,
		OmittedLineCount: omittedLineCount,
	}
}

// summarizeShellPresenterResult returns up to five display lines for a shell tool result and the number of omitted output lines. It accepts either the shell tool's
// JSON result payload or a raw result string; tool errors are returned as a single "Error: ..." line.
func summarizeShellPresenterResult(result llmstream.ToolResult) ([]string, int) {
	trimmed := strings.TrimSpace(result.Result)
	if trimmed == "" {
		return nil, 0
	}

	var payload struct {
		Content string `json:"content"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
		if strings.TrimSpace(payload.Error) != "" {
			return []string{"Error: " + strings.TrimSpace(payload.Error)}, 0
		}
		if payload.Content != "" {
			return summarizeShellPresenterOutput(payload.Content, 5)
		}
	}

	if result.IsError {
		return []string{"Error: " + trimmed}, 0
	}
	return summarizeShellPresenterOutput(trimmed, 5)
}

// The summarizeShellPresenterOutput function returns display lines for shell output and the number of lines omitted. It starts after an "Output:" marker when present,
// trims surrounding empty lines, and limits the result to maxLines when maxLines is positive.
func summarizeShellPresenterOutput(content string, maxLines int) ([]string, int) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")

	start := 0
	for i, line := range lines {
		if strings.TrimSpace(line) == "Output:" {
			start = i + 1
			break
		}
	}

	lines = trimEmptyShellPresenterLines(lines[start:])
	if len(lines) == 0 {
		return nil, 0
	}

	omittedLineCount := 0
	if maxLines > 0 && len(lines) > maxLines {
		omittedLineCount = len(lines) - maxLines
		lines = lines[:maxLines]
	}

	return lines, omittedLineCount
}

func trimEmptyShellPresenterLines(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
