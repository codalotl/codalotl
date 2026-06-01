package coretools

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
)

// ToolNameSkillShell is the registered tool name for the skill_shell tool.
const ToolNameSkillShell = "skill_shell"

//go:embed skill_shell.md
var descriptionSkillShell string

// The toolSkillShell type implements the skill_shell tool for commands requested by a named skill.
type toolSkillShell struct {
	sandboxAbsDir string                // This is the absolute sandbox root used as the default working directory and relative cwd base.
	authorizer    authdomain.Authorizer // This authorizes shell commands before they run.
}

// The skillShellParams type is the JSON parameter set for the skill_shell tool.
type skillShellParams struct {
	Command           []string `json:"command"`            // Command is the argv vector to execute; the first element is the executable.
	Skill             string   `json:"skill"`              // Skill is the name of the skill requesting the command.
	TimeoutMS         int64    `json:"timeout_ms"`         // TimeoutMS is the optional command timeout in milliseconds; values <= 0 use the default timeout.
	Cwd               string   `json:"cwd"`                // Cwd is the optional working directory; an empty value uses the sandbox root.
	MaxOutputBytes    int      `json:"max_output_bytes"`   // MaxOutputBytes is the optional output byte limit; zero uses the default limit.
	RequestPermission bool     `json:"request_permission"` // RequestPermission asks the authorizer to allow or prompt for a command that may otherwise be denied.
}

// NewSkillShellTool returns a skill_shell tool that authorizes commands with authorizer and resolves working directories relative to authorizer's sandbox. The authorizer
// must be non-nil.
func NewSkillShellTool(authorizer authdomain.Authorizer) llmstream.Tool {
	abs := authorizer.SandboxDir()
	return &toolSkillShell{
		sandboxAbsDir: abs,
		authorizer:    authorizer,
	}
}

// Name returns the registered skill_shell tool name.
func (t *toolSkillShell) Name() string { return ToolNameSkillShell }

// Presenter returns the shell presenter used for skill_shell tool calls and results.
func (t *toolSkillShell) Presenter() llmstream.Presenter { return shellPresenterInstance }

// Info returns the tool definition for skill_shell, including its embedded description, required argv-style command and skill parameters, and optional timeout,
// working-directory, output-limit, and permission-request parameters.
func (t *toolSkillShell) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{
		Name:        ToolNameSkillShell,
		Description: strings.TrimSpace(descriptionSkillShell),
		Parameters: map[string]any{
			"command": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Command and args (argv style), e.g., ['go','test','./...']",
			},
			"skill": map[string]any{
				"type":        "string",
				"description": "Required name of Skill that indicated this shell command",
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
		Required: []string{"command", "skill"},
	}
}

// Run duplicates the semantics of toolShell.Run for now, so it can be independently evolved.
func (t *toolSkillShell) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	var params skillShellParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewToolErrorResult(call, fmt.Sprintf("error parsing parameters: %s", err), err)
	}

	if len(params.Command) == 0 {
		return llmstream.NewErrorToolResult("command is required", call)
	}

	if params.Skill == "" {
		return llmstream.NewErrorToolResult("skill is required", call)
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

// The normalizeCwd method returns a cleaned absolute working directory for cwd. It resolves relative paths against the skill shell tool's sandbox root and cleans
// absolute paths as given. It validates that cwd and the sandbox root are set, but it does not stat or authorize the path.
func (t *toolSkillShell) normalizeCwd(cwd string) (string, error) {
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
