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
		tool := NewUpdateUsageTool(pkg.AbsolutePath(), auth, dummyPackageToolset())

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
		tool := NewUpdateUsageTool(pkg.AbsolutePath(), auth, dummyPackageToolset())

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
