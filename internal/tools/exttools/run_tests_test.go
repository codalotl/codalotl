package exttools

import (
	"context"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRunTests_Run_VerboseSingleTest(t *testing.T) {
	gocodetesting.WithMultiCode(t, map[string]string{
		"main.go": gocodetesting.Dedent(`
			package mypkg

			func sum(a, b int) int {
				return a + b
			}
		`),
		"main_test.go": gocodetesting.Dedent(`
			package mypkg

			import "testing"

			func TestOnly(t *testing.T) {
				if sum(2, 3) != 5 {
					t.Fatalf("sum should be 5")
				}
			}

			func TestOther(t *testing.T) {
				if sum(1, 1) != 2 {
					t.Fatalf("sum should be 2")
				}
			}
		`),
	}, func(pkg *gocode.Package) {
		auth := authdomain.NewAutoApproveAuthorizer(pkg.Module.AbsolutePath)
		tool := NewRunTestsTool(auth)
		call := llmstream.ToolCall{
			CallID: "call1",
			Name:   ToolNameRunTests,
			Type:   "function_call",
			Input:  `{"path":"mypkg","test_name":"TestOnly","verbose":true}`,
		}

		res := tool.Run(context.Background(), call)
		assert.False(t, res.IsError)
		assert.Nil(t, res.SourceErr)
		assert.Contains(t, res.Result, `<test-status ok="true">`)
		assert.Contains(t, res.Result, "$ go test -v -run TestOnly ./mypkg")
		assert.Contains(t, res.Result, "=== RUN   TestOnly")
		assert.NotContains(t, res.Result, "TestOther")
		assert.Contains(t, res.Result, "PASS")
		assert.Contains(t, res.Result, "</test-status>")
	})
}
