package gograph

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWeaklyConnectedComponents(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		expected [][]string
	}{
		{
			name:     "empty package",
			src:      "package testpkg",
			expected: nil, // Expect nil for no nodes
		},
		{
			name: "all separate",
			src: dedent(`
				type A struct{}
				type B struct{}
				var C = 1
			`),
			expected: [][]string{{"A"}, {"B"}, {"C"}},
		},
		{
			name: "simple chain",
			src: dedent(`
				type A struct{ F B }
				type B struct{ F C }
				type C struct{}
			`),
			expected: [][]string{{"A", "B", "C"}},
		},
		{
			name: "multiple components",
			src: dedent(`
				type A struct { F B }
				type B struct {}

				type C struct { F D }
				type D struct {}

				type E struct{}
			`),
			expected: [][]string{{"A", "B"}, {"C", "D"}, {"E"}},
		},
		{
			name: "with a cycle",
			src: dedent(`
				type A struct { F B }
				type B struct { F A }
				type C struct {}
			`),
			expected: [][]string{{"A", "B"}, {"C"}},
		},
		{
			name: "complex graph",
			src: dedent(`
				type A struct { F B }
				type B struct { F C }
				type C struct {}

				type D struct { F E }
				type E struct { F D }

				type F struct {}
				var G F
				`),
			expected: [][]string{{"A", "B", "C"}, {"D", "E"}, {"F", "G"}},
		},
		{
			name: "method",
			src: dedent(`
				type A struct {}
				func (a *A) Foo() {}
				`),
			expected: [][]string{{"A", "*A.Foo"}},
		},
		{
			name: "anonymous identifiers",
			src: dedent(`
				var _, x = foo()
				var y, _ = foo()
				func foo() (int, int) {return 1, 2}
				type Z struct{}
			`),
			expected: [][]string{{"_:test.go:3:5", "_:test.go:4:8", "foo", "x", "y"}, {"Z"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := newTestPackage(t, tt.src)
			g, err := NewGoGraph(pkg)
			require.NoError(t, err)

			components := g.WeaklyConnectedComponents()

			if tt.expected == nil {
				assert.Nil(t, components)
				return
			}
			require.NotNil(t, components, "components should not be nil")

			actual := make([][]string, len(components))
			for i, compMap := range components {
				compSlice := make([]string, 0, len(compMap))
				for ident := range compMap {
					compSlice = append(compSlice, ident)
				}
				sort.Strings(compSlice)
				actual[i] = compSlice
			}

			sort.Slice(actual, func(i, j int) bool {
				if len(actual[i]) == 0 || len(actual[j]) == 0 {
					return len(actual[i]) < len(actual[j])
				}
				return actual[i][0] < actual[j][0]
			})

			for i := range tt.expected {
				sort.Strings(tt.expected[i])
			}
			sort.Slice(tt.expected, func(i, j int) bool {
				if len(tt.expected[i]) == 0 || len(tt.expected[j]) == 0 {
					return len(tt.expected[i]) < len(tt.expected[j])
				}
				return tt.expected[i][0] < tt.expected[j][0]
			})

			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestStronglyConnectedComponents(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		expected [][]string
	}{
		{
			name:     "empty package",
			src:      "package testpkg",
			expected: nil,
		},
		{
			name: "all separate",
			src: dedent(`
				type A struct{}
				type B struct{}
				var C = 1
			`),
			expected: [][]string{{"A"}, {"B"}, {"C"}},
		},
		{
			name: "simple chain",
			src: dedent(`
				type A struct{ F B }
				type B struct{ F C }
				type C struct{}
			`),
			expected: [][]string{{"A"}, {"B"}, {"C"}},
		},
		{
			name: "simple cycle",
			src: dedent(`
				type A struct { F B }
				type B struct { F A }
				type C struct {}
			`),
			expected: [][]string{{"A", "B"}, {"C"}},
		},
		{
			name: "larger cycle",
			src: dedent(`
				type A struct { F B }
				type B struct { F C }
				type C struct { F A }
				var D = 1
			`),
			expected: [][]string{{"A", "B", "C"}, {"D"}},
		},
		{
			name: "complex graph with multiple cycles",
			src: dedent(`
				type A struct { F B } // A -> B
				type B struct { F C } // B -> C
				type C struct { F A } // C -> A (Cycle 1: A,B,C)
				type D struct { F E } // D -> E
				type E struct { F D } // E -> D (Cycle 2: D,E)
				type F struct { F G } // F -> G
				type G struct {}
				var H = 1
				`),
			expected: [][]string{{"A", "B", "C"}, {"D", "E"}, {"F"}, {"G"}, {"H"}},
		},
		{
			name: "method cycle",
			src: dedent(`
				type A struct {}
				func (a *A) Foo() { b := B{}; b.Bar() }
				type B struct {}
				func (b *B) Bar() { a := A{}; a.Foo() }
			`),
			expected: [][]string{{"*A.Foo", "*B.Bar"}, {"A"}, {"B"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := newTestPackage(t, tt.src)
			g, err := NewGoGraph(pkg)
			require.NoError(t, err)

			components := g.StronglyConnectedComponents()

			if tt.expected == nil {
				assert.Nil(t, components)
				return
			}
			require.NotNil(t, components, "components should not be nil")

			actual := make([][]string, len(components))
			for i, compMap := range components {
				compSlice := make([]string, 0, len(compMap))
				for ident := range compMap {
					compSlice = append(compSlice, ident)
				}
				sort.Strings(compSlice)
				actual[i] = compSlice
			}

			// Sort for stable comparison
			sort.Slice(actual, func(i, j int) bool {
				if len(actual[i]) == 0 || len(actual[j]) == 0 {
					return len(actual[i]) < len(actual[j])
				}
				return actual[i][0] < actual[j][0]
			})

			for i := range tt.expected {
				sort.Strings(tt.expected[i])
			}
			sort.Slice(tt.expected, func(i, j int) bool {
				if len(tt.expected[i]) == 0 || len(tt.expected[j]) == 0 {
					return len(tt.expected[i]) < len(tt.expected[j])
				}
				return tt.expected[i][0] < tt.expected[j][0]
			})

			assert.Equal(t, tt.expected, actual)
		})
	}
}
