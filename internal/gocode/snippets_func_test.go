package gocode

import (
	"fmt"
	"go/token"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsTestFunc(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		fileName string
		expected bool
	}{
		{
			name:     "regular function in non-test file",
			source:   `func DoSomething() {}`,
			fileName: "main.go",
			expected: false,
		},
		{
			name:     "test function with correct signature",
			source:   `import "testing"; func TestSomething(t *testing.T) {}`,
			fileName: "main_test.go",
			expected: true,
		},
		{
			name:     "test function with return",
			source:   `import "testing"; func TestSomething(t *testing.T) bool {return true}`,
			fileName: "main_test.go",
			expected: false,
		},
		{
			name:     "test function with lowercase after Test",
			source:   `import "testing"; func Testsomething(t *testing.T) {}`,
			fileName: "main_test.go",
			expected: false,
		},
		{
			name:     "test function with wrong signature no params",
			source:   `func TestSomething() {}`,
			fileName: "main_test.go",
			expected: false,
		},
		{
			name:     "test function with wrong signature wrong type",
			source:   `func TestSomething(x string) {}`,
			fileName: "main_test.go",
			expected: false,
		},
		{
			name:     "test function with multiple params",
			source:   `import "testing"; func TestSomething(t *testing.T, x string) {}`,
			fileName: "main_test.go",
			expected: false,
		},
		{
			name:     "test function with non-pointer testing.T",
			source:   `import "testing"; func TestSomething(t testing.T) {}`,
			fileName: "main_test.go",
			expected: false,
		},
		{
			name:     "benchmark function",
			source:   `import "testing"; func BenchmarkSort(b *testing.B) {}`,
			fileName: "sort_test.go",
			expected: true,
		},
		{
			name:     "benchmark with lowercase after Benchmark",
			source:   `import "testing"; func Benchmarksort(b *testing.B) {}`,
			fileName: "sort_test.go",
			expected: false,
		},
		{
			name:     "benchmark with wrong param type",
			source:   `import "testing"; func BenchmarkSort(t *testing.T) {}`,
			fileName: "sort_test.go",
			expected: false,
		},
		{
			name:     "example function",
			source:   `func ExampleSort() {}`,
			fileName: "example_test.go",
			expected: true,
		},
		{
			name:     "example function with underscore",
			source:   `func Example_output() {}`,
			fileName: "example_test.go",
			expected: true,
		},
		{
			name: "example function invalid: has params",
			source: dedent(`
				func ExamplePrint(x int) {
					// Output: hello
				}`),
			fileName: "example_test.go",
			expected: false,
		},
		{
			name:     "example function invalid: has return value",
			source:   `func ExamplePrint() int { return 1 }`,
			fileName: "example_test.go",
			expected: false,
		},
		{
			name:     "fuzz function",
			source:   `import "testing"; func FuzzParse(f *testing.F) {}`,
			fileName: "parse_test.go",
			expected: true,
		},
		{
			name:     "fuzz with lowercase after Fuzz",
			source:   `import "testing"; func Fuzzparse(f *testing.F) {}`,
			fileName: "parse_test.go",
			expected: false,
		},
		{
			name: "method with test name",
			source: dedent(`
				import "testing"
				type MyType struct{}
				func (m *MyType) TestMethod(t *testing.T) {}`),
			fileName: "mytype_test.go",
			expected: false,
		},
		{
			name:     "test function in non-test file",
			source:   `import "testing"; func TestSomething(t *testing.T) {}`,
			fileName: "main.go",
			expected: false,
		},
		{
			name:     "test with aliased import",
			source:   `import t "testing"; func TestSomething(test *t.T) {}`,
			fileName: "main_test.go",
			expected: false,
		},
		{
			name: "test with custom testing type",
			source: dedent(`
				type testing struct{}
				type T struct{}
				func TestSomething(t *testing.T) {}`),
			fileName: "main_test.go",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Wrap source in package declaration
			fullSource := fmt.Sprintf("package main\n%s", tt.source)

			// Create a File with the test source
			goFile := &File{
				FileName:         tt.fileName,
				RelativeFileName: tt.fileName,
				AbsolutePath:     "/tmp/" + tt.fileName,
				Contents:         []byte(fullSource),
				PackageName:      "main",
				IsTest:           false, // Will be set by extractSnippets based on filename
			}

			// Parse the file to create AST
			fset := token.NewFileSet()
			_, err := goFile.Parse(fset)
			require.NoError(t, err)

			// Extract snippets
			funcSnippets, _, _, _, err := extractSnippets(goFile)
			require.NoError(t, err)

			// Should have exactly one function
			require.Len(t, funcSnippets, 1, "expected exactly one function")

			// Test IsTestFunc
			result := funcSnippets[0].IsTestFunc()
			assert.Equal(t, tt.expected, result)
		})
	}
}
