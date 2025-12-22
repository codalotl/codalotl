package pkgtools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/codeunit"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/prompt"
	"github.com/codalotl/codalotl/internal/subagents/packagemode"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
)

//go:embed change_api.md
var descriptionChangeAPI string

const ToolNameChangeAPI = "change_api"

type toolChangeAPI struct {
	sandboxAbsDir string
	authorizer    authdomain.Authorizer
	toolset       toolsetinterface.PackageToolset

	// pkgDirAbsPath is the package directory of the agent that is invoking this tool.
	// The tool only allows changing packages that this package directly imports.
	pkgDirAbsPath string
}

type changeAPIParams struct {
	ImportPath   string `json:"import_path"`
	Instructions string `json:"instructions"`
}

// NewChangeAPITool creates a tool that can update upstream packages that the current package directly imports.
//
// authorizer should be a sandbox authorizer (not a package-jail authorizer). If the calling agent is jailed, pass authorizer.WithoutCodeUnit().
// toolset should be the package agent toolset injected into the subagent (ex: toolsets.PackageAgentTools).
func NewChangeAPITool(pkgDirAbsPath string, authorizer authdomain.Authorizer, toolset toolsetinterface.PackageToolset) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	return &toolChangeAPI{
		sandboxAbsDir: sandboxAbsDir,
		authorizer:    authorizer,
		toolset:       toolset,
		pkgDirAbsPath: filepath.Clean(pkgDirAbsPath),
	}
}

func (t *toolChangeAPI) Name() string {
	return ToolNameChangeAPI
}

func (t *toolChangeAPI) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{
		Name:        ToolNameChangeAPI,
		Description: strings.TrimSpace(descriptionChangeAPI),
		Parameters: map[string]any{
			"import_path": map[string]any{
				"type":        "string",
				"description": "Import path of the upstream package to modify. Must be directly imported by the current package.",
			},
			"instructions": map[string]any{
				"type":        "string",
				"description": "What to change and why. Include enough context for a new agent to update the package safely.",
			},
		},
		Required: []string{"import_path", "instructions"},
	}
}

func (t *toolChangeAPI) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	var params changeAPIParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return coretools.NewToolErrorResult(call, fmt.Sprintf("error parsing parameters: %s", err), err)
	}

	importPathParam := strings.TrimSpace(params.ImportPath)
	if importPathParam == "" {
		return llmstream.NewErrorToolResult("import_path is required", call)
	}
	instructions := strings.TrimSpace(params.Instructions)
	if instructions == "" {
		return llmstream.NewErrorToolResult("instructions is required", call)
	}

	mod, err := gocode.NewModule(t.sandboxAbsDir)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	currentPkgAbsDir := t.pkgDirAbsPath
	if !filepath.IsAbs(currentPkgAbsDir) {
		currentPkgAbsDir = filepath.Join(t.sandboxAbsDir, currentPkgAbsDir)
	}
	currentPkgAbsDir = filepath.Clean(currentPkgAbsDir)

	if t.authorizer != nil {
		if authErr := t.authorizer.IsAuthorizedForRead(false, "", ToolNameChangeAPI, currentPkgAbsDir); authErr != nil {
			return coretools.NewToolErrorResult(call, authErr.Error(), authErr)
		}
	}

	relCurrentDir, err := filepath.Rel(mod.AbsolutePath, currentPkgAbsDir)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}
	if relCurrentDir == ".." || strings.HasPrefix(relCurrentDir, ".."+string(filepath.Separator)) {
		return coretools.NewToolErrorResult(call, fmt.Sprintf("current package directory %q is outside module %q", currentPkgAbsDir, mod.AbsolutePath), nil)
	}
	if relCurrentDir == "." {
		relCurrentDir = ""
	}

	currentPkg, err := mod.LoadPackageByRelativeDir(filepath.ToSlash(relCurrentDir))
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	targetImportPath, targetRelDir, err := resolveImportPath(mod.Name, importPathParam)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}
	if targetImportPath == currentPkg.ImportPath {
		return llmstream.NewErrorToolResult("import_path must refer to an upstream package (not the current package)", call)
	}

	allowed := make(map[string]struct{})
	for _, imp := range currentPkg.ImportPathsModule() {
		allowed[imp] = struct{}{}
	}
	if _, ok := allowed[targetImportPath]; !ok {
		return llmstream.NewErrorToolResult(fmt.Sprintf("import_path %q is not a direct module import of the current package %q", targetImportPath, currentPkg.ImportPath), call)
	}

	targetAbsDir := mod.AbsolutePath
	if targetRelDir != "" {
		targetAbsDir = filepath.Join(mod.AbsolutePath, filepath.FromSlash(targetRelDir))
	}
	targetAbsDir = filepath.Clean(targetAbsDir)

	if t.authorizer != nil {
		if authErr := t.authorizer.IsAuthorizedForWrite(false, "", ToolNameChangeAPI, targetAbsDir); authErr != nil {
			return coretools.NewToolErrorResult(call, authErr.Error(), authErr)
		}
	}

	if t.toolset == nil {
		return coretools.NewToolErrorResult(call, "toolset unavailable", nil)
	}

	agentCreator, err := subAgentCreatorFromContextSafe(ctx)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	// Ensure the target package exists and is loadable (helps produce better errors than failing later).
	if _, err := mod.LoadPackageByImportPath(targetImportPath); err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	unit, err := codeunit.NewCodeUnit(fmt.Sprintf("package %s", targetImportPath), targetAbsDir)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}
	unit.IncludeEntireSubtree()

	pkgAuthorizer := authdomain.NewCodeUnitAuthorizer(unit, t.authorizer)

	answer, err := packagemode.Run(
		ctx,
		agentCreator,
		pkgAuthorizer,
		targetAbsDir,
		t.toolset,
		instructions,
		prompt.GoPackageModePromptKindFull,
	)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	return llmstream.ToolResult{
		CallID: call.CallID,
		Name:   call.Name,
		Type:   call.Type,
		Result: strings.TrimSpace(answer),
	}
}

func subAgentCreatorFromContextSafe(ctx context.Context) (creator agent.SubAgentCreator, err error) {
	defer func() {
		if r := recover(); r != nil {
			creator = nil
			err = fmt.Errorf("unable to create subagent")
		}
	}()

	creator = agent.SubAgentCreatorFromContext(ctx)
	if creator == nil {
		return nil, fmt.Errorf("unable to create subagent")
	}
	return creator, nil
}
