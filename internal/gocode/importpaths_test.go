package gocode

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestImportPathsPopulation verifies that Package.ImportPaths collects all unique import paths from every file in the package, regardless of aliasing style (regular,
// dot, blank, or named).
func TestImportPathsPopulation(t *testing.T) {
	// Create a temporary directory to act as a module root.
	tempDir, err := os.MkdirTemp("", "gocode-importpaths-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Sample source containing various import styles.
	source := `package foo

import (
    "fmt"
    m "math"
    _ "net/http"
    . "strings"
)

func Bar() {}
`

	fileName := "main.go"
	filePath := filepath.Join(tempDir, fileName)
	err = os.WriteFile(filePath, []byte(source), 0644)
	assert.NoError(t, err)

	// Create a dummy module encompassing the package.
	module := &Module{
		Name:         "testmodule",
		AbsolutePath: tempDir,
		Packages:     make(map[string]*Package),
	}

	// Construct and parse the package.
	pkg, err := NewPackage("", tempDir, []string{fileName}, module)
	assert.NoError(t, err)

	// Expected set of import paths.
	expected := []string{"fmt", "math", "net/http", "strings"}

	// Verify that each expected import path is present.
	for _, pth := range expected {
		_, ok := pkg.ImportPaths[pth]
		assert.Truef(t, ok, "expected import path %q not found", pth)
	}

	// Verify that no unexpected import paths are present.
	assert.Equal(t, len(expected), len(pkg.ImportPaths), "import path count mismatch")
}

func TestImportPathCategories(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "importpaths-")
	assert.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// --- Create a minimal go.mod ---
	gomod := `module example.com/mymod

go 1.21
`
	err = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(gomod), 0644)
	assert.NoError(t, err)

	// --- foo package inside module (example.com/mymod/foo) ---
	fooDir := filepath.Join(tmpDir, "foo")
	err = os.MkdirAll(fooDir, 0755)
	assert.NoError(t, err)
	fooSrc := `package foo

    func Hello() string { return "hi" }
`
	err = os.WriteFile(filepath.Join(fooDir, "foo.go"), []byte(fooSrc), 0644)
	assert.NoError(t, err)

	// --- root package that imports stdlib, module and third-party packages ---
	mainSrc := `package main

import (
    "fmt"
    "example.com/mymod/foo"
    "github.com/stretchr/testify/assert"
)

func main() {
    _ = fmt.Sprintf("%s", foo.Hello())
    _ = assert.ObjectsAreEqual(1, 1)
}
`
	err = os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(mainSrc), 0644)
	assert.NoError(t, err)

	// --- Load module and root package ---
	mod, err := NewModule(tmpDir)
	assert.NoError(t, err)

	// Read all packages so that import resolution happens.
	err = mod.LoadAllPackages()
	assert.NoError(t, err)

	pkg := mod.Packages[mod.Name] // root package import path is module name
	if !assert.NotNil(t, pkg, "root package not found") {
		return
	}

	modImports := pkg.ImportPathsModule()
	stdImports := pkg.ImportPathsStdlib()
	vendImports := pkg.ImportPathsVendor()

	assert.ElementsMatch(t, []string{"example.com/mymod/foo"}, modImports)
	assert.ElementsMatch(t, []string{"fmt"}, stdImports)
	assert.ElementsMatch(t, []string{"github.com/stretchr/testify/assert"}, vendImports)

	// Ensure memoisation: repeated call returns identical slice reference (ptr compare)
	modImports2 := pkg.ImportPathsModule()
	assert.Equal(t, &modImports[0], &modImports2[0])
}

func TestImportPathCategories_NoDotModule(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "importpaths-nodot-")
	assert.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Minimal go.mod without dot in module path
	gomod := `module mymod

go 1.21
`
	err = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(gomod), 0644)
	assert.NoError(t, err)

	// sub package inside module: mymod/internalpkg
	internalDir := filepath.Join(tmpDir, "internalpkg")
	assert.NoError(t, os.MkdirAll(internalDir, 0755))
	internalSrc := `package internalpkg
    func Ping() {}
    `
	assert.NoError(t, os.WriteFile(filepath.Join(internalDir, "internal.go"), []byte(internalSrc), 0644))

	// root package referencing imports
	mainSrc := `package main
import (
    "bytes"
    "mymod/internalpkg"
    "github.com/stretchr/testify/require"
)
func main() {
    _ = bytes.Equal([]byte("a"), []byte("a"))
    _ = internalpkg.Ping
    _ = require.NoError
}
`
	assert.NoError(t, os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(mainSrc), 0644))

	mod, err := NewModule(tmpDir)
	assert.NoError(t, err)
	assert.Equal(t, "mymod", mod.Name)

	err = mod.LoadAllPackages()
	assert.NoError(t, err)

	pkg := mod.Packages[mod.Name]
	if !assert.NotNil(t, pkg) {
		return
	}

	assert.ElementsMatch(t, []string{"mymod/internalpkg"}, pkg.ImportPathsModule())
	assert.ElementsMatch(t, []string{"bytes"}, pkg.ImportPathsStdlib())
	assert.ElementsMatch(t, []string{"github.com/stretchr/testify/require"}, pkg.ImportPathsVendor())
}
