package updatedocs

import (
	"go/ast"
	"go/parser"
	"testing"
)

func TestTypesSameShape(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		snippet  string
		expected bool
	}{
		// Basic type comparisons
		{
			name:     "same basic type",
			source:   "int",
			snippet:  "int",
			expected: true,
		},
		{
			name:     "different basic types",
			source:   "int",
			snippet:  "int64",
			expected: false,
		},
		{
			name:     "basic type vs struct",
			source:   "int",
			snippet:  "struct{}",
			expected: false,
		},
		{
			name:     "basic type vs type alias",
			source:   "int",
			snippet:  "myIntType",
			expected: false,
		},

		// Pointer type comparisons
		{
			name:     "different pointer element types",
			source:   "*int",
			snippet:  "*string",
			expected: false,
		},

		// Selector expression comparisons
		{
			name:     "different selector expressions",
			source:   "pkg1.T",
			snippet:  "pkg2.U",
			expected: false,
		},

		// Struct type comparisons
		{
			name:     "empty structs",
			source:   "struct{}",
			snippet:  "struct{}",
			expected: true,
		},
		{
			name:     "identical structs",
			source:   "struct{Foo int; Bar string}",
			snippet:  "struct{Foo int; Bar string}",
			expected: true,
		},
		{
			name:     "subset struct fields",
			source:   "struct{Foo int; Bar string}",
			snippet:  "struct{Bar string}",
			expected: true,
		},
		{
			name:     "empty struct subset",
			source:   "struct{Foo int; Bar string}",
			snippet:  "struct{}",
			expected: true,
		},
		{
			name:     "extra field in snippet",
			source:   "struct{Foo int; Bar string}",
			snippet:  "struct{Baz int}",
			expected: false,
		},
		{
			name:     "field type mismatch",
			source:   "struct{Foo int; Bar string}",
			snippet:  "struct{Foo string}",
			expected: false,
		},
		{
			name:     "field reordering",
			source:   "struct{Foo int; Bar string}",
			snippet:  "struct{Bar string; Foo int}",
			expected: true,
		},
		{
			name:     "too many fields in snippet",
			source:   "struct{Foo int; Bar string}",
			snippet:  "struct{Foo int; Bar string; Baz int}",
			expected: false,
		},
		{
			name:     "nested structs",
			source:   "struct{Foo struct{Bar string; Baz string}}",
			snippet:  "struct{Foo struct{Bar string}}",
			expected: true,
		},
		{
			name:     "struct vs type alias",
			source:   "struct{Foo int}",
			snippet:  "Bar",
			expected: false,
		},
		{
			name:     "snippet adds embedded struct field",
			source:   "struct{}",
			snippet:  "struct{T}",
			expected: false,
		},
		{
			name:     "identical embedded struct fields",
			source:   "struct{T}",
			snippet:  "struct{T}",
			expected: true,
		},
		{
			name:     "struct field element type differs via pointer",
			source:   "struct{Foo *int}",
			snippet:  "struct{Foo *string}",
			expected: false,
		},
		{
			name:     "struct multi identifier per field - exact match",
			source:   "struct{Foo, Bar int}",
			snippet:  "struct{Foo, Bar int}",
			expected: true,
		},
		{
			name:     "struct multi identifier per field - subset not ok",
			source:   "struct{Foo, Bar int}",
			snippet:  "struct{Foo int}",
			expected: false,
		},
		{
			name:     "struct multi identifier per field - subset not ok and mismatch type not ok",
			source:   "struct{Foo, Bar int}",
			snippet:  "struct{Foo string}",
			expected: false,
		},
		{
			name:     "struct multi identifier per field - mismatch type not ok",
			source:   "struct{Foo, Bar int}",
			snippet:  "struct{Foo, Bar string}",
			expected: false,
		},
		{
			name:     "struct multi identifier per field - superset not ok",
			source:   "struct{Foo, Bar int}",
			snippet:  "struct{Foo, Bar, Baz int}",
			expected: false,
		},

		// Interface type comparisons
		{
			name:     "empty interfaces",
			source:   "interface{}",
			snippet:  "interface{}",
			expected: true,
		},
		{
			name:     "empty interface subset",
			source:   "interface{Foo()}",
			snippet:  "interface{}",
			expected: true,
		},
		{
			name:     "identical interface methods",
			source:   "interface{Foo()}",
			snippet:  "interface{Foo()}",
			expected: true,
		},
		{
			name:     "subset interface methods",
			source:   "interface{Foo(); Bar()}",
			snippet:  "interface{Foo()}",
			expected: true,
		},
		{
			name:     "extra method in snippet",
			source:   "interface{Foo()}",
			snippet:  "interface{Foo(); Bar()}",
			expected: false,
		},
		{
			name:     "method parameter match",
			source:   "interface{Foo(int)}",
			snippet:  "interface{Foo(int)}",
			expected: true,
		},
		{
			name:     "method parameter mismatch",
			source:   "interface{Foo(int)}",
			snippet:  "interface{Foo(string)}",
			expected: false,
		},
		{
			name:     "method parameter count mismatch",
			source:   "interface{Foo(int)}",
			snippet:  "interface{Foo(int, string)}",
			expected: false,
		},
		{
			name:     "method parameter count mismatch reverse",
			source:   "interface{Foo(int, string)}",
			snippet:  "interface{Foo(int)}",
			expected: false,
		},
		{
			name:     "snippet adds embedded interface",
			source:   "interface{}",
			snippet:  "interface{I}",
			expected: false,
		},
		{
			name:     "identical embedded interfaces",
			source:   "interface{I}",
			snippet:  "interface{I}",
			expected: true,
		},

		// Array type comparisons
		{
			name:     "identical array types",
			source:   "[5]int",
			snippet:  "[5]int",
			expected: true,
		},
		{
			name:     "array element type mismatch",
			source:   "[5]int",
			snippet:  "[5]string",
			expected: false,
		},
		{
			name:     "array length mismatch",
			source:   "[5]int",
			snippet:  "[10]int",
			expected: false,
		},
		{
			name:     "slice vs array",
			source:   "[]int",
			snippet:  "[5]int",
			expected: false,
		},
		{
			name:     "array with const",
			source:   "[n]int",
			snippet:  "[n]int",
			expected: true,
		},
		{
			name:     "array with const arrithmatic",
			source:   "[n+1]int",
			snippet:  "[n+1]int",
			expected: true,
		},
		{
			name:     "array with const mismatch",
			source:   "[n]int",
			snippet:  "[k]int",
			expected: false,
		},
		{
			name:     "array length binary expr mismatch",
			source:   "[n+1]int",
			snippet:  "[n+2]int",
			expected: false,
		},
		{
			name:     "array length selector equality",
			source:   "[pkg.N]int",
			snippet:  "[pkg.N]int",
			expected: true,
		},
		{
			name:     "array length call expr mismatch",
			source:   "[len(a)]int",
			snippet:  "[len(b)]int",
			expected: false,
		},

		// Map type comparisons
		{
			name:     "identical map types",
			source:   "map[int]string",
			snippet:  "map[int]string",
			expected: true,
		},
		{
			name:     "map key type mismatch",
			source:   "map[int]string",
			snippet:  "map[string]string",
			expected: false,
		},
		{
			name:     "map value type mismatch",
			source:   "map[int]string",
			snippet:  "map[int]int",
			expected: false,
		},

		// Channel type comparisons
		{
			name:     "identical channel types",
			source:   "chan int",
			snippet:  "chan int",
			expected: true,
		},
		{
			name:     "channel direction mismatch",
			source:   "chan<- int",
			snippet:  "<-chan int",
			expected: false,
		},
		{
			name:     "channel element type mismatch",
			source:   "chan int",
			snippet:  "chan string",
			expected: false,
		},

		// Function type comparisons
		{
			name:     "identical function types",
			source:   "func(int) string",
			snippet:  "func(int) string",
			expected: true,
		},
		{
			name:     "function parameter mismatch",
			source:   "func(int) string",
			snippet:  "func(string) string",
			expected: false,
		},
		{
			name:     "function result mismatch",
			source:   "func(int) string",
			snippet:  "func(int) int",
			expected: false,
		},
		{
			name:     "function parameter count mismatch",
			source:   "func(int, string) string",
			snippet:  "func(int) string",
			expected: false,
		},
		{
			name:     "function result count mismatch",
			source:   "func(int) (string, error)",
			snippet:  "func(int) string",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourceExpr, err := parseExpr(tt.source)
			if err != nil {
				t.Fatalf("failed to parse source: %v", err)
			}

			snippetExpr, err := parseExpr(tt.snippet)
			if err != nil {
				t.Fatalf("failed to parse snippet: %v", err)
			}

			got := typesSameShape(sourceExpr, snippetExpr)
			if got != tt.expected {
				t.Errorf("typesSameShape() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// parseExpr parses a Go expression string into an ast.Expr.
func parseExpr(expr string) (ast.Expr, error) {
	exprAst, err := parser.ParseExpr(expr)
	if err != nil {
		return nil, err
	}
	return exprAst, nil
}
