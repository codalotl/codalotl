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
	"path/filepath"
	"runtime"
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
	Path        string   `json:"path"`
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

func resolvePackagePath(mod *gocode.Module, input string) (moduleAbsDir string, packageAbsDir string, packageRelDir string, fqImportPath string, fnErr error) {
	if mod == nil {
		return "", "", "", "", errors.New("module required")
	}
	if input == "" {
		return "", "", "", "", errors.New("path is required")
	}

	// Heuristic preference:
	// - likely import paths (example.com/..., module-name/...): resolve as import first
	// - otherwise (internal/...): resolve as sandbox-relative dir first
	preferImport := strings.HasPrefix(input, mod.Name+"/") || input == mod.Name || hasDotInFirstPathSegment(input)

	tryImport := func() (string, string, string, string, error) {
		m, p, r, ip, err := mod.ResolvePackageByImport(input)
		return m, p, r, ip, err
	}
	tryRelDir := func() (string, string, string, string, error) {
		m, p, r, ip, err := mod.ResolvePackageByRelativeDir(input)
		return m, p, r, ip, err
	}

	if preferImport {
		m, p, r, ip, err := tryImport()
		if err == nil {
			return m, p, r, ip, nil
		}
		if !errors.Is(err, gocode.ErrResolveNotFound) {
			return "", "", "", "", err
		}

		m, p, r, ip, err = tryRelDir()
		if err == nil {
			return m, p, r, ip, nil
		}
		if errors.Is(err, gocode.ErrResolveNotFound) {
			return "", "", "", "", fmt.Errorf("package %q could not be resolved from this module's build context", input)
		}
		return "", "", "", "", err
	}

	m, p, r, ip, err := tryRelDir()
	if err == nil {
		return m, p, r, ip, nil
	}
	if !errors.Is(err, gocode.ErrResolveNotFound) {
		return "", "", "", "", err
	}

	m, p, r, ip, err = tryImport()
	if err == nil {
		return m, p, r, ip, nil
	}
	if errors.Is(err, gocode.ErrResolveNotFound) {
		return "", "", "", "", fmt.Errorf("package %q could not be resolved from this module's build context", input)
	}
	return "", "", "", "", err
}

func loadPackageForResolved(baseMod *gocode.Module, moduleAbsDir string, packageAbsDir string, packageRelDir string, fqImportPath string) (*gocode.Package, error) {
	if baseMod == nil {
		return nil, errors.New("module required")
	}
	if packageAbsDir == "" {
		return nil, errors.New("package directory not resolved")
	}

	relDir, err := resolvedRelDir(moduleAbsDir, packageAbsDir, packageRelDir)
	if err != nil {
		return nil, err
	}

	// If we're still inside the sandbox module, use the already-loaded module cache.
	if moduleAbsDir == baseMod.AbsolutePath {
		pkg, err := baseMod.LoadPackageByRelativeDir(relDir)
		if err != nil {
			return nil, err
		}
		pkg.ImportPath = fqImportPath
		return pkg, nil
	}

	// For dependencies (or nested modules), load via a Module rooted at the resolved module dir.
	if moduleAbsDir != "" {
		depMod, err := gocode.NewModule(moduleAbsDir)
		if err != nil {
			return nil, err
		}
		pkg, err := depMod.LoadPackageByRelativeDir(relDir)
		if err != nil {
			return nil, err
		}
		pkg.ImportPath = fqImportPath
		return pkg, nil
	}

	// Standard library packages are not within a module. We still load them for docs.
	stdRootAbsDir, stdRelDir := stdlibRootAndRel(packageAbsDir)
	stdMod := &gocode.Module{
		Name:         "",
		AbsolutePath: stdRootAbsDir,
		Packages:     map[string]*gocode.Package{},
	}
	pkg, err := stdMod.ReadPackage(stdRelDir, nil)
	if err != nil {
		return nil, err
	}
	pkg.ImportPath = fqImportPath
	return pkg, nil
}

func resolvedRelDir(moduleAbsDir string, packageAbsDir string, packageRelDir string) (string, error) {
	if packageRelDir != "" {
		if packageRelDir == "." {
			return ".", nil
		}
		return packageRelDir, nil
	}
	if moduleAbsDir == "" {
		return ".", nil
	}
	rel, err := filepath.Rel(moduleAbsDir, packageAbsDir)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	if rel == "" {
		return ".", nil
	}
	return rel, nil
}

func stdlibRootAndRel(packageAbsDir string) (rootAbsDir string, relDir string) {
	if packageAbsDir == "" {
		return "", "."
	}
	goroot := runtime.GOROOT()
	if goroot != "" {
		gorootSrc := filepath.Join(goroot, "src")
		if isWithinDir(gorootSrc, packageAbsDir) {
			rel, err := filepath.Rel(gorootSrc, packageAbsDir)
			if err == nil {
				rel = filepath.ToSlash(rel)
				if rel == "" {
					return gorootSrc, "."
				}
				return gorootSrc, rel
			}
		}
	}

	// Fallback: treat the package dir as the root. This yields correct docs even if we can't derive GOROOT.
	return packageAbsDir, "."
}

func hasDotInFirstPathSegment(p string) bool {
	seg, _, _ := strings.Cut(p, "/")
	return strings.Contains(seg, ".")
}

func isWithinDir(parentAbsDir string, childAbsPath string) bool {
	if parentAbsDir == "" || childAbsPath == "" {
		return false
	}
	rel, err := filepath.Rel(parentAbsDir, childAbsPath)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	return rel == "." || (!strings.HasPrefix(rel, "../") && rel != "..")
}
