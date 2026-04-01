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
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewChangeAPITool_StoresLintSteps(t *testing.T) {
	sandbox := t.TempDir()
	steps := []lints.Step{{ID: "custom"}}
	model := llmmodel.DefaultModel
	invoker := &fakeAgentInvoker{}

	tool := NewChangeAPITool(sandbox, authdomain.NewAutoApproveAuthorizer(sandbox), dummyPackageToolset(), model, steps, ChangeAPIToolOptions{
		AgentInvoker: invoker,
	})
	changeTool, ok := tool.(*toolChangeAPI)
	require.True(t, ok)
	assert.Equal(t, model, changeTool.model)
	assert.Equal(t, steps, changeTool.lintSteps)
	assert.Equal(t, invoker, changeTool.agentInvoker)
}

func TestChangeAPI_MissingImportPath(t *testing.T) {
	withUpstreamFixture(t, func(pkg *gocode.Package) {
		auth := authdomain.NewAutoApproveAuthorizer(pkg.Module.AbsolutePath)
		tool := NewChangeAPITool(pkg.AbsolutePath(), auth, dummyPackageToolset(), llmmodel.DefaultModel, nil)

		res := tool.Run(context.Background(), llmstream.ToolCall{
			CallID: "call-missing-import-path",
			Name:   ToolNameChangeAPI,
			Type:   "function_call",
			Input:  `{"instructions":"do something"}`,
		})

		assert.True(t, res.IsError)
		assert.Contains(t, res.Result, "path is required")
	})
}

func TestChangeAPI_MissingInstructions(t *testing.T) {
	withUpstreamFixture(t, func(pkg *gocode.Package) {
		auth := authdomain.NewAutoApproveAuthorizer(pkg.Module.AbsolutePath)
		tool := NewChangeAPITool(pkg.AbsolutePath(), auth, dummyPackageToolset(), llmmodel.DefaultModel, nil)

		res := tool.Run(context.Background(), llmstream.ToolCall{
			CallID: "call-missing-instructions",
			Name:   ToolNameChangeAPI,
			Type:   "function_call",
			Input:  `{"path":"upstream"}`,
		})

		assert.True(t, res.IsError)
		assert.Contains(t, res.Result, "instructions is required")
	})
}

func TestChangeAPI_RejectsNotImportedPackage(t *testing.T) {
	withUpstreamFixture(t, func(pkg *gocode.Package) {
		auth := authdomain.NewAutoApproveAuthorizer(pkg.Module.AbsolutePath)
		tool := NewChangeAPITool(pkg.AbsolutePath(), auth, dummyPackageToolset(), llmmodel.DefaultModel, nil)

		res := tool.Run(context.Background(), llmstream.ToolCall{
			CallID: "call-not-imported",
			Name:   ToolNameChangeAPI,
			Type:   "function_call",
			Input:  `{"path":"otherpkg","instructions":"update API"}`,
		})

		assert.True(t, res.IsError)
		assert.Contains(t, res.Result, "is not a direct import")
	})
}

func TestChangeAPI_ImportedPackage_ReachesSubagentCheck(t *testing.T) {
	withUpstreamFixture(t, func(pkg *gocode.Package) {
		auth := authdomain.NewAutoApproveAuthorizer(pkg.Module.AbsolutePath)
		tool := NewChangeAPITool(pkg.AbsolutePath(), auth, dummyPackageToolset(), llmmodel.DefaultModel, nil)

		res := tool.Run(context.Background(), llmstream.ToolCall{
			CallID: "call-imported",
			Name:   ToolNameChangeAPI,
			Type:   "function_call",
			Input:  `{"path":"upstream","instructions":"Change the exported API in some way."}`,
		})

		// In unit tests, we don't wire a SubAgentCreator into context; this asserts we got past validation.
		assert.True(t, res.IsError)
		assert.Contains(t, res.Result, "unable to create subagent")
	})
}

func TestChangeAPI_RejectsPackagesOutsideSandbox(t *testing.T) {
	withUpstreamFixture(t, func(pkg *gocode.Package) {
		auth := authdomain.NewAutoApproveAuthorizer(pkg.Module.AbsolutePath)
		tool := NewChangeAPITool(pkg.AbsolutePath(), auth, dummyPackageToolset(), llmmodel.DefaultModel, nil)

		res := tool.Run(context.Background(), llmstream.ToolCall{
			CallID: "call-stdlib",
			Name:   ToolNameChangeAPI,
			Type:   "function_call",
			Input:  `{"path":"fmt","instructions":"please change fmt"}`,
		})

		assert.True(t, res.IsError)
		assert.Contains(t, res.Result, "outside the sandbox")
	})
}

func TestInvokeChangeAPIAgent_UsesPackageModeAgentAndPassesInstructions(t *testing.T) {
	sandboxDir := t.TempDir()
	authorizer := authdomain.NewAutoApproveAuthorizer(sandboxDir)
	creator := &fakeAgentCreator{}
	invoker := &fakeAgentInvoker{
		events: successfulClarifyEvents("updated upstream package"),
	}
	lintSteps := []lints.Step{{ID: "custom"}}
	packageDir := filepath.Join(sandboxDir, "upstream")

	answer, err := invokeChangeAPIAgent(
		context.Background(),
		invoker,
		creator,
		sandboxDir,
		authorizer,
		packageDir,
		"mock-model",
		lintSteps,
		invoker,
		"Update the exported API safely.",
	)
	require.NoError(t, err)
	assert.Equal(t, "updated upstream package", answer)
	assert.Equal(t, changeAPIAgentName, invoker.invokedAgentName)
	assert.Equal(t, creator, invoker.req.AgentCreator)
	assert.Equal(t, authorizer, invoker.req.CallerAuthorizer)
	assert.Equal(t, sandboxDir, invoker.req.CallerSandboxDir)
	assert.Equal(t, sandboxDir, invoker.req.ToolOptions.SandboxDir)
	assert.Equal(t, packageDir, invoker.req.ToolOptions.GoPkgAbsDir)
	assert.Equal(t, llmmodel.ModelID("mock-model"), invoker.req.ToolOptions.Model)
	assert.Equal(t, lintSteps, invoker.req.ToolOptions.LintSteps)
	assert.Equal(t, invoker, invoker.req.ToolOptions.AgentInvoker)
	require.Len(t, invoker.req.Messages, 1)
	assert.Equal(t, "Update the exported API safely.", invoker.req.Messages[0])
}

func TestInvokeChangeAPIAgent_RequiresInvoker(t *testing.T) {
	_, err := invokeChangeAPIAgent(
		context.Background(),
		nil,
		fakeAgentCreator{},
		t.TempDir(),
		nil,
		t.TempDir(),
		"",
		nil,
		nil,
		"Update it.",
	)
	assert.EqualError(t, err, "change_api agent unavailable")
}

func dummyPackageToolset() toolsetinterface.Toolset {
	return func(_ toolsetinterface.Options) ([]llmstream.Tool, error) {
		return nil, nil
	}
}

func withUpstreamFixture(t *testing.T, f func(*gocode.Package)) {
	t.Helper()

	gocodetesting.WithMultiCode(t, map[string]string{
		"mypkg.go": gocodetesting.Dedent(`
			package mypkg

			import "mymodule/upstream"

			func UseUpstream() string {
				return upstream.Hello()
			}
		`),
	}, func(pkg *gocode.Package) {
		t.Helper()

		err := gocodetesting.AddPackage(t, pkg.Module, "upstream", map[string]string{
			"upstream.go": gocodetesting.Dedent(`
				package upstream

				// Hello returns a greeting.
				func Hello() string { return "hi" }
			`),
		})
		require.NoError(t, err)

		err = gocodetesting.AddPackage(t, pkg.Module, "otherpkg", map[string]string{
			"otherpkg.go": gocodetesting.Dedent(`
				package otherpkg

				func Ok() bool { return true }
			`),
		})
		require.NoError(t, err)

		// Sanity check: current package dir is inside the module, since tests depend on path math.
		rel, err := filepath.Rel(pkg.Module.AbsolutePath, pkg.AbsolutePath())
		require.NoError(t, err)
		require.False(t, rel == ".." || filepath.IsAbs(rel))

		f(pkg)
	})
}
