package agentbuilder

import (
	"github.com/codalotl/codalotl/internal/agentregistry"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/prompt"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
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

	if err := registry.ValidateTools(); err != nil {
		return nil, err
	}

	return registry, nil
}

func genericTools() map[string]toolsetinterface.Tool {
	return map[string]toolsetinterface.Tool{
		coretools.ToolNameApplyPatch: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return coretools.NewApplyPatchTool(opts.Authorizer, true, nil), nil
		},
		coretools.ToolNameEdit: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return coretools.NewEditTool(opts.Authorizer), nil
		},
		coretools.ToolNameDelete: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return coretools.NewDeleteTool(opts.Authorizer), nil
		},
		coretools.ToolNameLS: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return coretools.NewLsTool(opts.Authorizer), nil
		},
		coretools.ToolNameReadFile: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return coretools.NewReadFileTool(opts.Authorizer), nil
		},
		coretools.ToolNameShell: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return coretools.NewShellTool(opts.Authorizer), nil
		},
		coretools.ToolNameUpdatePlan: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return coretools.NewUpdatePlanTool(opts.Authorizer), nil
		},
		coretools.ToolNameWrite: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return coretools.NewWriteTool(opts.Authorizer), nil
		},
	}
}

func buildGenericToolNames(opts toolsetinterface.Options) ([]string, error) {
	toolNames := []string{}
	if opts.Model.ProviderID() == llmmodel.ProviderIDOpenAI {
		toolNames = append(toolNames, coretools.ToolNameApplyPatch)
	} else {
		toolNames = append(toolNames,
			coretools.ToolNameEdit,
			coretools.ToolNameWrite,
			coretools.ToolNameDelete,
		)
	}
	toolNames = append(toolNames,
		coretools.ToolNameShell,
		coretools.ToolNameUpdatePlan,
	)
	return toolNames, nil
}
