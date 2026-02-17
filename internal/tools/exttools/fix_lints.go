package exttools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/llmstream"
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
	lintSteps     []lints.Step
}

type fixLintsParams struct {
	Path string `json:"path"`
}

func NewFixLintsTool(authorizer authdomain.Authorizer, lintSteps []lints.Step) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	return &toolFixLints{
		sandboxAbsDir: sandboxAbsDir,
		authorizer:    authorizer,
		lintSteps:     lintSteps,
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

	output, err := FixLints(ctx, t.sandboxAbsDir, absPkgPath, t.lintSteps)
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

// FixLints runs the configured lint pipeline against targetPath (file or directory), returning a lint-status XML block.
func FixLints(ctx context.Context, sandboxDir string, targetPath string, steps []lints.Step) (string, error) {
	return runLints(ctx, sandboxDir, targetPath, steps, lints.SituationFix)
}

// CheckLints runs the configured lint pipeline in check mode against targetPath (file or directory), returning a lint-status XML block.
func CheckLints(ctx context.Context, sandboxDir string, targetPath string, steps []lints.Step) (string, error) {
	return runLints(ctx, sandboxDir, targetPath, steps, lints.SituationTests)
}

func runLints(ctx context.Context, sandboxDir string, targetPath string, steps []lints.Step, situation lints.Situation) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if steps == nil {
		steps = lints.DefaultSteps()
	}

	return lints.Run(ctx, sandboxDir, targetPath, steps, situation)
}
