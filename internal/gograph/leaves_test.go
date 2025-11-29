package gograph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLeavesOf(t *testing.T) {
	tests := []struct {
		name               string
		src                string
		idents             []string
		disconnectedIdents []string
		expectedLeaves     []string
	}{
		{
			name: "simple DAG",
			src: `
				type A struct { B }
				type B struct { C }
				type C struct {}`,
			idents:         []string{"A"},
			expectedLeaves: []string{"C"},
		},
		{
			name: "node becomes leaf due to disconnect",
			src: `
				type A struct { B }
				type B struct { C }
				type C struct {}`,
			idents:             []string{"A"},
			disconnectedIdents: []string{"C"},
			expectedLeaves:     []string{"B"},
		},
		{
			name: "multiple leaves",
			src: `
				type A struct { B; C }
				type B struct {}
				type C struct {}`,
			idents:         []string{"A"},
			expectedLeaves: []string{"B", "C"},
		},
		{
			name: "contained SCC",
			src: `
				type A struct { B }
				type B struct { C }
				type C struct { B }`,
			idents:         []string{"A"},
			expectedLeaves: []string{"B", "C"},
		},
		{
			name: "SCC with exit to a leaf",
			src: `
				type A struct { B }
				type B struct { C; D }
				type C struct { B }
				type D struct {}`,
			idents:         []string{"A"},
			expectedLeaves: []string{"D"},
		},
		{
			name: "start from within an SCC",
			src: `
				type A struct { B }
				type B struct { C }
				type C struct { B }`,
			idents:         []string{"B"},
			expectedLeaves: []string{"B", "C"},
		},
		{
			name: "start from a disconnected ident",
			src: `
				type A struct { B }
				type B struct {}`,
			idents:             []string{"C"},
			disconnectedIdents: []string{"C"},
			expectedLeaves:     nil,
		},
		{
			name: "multiple idents",
			src: `
				type A struct { B }
				type B struct {}
				type X struct { Y }
				type Y struct {}`,
			idents:         []string{"A", "X"},
			expectedLeaves: []string{"B", "Y"},
		},
		{
			name: "ident is a leaf",
			src: `
				type A struct {}`,
			idents:         []string{"A"},
			expectedLeaves: []string{"A"},
		},
		{
			name:           "no idents",
			src:            `type A struct{}`,
			idents:         []string{},
			expectedLeaves: nil,
		},
		{
			name: "disconnected ident is a leaf of another path",
			src: `
				type A struct { B }
				type B struct {}
			`,
			idents:             []string{"A"},
			disconnectedIdents: []string{"B"},
			expectedLeaves:     []string{"A"},
		},
		{
			name: "SCC with one exit that is disconnected",
			src: `
				type A struct { B }
				type B struct { C; D }
				type C struct { B }
				type D struct {}`,
			idents:             []string{"A"},
			disconnectedIdents: []string{"D"},
			expectedLeaves:     []string{"B", "C"},
		},
		{
			name: "no leaves found but not in SCC",
			src: `
				func f() { f() }
				type A struct{} // to have a node
			`,
			idents:         []string{"A"},
			expectedLeaves: []string{"A"},
		},
		{
			name: "disconnected ident in result scc",
			src: `
				type A struct {B}
				type B struct {C}
				type C struct {B}
			`,
			idents:             []string{"A"},
			disconnectedIdents: []string{"C"},
			expectedLeaves:     []string{"B"},
		},
		{
			name: "complex scc with exit",
			src: `
				type A struct{B}
				type B struct{C}
				type C struct{D}
				type D struct{B; E}
				type E struct{}
			`,
			idents:         []string{"A"},
			expectedLeaves: []string{"E"},
		},
		{
			name: "complex scc no exit",
			src: `
				type A struct{B}
				type B struct{C}
				type C struct{D}
				type D struct{B; E}
				type E struct{B}
			`,
			idents:         []string{"A"},
			expectedLeaves: []string{"B", "C", "D", "E"},
		},
		{
			name: "cycle-into-cycle then leaf",
			src: `
				type A struct { B }
				type B struct { A; D }
				type D struct { E }
				type E struct { D; F }
				type F struct {}`,
			idents:             []string{"A"},
			disconnectedIdents: []string{},
			expectedLeaves:     []string{"F"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pkg := newTestPackage(t, dedent(tc.src))
			g, err := NewGoGraph(pkg)
			require.NoError(t, err)

			leaves := g.LeavesOf(tc.idents, tc.disconnectedIdents)
			assert.ElementsMatch(t, tc.expectedLeaves, leaves)
		})
	}
}
