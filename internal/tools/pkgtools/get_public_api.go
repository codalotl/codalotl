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

//go:embed get_public_api.md
var descriptionGetPublicAPI string

// ToolNameGetPublicAPI is the registered name of the get_public_api tool.
const ToolNameGetPublicAPI = "get_public_api"

// The toolGetPublicAPI type implements the get_public_api tool by returning public API documentation for a resolved package and optional identifiers.
type toolGetPublicAPI struct {
	sandboxAbsDir string                // The sandbox root is used to resolve sandbox-relative package paths.
	authorizer    authdomain.Authorizer // The authorizer controls reads of sandbox packages.
}

// The getPublicAPIParams type contains JSON parameters for the get_public_api tool.
type getPublicAPIParams struct {
	Path        string   `json:"path"`        // Path identifies the package whose public API should be read.
	Identifiers []string `json:"identifiers"` // Identifiers optionally restrict output to specific public API identifiers.
}

var getPublicAPIPresenterInstance llmstream.Presenter = getPublicAPIPresenter{}

// The getPublicAPIPresenter type formats get_public_api tool summaries and requested identifiers.
type getPublicAPIPresenter struct{}

// NewGetPublicAPITool returns a get_public_api tool that resolves package paths from the authorizer sandbox and authorizes package reads with authorizer.
//
// The authorizer must be non-nil.
func NewGetPublicAPITool(authorizer authdomain.Authorizer) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	return &toolGetPublicAPI{
		sandboxAbsDir: sandboxAbsDir,
		authorizer:    authorizer,
	}
}

// Name returns the registered tool name, "get_public_api".
func (t *toolGetPublicAPI) Name() string {
	return ToolNameGetPublicAPI
}

// Presenter returns the presenter that formats get_public_api calls and public API results.
func (t *toolGetPublicAPI) Presenter() llmstream.Presenter {
	return getPublicAPIPresenterInstance
}

// Present returns the get_public_api presentation for call. The presentation replaces prior progress, summarizes the requested package, and includes requested identifiers
// in the body when the call supplied any.
func (p getPublicAPIPresenter) Present(call llmstream.ToolCall, result *llmstream.ToolResult) llmstream.Presentation {
	presentation := pkgToolReplaceSummaryPresentation(getPublicAPIPresenterSummary(call))
	if body, ok := getPublicAPIPresenterBody(call); ok {
		presentation.Body = body
	}
	return presentation
}

func getPublicAPIPresenterSummary(call llmstream.ToolCall) llmstream.Line {
	path, _, ok := getPublicAPIPresenterParams(call)
	target := path
	if !ok || target == "" {
		target = strings.TrimSpace(call.Name)
	}
	if target == "" {
		return pkgToolActionSummary("Read Public API")
	}
	return pkgToolActionSummary("Read Public API", llmstream.Segment{Text: target, Role: llmstream.RoleNormal})
}

func getPublicAPIPresenterBody(call llmstream.ToolCall) (llmstream.Output, bool) {
	_, identifiers, ok := getPublicAPIPresenterParams(call)
	if !ok || len(identifiers) == 0 {
		return llmstream.Output{}, false
	}
	return llmstream.Output{
		Lines: []string{strings.Join(identifiers, ", ")},
	}, true
}

func getPublicAPIPresenterParams(call llmstream.ToolCall) (string, []string, bool) {
	var params getPublicAPIParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return "", nil, false
	}

	path := strings.TrimSpace(params.Path)
	identifiers := make([]string, 0, len(params.Identifiers))
	for _, identifier := range params.Identifiers {
		identifier = strings.TrimSpace(identifier)
		if identifier != "" {
			identifiers = append(identifiers, identifier)
		}
	}
	return path, identifiers, path != "" || len(identifiers) > 0
}

// Info returns the LLM-facing metadata for the get_public_api tool, including its required package path and optional identifier filter.
func (t *toolGetPublicAPI) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{
		Name:        ToolNameGetPublicAPI,
		Description: strings.TrimSpace(descriptionGetPublicAPI),
		Parameters: map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "A Go package directory (relative to the sandbox) or a Go import path.",
			},
			"identifiers": map[string]any{
				"type":        "array",
				"description": "Optionally, supply specific identifiers to fetch docs for.",
				"items": map[string]any{
					"type": "string",
				},
			},
		},
		Required: []string{"path"},
	}
}

// Run executes the get_public_api tool call and returns godoc-like public API documentation for a package. The call input must be JSON containing path and may include
// identifiers to restrict output to selected public API symbols. The package path may be sandbox-relative or an import path. Run returns an error tool result for
// invalid input, package resolution or authorization failures, package loading failures, or documentation generation errors.
func (t *toolGetPublicAPI) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	_ = ctx

	var params getPublicAPIParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return coretools.NewToolErrorResult(call, fmt.Sprintf("error parsing parameters: %s", err), err)
	}

	if params.Path == "" {
		return llmstream.NewErrorToolResult("path is required", call)
	}

	mod, err := gocode.NewModule(t.sandboxAbsDir)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	moduleAbsDir, packageAbsDir, packageRelDir, resolvedImportPath, err := resolvePackagePath(mod, params.Path)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	if t.authorizer != nil && isWithinDir(t.sandboxAbsDir, packageAbsDir) {
		// Only prompt/deny for sandbox reads; resolved dependency/stdlib packages are always readable.
		if authErr := t.authorizer.IsAuthorizedForRead(false, "", ToolNameGetPublicAPI, packageAbsDir); authErr != nil {
			return coretools.NewToolErrorResult(call, authErr.Error(), authErr)
		}
	}

	pkg, err := loadPackageForResolved(mod, moduleAbsDir, packageAbsDir, packageRelDir, resolvedImportPath)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "package directory does not exist") {
			return coretools.NewToolErrorResult(call, errMsg, err)
		}

		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	// If identifiers were provided, limit documentation to those identifiers.
	// PublicPackageDocumentation handles method formats such as "*T.M" and "T.M".
	doc, err := gocodecontext.PublicPackageDocumentation(pkg, params.Identifiers...)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	return llmstream.ToolResult{
		CallID: call.CallID,
		Name:   call.Name,
		Type:   call.Type,
		Result: doc,
	}
}
