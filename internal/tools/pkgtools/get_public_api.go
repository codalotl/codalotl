package pkgtools

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodecontext"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"path"
	"path/filepath"
	"strings"
)

//go:embed get_public_api.md
var descriptionGetPublicAPI string

const ToolNameGetPublicAPI = "get_public_api"

type toolGetPublicAPI struct {
	sandboxAbsDir string
	authorizer    authdomain.Authorizer
}

type getPublicAPIParams struct {
	ImportPath  string   `json:"import_path"`
	Identifiers []string `json:"identifiers"`
}

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

func (t *toolGetPublicAPI) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{
		Name:        ToolNameGetPublicAPI,
		Description: strings.TrimSpace(descriptionGetPublicAPI),
		Parameters: map[string]any{
			"import_path": map[string]any{
				"type":        "string",
				"description": "Get the API for this Go package.",
			},
			"identifiers": map[string]any{
				"type":        "array",
				"description": "Optionally, supply specific identifiers to fetch docs for.",
				"items": map[string]any{
					"type": "string",
				},
			},
		},
		Required: []string{"import_path"},
	}
}

func (t *toolGetPublicAPI) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	_ = ctx

	var params getPublicAPIParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return coretools.NewToolErrorResult(call, fmt.Sprintf("error parsing parameters: %s", err), err)
	}

	if params.ImportPath == "" {
		return llmstream.NewErrorToolResult("import_path is required", call)
	}

	mod, err := gocode.NewModule(t.sandboxAbsDir)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	resolvedImportPath, relativeDir, err := resolveImportPath(mod.Name, params.ImportPath)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	absPackageDir := mod.AbsolutePath
	if relativeDir != "" {
		absPackageDir = filepath.Join(mod.AbsolutePath, relativeDir)
	}

	if t.authorizer != nil {
		if authErr := t.authorizer.IsAuthorizedForRead(false, "", ToolNameGetPublicAPI, absPackageDir); authErr != nil {
			return coretools.NewToolErrorResult(call, authErr.Error(), authErr)
		}
	}

	pkg, err := mod.LoadPackageByImportPath(resolvedImportPath)
	if err != nil {
		if errors.Is(err, gocode.ErrImportNotInModule) {
			return coretools.NewToolErrorResult(call, fmt.Sprintf("import path %q is not within this module", resolvedImportPath), err)
		}

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

// resolveImportPath rewrites caller input so we can locate the package on disk.
//   - moduleImportPath is the module path declared in go.mod (ex: "github.com/user/project").
//   - packageImportPath is the import path the tool caller supplied (ex: "github.com/user/project/pkg/foo" or just "pkg/foo").
//
// The function returns:
//  1. the fully-qualified import path
//  2. the directory path relative to the module root (slash-separated)
//  3. an error if the import path is outside the module or otherwise invalid
func resolveImportPath(moduleImportPath, packageImportPath string) (string, string, error) {
	if packageImportPath == "" {
		return "", "", fmt.Errorf("import_path %q is invalid", packageImportPath)
	}
	if moduleImportPath == "" {
		return "", "", errors.New("module name required")
	}

	if strings.HasPrefix(packageImportPath, "/") || strings.Contains(packageImportPath, "\\") {
		return "", "", fmt.Errorf("import_path %q is invalid", packageImportPath)
	}

	segments := strings.Split(packageImportPath, "/")
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return "", "", fmt.Errorf("import_path %q is invalid", packageImportPath)
		}
	}

	if packageImportPath == moduleImportPath {
		return packageImportPath, "", nil
	}
	if relative, ok := strings.CutPrefix(packageImportPath, moduleImportPath+"/"); ok {
		return packageImportPath, relative, nil
	}

	relativePath := strings.Join(segments, "/")
	return path.Join(moduleImportPath, relativePath), relativePath, nil
}
