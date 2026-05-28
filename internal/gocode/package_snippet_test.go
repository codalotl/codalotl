package gocode

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetSnippet(t *testing.T) {
	codeContent := dedent(`
		package testpkg

		// Version is the current version
		const Version = "1.0.0"

		// Config holds configuration
		var Config = struct{
			Name string
		}{
			Name: "test",
		}

		// Greeter greets people
		type Greeter struct {
			Name string
		}

		// Greet returns a greeting
		func (g *Greeter) Greet() string {
			return "Hello, " + g.Name
		}

		// Hello says hello
		func Hello() string {
			return "Hello, World!"
		}
	`)

	pkg, _ := newTestPackageFromFiles(t, map[string]string{"main.go": codeContent})

	tests := []struct {
		name       string
		identifier string
		check      func(t *testing.T, snippet Snippet)
	}{
		{
			name:       "function",
			identifier: "Hello",
			check: func(t *testing.T, snippet Snippet) {
				t.Helper()
				fs, ok := snippet.(*FuncSnippet)
				require.True(t, ok)
				assert.Equal(t, "Hello", fs.Name)
			},
		},
		{
			name:       "method",
			identifier: "*Greeter.Greet",
			check: func(t *testing.T, snippet Snippet) {
				t.Helper()
				fs, ok := snippet.(*FuncSnippet)
				require.True(t, ok)
				assert.Equal(t, "Greet", fs.Name)
				assert.Equal(t, "*Greeter", fs.ReceiverType)
			},
		},
		{
			name:       "type",
			identifier: "Greeter",
			check: func(t *testing.T, snippet Snippet) {
				t.Helper()
				ts, ok := snippet.(*TypeSnippet)
				require.True(t, ok)
				assert.Contains(t, ts.Identifiers, "Greeter")
			},
		},
		{
			name:       "const",
			identifier: "Version",
			check: func(t *testing.T, snippet Snippet) {
				t.Helper()
				vs, ok := snippet.(*ValueSnippet)
				require.True(t, ok)
				assert.Contains(t, vs.Identifiers, "Version")
				assert.False(t, vs.IsVar)
			},
		},
		{
			name:       "var",
			identifier: "Config",
			check: func(t *testing.T, snippet Snippet) {
				t.Helper()
				vs, ok := snippet.(*ValueSnippet)
				require.True(t, ok)
				assert.True(t, vs.IsVar)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snippet := pkg.GetSnippet(tt.identifier)
			require.NotNil(t, snippet)
			tt.check(t, snippet)
		})
	}

	assert.Nil(t, pkg.GetSnippet("NonExistent"))
}
