package pkgtools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/codeunit"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gousage"
	"github.com/codalotl/codalotl/internal/llmstream"
	updateusageagent "github.com/codalotl/codalotl/internal/subagents/updateusage"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
	"path/filepath"
	"strings"
)

//go:embed update_usage.md
var descriptionUpdateUsage string

const ToolNameUpdateUsage = "update_usage"

type toolUpdateUsage struct {
	sandboxAbsDir string
	authorizer    authdomain.Authorizer
	toolset       toolsetinterface.PackageToolset
	pkgDirAbsPath string
}

type updateUsageParams struct {
	Instructions string   `json:"instructions"`
	Paths        []string `json:"paths"`
}

// authorizer here should be the "sandbox" authorizer, not a package-jailed authorizer.
// pkgDirAbsPath here is the package dir that NewUpdateUsageTool is built to serve (i.e., update packages that depend on it)
// toolset is the tools that the subagent doing the updating will ahve access to.
func NewUpdateUsageTool(sandboxAbsDir string, pkgDirAbsPath string, authorizer authdomain.Authorizer, toolset toolsetinterface.PackageToolset) llmstream.Tool {
	return &toolUpdateUsage{
		sandboxAbsDir: filepath.Clean(sandboxAbsDir),
		authorizer:    authorizer,
		toolset:       toolset,
		pkgDirAbsPath: filepath.Clean(pkgDirAbsPath),
	}
}

func (t *toolUpdateUsage) Name() string {
	return ToolNameUpdateUsage
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
				"description": "Absolute or sandbox-relative paths (files or directories) within downstream packages that should be updated.",
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

	if t.toolset == nil {
		return coretools.NewToolErrorResult(call, "toolset unavailable", nil)
	}

	agentCreator := agent.SubAgentCreatorFromContext(ctx)
	if agentCreator == nil {
		return coretools.NewToolErrorResult(call, "unable to create subagent", nil)
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

	usageByAbsPath := make(map[string]gousage.Usage, len(usages))
	for _, usage := range usages {
		usageByAbsPath[usage.AbsolutePath] = usage
	}

	packagePaths := make(map[string][]string)
	orderedPackages := make([]string, 0)

	for _, rawPath := range params.Paths {
		if strings.TrimSpace(rawPath) == "" {
			return llmstream.NewErrorToolResult("paths must not contain empty entries", call)
		}

		cleanPath := filepath.Clean(rawPath)
		var absPath string
		if filepath.IsAbs(cleanPath) {
			absPath = cleanPath
		} else {
			absPath = filepath.Join(t.sandboxAbsDir, cleanPath)
		}

		var matchedUsage *gousage.Usage
		for i := range usages {
			usage := &usages[i]
			relToUsage, err := filepath.Rel(usage.AbsolutePath, absPath)
			if err != nil {
				continue
			}
			if relToUsage == "." || (relToUsage != ".." && !strings.HasPrefix(relToUsage, ".."+string(filepath.Separator))) {
				matchedUsage = usage
				break
			}
		}

		if matchedUsage == nil {
			return llmstream.NewErrorToolResult(fmt.Sprintf("path %q is not within any downstream package directories", rawPath), call)
		}

		targetAbsPath := matchedUsage.AbsolutePath
		if _, exists := packagePaths[targetAbsPath]; !exists {
			orderedPackages = append(orderedPackages, targetAbsPath)
		}
		packagePaths[targetAbsPath] = append(packagePaths[targetAbsPath], absPath)
	}

	if len(packagePaths) == 0 {
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

	results := make([]string, 0, len(orderedPackages))
	for _, targetAbsPath := range orderedPackages {
		usage := usageByAbsPath[targetAbsPath]

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

		targetPaths := packagePaths[targetAbsPath]
		targetLines := make([]string, 0, len(targetPaths))
		for _, p := range targetPaths {
			rel, err := filepath.Rel(targetAbsPath, p)
			if err != nil || rel == "." {
				targetLines = append(targetLines, p)
				continue
			}
			targetLines = append(targetLines, fmt.Sprintf("%s (abs: %s)", filepath.ToSlash(rel), p))
		}

		packageInstructions := instructions
		if len(targetLines) > 0 {
			packageInstructions = fmt.Sprintf("%s\n\nTarget paths for this package:\n- %s", instructions, strings.Join(targetLines, "\n- "))
		}

		answer, err := updateusageagent.UpdateUsage(ctx, agentCreator, t.sandboxAbsDir, pkgAuthorizer, targetAbsPath, t.toolset, packageInstructions)
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
