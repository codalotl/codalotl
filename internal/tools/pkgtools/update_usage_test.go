package pkgtools

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewUpdateUsageTool_StoresLintSteps(t *testing.T) {
	sandbox := t.TempDir()
	steps := []lints.Step{{ID: "custom"}}
	model := llmmodel.DefaultModel
	invoker := &fakeAgentInvoker{}

	tool := NewUpdateUsageTool(sandbox, authdomain.NewAutoApproveAuthorizer(sandbox), dummyPackageToolset(), model, steps, UpdateUsageToolOptions{
		AgentInvoker: invoker,
	})
	updateTool, ok := tool.(*toolUpdateUsage)
	require.True(t, ok)
	assert.Equal(t, model, updateTool.model)
	assert.Equal(t, steps, updateTool.lintSteps)
	assert.Equal(t, invoker, updateTool.agentInvoker)
}

func TestUpdateUsageTool_ExposesPresenter(t *testing.T) {
	sandbox := t.TempDir()
	tool := NewUpdateUsageTool(sandbox, authdomain.NewAutoApproveAuthorizer(sandbox), dummyPackageToolset(), llmmodel.DefaultModel, nil)

	assert.NotNil(t, tool.Presenter())
}

func TestUpdateUsagePresenter(t *testing.T) {
	sandbox := t.TempDir()
	tool := NewUpdateUsageTool(sandbox, authdomain.NewAutoApproveAuthorizer(sandbox), dummyPackageToolset(), llmmodel.DefaultModel, nil)
	presenter := tool.Presenter()

	require.NotNil(t, presenter)

	call := llmstream.ToolCall{
		Name: ToolNameUpdateUsage,
		Input: `{
  "instructions": "Update the callsites to conform to this new API.",
  "paths": ["some/path", "other/path", "third/path", "fourth/path", "fifth/path", "sixth/path", "seventh/path"]
}`,
	}
	result := &llmstream.ToolResult{
		Name:   ToolNameUpdateUsage,
		Result: `{"success":true}`,
	}

	callPresentation := presenter.Present(call, nil)
	resultPresentation := presenter.Present(call, result)

	assert.Equal(t, llmstream.CompletionBehaviorAppend, callPresentation.Behavior)
	assert.Equal(t, llmstream.CompletionBehaviorAppend, resultPresentation.Behavior)
	assert.Equal(t, llmstream.Line{
		JoinWithSpace: true,
		Segments: []llmstream.Segment{
			{Text: "Updating Usage", Role: llmstream.RoleAction},
			{Text: "in", Role: llmstream.RoleAccent},
			{Text: "some/path, other/path, third/path", Role: llmstream.RoleNormal},
			{Text: "(4 more)", Role: llmstream.RoleAccent},
		},
	}, callPresentation.Summary)
	assert.Equal(t, llmstream.Line{
		JoinWithSpace: true,
		Segments: []llmstream.Segment{
			{Text: "Updated Usage", Role: llmstream.RoleAction},
			{Text: "in", Role: llmstream.RoleAccent},
			{Text: "some/path, other/path, third/path", Role: llmstream.RoleNormal},
			{Text: "(4 more)", Role: llmstream.RoleAccent},
		},
	}, resultPresentation.Summary)
	assert.Equal(t, llmstream.Output{
		Lines: []string{"Update the callsites to conform to this new API."},
	}, callPresentation.Body)
	assert.Nil(t, resultPresentation.Body)
}

func TestUpdateUsage_Run_DownstreamPackagePath_ReachesSubagentCheck(t *testing.T) {
	withSimplePackage(t, func(pkg *gocode.Package) {
		err := gocodetesting.AddPackage(t, pkg.Module, "consumer", map[string]string{
			"consumer.go": gocodetesting.Dedent(`
				package consumer

				import "mymodule/mypkg"

				func UseHello() string {
					return mypkg.Hello()
				}
			`),
		})
		require.NoError(t, err)

		auth := authdomain.NewAutoApproveAuthorizer(pkg.Module.AbsolutePath)
		tool := NewUpdateUsageTool(pkg.AbsolutePath(), auth, dummyPackageToolset(), llmmodel.DefaultModel, nil)

		res := tool.Run(context.Background(), llmstream.ToolCall{
			CallID: "call-update-usage",
			Name:   ToolNameUpdateUsage,
			Type:   "function_call",
			Input:  `{"instructions":"Update callers of Hello()","paths":["consumer"]}`,
		})

		// Like change_api, unit tests don't wire a SubAgentCreator into context.
		assert.True(t, res.IsError)
		assert.Contains(t, res.Result, "unable to create subagent")
	})
}

func TestUpdateUsage_Run_RejectsAbsolutePaths(t *testing.T) {
	withSimplePackage(t, func(pkg *gocode.Package) {
		err := gocodetesting.AddPackage(t, pkg.Module, "consumer", map[string]string{
			"consumer.go": gocodetesting.Dedent(`
				package consumer

				import "mymodule/mypkg"

				func UseHello() string {
					return mypkg.Hello()
				}
			`),
		})
		require.NoError(t, err)

		auth := authdomain.NewAutoApproveAuthorizer(pkg.Module.AbsolutePath)
		tool := NewUpdateUsageTool(pkg.AbsolutePath(), auth, dummyPackageToolset(), llmmodel.DefaultModel, nil)

		res := tool.Run(context.Background(), llmstream.ToolCall{
			CallID: "call-update-usage-abs",
			Name:   ToolNameUpdateUsage,
			Type:   "function_call",
			Input:  `{"instructions":"Update callers","paths":["/tmp"]}`,
		})

		assert.True(t, res.IsError)
		assert.Contains(t, res.Result, "absolute paths are not allowed")
	})
}

func TestInvokeUpdateUsageAgent_UsesLimitedPackageAgentAndPassesInstructions(t *testing.T) {
	sandboxDir := t.TempDir()
	authorizer := authdomain.NewAutoApproveAuthorizer(sandboxDir)
	creator := &fakeAgentCreator{}
	invoker := &fakeAgentInvoker{
		events: successfulClarifyEvents("updated downstream package"),
	}
	lintSteps := []lints.Step{{ID: "custom"}}
	packageDir := filepath.Join(sandboxDir, "consumer")

	answer, err := invokeUpdateUsageAgent(
		context.Background(),
		invoker,
		creator,
		sandboxDir,
		authorizer,
		packageDir,
		"mock-model",
		lintSteps,
		invoker,
		"Update downstream callers safely.",
	)
	require.NoError(t, err)
	assert.Equal(t, "updated downstream package", answer)
	assert.Equal(t, updateUsageAgentName, invoker.invokedAgentName)
	assert.Equal(t, creator, invoker.req.AgentCreator)
	assert.Equal(t, authorizer, invoker.req.CallerAuthorizer)
	assert.Equal(t, sandboxDir, invoker.req.CallerSandboxDir)
	assert.Equal(t, sandboxDir, invoker.req.ToolOptions.SandboxDir)
	assert.Equal(t, packageDir, invoker.req.ToolOptions.GoPkgAbsDir)
	assert.Equal(t, llmmodel.ModelID("mock-model"), invoker.req.ToolOptions.Model)
	assert.Equal(t, lintSteps, invoker.req.ToolOptions.LintSteps)
	assert.Equal(t, invoker, invoker.req.ToolOptions.AgentInvoker)
	require.Len(t, invoker.req.Messages, 1)
	assert.Equal(t, "Update downstream callers safely.", invoker.req.Messages[0])
}

func TestInvokeUpdateUsageAgent_RequiresInvoker(t *testing.T) {
	_, err := invokeUpdateUsageAgent(
		context.Background(),
		nil,
		fakeAgentCreator{},
		t.TempDir(),
		nil,
		t.TempDir(),
		"",
		nil,
		nil,
		"Update callers.",
	)
	assert.EqualError(t, err, "update_usage agent unavailable")
}
