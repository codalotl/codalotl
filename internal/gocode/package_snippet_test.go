package gocode

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetSnippet(t *testing.T) {
	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "gopackage-getsnippet-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create a simple Go file with various snippets
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
	testFile := filepath.Join(tempDir, "main.go")
	err = os.WriteFile(testFile, []byte(codeContent), 0644)
	assert.NoError(t, err)

	// Create a module for the test
	module := &Module{
		Name:         "testmodule",
		AbsolutePath: tempDir,
		Packages:     make(map[string]*Package),
	}

	// Create a new package
	pkg, err := NewPackage("", tempDir, []string{"main.go"}, module)
	assert.NoError(t, err)
	assert.NotNil(t, pkg)

	// Test getting function snippet
	funcSnippet := pkg.GetSnippet("Hello")
	assert.NotNil(t, funcSnippet, "Expected to find Hello function snippet")
	fs, ok := funcSnippet.(*FuncSnippet)
	assert.True(t, ok, "Expected FuncSnippet type")
	assert.Equal(t, "Hello", fs.Name)

	// Test getting method snippet
	methodSnippet := pkg.GetSnippet("*Greeter.Greet")
	assert.NotNil(t, methodSnippet, "Expected to find Greeter.Greet method snippet")
	fs, ok = methodSnippet.(*FuncSnippet)
	assert.True(t, ok, "Expected FuncSnippet type for method")
	assert.Equal(t, "Greet", fs.Name)
	assert.Equal(t, "*Greeter", fs.ReceiverType)

	// Test getting type snippet
	typeSnippet := pkg.GetSnippet("Greeter")
	assert.NotNil(t, typeSnippet, "Expected to find Greeter type snippet")
	ts, ok := typeSnippet.(*TypeSnippet)
	assert.True(t, ok, "Expected TypeSnippet type")
	assert.Contains(t, ts.Identifiers, "Greeter")

	// Test getting value snippet
	valueSnippet := pkg.GetSnippet("Version")
	assert.NotNil(t, valueSnippet, "Expected to find Version const snippet")
	vs, ok := valueSnippet.(*ValueSnippet)
	assert.True(t, ok, "Expected ValueSnippet type")
	assert.Contains(t, vs.Identifiers, "Version")
	assert.False(t, vs.IsVar, "Expected Version to be a const, not var")

	// Test getting var snippet
	varSnippet := pkg.GetSnippet("Config")
	assert.NotNil(t, varSnippet, "Expected to find Config var snippet")
	vs, ok = varSnippet.(*ValueSnippet)
	assert.True(t, ok, "Expected ValueSnippet type for var")
	assert.True(t, vs.IsVar, "Expected Config to be a var, not const")

	// Test non-existent identifier
	notFound := pkg.GetSnippet("NonExistent")
	assert.Nil(t, notFound, "Expected nil for non-existent identifier")
}
