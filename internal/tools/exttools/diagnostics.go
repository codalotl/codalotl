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

//go:embed diagnostics.md
var descriptionDiagnostics string

const ToolNameDiagnostics = "diagnostics"

type toolDiagnostics struct {
	sandboxAbsDir string
	authorizer    authdomain.Authorizer
}

type diagnosticsParams struct {
	Path string `json:"path"`
}

func NewDiagnosticsTool(sandboxAbsDir string, authorizer authdomain.Authorizer) llmstream.Tool {
	return &toolDiagnostics{
		sandboxAbsDir: sandboxAbsDir,
		authorizer:    authorizer,
	}
}

func (t *toolDiagnostics) Name() string {
	return ToolNameDiagnostics
}

func (t *toolDiagnostics) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{
		Name:        ToolNameDiagnostics,
		Description: strings.TrimSpace(descriptionDiagnostics),
		Parameters: map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The path to the file or directory to get diagnostics for (absolute, or relative to sandbox dir)",
			},
		},
		Required: []string{"path"},
	}
}

func (t *toolDiagnostics) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	var params diagnosticsParams
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
		if authErr := t.authorizer.IsAuthorizedForRead(false, "", ToolNameDiagnostics, absPkgPath); authErr != nil {
			return coretools.NewToolErrorResult(call, authErr.Error(), authErr)
		}
	}

	output, err := RunDiagnostics(ctx, t.sandboxAbsDir, absPkgPath)
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

// RunDiagnostics executes `go build -o /dev/null` for pkgDirPath using cmdrunner, returning a diagnostics-status XML block.
// The command runs from the package's manifest directory and targets the package via a relative path. Build failures are
// reflected in the XML but do not surface as Go errors; only execution or templating failures return an error.
func RunDiagnostics(ctx context.Context, sandboxDir string, pkgDirPath string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	runner := newGoDiagnosticsRunner()
	result, err := runner.Run(ctx, sandboxDir, map[string]any{
		"path": pkgDirPath,
	})
	if err != nil {
		return "", err
	}

	return result.ToXML("diagnostics-status"), nil
}

func newGoDiagnosticsRunner() *cmdrunner.Runner {
	const successMessage = "build succeeded"

	inputSchema := map[string]cmdrunner.InputType{
		"path": cmdrunner.InputTypePathDir,
	}
	runner := cmdrunner.NewRunner(inputSchema, []string{"path"})
	runner.AddCommand(cmdrunner.Command{
		Command: "go",
		Args: []string{
			"build",
			"-o",
			"{{ .DevNull }}",
			"{{ if eq .path (manifestDir .path) }}.{{ else }}./{{ relativeTo .path (manifestDir .path) }}{{ end }}",
		},
		CWD:               "{{ manifestDir .path }}",
		MessageIfNoOutput: successMessage,
	})
	return runner
}
