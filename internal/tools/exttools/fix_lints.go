package exttools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/q/cmdrunner"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"strings"
)

//go:embed fix_lints.md
var descriptionFixLints string

const ToolNameFixLints = "fix_lints"

type toolFixLints struct {
	sandboxAbsDir string
	authorizer    authdomain.Authorizer
}

type fixLintsParams struct {
	Path string `json:"path"`
}

func NewFixLintsTool(authorizer authdomain.Authorizer) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	return &toolFixLints{
		sandboxAbsDir: sandboxAbsDir,
		authorizer:    authorizer,
	}
}

func (t *toolFixLints) Name() string {
	return ToolNameFixLints
}

func (t *toolFixLints) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{
		Name:        ToolNameFixLints,
		Description: strings.TrimSpace(descriptionFixLints),
		Parameters: map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The path to the file or directory to fix lints for (absolute, or relative to sandbox dir)",
			},
		},
		Required: []string{"path"},
	}
}

func (t *toolFixLints) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	_ = ctx

	var params fixLintsParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return coretools.NewToolErrorResult(call, fmt.Sprintf("error parsing parameters: %s", err), err)
	}

	if params.Path == "" {
		return llmstream.NewErrorToolResult("path is required", call)
	}

	absPkgPath, _, normErr := coretools.NormalizePath(params.Path, t.sandboxAbsDir, coretools.WantPathTypeDir, true)
	if normErr != nil {
		return coretools.NewToolErrorResult(call, normErr.Error(), normErr)
	}

	if t.authorizer != nil {
		if authErr := t.authorizer.IsAuthorizedForWrite(false, "", ToolNameFixLints, absPkgPath); authErr != nil {
			return coretools.NewToolErrorResult(call, authErr.Error(), authErr)
		}
	}

	output, err := FixLints(ctx, t.sandboxAbsDir, absPkgPath)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	return llmstream.ToolResult{
		CallID: call.CallID,
		Name:   call.Name,
		Type:   call.Type,
		Result: output,
	}
}

// FixLints runs gofmt with -l -w against targetPath (file or directory), returning a lint-status XML block.
// The command output is included; if gofmt makes no changes, a helpful message is returned instead.
func FixLints(ctx context.Context, sandboxDir string, targetPath string) (string, error) {
	return runGoFmt(ctx, sandboxDir, targetPath, true)
}

// CheckLints runs gofmt -l against targetPath (file or directory), returning a lint-status XML block.
// The command output is included; if no formatting issues are found, a helpful message is returned instead.
func CheckLints(ctx context.Context, sandboxDir string, targetPath string) (string, error) {
	return runGoFmt(ctx, sandboxDir, targetPath, false)
}

func runGoFmt(ctx context.Context, sandboxDir string, targetPath string, write bool) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	runner := newGoFmtRunner(write)
	result, err := runner.Run(ctx, sandboxDir, map[string]any{
		"path": targetPath,
	})
	if err != nil {
		return "", err
	}

	return result.ToXML("lint-status"), nil
}

func newGoFmtRunner(write bool) *cmdrunner.Runner {
	inputSchema := map[string]cmdrunner.InputType{
		"path": cmdrunner.InputTypePathAny,
	}

	runner := cmdrunner.NewRunner(inputSchema, []string{"path"})

	args := []string{"-l"}
	if write {
		args = append(args, "-w")
	}
	args = append(args, "{{ relativeTo .path (manifestDir .path) }}")

	runner.AddCommand(cmdrunner.Command{
		Command:                "gofmt",
		Args:                   args,
		OutcomeFailIfAnyOutput: !write,
		MessageIfNoOutput:      "no issues found",
		CWD:                    "{{ manifestDir .path }}",
	})

	return runner
}
