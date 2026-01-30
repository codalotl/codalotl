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

type toolLs struct {
	sandboxAbsDir string
	authorizer    authdomain.Authorizer
}

type ParamsLS struct {
	Path              string `json:"path"`
	RequestPermission bool   `json:"request_permission"`
}

const (
	ToolNameLS = "ls"
)

func NewLsTool(authorizer authdomain.Authorizer) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	return &toolLs{
		sandboxAbsDir: sandboxAbsDir,
		authorizer:    authorizer,
	}
}

func (t *toolLs) Name() string {
	return ToolNameLS
}

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

// RunLs executes the equivalent of `ls -1p` from absPath and returns the command output rendered as <ls> XML.
// We do not run an actual shell command (for portability); instead, we build a cmdrunner.Result directly and
// let cmdrunner's XML rendering handle formatting.
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
