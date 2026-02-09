package exttools

import (
	"context"
	"fmt"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFixLints_Run_FixesFormatting(t *testing.T) {
	gocodetesting.WithMultiCode(t, map[string]string{
		"main.go": gocodetesting.Dedent(`
			package mypkg

			import "fmt"
			func main(){
				fmt.Println("hi")
			}
		`),
	}, func(pkg *gocode.Package) {
		auth := authdomain.NewAutoApproveAuthorizer(pkg.Module.AbsolutePath)
		tool := NewFixLintsTool(auth, nil)
		call := llmstream.ToolCall{
			CallID: "call1",
			Name:   ToolNameFixLints,
			Type:   "function_call",
			Input:  `{"path":"mypkg"}`,
		}

		res := tool.Run(context.Background(), call)
		assert.False(t, res.IsError)
		assert.Nil(t, res.SourceErr)

		pkgDir := filepath.Join(pkg.Module.AbsolutePath, "mypkg")
		expectedOutput := fmt.Sprintf(
			"<lint-status ok=\"true\" mode=\"fix\">\n$ gofmt -l -w mypkg\n%s\n</lint-status>",
			filepath.Join("mypkg", "main.go"),
		)
		assert.Equal(t, expectedOutput, res.Result)

		formatted, readErr := os.ReadFile(filepath.Join(pkgDir, "main.go"))
		require.NoError(t, readErr)
		expected := "package mypkg\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n"
		assert.Equal(t, expected, string(formatted))
	})
}

func TestFixLints_Run_NoChangesNeeded(t *testing.T) {
	gocodetesting.WithMultiCode(t, map[string]string{
		"main.go": gocodetesting.Dedent(`
			package mypkg

			func main() {}
		`),
	}, func(pkg *gocode.Package) {
		auth := authdomain.NewAutoApproveAuthorizer(pkg.Module.AbsolutePath)
		tool := NewFixLintsTool(auth, nil)
		input := fmt.Sprintf(`{"path":%q}`, filepath.Join("mypkg", "main.go"))
		call := llmstream.ToolCall{
			CallID: "call2",
			Name:   ToolNameFixLints,
			Type:   "function_call",
			Input:  input,
		}

		res := tool.Run(context.Background(), call)
		assert.False(t, res.IsError)
		assert.Nil(t, res.SourceErr)

		pkgDir := filepath.Join(pkg.Module.AbsolutePath, "mypkg")
		expectedOutput := "<lint-status ok=\"true\" message=\"no issues found\" mode=\"fix\">\n$ gofmt -l -w mypkg\n</lint-status>"
		assert.Equal(t, expectedOutput, res.Result)

		contents, readErr := os.ReadFile(filepath.Join(pkgDir, "main.go"))
		require.NoError(t, readErr)
		expected := "package mypkg\n\nfunc main() {}\n"
		assert.Equal(t, expected, string(contents))
	})
}

func TestCheckLints_NoIssues(t *testing.T) {
	gocodetesting.WithMultiCode(t, map[string]string{
		"main.go": gocodetesting.Dedent(`
			package mypkg

			func main() {}
		`),
	}, func(pkg *gocode.Package) {
		pkgDir := filepath.Join(pkg.Module.AbsolutePath, "mypkg")
		output, err := CheckLints(context.Background(), pkg.Module.AbsolutePath, pkgDir, nil)
		require.NoError(t, err)

		expectedOutput := "<lint-status ok=\"true\" message=\"no issues found\" mode=\"check\">\n$ gofmt -l mypkg\n</lint-status>"
		assert.Equal(t, expectedOutput, output)
	})
}

func TestCheckLints_FindsIssues(t *testing.T) {
	gocodetesting.WithMultiCode(t, map[string]string{
		"main.go": gocodetesting.Dedent(`
			package mypkg

			func main(){
			}
		`),
	}, func(pkg *gocode.Package) {
		pkgDir := filepath.Join(pkg.Module.AbsolutePath, "mypkg")
		filePath := filepath.Join(pkgDir, "main.go")
		before, readErr := os.ReadFile(filePath)
		require.NoError(t, readErr)

		output, err := CheckLints(context.Background(), pkg.Module.AbsolutePath, pkgDir, nil)
		require.NoError(t, err)

		expectedOutput := "<lint-status ok=\"false\" mode=\"check\">\n$ gofmt -l mypkg\nmypkg/main.go\n</lint-status>"
		assert.Equal(t, expectedOutput, output)

		after, readAgainErr := os.ReadFile(filePath)
		require.NoError(t, readAgainErr)
		assert.Equal(t, string(before), string(after))
	})
}
