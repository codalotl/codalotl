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
	"github.com/codalotl/codalotl/internal/gousage"
	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
)

//go:embed update_usage.md
var descriptionUpdateUsage string

const ToolNameUpdateUsage = "update_usage"

// This mirrors internal/agentbuilder.AgentLimitedPackageMode without importing that package and creating an import cycle.
const updateUsageAgentName = "limited_package_mode"

type toolUpdateUsage struct {
	sandboxAbsDir string
	authorizer    authdomain.Authorizer
	toolset       toolsetinterface.Toolset
	agentInvoker  toolsetinterface.AgentInvoker
	model         llmmodel.ModelID
	pkgDirAbsPath string
	lintSteps     []lints.Step
}

type updateUsageParams struct {
	Instructions string   `json:"instructions"`
	Paths        []string `json:"paths"`
}

type UpdateUsageToolOptions struct {
	AgentInvoker toolsetinterface.AgentInvoker
}

// authorizer should be the "sandbox" authorizer, not a package-jailed authorizer.
//
// pkgDirAbsPath is the package dir that NewUpdateUsageTool is built to serve (i.e., update packages that depend on it).
//
// toolset is retained for compatibility with existing builders; registry-backed subagent invocation is driven by AgentInvoker.
func NewUpdateUsageTool(pkgDirAbsPath string, authorizer authdomain.Authorizer, toolset toolsetinterface.Toolset, model llmmodel.ModelID, lintSteps []lints.Step, options ...UpdateUsageToolOptions) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	var option UpdateUsageToolOptions
	if len(options) > 0 {
		option = options[0]
	}
	return &toolUpdateUsage{
		sandboxAbsDir: sandboxAbsDir,
		authorizer:    authorizer,
		toolset:       toolset,
		agentInvoker:  option.AgentInvoker,
		model:         model,
		pkgDirAbsPath: filepath.Clean(pkgDirAbsPath),
		lintSteps:     lintSteps,
	}
}

func (t *toolUpdateUsage) Name() string {
	return ToolNameUpdateUsage
}

func (t *toolUpdateUsage) Presenter() llmstream.Presenter {
	return nil
}

func (t *toolUpdateUsage) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{
		Name:        ToolNameUpdateUsage,
		Description: strings.TrimSpace(descriptionUpdateUsage),
		Parameters: map[string]any{
			"instructions": map[string]any{
				"type":        "string",
				"description": "Instructions for a new Agent to update a downstream package.",
			},
			"paths": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "string",
				},
				"description": "Array of Go package directories (relative to the sandbox) or Go import paths, each a downstream package that should be updated.",
			},
		},
		Required: []string{"instructions", "paths"},
	}
}

func (t *toolUpdateUsage) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	var params updateUsageParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return coretools.NewToolErrorResult(call, fmt.Sprintf("error parsing parameters: %s", err), err)
	}

	if params.Instructions == "" {
		return llmstream.NewErrorToolResult("instructions is required", call)
	}

	if len(params.Paths) == 0 {
		return llmstream.NewErrorToolResult("paths is required", call)
	}

	mod, err := gocode.NewModule(t.sandboxAbsDir)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	pkgAbsDir := t.pkgDirAbsPath
	if !filepath.IsAbs(pkgAbsDir) {
		pkgAbsDir = filepath.Join(t.sandboxAbsDir, pkgAbsDir)
	}
	pkgAbsDir = filepath.Clean(pkgAbsDir)

	if t.authorizer != nil {
		if authErr := t.authorizer.IsAuthorizedForRead(false, "", ToolNameUpdateUsage, pkgAbsDir); authErr != nil {
			return coretools.NewToolErrorResult(call, authErr.Error(), authErr)
		}
	}

	relativeDir, err := filepath.Rel(mod.AbsolutePath, pkgAbsDir)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}
	if relativeDir == ".." || strings.HasPrefix(relativeDir, ".."+string(filepath.Separator)) {
		return coretools.NewToolErrorResult(call, fmt.Sprintf("package directory %q is outside module %q", pkgAbsDir, mod.AbsolutePath), nil)
	}
	if relativeDir == "." {
		relativeDir = ""
	}

	relativeDirSlash := filepath.ToSlash(relativeDir)
	pkg, err := mod.LoadPackageByRelativeDir(relativeDirSlash)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	usages, err := gousage.UsedBy(pkg)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	if len(usages) == 0 {
		return llmstream.ToolResult{
			CallID: call.CallID,
			Name:   call.Name,
			Type:   call.Type,
			Result: "no downstream packages import this package",
		}
	}

	for i := range usages {
		usages[i].AbsolutePath = filepath.Clean(usages[i].AbsolutePath)
	}

	usageByImportPath := make(map[string]gousage.Usage, len(usages))
	for _, usage := range usages {
		usageByImportPath[usage.ImportPath] = usage
	}

	orderedImports := make([]string, 0, len(params.Paths))
	seenImport := make(map[string]bool, len(params.Paths))

	for _, rawPath := range params.Paths {
		if strings.TrimSpace(rawPath) == "" {
			return llmstream.NewErrorToolResult("paths must not contain empty entries", call)
		}

		resolved, err := resolveToolPackageRef(mod, rawPath)
		if err != nil {
			return coretools.NewToolErrorResult(call, err.Error(), err)
		}
		if err := validateResolvedPackageRefInSandbox(t.sandboxAbsDir, rawPath, resolved); err != nil {
			return llmstream.NewErrorToolResult(err.Error(), call)
		}
		if err := validateResolvedPackageRefInModule(mod.AbsolutePath, rawPath, resolved); err != nil {
			return llmstream.NewErrorToolResult(err.Error(), call)
		}

		usage, ok := usageByImportPath[resolved.ImportPath]
		if !ok {
			return llmstream.NewErrorToolResult(fmt.Sprintf("path %q resolves to %q, which is not a downstream package that imports %q", rawPath, resolved.ImportPath, pkg.ImportPath), call)
		}

		if !seenImport[usage.ImportPath] {
			seenImport[usage.ImportPath] = true
			orderedImports = append(orderedImports, usage.ImportPath)
		}
	}

	if len(orderedImports) == 0 {
		return llmstream.NewErrorToolResult("no valid downstream package paths provided", call)
	}

	instructions := params.Instructions

	// fmt.Println("instructions:")
	// fmt.Println(instructions)
	// fmt.Println("target packages:")
	// for _, pkgPath := range orderedPackages {
	// 	usage := usageByAbsPath[pkgPath]
	// 	fmt.Printf("- %s (%s)\n", usage.ImportPath, pkgPath)
	// 	for _, path := range packagePaths[pkgPath] {
	// 		fmt.Printf("  - %s\n", path)
	// 	}
	// }

	agentCreator, err := subAgentCreatorFromContextSafe(ctx)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	results := make([]string, 0, len(orderedImports))
	for _, importPath := range orderedImports {
		usage := usageByImportPath[importPath]
		targetAbsPath := usage.AbsolutePath

		if t.authorizer != nil {
			if authErr := t.authorizer.IsAuthorizedForWrite(false, "", ToolNameUpdateUsage, targetAbsPath); authErr != nil {
				return coretools.NewToolErrorResult(call, authErr.Error(), authErr)
			}
		}

		unit, err := codeunit.NewCodeUnit(fmt.Sprintf("package %s", usage.ImportPath), targetAbsPath)
		if err != nil {
			return coretools.NewToolErrorResult(call, err.Error(), err)
		}
		unit.IncludeEntireSubtree()

		pkgAuthorizer := authdomain.NewCodeUnitAuthorizer(unit, t.authorizer)

		answer, err := invokeUpdateUsageAgent(
			ctx,
			t.agentInvoker,
			agentCreator,
			t.sandboxAbsDir,
			pkgAuthorizer,
			targetAbsPath,
			t.model,
			t.lintSteps,
			t.agentInvoker,
			instructions,
		)
		if err != nil {
			return coretools.NewToolErrorResult(call, err.Error(), err)
		}

		if strings.TrimSpace(answer) == "" {
			results = append(results, fmt.Sprintf("%s: no changes reported", usage.ImportPath))
			continue
		}

		results = append(results, fmt.Sprintf("%s:\n%s", usage.ImportPath, strings.TrimSpace(answer)))
	}

	return llmstream.ToolResult{
		CallID: call.CallID,
		Name:   call.Name,
		Type:   call.Type,
		Result: strings.Join(results, "\n\n"),
	}
}

func invokeUpdateUsageAgent(ctx context.Context, invoker toolsetinterface.AgentInvoker, agentCreator agent.AgentCreator, sandboxAbsDir string, pkgAuthorizer authdomain.Authorizer, packageAbsDir string, model llmmodel.ModelID, lintSteps []lints.Step, nestedAgentInvoker toolsetinterface.AgentInvoker, instructions string) (string, error) {
	if invoker == nil {
		return "", fmt.Errorf("update_usage agent unavailable")
	}

	req := toolsetinterface.InvokeRequest{
		AgentCreator:     agentCreator,
		CallerAuthorizer: pkgAuthorizer,
		CallerSandboxDir: sandboxAbsDir,
		ToolOptions: toolsetinterface.Options{
			SandboxDir:   sandboxAbsDir,
			GoPkgAbsDir:  packageAbsDir,
			Model:        model,
			LintSteps:    lintSteps,
			AgentInvoker: nestedAgentInvoker,
		},
		Messages: []string{instructions},
	}

	events, err := invoker.Invoke(ctx, updateUsageAgentName, req)
	if err != nil {
		return "", err
	}

	return agent.CollectFinalAssistantText(ctx, events)
}
