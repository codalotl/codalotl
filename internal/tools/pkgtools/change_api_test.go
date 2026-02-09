package pkgtools

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChangeAPI_MissingImportPath(t *testing.T) {
	withUpstreamFixture(t, func(pkg *gocode.Package) {
		auth := authdomain.NewAutoApproveAuthorizer(pkg.Module.AbsolutePath)
		tool := NewChangeAPITool(pkg.AbsolutePath(), auth, dummyPackageToolset())

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
		tool := NewChangeAPITool(pkg.AbsolutePath(), auth, dummyPackageToolset())

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
		tool := NewChangeAPITool(pkg.AbsolutePath(), auth, dummyPackageToolset())

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
		tool := NewChangeAPITool(pkg.AbsolutePath(), auth, dummyPackageToolset())

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
		tool := NewChangeAPITool(pkg.AbsolutePath(), auth, dummyPackageToolset())

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
