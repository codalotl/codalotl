package exttools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/q/cmdrunner"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
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
	Env      string `json:"env"`
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
			"env": map[string]any{
				"type":        "string",
				"description": "Optional env vars for go test (ex: `MYVAR=1 OTHERVAR=2`)",
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

	output, err := RunTests(ctx, t.sandboxAbsDir, absPkgPath, params.TestName, params.Verbose, params.Env)
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

// RunTests returns the output of the `go test` command, optionally verbose, optionally matched with namePattern, and optionally with env var assignments in env.
// ctx controls command cancellation; if nil, context.Background is used. The result is wrapped in a <test-status> XML tag:
//
//	<test-status ok="true">
//	$ go test -run TestMyTest ./codeai/tools
//	ok
//	</test-status>
//
// An error is only returned if the inputs are invalid (ex: pkgDirPath can't be found).
func RunTests(ctx context.Context, sandboxDir string, pkgDirPath string, namePattern string, verbose bool, env string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	envAssignments, err := parseEnvAssignments(env)
	if err != nil {
		return "", err
	}

	runner := newGoTestRunner(envAssignments)
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

func newGoTestRunner(envAssignments []string) *cmdrunner.Runner {
	inputSchema := map[string]cmdrunner.InputType{
		"path":        cmdrunner.InputTypePathDir,
		"namePattern": cmdrunner.InputTypeString,
		"verbose":     cmdrunner.InputTypeBool,
		"Lang":        cmdrunner.InputTypeString,
	}
	runner := cmdrunner.NewRunner(inputSchema, []string{"path"})
	testArgs := []string{
		"{{ if .verbose }}-v{{ end }}",
		"{{ if ne .namePattern \"\" }}-run{{ end }}",
		"{{ if ne .namePattern \"\" }}{{ .namePattern }}{{ end }}",
		"{{ if eq .path (manifestDir .path) }}.{{ else }}./{{ relativeTo .path (manifestDir .path) }}{{ end }}",
	}
	runner.AddCommand(cmdrunner.Command{
		Command: "go",
		Args: append([]string{
			"test",
		}, testArgs...),
		CWD: "{{ manifestDir .path }}",
		Env: append([]string(nil), envAssignments...),
	})
	return runner
}
func parseEnvAssignments(env string) ([]string, error) {
	if env == "" {
		return nil, nil
	}
	assignments := strings.Fields(env)
	for _, assignment := range assignments {
		key, _, found := strings.Cut(assignment, "=")
		if !found || key == "" {
			return nil, fmt.Errorf("invalid env assignment %q: expected KEY=VALUE", assignment)
		}
		if !isValidEnvKey(key) {
			return nil, fmt.Errorf("invalid env key %q", key)
		}
	}
	return assignments, nil
}
func isValidEnvKey(key string) bool {
	for i := 0; i < len(key); i++ {
		ch := key[i]
		if i == 0 {
			if ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') {
				continue
			}
			return false
		}
		if ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') {
			continue
		}
		return false
	}
	return true
}
