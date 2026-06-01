package pkgtools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodecontext"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
)

//go:embed get_usage.md
var descriptionGetUsage string

// ToolNameGetUsage is the registered name of the get_usage tool.
const ToolNameGetUsage = "get_usage"

// The toolGetUsage type implements the get_usage tool by reporting uses of an identifier defined by a package.
type toolGetUsage struct {
	sandboxAbsDir string                // The sandbox root is used to resolve sandbox-relative package paths.
	authorizer    authdomain.Authorizer // The authorizer controls reads of sandbox packages.
}

// getUsageParams contains the JSON parameters for a get_usage call. All fields are required.
type getUsageParams struct {
	// DefiningPackagePath identifies the package that defines Identifier, as a sandbox-relative directory or Go import path.
	DefiningPackagePath string `json:"defining_package_path"`

	// Identifier names the symbol whose references should be summarized.
	Identifier string `json:"identifier"`
}

var getUsagePresenterInstance llmstream.Presenter = getUsagePresenter{}

// The getUsagePresenter type formats get_usage tool summaries and result counts.
type getUsagePresenter struct{}

// NewGetUsageTool returns a get_usage tool that resolves package paths from the authorizer sandbox and authorizes package reads with authorizer.
//
// The authorizer must be non-nil.
func NewGetUsageTool(authorizer authdomain.Authorizer) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	return &toolGetUsage{
		sandboxAbsDir: sandboxAbsDir,
		authorizer:    authorizer,
	}
}

// Name returns ToolNameGetUsage.
func (t *toolGetUsage) Name() string {
	return ToolNameGetUsage
}

// Presenter returns the presenter that formats get_usage calls and usage results.
func (t *toolGetUsage) Presenter() llmstream.Presenter {
	return getUsagePresenterInstance
}

// Present returns the get_usage presentation for call and result. It summarizes the requested defining package and identifier, and when a completed result can be
// counted, adds a body with the number of usage results found.
func (p getUsagePresenter) Present(call llmstream.ToolCall, result *llmstream.ToolResult) llmstream.Presentation {
	presentation := pkgToolReplaceSummaryPresentation(getUsagePresenterSummary(call))
	if result == nil {
		return presentation
	}

	count, ok := pkgToolUsageResultCount(*result)
	if !ok {
		return presentation
	}

	presentation.Body = pkgToolUsageSummaryLine(count)
	return presentation
}

// getUsagePresenterSummary builds the space-joined summary line for a get_usage call. It renders "Read Usage" followed by the requested defining package and identifier;
// when the package is unavailable, it uses the tool name as the target.
func getUsagePresenterSummary(call llmstream.ToolCall) llmstream.Line {
	pkg, identifier, ok := getUsagePresenterParams(call)
	target := pkg
	if !ok || target == "" {
		target = strings.TrimSpace(call.Name)
	}

	segments := []llmstream.Segment{
		{Text: "Read Usage", Role: llmstream.RoleAction},
	}
	if target != "" {
		segments = append(segments, llmstream.Segment{Text: target, Role: llmstream.RoleNormal})
	}
	if identifier != "" {
		segments = append(segments, llmstream.Segment{Text: identifier, Role: llmstream.RoleNormal})
	}
	return llmstream.Line{
		JoinWithSpace: true,
		Segments:      segments,
	}
}

func getUsagePresenterParams(call llmstream.ToolCall) (string, string, bool) {
	var params getUsageParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return "", "", false
	}

	pkg := strings.TrimSpace(params.DefiningPackagePath)
	identifier := strings.TrimSpace(params.Identifier)
	return pkg, identifier, pkg != "" || identifier != ""
}

// Info returns the LLM-facing metadata for the get_usage tool, including the required defining package path and identifier parameters.
func (t *toolGetUsage) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{
		Name:        ToolNameGetUsage,
		Description: strings.TrimSpace(descriptionGetUsage),
		Parameters: map[string]any{
			"defining_package_path": map[string]any{
				"type":        "string",
				"description": "A Go package directory (relative to the sandbox) or a Go import path. Must resolve to the package defining the identifier.",
			},
			"identifier": map[string]any{
				"type":        "string",
				"description": "The identifier defined in defining_package_path.",
			},
		},
		Required: []string{"defining_package_path", "identifier"},
	}
}

// Run executes the get_usage tool call and returns an LLM-readable summary of references to a package-defined identifier. The call input must be JSON containing
// defining_package_path and identifier. The package path may be sandbox-relative or an import path, and parenthesized method identifiers are normalized before lookup.
// Run returns an error tool result for invalid input, package resolution or authorization failures, or usage lookup errors.
func (t *toolGetUsage) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	_ = ctx

	var params getUsageParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return coretools.NewToolErrorResult(call, fmt.Sprintf("error parsing parameters: %s", err), err)
	}

	if params.DefiningPackagePath == "" {
		return llmstream.NewErrorToolResult("defining_package_path is required", call)
	}

	if params.Identifier == "" {
		return llmstream.NewErrorToolResult("identifier is required", call)
	}

	identifier := gocode.DeparenthesizeIdentifier(params.Identifier)

	mod, err := gocode.NewModule(t.sandboxAbsDir)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	_, packageAbsDir, _, _, err := resolvePackagePath(mod, params.DefiningPackagePath)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	if t.authorizer != nil && isWithinDir(t.sandboxAbsDir, packageAbsDir) {
		// Only prompt/deny for sandbox reads; resolved dependency/stdlib packages are always readable.
		if authErr := t.authorizer.IsAuthorizedForRead(false, "", ToolNameGetUsage, packageAbsDir); authErr != nil {
			return coretools.NewToolErrorResult(call, authErr.Error(), authErr)
		}
	}

	_, usageSummary, err := gocodecontext.IdentifierUsage(packageAbsDir, identifier, true)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	return llmstream.ToolResult{
		CallID: call.CallID,
		Name:   call.Name,
		Type:   call.Type,
		Result: usageSummary,
	}
}
