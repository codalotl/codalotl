package exttools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
)

//go:embed fix_lints.md
var descriptionFixLints string

// ToolNameFixLints is the registered tool name for the lint-fixing tool.
const ToolNameFixLints = "fix_lints"

var fixLintsPresenterInstance llmstream.Presenter = fixLintsPresenter{}

// toolFixLints implements the lint-fixing tool for a package path.
type toolFixLints struct {
	sandboxAbsDir string                // This is the absolute sandbox root used to resolve requested paths.
	authorizer    authdomain.Authorizer // This authorizes writes before lint fixes run.
	lintSteps     []lints.Step          // This is the configured lint pipeline used in fix mode.
}

// fixLintsParams contains the JSON parameters for the lint-fixing tool.
type fixLintsParams struct {
	Path string `json:"path"` // This is the file or directory path whose package lints should be fixed.
}

// NewFixLintsTool returns a tool that fixes lint issues for a package path. The tool resolves requested paths from authorizer's sandbox, uses authorizer to authorize
// writes, and runs the provided lint steps in fix mode. authorizer must be non-nil.
func NewFixLintsTool(authorizer authdomain.Authorizer, lintSteps []lints.Step) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	return &toolFixLints{
		sandboxAbsDir: sandboxAbsDir,
		authorizer:    authorizer,
		lintSteps:     lintSteps,
	}
}

// Name returns ToolNameFixLints.
func (t *toolFixLints) Name() string {
	return ToolNameFixLints
}

// Presenter returns the lint-fixing presentation formatter.
func (t *toolFixLints) Presenter() llmstream.Presenter {
	return fixLintsPresenterInstance
}

// fixLintsPresenter formats lint-fixing tool calls and results for display.
type fixLintsPresenter struct{}

// Present returns the display presentation for a lint-fixing tool call or result.
func (p fixLintsPresenter) Present(call llmstream.ToolCall, result *llmstream.ToolResult) llmstream.Presentation {
	action := "Fix Lints"
	if result != nil {
		action = "Fixed Lints"
	}

	presentation := extToolSummaryPresentation(action, fixLintsPresenterTarget(call))
	if result == nil {
		return presentation
	}

	content, ok := extToolResultContent(*result)
	if !ok {
		return presentation
	}

	content = stripOuterXMLTag(strings.TrimSpace(content))
	content = stripFixLintsCommandWrappers(content)
	if output, ok := summarizePresenterOutput(content, 5); ok {
		presentation.Body = output
	}

	return presentation
}

// Info returns the lint-fixing tool metadata and parameter schema.
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

// Run fixes lints for the requested package path and returns the lint output.
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

	output, err := runLints(ctx, t.sandboxAbsDir, absPkgPath, t.lintSteps, lints.SituationFix)
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

func runLints(ctx context.Context, sandboxDir string, targetPath string, steps []lints.Step, situation lints.Situation) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if steps == nil {
		steps = lints.DefaultSteps()
	}

	return lints.Run(ctx, sandboxDir, targetPath, steps, situation)
}

func fixLintsPresenterTarget(call llmstream.ToolCall) string {
	var params fixLintsParams
	if err := json.Unmarshal([]byte(call.Input), &params); err == nil {
		if path := strings.TrimSpace(params.Path); path != "" {
			return path
		}
	}
	if name := strings.TrimSpace(call.Name); name != "" {
		return name
	}
	return ToolNameFixLints
}

// stripFixLintsCommandWrappers returns content with fix-lint wrapper tag lines removed. It removes command and lint-status opening and closing tag lines, normalizes
// CRLF line endings to LF, and preserves other lines. Whitespace-only input returns an empty string.
func stripFixLintsCommandWrappers(content string) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}

	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "<command") || strings.HasPrefix(trimmed, "</command") {
			continue
		}
		if strings.HasPrefix(trimmed, "<lint-status") || strings.HasPrefix(trimmed, "</lint-status") {
			continue
		}
		out = append(out, line)
	}

	return strings.Join(out, "\n")
}
