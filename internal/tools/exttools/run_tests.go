package exttools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/q/cmdrunner"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"strings"
)

//go:embed run_tests.md
var descriptionRunTests string

const ToolNameRunTests = "run_tests"

type toolRunTests struct {
	sandboxAbsDir string
	authorizer    authdomain.Authorizer
	lintSteps     []lints.Step
}

type runTestsParams struct {
	Path     string `json:"path"`
	TestName string `json:"test_name"`
	Verbose  bool   `json:"verbose"`
}

func NewRunTestsTool(authorizer authdomain.Authorizer, lintSteps []lints.Step) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	return &toolRunTests{
		sandboxAbsDir: sandboxAbsDir,
		authorizer:    authorizer,
		lintSteps:     lintSteps,
	}
}

func (t *toolRunTests) Name() string {
	return ToolNameRunTests
}

func (t *toolRunTests) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{
		Name:        ToolNameRunTests,
		Description: strings.TrimSpace(descriptionRunTests),
		Parameters: map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Filesystem path to the Go package to test (absolute, or relative to the sandbox directory)",
			},
			"test_name": map[string]any{
				"type":        "string",
				"description": "Optional test name to pass via go test -run",
			},
			"verbose": map[string]any{
				"type":        "boolean",
				"description": "Optional flag to run go test with -v",
			},
		},
		Required: []string{"path"},
	}
}

func (t *toolRunTests) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	var params runTestsParams
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
		if authErr := t.authorizer.IsAuthorizedForRead(false, "", ToolNameRunTests, absPkgPath); authErr != nil {
			return coretools.NewToolErrorResult(call, authErr.Error(), authErr)
		}
	}

	output, err := RunTests(ctx, t.sandboxAbsDir, absPkgPath, params.TestName, params.Verbose)
	if err != nil {
		return coretools.NewToolErrorResult(call, fmt.Sprintf("failed to run go test: %v", err), err)
	}
	lintOutput, err := runLints(ctx, t.sandboxAbsDir, absPkgPath, t.lintSteps, lints.SituationTests)
	if err != nil {
		return coretools.NewToolErrorResult(call, fmt.Sprintf("failed to run lints: %v", err), err)
	}
	if strings.HasSuffix(output, "\n") {
		output += lintOutput
	} else {
		output += "\n" + lintOutput
	}

	return llmstream.ToolResult{
		CallID: call.CallID,
		Name:   call.Name,
		Type:   call.Type,
		Result: output,
	}
}

// RunTests returns the output of the `go test` command, optionally verbose, optionally matched with namePattern. ctx controls command cancellation; if nil, context.Background
// is used. The result is wrapped in a <test-status> XML tag:
//
//	<test-status ok="true">
//	$ go test -run TestMyTest ./codeai/tools
//	ok
//	</test-status>
//
// An error is only returned if the inputs are invalid (ex: pkgDirPath can't be found).
func RunTests(ctx context.Context, sandboxDir string, pkgDirPath string, namePattern string, verbose bool) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	runner := newGoTestRunner()
	result, err := runner.Run(ctx, sandboxDir, map[string]any{
		"path":        pkgDirPath,
		"namePattern": namePattern,
		"verbose":     verbose,
		"Lang":        "go",
	})
	if err != nil {
		return "", err
	}

	return result.ToXML("test-status"), nil
}

func newGoTestRunner() *cmdrunner.Runner {
	inputSchema := map[string]cmdrunner.InputType{
		"path":        cmdrunner.InputTypePathDir,
		"namePattern": cmdrunner.InputTypeString,
		"verbose":     cmdrunner.InputTypeBool,
		"Lang":        cmdrunner.InputTypeString,
	}
	runner := cmdrunner.NewRunner(inputSchema, []string{"path"})
	runner.AddCommand(cmdrunner.Command{
		Command: "go",
		Args: []string{
			"test",
			"{{ if .verbose }}-v{{ end }}",
			"{{ if ne .namePattern \"\" }}-run{{ end }}",
			"{{ if ne .namePattern \"\" }}{{ .namePattern }}{{ end }}",
			"{{ if eq .path (manifestDir .path) }}.{{ else }}./{{ relativeTo .path (manifestDir .path) }}{{ end }}",
		},
		CWD: "{{ manifestDir .path }}",
	})
	return runner
}
