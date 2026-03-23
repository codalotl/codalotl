package agentbuilder

import (
	"context"
	"errors"
	"testing"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/prompt"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRegistry_RegistersGenericAgent(t *testing.T) {
	registry, err := BuildRegistry()
	require.NoError(t, err)

	require.NoError(t, registry.ValidateTools())

	defs := registry.List()
	require.Len(t, defs, 1)
	assert.Equal(t, AgentGeneric, defs[0].Name)

	_, ok := registry.Lookup(AgentGeneric)
	assert.True(t, ok)

	_, ok = registry.Lookup(AgentPackageMode)
	assert.False(t, ok)
}

func TestBuildRegistry_InvokeGeneric_OpenAIUsesApplyPatch(t *testing.T) {
	gotPrompt, gotTools := invokeGenericForModel(t, llmmodel.ProviderIDOpenAI.DefaultModel())

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
	_, gotTools := invokeGenericForModel(t, llmmodel.ProviderIDAnthropic.DefaultModel())

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

func invokeGenericForModel(t *testing.T, model llmmodel.ModelID) (string, []string) {
	t.Helper()

	registry, err := BuildRegistry()
	require.NoError(t, err)

	sandbox := t.TempDir()
	creator := &captureAgentCreator{err: errors.New("stop")}

	_, err = registry.Invoke(context.Background(), AgentGeneric, toolsetinterface.InvokeRequest{
		AgentCreator: creator,
		ToolOptions: toolsetinterface.Options{
			Model:      model,
			Authorizer: authdomain.NewAutoApproveAuthorizer(sandbox),
			SandboxDir: sandbox,
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
