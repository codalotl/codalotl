package agentbuilder

import (
	"github.com/codalotl/codalotl/internal/agentregistry"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/prompt"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"github.com/codalotl/codalotl/internal/tools/exttools"
	"github.com/codalotl/codalotl/internal/tools/pkgtools"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
	"github.com/codalotl/codalotl/internal/tools/toolsets"
)

const (
	AgentGeneric     string = "generic"
	AgentPackageMode string = "package_mode"
)

// BuildRegistry builds the registry.
func BuildRegistry() (*agentregistry.Registry, error) {
	registry := agentregistry.NewRegistry()

	for toolName, tool := range genericTools() {
		if err := registry.RegisterTool(toolName, tool); err != nil {
			return nil, err
		}
	}

	if err := registry.RegisterAgent(agentregistry.Definition{
		Name:        AgentGeneric,
		Description: "General-purpose agent with core file editing and shell tools.",
		ToolNames: []string{
			coretools.ToolNameReadFile,
			coretools.ToolNameLS,
		},
		ToolsBuilder: buildGenericToolNames,
		SystemPromptBuilder: func(options agentregistry.BuildOptions) (string, error) {
			return prompt.GetBasicPrompt(), nil
		},
	}); err != nil {
		return nil, err
	}

	if err := registry.RegisterAgent(agentregistry.Definition{
		Name:        AgentPackageMode,
		Description: "Go package-focused agent with package-jail editing, testing, and API analysis tools.",
		ToolNames: []string{
			coretools.ToolNameReadFile,
			coretools.ToolNameLS,
		},
		ToolsBuilder: buildPackageModeToolNames,
		SystemPromptBuilder: func(options agentregistry.BuildOptions) (string, error) {
			return prompt.GetGoPackageModeModePrompt(prompt.GoPackageModePromptKindFull), nil
		},
		AuthPolicy: agentregistry.AuthPolicyPackage,
	}); err != nil {
		return nil, err
	}

	if err := registry.ValidateTools(); err != nil {
		return nil, err
	}

	return registry, nil
}

func genericTools() map[string]toolsetinterface.Tool {
	return map[string]toolsetinterface.Tool{
		coretools.ToolNameApplyPatch: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return coretools.NewApplyPatchTool(opts.Authorizer, true, packageModePostChecks(opts)), nil
		},
		coretools.ToolNameEdit: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			postChecks := packageModePostChecks(opts)
			if postChecks == nil {
				return coretools.NewEditTool(opts.Authorizer), nil
			}
			return coretools.NewEditTool(opts.Authorizer, postChecks), nil
		},
		coretools.ToolNameDelete: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return coretools.NewDeleteTool(opts.Authorizer), nil
		},
		exttools.ToolNameDiagnostics: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return exttools.NewDiagnosticsTool(opts.Authorizer), nil
		},
		pkgtools.ToolNameChangeAPI: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return pkgtools.NewChangeAPITool(
				opts.GoPkgAbsDir,
				opts.Authorizer.WithoutCodeUnit(),
				changeAPIToolset(opts),
				opts.Model,
				opts.LintSteps,
			), nil
		},
		pkgtools.ToolNameClarifyPublicAPI: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return pkgtools.NewClarifyPublicAPITool(opts.Authorizer.WithoutCodeUnit(), toolsets.SimpleReadOnlyTools), nil
		},
		coretools.ToolNameLS: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return coretools.NewLsTool(opts.Authorizer), nil
		},
		exttools.ToolNameFixLints: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return exttools.NewFixLintsTool(opts.Authorizer, opts.LintSteps), nil
		},
		pkgtools.ToolNameGetPublicAPI: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return pkgtools.NewGetPublicAPITool(opts.Authorizer.WithoutCodeUnit()), nil
		},
		pkgtools.ToolNameGetUsage: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return pkgtools.NewGetUsageTool(opts.Authorizer.WithoutCodeUnit()), nil
		},
		pkgtools.ToolNameModuleInfo: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return pkgtools.NewModuleInfoTool(opts.Authorizer.WithoutCodeUnit()), nil
		},
		coretools.ToolNameReadFile: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return coretools.NewReadFileTool(opts.Authorizer), nil
		},
		exttools.ToolNameRunProjectTests: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return exttools.NewRunProjectTestsTool(opts.GoPkgAbsDir, opts.Authorizer.WithoutCodeUnit()), nil
		},
		exttools.ToolNameRunTests: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return exttools.NewRunTestsTool(opts.Authorizer, opts.LintSteps), nil
		},
		coretools.ToolNameShell: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return coretools.NewShellTool(opts.Authorizer), nil
		},
		coretools.ToolNameSkillShell: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return coretools.NewSkillShellTool(opts.Authorizer), nil
		},
		coretools.ToolNameUpdatePlan: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return coretools.NewUpdatePlanTool(opts.Authorizer), nil
		},
		pkgtools.ToolNameUpdateUsage: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return pkgtools.NewUpdateUsageTool(
				opts.GoPkgAbsDir,
				opts.Authorizer.WithoutCodeUnit(),
				toolsets.LimitedPackageAgentTools,
				opts.Model,
				opts.LintSteps,
			), nil
		},
		coretools.ToolNameWrite: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			postChecks := packageModePostChecks(opts)
			if postChecks == nil {
				return coretools.NewWriteTool(opts.Authorizer), nil
			}
			return coretools.NewWriteTool(opts.Authorizer, postChecks), nil
		},
	}
}

func packageModePostChecks(opts toolsetinterface.Options) *coretools.ApplyPatchPostChecks {
	if opts.AgentName != AgentPackageMode {
		return nil
	}
	return toolsets.PackagePostChecks(opts.LintSteps)
}

func changeAPIToolset(opts toolsetinterface.Options) toolsetinterface.Toolset {
	if opts.AgentName == AgentPackageMode {
		return toolsets.PackageAgentTools
	}
	return toolsets.LimitedPackageAgentTools
}

func buildGenericToolNames(opts toolsetinterface.Options) ([]string, error) {
	toolNames := buildEditFileToolNames(opts.Model)
	toolNames = append(toolNames,
		coretools.ToolNameShell,
		coretools.ToolNameUpdatePlan,
	)
	return toolNames, nil
}

func buildPackageModeToolNames(opts toolsetinterface.Options) ([]string, error) {
	toolNames := buildEditFileToolNames(opts.Model)
	toolNames = append(toolNames,
		coretools.ToolNameSkillShell,
		coretools.ToolNameUpdatePlan,
		exttools.ToolNameDiagnostics,
		exttools.ToolNameFixLints,
		exttools.ToolNameRunTests,
		exttools.ToolNameRunProjectTests,
		pkgtools.ToolNameModuleInfo,
		pkgtools.ToolNameGetPublicAPI,
		pkgtools.ToolNameClarifyPublicAPI,
		pkgtools.ToolNameGetUsage,
		pkgtools.ToolNameUpdateUsage,
		pkgtools.ToolNameChangeAPI,
	)
	return toolNames, nil
}

func buildEditFileToolNames(model llmmodel.ModelID) []string {
	if model.ProviderID() == llmmodel.ProviderIDOpenAI {
		return []string{coretools.ToolNameApplyPatch}
	}

	return []string{
		coretools.ToolNameEdit,
		coretools.ToolNameWrite,
		coretools.ToolNameDelete,
	}
}
