package gocode

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverModules_RecursiveDiscovery(t *testing.T) {
	t.Setenv("GOWORK", "off")

	root := t.TempDir()
	rootMod := writeDiscoverTestModule(t, filepath.Join(root, "rootmod"), "example.com/rootmod")
	nestedMod := writeDiscoverTestModule(t, filepath.Join(root, "rootmod", "nested"), "example.com/nested")
	siblingMod := writeDiscoverTestModule(t, filepath.Join(root, "sibling"), "example.com/sibling")
	fileRoot := writeDiscoverTestFile(t, root, "input.txt", "not go source\n")

	modules, err := DiscoverModules(fileRoot)
	require.NoError(t, err)

	assert.Equal(t, []string{rootMod, nestedMod, siblingMod}, discoverModuleAbsPaths(modules))
	assert.Equal(t, []string{"example.com/rootmod", "example.com/nested", "example.com/sibling"}, discoverModuleNames(modules))
}

func TestDiscoverModules_WorkspaceDiscovery(t *testing.T) {
	t.Setenv("GOWORK", "")

	root := t.TempDir()
	moduleA := writeDiscoverTestModule(t, filepath.Join(root, "a"), "example.com/a")
	moduleB := writeDiscoverTestModule(t, filepath.Join(root, "b"), "example.com/b")
	writeDiscoverTestModule(t, filepath.Join(root, "ignored"), "example.com/ignored")
	fileRoot := writeDiscoverTestFile(t, filepath.Join(root, "a"), "a.go", "package a\n")
	writeDiscoverTestFile(t, root, "go.work", "go 1.24\n\nuse (\n\t./b\n\t./a\n)\n")

	modules, err := DiscoverModules(fileRoot)
	require.NoError(t, err)

	assert.Equal(t, []string{moduleA, moduleB}, discoverModuleAbsPaths(modules))
	assert.Equal(t, []string{"example.com/a", "example.com/b"}, discoverModuleNames(modules))
}

func TestDiscoverModules_GOWORKAutoUsesWorkspaceDiscovery(t *testing.T) {
	t.Setenv("GOWORK", "auto")

	root := t.TempDir()
	moduleA := writeDiscoverTestModule(t, filepath.Join(root, "a"), "example.com/a")
	moduleB := writeDiscoverTestModule(t, filepath.Join(root, "b"), "example.com/b")
	writeDiscoverTestModule(t, filepath.Join(root, "ignored"), "example.com/ignored")
	writeDiscoverTestFile(t, root, "go.work", "go 1.24\n\nuse (\n\t./b\n\t./a\n)\n")

	modules, err := DiscoverModules(filepath.Join(root, "a"))
	require.NoError(t, err)

	assert.Equal(t, []string{moduleA, moduleB}, discoverModuleAbsPaths(modules))
	assert.Equal(t, []string{"example.com/a", "example.com/b"}, discoverModuleNames(modules))
}

func TestDiscoverModules_SkipsExcludedDirsExceptRoot(t *testing.T) {
	t.Setenv("GOWORK", "off")

	root := t.TempDir()
	included := writeDiscoverTestModule(t, filepath.Join(root, "included"), "example.com/included")
	hidden := writeDiscoverTestModule(t, filepath.Join(root, ".hidden"), "example.com/hidden")
	writeDiscoverTestModule(t, filepath.Join(root, "_private"), "example.com/private")
	writeDiscoverTestModule(t, filepath.Join(root, "testdata"), "example.com/testdata")
	writeDiscoverTestModule(t, filepath.Join(root, "vendor"), "example.com/vendor")

	modules, err := DiscoverModules(root)
	require.NoError(t, err)

	assert.Equal(t, []string{included}, discoverModuleAbsPaths(modules))

	modules, err = DiscoverModules(hidden)
	require.NoError(t, err)

	assert.Equal(t, []string{hidden}, discoverModuleAbsPaths(modules))
	assert.Equal(t, []string{"example.com/hidden"}, discoverModuleNames(modules))
}

func writeDiscoverTestModule(t *testing.T, dir, moduleName string) string {
	t.Helper()

	require.NoError(t, os.MkdirAll(dir, 0o755))
	writeDiscoverTestFile(t, dir, "go.mod", fmt.Sprintf("module %s\n\ngo 1.24\n", moduleName))
	return filepath.Clean(dir)
}

func writeDiscoverTestFile(t *testing.T, dir, name, contents string) string {
	t.Helper()

	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
	return path
}

func discoverModuleAbsPaths(modules []*Module) []string {
	paths := make([]string, 0, len(modules))
	for _, module := range modules {
		paths = append(paths, module.AbsolutePath)
	}
	return paths
}

func discoverModuleNames(modules []*Module) []string {
	names := make([]string, 0, len(modules))
	for _, module := range modules {
		names = append(names, module.Name)
	}
	return names
}
