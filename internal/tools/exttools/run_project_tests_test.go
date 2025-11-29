package exttools

import (
	"context"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"github.com/codalotl/codalotl/internal/llmstream"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRunProjectTests_SimpleModule(t *testing.T) {
	gocodetesting.WithMultiCode(t, map[string]string{
		"main.go": gocodetesting.Dedent(`
			package mypkg

			func add(a, b int) int { return a + b }
		`),
		"main_test.go": gocodetesting.Dedent(`
			package mypkg
			import "testing"
			func TestAdd(t *testing.T) {
				if add(2, 3) != 5 {
					t.Fatalf("want 5")
				}
			}
		`),
	}, func(pkg *gocode.Package) {
		tool := NewRunProjectTestsTool(pkg.Module.AbsolutePath, pkg.AbsolutePath(), nil)
		call := llmstream.ToolCall{
			CallID: "call1",
			Name:   ToolNameRunProjectTests,
			Type:   "function_call",
			Input:  `{}`,
		}

		res := tool.Run(context.Background(), call)
		assert.False(t, res.IsError)
		assert.Nil(t, res.SourceErr)
		assert.Contains(t, res.Result, `<test-status ok="true">`)
		assert.Contains(t, res.Result, "$ go test ./...")
		// Non-verbose go test output should include an "ok <importpath>" line.
		assert.Contains(t, res.Result, "ok  \tmymodule/mypkg")
		assert.Contains(t, res.Result, "</test-status>")
	})
}
