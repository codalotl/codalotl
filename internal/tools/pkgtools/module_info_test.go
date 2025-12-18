package pkgtools

import (
	"context"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/llmstream"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModuleInfo_RunDefault(t *testing.T) {
	withSimplePackage(t, func(pkg *gocode.Package) {
		tool := NewModuleInfoTool(pkg.Module.AbsolutePath, nil)
		call := llmstream.ToolCall{
			CallID: "call-module-info",
			Name:   ToolNameModuleInfo,
			Type:   "function_call",
			Input:  `{}`,
		}

		res := tool.Run(context.Background(), call)
		assert.False(t, res.IsError)
		assert.Nil(t, res.SourceErr)

		// From go.mod (created by gocodetesting.WithMultiCode).
		assert.Contains(t, res.Result, "module mymodule")

		// From package list.
		assert.Contains(t, res.Result, "- mymodule/mypkg")
	})
}

func TestModuleInfo_RunSearchFilter(t *testing.T) {
	withSimplePackage(t, func(pkg *gocode.Package) {
		tool := NewModuleInfoTool(pkg.Module.AbsolutePath, nil)
		call := llmstream.ToolCall{
			CallID: "call-module-info-search",
			Name:   ToolNameModuleInfo,
			Type:   "function_call",
			Input:  `{"package_search":"^mymodule/mypkg$"}`,
		}

		res := tool.Run(context.Background(), call)
		assert.False(t, res.IsError)
		assert.Nil(t, res.SourceErr)
		assert.Contains(t, res.Result, "- mymodule/mypkg")
	})
}

func TestModuleInfo_RunIncludeDepPackages_NoDepsIsOK(t *testing.T) {
	// The fixture module has no explicit direct deps; this test just asserts the flag is accepted.
	withSimplePackage(t, func(pkg *gocode.Package) {
		tool := NewModuleInfoTool(pkg.Module.AbsolutePath, nil)
		call := llmstream.ToolCall{
			CallID: "call-module-info-deps",
			Name:   ToolNameModuleInfo,
			Type:   "function_call",
			Input:  `{"include_dependency_packages":true}`,
		}

		res := tool.Run(context.Background(), call)
		assert.False(t, res.IsError)
		assert.Nil(t, res.SourceErr)
		assert.Contains(t, res.Result, "- mymodule/mypkg")
	})
}

func TestModuleInfo_RunEmptyInputIsOK(t *testing.T) {
	withSimplePackage(t, func(pkg *gocode.Package) {
		tool := NewModuleInfoTool(pkg.Module.AbsolutePath, nil)
		call := llmstream.ToolCall{
			CallID: "call-module-info-empty",
			Name:   ToolNameModuleInfo,
			Type:   "function_call",
			Input:  "",
		}

		res := tool.Run(context.Background(), call)
		assert.False(t, res.IsError)
		assert.Nil(t, res.SourceErr)
		assert.Contains(t, res.Result, "module mymodule")
	})
}

func TestModuleInfo_RunSearchNoMatches(t *testing.T) {
	withSimplePackage(t, func(pkg *gocode.Package) {
		tool := NewModuleInfoTool(pkg.Module.AbsolutePath, nil)
		call := llmstream.ToolCall{
			CallID: "call-module-info-no-matches",
			Name:   ToolNameModuleInfo,
			Type:   "function_call",
			Input:  `{"package_search":"doesnotmatch"}`,
		}

		res := tool.Run(context.Background(), call)
		assert.False(t, res.IsError)
		assert.Nil(t, res.SourceErr)
		assert.Contains(t, res.Result, "(no matching packages)")
	})
}
