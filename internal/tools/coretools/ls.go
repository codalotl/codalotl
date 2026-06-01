package coretools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/q/cmdrunner"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"os"
	"sort"
	"strings"
)

//go:embed ls.md
var descriptionLs string

// The toolLs type implements the ls tool with sandbox-relative path resolution and read authorization.
type toolLs struct {
	sandboxAbsDir string                // This is the absolute sandbox root used to resolve directory paths.
	authorizer    authdomain.Authorizer // This authorizes directory reads before they run.
}

// ParamsLS contains the JSON arguments for the ls tool.
type ParamsLS struct {
	// Path is the directory to list, or a file whose containing directory should be listed. Relative paths are resolved from the sandbox root.
	Path string `json:"path"`

	// RequestPermission asks for approval to read the directory when policy requires it.
	RequestPermission bool `json:"request_permission"`
}

const (
	ToolNameLS = "ls" // ToolNameLS is the registered name of the directory listing tool.
)

// NewLsTool returns an ls tool that lists authorized directories resolved relative to authorizer's sandbox. The authorizer must be non-nil.
func NewLsTool(authorizer authdomain.Authorizer) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	return &toolLs{
		sandboxAbsDir: sandboxAbsDir,
		authorizer:    authorizer,
	}
}

// Name returns the registered directory listing tool name.
func (t *toolLs) Name() string {
	return ToolNameLS
}

// Presenter returns the semantic presenter for ls tool calls and results.
func (t *toolLs) Presenter() llmstream.Presenter {
	return lsPresenterInstance
}

// Info returns the tool metadata for ls, including its embedded description and JSON parameters. The returned schema requires path and accepts request_permission
// for authorization escalation.
func (t *toolLs) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{
		Name:        ToolNameLS,
		Description: strings.TrimSpace(descriptionLs),
		Parameters: map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The path to the directory to list (absolute, or relative to sandbox dir)",
			},
			"request_permission": map[string]any{
				"type":        "boolean",
				"description": "Optionally request the user's permission to run this command. Set to true for material access outside sandbox dir",
			},
		},
		Required: []string{"path"},
	}
}

// Run executes an ls tool call. It decodes the JSON parameters, requires a non-empty path, normalizes it as an existing directory, authorizes the read, and returns
// the RunLs output. Parameter, authorization, normalization, and listing failures are returned as error tool results.
func (t *toolLs) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	var params ParamsLS

	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewToolErrorResult(call, fmt.Sprintf("error parsing parameters: %s", err), err)
	}

	// Validate and normalize the path into an absolute path within sandboxAbsDir
	if strings.TrimSpace(params.Path) == "" {
		return llmstream.NewErrorToolResult("path is required", call)
	}

	absResolved, _, normErr := NormalizePath(params.Path, t.sandboxAbsDir, WantPathTypeDir, true)
	if normErr != nil {
		return NewToolErrorResult(call, normErr.Error(), normErr)
	}

	if t.authorizer != nil {
		if authErr := t.authorizer.IsAuthorizedForRead(params.RequestPermission, "", ToolNameLS, absResolved); authErr != nil {
			return NewToolErrorResult(call, authErr.Error(), authErr)
		}
	}

	output, err := RunLs(ctx, absResolved)
	if err != nil {
		return NewToolErrorResult(call, err.Error(), err)
	}

	return llmstream.ToolResult{
		CallID: call.CallID,
		Name:   call.Name,
		Type:   call.Type,
		Result: output,
	}
}

// RunLs executes the equivalent of `ls -1p` from absPath and returns the command output rendered as <ls> XML. We do not run an actual shell command (for portability);
// instead, we build a cmdrunner.Result directly and let cmdrunner's XML rendering handle formatting.
func RunLs(ctx context.Context, absPath string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("ls: stat path: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("ls: path is not a directory: %s", absPath)
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return "", fmt.Errorf("ls: read dir: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	items := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}

		if entry.IsDir() {
			name += "/"
		}
		items = append(items, name)
	}

	sort.Strings(items)

	// Construct a synthetic cmdrunner result for consistent XML formatting.
	out := strings.Join(items, "\n")
	if len(out) > 0 {
		out += "\n"
	}
	res := cmdrunner.Result{
		Results: []cmdrunner.CommandResult{
			{
				Command:    "ls",
				Args:       []string{"-1p"},
				CWD:        absPath,
				Output:     out,
				ExecStatus: cmdrunner.ExecStatusCompleted,
				Outcome:    cmdrunner.OutcomeSuccess,
				ExitCode:   0,
				// Include cwd in the rendered <ls> tag so callers know where this listing
				// was executed from.
				ShowCWD:           true,
				Signal:            "",
				MessageIfNoOutput: "",
			},
		},
	}
	return res.ToXML("ls"), nil
}

var lsPresenterInstance llmstream.Presenter = lsPresenter{}

// An lsPresenter presents ls tool calls as replacement summaries in the form "List <path>".
type lsPresenter struct{}

// Present returns a replacement presentation with the summary "List <path>" for an ls tool call. It uses the requested path when available and falls back to the
// call or tool name; result is ignored.
func (p lsPresenter) Present(call llmstream.ToolCall, result *llmstream.ToolResult) llmstream.Presentation {
	_ = result

	return llmstream.Presentation{
		Behavior: llmstream.CompletionBehaviorReplace,
		Summary: llmstream.Line{
			JoinWithSpace: true,
			Segments: []llmstream.Segment{
				{Text: "List", Role: llmstream.RoleAction},
				{Text: lsPresenterTarget(call), Role: llmstream.RoleNormal},
			},
		},
	}
}

func lsPresenterTarget(call llmstream.ToolCall) string {
	var params ParamsLS
	if err := json.Unmarshal([]byte(call.Input), &params); err == nil {
		if path := strings.TrimSpace(params.Path); path != "" {
			return path
		}
	}

	name := strings.TrimSpace(call.Name)
	if name == "" {
		name = ToolNameLS
	}
	return name
}
