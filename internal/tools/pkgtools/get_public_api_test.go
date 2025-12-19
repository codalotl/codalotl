package pkgtools

import (
	"context"
	"fmt"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
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
			Input:  `{"import_path":"mypkg"}`,
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
			Input:  fmt.Sprintf(`{"import_path":%q}`, pkg.Module.Name+"/mypkg"),
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
			Input:  `{"import_path":"github.com/other/module"}`,
		}

		res := tool.Run(context.Background(), call)
		assert.True(t, res.IsError)
		assert.NotNil(t, res.SourceErr)
		assert.Contains(t, res.Result, "package directory does not exist")
	})
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
