package agentbuilder

import (
	"context"
	"errors"
	"testing"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/agentregistry"
	"github.com/codalotl/codalotl/internal/codeunit"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/prompt"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"github.com/codalotl/codalotl/internal/tools/exttools"
	"github.com/codalotl/codalotl/internal/tools/pkgtools"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRegistry_RegistersGenericAndPackageModeAgents(t *testing.T) {
	registry, err := BuildRegistry()
	require.NoError(t, err)

	require.NoError(t, registry.ValidateTools())

	defs := registry.List()
	require.Len(t, defs, 2)

	genericDef, ok := registry.Lookup(AgentGeneric)
	assert.True(t, ok)
	assert.Equal(t, AgentGeneric, genericDef.Name)

	packageModeDef, ok := registry.Lookup(AgentPackageMode)
	assert.True(t, ok)
	assert.Equal(t, AgentPackageMode, packageModeDef.Name)
	assert.Equal(t, agentregistry.AuthPolicyPackage, packageModeDef.AuthPolicy)
	assert.Nil(t, packageModeDef.InitialTurnsBuilder)
}

func TestBuildRegistry_InvokeGeneric_OpenAIUsesApplyPatch(t *testing.T) {
	gotPrompt, gotTools := invokeAgentForModel(t, AgentGeneric, llmmodel.ProviderIDOpenAI.DefaultModel())

	assert.Equal(t, prompt.GetBasicPrompt(), gotPrompt)
	assert.Equal(t, []string{
		coretools.ToolNameReadFile,
		coretools.ToolNameLS,
		coretools.ToolNameApplyPatch,
		coretools.ToolNameShell,
		coretools.ToolNameUpdatePlan,
	}, gotTools)
}

func TestBuildRegistry_InvokeGeneric_NonOpenAIUsesEditWriteDelete(t *testing.T) {
	_, gotTools := invokeAgentForModel(t, AgentGeneric, llmmodel.ProviderIDAnthropic.DefaultModel())

	assert.Equal(t, []string{
		coretools.ToolNameReadFile,
		coretools.ToolNameLS,
		coretools.ToolNameEdit,
		coretools.ToolNameWrite,
		coretools.ToolNameDelete,
		coretools.ToolNameShell,
		coretools.ToolNameUpdatePlan,
	}, gotTools)
}

func TestBuildRegistry_InvokePackageMode_OpenAIUsesPackagePromptAndTools(t *testing.T) {
	gotPrompt, gotTools := invokeAgentForModel(t, AgentPackageMode, llmmodel.ProviderIDOpenAI.DefaultModel())

	assert.Equal(t, prompt.GetGoPackageModeModePrompt(prompt.GoPackageModePromptKindFull), gotPrompt)
	assert.Equal(t, []string{
		coretools.ToolNameReadFile,
		coretools.ToolNameLS,
		coretools.ToolNameApplyPatch,
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
	}, gotTools)
}

func TestBuildRegistry_InvokePackageMode_NonOpenAIUsesEditWriteDelete(t *testing.T) {
	_, gotTools := invokeAgentForModel(t, AgentPackageMode, llmmodel.ProviderIDAnthropic.DefaultModel())

	assert.Equal(t, []string{
		coretools.ToolNameReadFile,
		coretools.ToolNameLS,
		coretools.ToolNameEdit,
		coretools.ToolNameWrite,
		coretools.ToolNameDelete,
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
	}, gotTools)
}

func invokeAgentForModel(t *testing.T, agentName string, model llmmodel.ModelID) (string, []string) {
	t.Helper()

	registry, err := BuildRegistry()
	require.NoError(t, err)

	sandbox := t.TempDir()
	creator := &captureAgentCreator{err: errors.New("stop")}
	authorizer := authdomain.NewAutoApproveAuthorizer(sandbox)

	if agentName == AgentPackageMode {
		unit, err := codeunit.NewCodeUnit("package .", sandbox)
		require.NoError(t, err)
		authorizer = authdomain.NewCodeUnitAuthorizer(unit, authorizer)
	}

	_, err = registry.Invoke(context.Background(), agentName, toolsetinterface.InvokeRequest{
		AgentCreator: creator,
		ToolOptions: toolsetinterface.Options{
			Model:       model,
			Authorizer:  authorizer,
			SandboxDir:  sandbox,
			GoPkgAbsDir: sandbox,
		},
	})
	require.ErrorContains(t, err, "stop")

	return creator.lastSystemPrompt, toolNames(creator.lastTools)
}

func toolNames(tools []llmstream.Tool) []string {
	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.Name()
	}
	return names
}

type captureAgentCreator struct {
	lastModel        llmmodel.ModelID
	lastSystemPrompt string
	lastTools        []llmstream.Tool
	err              error
}

func (c *captureAgentCreator) New(model llmmodel.ModelID, systemPrompt string, tools []llmstream.Tool) (*agent.Agent, error) {
	c.lastModel = model
	c.lastSystemPrompt = systemPrompt
	c.lastTools = tools
	return nil, c.err
}

func (c *captureAgentCreator) NewWithDefaultModel(systemPrompt string, tools []llmstream.Tool) (*agent.Agent, error) {
	c.lastSystemPrompt = systemPrompt
	c.lastTools = tools
	return nil, c.err
}

var _ agent.AgentCreator = (*captureAgentCreator)(nil)
