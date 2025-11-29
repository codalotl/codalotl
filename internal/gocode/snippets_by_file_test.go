package gocode

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func collectSnippetIDs(snippets []Snippet) [][]string {
	var ids [][]string
	for _, snippet := range snippets {
		ids = append(ids, snippet.IDs())
	}
	return ids
}

func TestSnippetsByFile(t *testing.T) {
	tempDir := t.TempDir()

	aFile := filepath.Join(tempDir, "a.go")
	aContents := `// Package sample provides fixtures for SnippetsByFile tests.
package sample

// ConstA is a constant in file a.
const ConstA = 42

// TypeA is a sample type.
type TypeA struct{}

// MethodA is a method on TypeA.
func (t *TypeA) MethodA() {}

// FuncA is a top-level function.
func FuncA() {}
`
	require.NoError(t, os.WriteFile(aFile, []byte(aContents), 0o644))

	bFile := filepath.Join(tempDir, "b.go")
	bContents := `package sample

var VarB = 1

func FuncB() {}
`
	require.NoError(t, os.WriteFile(bFile, []byte(bContents), 0o644))

	module := &Module{
		Name:         "example.com/sample",
		AbsolutePath: tempDir,
		Packages:     make(map[string]*Package),
	}

	pkg, err := NewPackage("", tempDir, []string{"a.go", "b.go"}, module)
	require.NoError(t, err)
	require.NotNil(t, pkg)

	grouped := pkg.SnippetsByFile(nil)
	require.Len(t, grouped, 2)
	require.Contains(t, grouped, "a.go")
	require.Contains(t, grouped, "b.go")

	var idsA [][]string
	for _, snippet := range grouped["a.go"] {
		idsA = append(idsA, snippet.IDs())
	}
	assert.Equal(t, [][]string{
		{PackageIdentifierPerFile("a.go")},
		{"ConstA"},
		{"TypeA"},
		{"*TypeA.MethodA"},
		{"FuncA"},
	}, idsA)

	var idsB [][]string
	for _, snippet := range grouped["b.go"] {
		idsB = append(idsB, snippet.IDs())
	}
	assert.Equal(t, [][]string{{"VarB"}, {"FuncB"}}, idsB)

	filtered := pkg.SnippetsByFile([]string{"ConstA", "FuncB", "missing"})
	require.Len(t, filtered, 2)
	assert.Equal(t, [][]string{{"ConstA"}}, collectSnippetIDs(filtered["a.go"]))
	assert.Equal(t, [][]string{{"FuncB"}}, collectSnippetIDs(filtered["b.go"]))

	primaryPackage := pkg.SnippetsByFile([]string{PackageIdentifier})
	require.Len(t, primaryPackage, 1)
	assert.Equal(t, [][]string{{PackageIdentifierPerFile("a.go")}}, collectSnippetIDs(primaryPackage["a.go"]))
}
