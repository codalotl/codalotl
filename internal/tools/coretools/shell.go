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
)

// Requirements:
//   - accept shell commands via a function_call.
//   - inputs: command (array of strings, argv style); timeout_ms; cwd (abs path)
//   - output: {"success": true|false, "content": content}, where content is like: "Command: go test .\nCode: 0\nDuration: 19ms\nOutput:\n<the output>"
//   - no security (allowlist, blacklist, etc)

const (
	ToolNameShell       = "shell"
	defaultShellTimeout = 120 * time.Second
)

//go:embed shell.md
var descriptionShell string

type toolShell struct {
	sandboxAbsDir string
	authorizer    authdomain.Authorizer
}

type shellParams struct {
	Command           []string `json:"command"`
	TimeoutMS         int64    `json:"timeout_ms"`
	Cwd               string   `json:"cwd"`
	RequestPermission bool     `json:"request_permission"`
}

func NewShellTool(authorizer authdomain.Authorizer) llmstream.Tool {
	abs := authorizer.SandboxDir()
	return &toolShell{
		sandboxAbsDir: abs,
		authorizer:    authorizer,
	}
}

func (t *toolShell) Name() string { return ToolNameShell }

func (t *toolShell) Presenter() llmstream.Presenter { return shellPresenterInstance }

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

type shellPresenter struct{}

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

func shellPresenterBody(result llmstream.ToolResult) []llmstream.Block {
	lines, omittedLineCount := summarizeShellPresenterResult(result)
	if len(lines) == 0 && omittedLineCount == 0 {
		return nil
	}

	return []llmstream.Block{
		llmstream.Output{
			Kind:             llmstream.OutputKindCommand,
			Lines:            lines,
			OmittedLineCount: omittedLineCount,
		},
	}
}

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
