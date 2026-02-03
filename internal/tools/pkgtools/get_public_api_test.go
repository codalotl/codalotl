package pkgtools

import (
	"context"
	"fmt"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetPublicAPI_RunRelativeImport(t *testing.T) {
	withSimplePackage(t, func(pkg *gocode.Package) {
		auth := authdomain.NewAutoApproveAuthorizer(pkg.Module.AbsolutePath)
		tool := NewGetPublicAPITool(auth)
		call := llmstream.ToolCall{
			CallID: "call-relative",
			Name:   ToolNameGetPublicAPI,
			Type:   "function_call",
			Input:  `{"path":"mypkg"}`,
		}

		res := tool.Run(context.Background(), call)
		assert.False(t, res.IsError)
		assert.Nil(t, res.SourceErr)
		assert.Contains(t, res.Result, "Package mypkg demonstrates documentation output.")
		assert.Contains(t, res.Result, "func Hello() string")
	})
}

func TestGetPublicAPI_RunQualifiedImport(t *testing.T) {
	withSimplePackage(t, func(pkg *gocode.Package) {
		auth := authdomain.NewAutoApproveAuthorizer(pkg.Module.AbsolutePath)
		tool := NewGetPublicAPITool(auth)
		call := llmstream.ToolCall{
			CallID: "call-qualified",
			Name:   ToolNameGetPublicAPI,
			Type:   "function_call",
			Input:  fmt.Sprintf(`{"path":%q}`, pkg.Module.Name+"/mypkg"),
		}

		res := tool.Run(context.Background(), call)
		assert.False(t, res.IsError)
		assert.Nil(t, res.SourceErr)
		assert.Contains(t, res.Result, "Package mypkg demonstrates documentation output.")
		assert.Contains(t, res.Result, "func Hello() string")
	})
}

func TestGetPublicAPI_RunInvalidOutsideModule(t *testing.T) {
	withSimplePackage(t, func(pkg *gocode.Package) {
		auth := authdomain.NewAutoApproveAuthorizer(pkg.Module.AbsolutePath)
		tool := NewGetPublicAPITool(auth)
		call := llmstream.ToolCall{
			CallID: "call-invalid",
			Name:   ToolNameGetPublicAPI,
			Type:   "function_call",
			Input:  `{"path":"github.com/other/module"}`,
		}

		res := tool.Run(context.Background(), call)
		assert.True(t, res.IsError)
		assert.NotNil(t, res.SourceErr)
		assert.Contains(t, res.Result, "could not be resolved")
	})
}

func TestGetPublicAPI_RunStdlibImport(t *testing.T) {
	withSimplePackage(t, func(pkg *gocode.Package) {
		auth := authdomain.NewAutoApproveAuthorizer(pkg.Module.AbsolutePath)
		tool := NewGetPublicAPITool(auth)
		call := llmstream.ToolCall{
			CallID: "call-stdlib",
			Name:   ToolNameGetPublicAPI,
			Type:   "function_call",
			Input:  `{"path":"fmt","identifiers":["Printf"]}`,
		}

		res := tool.Run(context.Background(), call)
		assert.False(t, res.IsError)
		assert.Nil(t, res.SourceErr)
		assert.Contains(t, res.Result, "func Printf(")
	})
}

func TestGetPublicAPI_RunDependencyImport(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	assert.True(t, ok)

	mod, err := gocode.NewModule(thisFile)
	if !assert.NoError(t, err) {
		return
	}

	auth := authdomain.NewAutoApproveAuthorizer(mod.AbsolutePath)
	tool := NewGetPublicAPITool(auth)
	call := llmstream.ToolCall{
		CallID: "call-dep",
		Name:   ToolNameGetPublicAPI,
		Type:   "function_call",
		Input:  `{"path":"github.com/stretchr/testify/assert","identifiers":["Equal"]}`,
	}

	res := tool.Run(context.Background(), call)
	assert.False(t, res.IsError)
	assert.Nil(t, res.SourceErr)
	assert.Contains(t, res.Result, "func Equal(")
}

func withSimplePackage(t *testing.T, f func(*gocode.Package)) {
	t.Helper()

	gocodetesting.WithMultiCode(t, map[string]string{
		"doc.go": gocodetesting.Dedent(`
			// Package mypkg demonstrates documentation output.
			package mypkg
		`),
		"mypkg.go": gocodetesting.Dedent(`
			package mypkg

			// Hello returns a friendly greeting.
			func Hello() string {
				return "hello"
			}
		`),
	}, f)
}
