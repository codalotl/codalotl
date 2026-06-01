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

// ToolNameDiagnostics is the registered tool name for the diagnostics tool.
const ToolNameDiagnostics = "diagnostics"

var diagnosticsPresenterInstance llmstream.Presenter = diagnosticsPresenter{}

// toolDiagnostics implements the diagnostics tool for collecting Go package diagnostics.
type toolDiagnostics struct {
	sandboxAbsDir string                // This is the absolute sandbox root used to resolve requested paths.
	authorizer    authdomain.Authorizer // This authorizes diagnostic reads before the tool runs.
}

// diagnosticsParams contains the JSON parameters for the diagnostics tool.
type diagnosticsParams struct {
	Path string `json:"path"` // This is the file or directory path whose package diagnostics should be collected.
}

// NewDiagnosticsTool returns a tool that collects Go package diagnostics. The tool resolves requested paths from authorizer's sandbox and uses authorizer to authorize
// diagnostic reads. authorizer must be non-nil.
func NewDiagnosticsTool(authorizer authdomain.Authorizer) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	return &toolDiagnostics{
		sandboxAbsDir: sandboxAbsDir,
		authorizer:    authorizer,
	}
}

// Name returns ToolNameDiagnostics.
func (t *toolDiagnostics) Name() string {
	return ToolNameDiagnostics
}

// Presenter returns the diagnostics presentation formatter.
func (t *toolDiagnostics) Presenter() llmstream.Presenter {
	return diagnosticsPresenterInstance
}

// diagnosticsPresenter formats diagnostics tool calls and results for display.
type diagnosticsPresenter struct{}

// Present returns the display presentation for a diagnostics tool call or result.
func (p diagnosticsPresenter) Present(call llmstream.ToolCall, result *llmstream.ToolResult) llmstream.Presentation {
	action := "Run Diagnostics"
	if result != nil {
		action = "Ran Diagnostics"
	}

	presentation := extToolSummaryPresentation(action, diagnosticsPresenterTarget(call))
	if result != nil {
		if success, ok := extToolResultSuccess(*result); ok && !success {
			presentation.ErrorBehavior = llmstream.ErrorBehaviorPresenterOwned
		}
	}
	return presentation
}

// Info returns the diagnostics tool metadata and parameter schema.
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

// Run executes diagnostics for the requested package path and returns the diagnostic output.
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

// RunDiagnostics executes `go build -o /dev/null` for pkgDirPath using cmdrunner, returning a diagnostics-status XML block. The command runs from the package's
// manifest directory and targets the package via a relative path. Build failures are reflected in the XML but do not surface as Go errors; only execution or templating
// failures return an error.
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

// newGoDiagnosticsRunner returns a command runner configured to collect Go build diagnostics.
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

func diagnosticsPresenterTarget(call llmstream.ToolCall) string {
	var params diagnosticsParams
	if err := json.Unmarshal([]byte(call.Input), &params); err == nil {
		if path := strings.TrimSpace(params.Path); path != "" {
			return path
		}
	}
	if name := strings.TrimSpace(call.Name); name != "" {
		return name
	}
	return ToolNameDiagnostics
}
