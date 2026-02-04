package pkgtools

import (
	"context"
	"testing"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetUsage_Info_UsesDefiningPackagePath(t *testing.T) {
	auth := authdomain.NewAutoApproveAuthorizer(t.TempDir())
	tool := NewGetUsageTool(auth)

	info := tool.Info()
	_, ok := info.Parameters["defining_package_path"]
	assert.True(t, ok)
	_, ok = info.Parameters["defining_package"]
	assert.False(t, ok)
	assert.Contains(t, info.Required, "defining_package_path")
}

func TestGetUsage_Run_RejectsOldParamName(t *testing.T) {
	withSimplePackage(t, func(pkg *gocode.Package) {
		auth := authdomain.NewAutoApproveAuthorizer(pkg.Module.AbsolutePath)
		tool := NewGetUsageTool(auth)

		res := tool.Run(context.Background(), llmstream.ToolCall{
			CallID: "call-old-param",
			Name:   ToolNameGetUsage,
			Type:   "function_call",
			Input:  `{"defining_package":"mymodule/mypkg","identifier":"Hello"}`,
		})

		assert.True(t, res.IsError)
		assert.Contains(t, res.Result, "defining_package_path is required")
	})
}

func TestGetUsage_Run_UsesDefiningPackagePath(t *testing.T) {
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
		tool := NewGetUsageTool(auth)

		res := tool.Run(context.Background(), llmstream.ToolCall{
			CallID: "call-new-param",
			Name:   ToolNameGetUsage,
			Type:   "function_call",
			Input:  `{"defining_package_path":"mymodule/mypkg","identifier":"Hello"}`,
		})

		assert.False(t, res.IsError)
		assert.Nil(t, res.SourceErr)
		assert.NotEmpty(t, res.Result)
	})
}

func TestGetUsage_Run_StdlibImport(t *testing.T) {
	withSimplePackage(t, func(pkg *gocode.Package) {
		auth := authdomain.NewAutoApproveAuthorizer(pkg.Module.AbsolutePath)
		tool := NewGetUsageTool(auth)

		res := tool.Run(context.Background(), llmstream.ToolCall{
			CallID: "call-stdlib",
			Name:   ToolNameGetUsage,
			Type:   "function_call",
			Input:  `{"defining_package_path":"fmt","identifier":"Printf"}`,
		})

		assert.False(t, res.IsError)
		assert.Nil(t, res.SourceErr)
		assert.NotEmpty(t, res.Result)
	})
}

func TestGetUsage_Run_UnresolvedImport(t *testing.T) {
	withSimplePackage(t, func(pkg *gocode.Package) {
		auth := authdomain.NewAutoApproveAuthorizer(pkg.Module.AbsolutePath)
		tool := NewGetUsageTool(auth)

		res := tool.Run(context.Background(), llmstream.ToolCall{
			CallID: "call-unresolved",
			Name:   ToolNameGetUsage,
			Type:   "function_call",
			Input:  `{"defining_package_path":"github.com/other/module","identifier":"Hello"}`,
		})

		assert.True(t, res.IsError)
		assert.NotNil(t, res.SourceErr)
		assert.Contains(t, res.Result, "could not be resolved")
	})
}
