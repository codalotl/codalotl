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

const ToolNameGetPublicAPI = "get_public_api"

type toolGetPublicAPI struct {
	sandboxAbsDir string
	authorizer    authdomain.Authorizer
}

type getPublicAPIParams struct {
	Path        string   `json:"path"`
	Identifiers []string `json:"identifiers"`
}

var getPublicAPIPresenterInstance llmstream.Presenter = getPublicAPIPresenter{}

type getPublicAPIPresenter struct{}

func NewGetPublicAPITool(authorizer authdomain.Authorizer) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	return &toolGetPublicAPI{
		sandboxAbsDir: sandboxAbsDir,
		authorizer:    authorizer,
	}
}

func (t *toolGetPublicAPI) Name() string {
	return ToolNameGetPublicAPI
}

func (t *toolGetPublicAPI) Presenter() llmstream.Presenter {
	return getPublicAPIPresenterInstance
}

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
