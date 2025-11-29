package exttools

import (
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"github.com/codalotl/codalotl/internal/llmstream"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDiagnostics_Run_SuccessfulBuild(t *testing.T) {
	gocodetesting.WithMultiCode(t, map[string]string{
		"main.go": dedent(`
			package main

			func main() {
				println("hello")
			}
		`),
	}, func(pkg *gocode.Package) {
		tool := NewDiagnosticsTool(pkg.Module.AbsolutePath, nil)
		call := llmstream.ToolCall{
			CallID: "call1",
			Name:   ToolNameDiagnostics,
			Type:   "function_call",
			Input:  `{"path":"mypkg"}`,
		}

		res := tool.Run(context.Background(), call)

		assert.False(t, res.IsError)
		assert.Nil(t, res.SourceErr)
		assert.Equal(t, "<diagnostics-status ok=\"true\" message=\"build succeeded\">\n$ go build -o /dev/null ./mypkg\n</diagnostics-status>", res.Result)
	})
}

func TestDiagnostics_Run_BuildError(t *testing.T) {
	gocodetesting.WithMultiCode(t, map[string]string{
		"main.go": dedent(`
			package main

			func foo() {
				return "hello" // cannot return value from function with no return type
			}

			func main() {
				foo()
			}
		`),
	}, func(pkg *gocode.Package) {
		tool := NewDiagnosticsTool(pkg.Module.AbsolutePath, nil)
		call := llmstream.ToolCall{
			CallID: "call2",
			Name:   ToolNameDiagnostics,
			Type:   "function_call",
			Input:  `{"path":"mypkg"}`,
		}

		res := tool.Run(context.Background(), call)
		assert.False(t, res.IsError)
		assert.Nil(t, res.SourceErr)
		assert.Equal(t, "<diagnostics-status ok=\"false\">\n$ go build -o /dev/null ./mypkg\n# mymodule/mypkg\nmypkg/main.go:4:9: too many return values\n\thave (string)\n\twant ()\n</diagnostics-status>", res.Result)
	})
}
