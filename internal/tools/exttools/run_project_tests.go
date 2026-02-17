package exttools

import (
	"context"
	_ "embed"
	"fmt"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/q/cmdrunner"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"strings"
)

//go:embed run_project_tests.md
var descriptionRunProjectTests string

const ToolNameRunProjectTests = "run_project_tests"

type toolRunProjectTests struct {
	sandboxAbsDir string
	pkgDirAbsPath string
	authorizer    authdomain.Authorizer
}

// type runProjectTestsParams struct{}

// NewRunProjectTestsTool runs `go test ./...`. if pkgDirAbsPath is set, we run the project tests with respect to that dir's containing Go module. Otherwise, run
// the command in sandboxAbsDir hope for the best.
func NewRunProjectTestsTool(pkgDirAbsPath string, authorizer authdomain.Authorizer) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	return &toolRunProjectTests{
		sandboxAbsDir: sandboxAbsDir,
		pkgDirAbsPath: pkgDirAbsPath,
		authorizer:    authorizer,
	}
}

func (t *toolRunProjectTests) Name() string {
	return ToolNameRunProjectTests
}

func (t *toolRunProjectTests) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{
		Name:        ToolNameRunProjectTests,
		Description: strings.TrimSpace(descriptionRunProjectTests),
		Parameters:  map[string]any{},
	}
}

func (t *toolRunProjectTests) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	basePath := t.pkgDirAbsPath

	absPath, _, normErr := coretools.NormalizePath(basePath, t.sandboxAbsDir, coretools.WantPathTypeDir, true)
	if normErr != nil {
		return coretools.NewToolErrorResult(call, normErr.Error(), normErr)
	}
	runner := newGoProjectTestRunner()
	result, err := runner.Run(ctx, t.sandboxAbsDir, map[string]any{
		"path": absPath,
		"Lang": "go",
	})
	if err != nil {
		return coretools.NewToolErrorResult(call, fmt.Sprintf("failed to run go test ./...: %v", err), err)
	}

	return llmstream.ToolResult{
		CallID: call.CallID,
		Name:   call.Name,
		Type:   call.Type,
		Result: result.ToXML("test-status"),
	}
}

func newGoProjectTestRunner() *cmdrunner.Runner {
	inputSchema := map[string]cmdrunner.InputType{
		"path": cmdrunner.InputTypePathDir,
		"Lang": cmdrunner.InputTypeString,
	}
	r := cmdrunner.NewRunner(inputSchema, []string{"path"})
	r.AddCommand(cmdrunner.Command{
		Command: "go",
		Args: []string{
			"test",
			"./...",
		},
		CWD: "{{ manifestDir .path }}",
	})
	return r
}
