package gocode

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestModule creates a temporary directory structure for a test Go module. It returns the module root path and a cleanup function.
func setupTestModule(t *testing.T) (string, func()) {
	t.Helper()

	// Create a temporary directory for the module
	tmpDir, err := os.MkdirTemp("", "testmodule-")
	require.NoError(t, err)

	// Create a go.mod file
	moduleName := "example.com/testmodule"
	goModContent := "module " + moduleName
	err = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goModContent), 0644)
	require.NoError(t, err)

	// Create a package in the root
	err = os.WriteFile(filepath.Join(tmpDir, "root.go"), []byte("package root"), 0644)
	require.NoError(t, err)

	// Create a subdirectory with a package
	pkgADir := filepath.Join(tmpDir, "pkga")
	err = os.Mkdir(pkgADir, 0755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(pkgADir, "a.go"), []byte("package pkga"), 0644)
	require.NoError(t, err)

	// Create another subdirectory with a package
	pkgBDir := filepath.Join(tmpDir, "pkgb")
	err = os.Mkdir(pkgBDir, 0755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(pkgBDir, "b.go"), []byte("package pkgb"), 0644)
	require.NoError(t, err)

	// Create a subdirectory without go files
	emptyDir := filepath.Join(tmpDir, "empty")
	err = os.Mkdir(emptyDir, 0755)
	require.NoError(t, err)

	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	return tmpDir, cleanup
}

func TestCloneWithoutPackages(t *testing.T) {
	moduleRoot, cleanup := setupTestModule(t)
	defer cleanup()

	mod, err := NewModule(moduleRoot)
	require.NoError(t, err)

	// Load a package to ensure the original module has packages
	_, err = mod.LoadPackageByRelativeDir("pkga")
	require.NoError(t, err)
	assert.NotEmpty(t, mod.Packages)

	clone, err := mod.CloneWithoutPackages()
	require.NoError(t, err)

	// Assertions for the cloned module
	assert.NotEqual(t, mod.AbsolutePath, clone.AbsolutePath)
	assert.Equal(t, mod.Name, clone.Name)
	assert.Empty(t, clone.Packages)
	assert.True(t, clone.cloned)
	assert.False(t, mod.cloned)

	// Check if go.mod was copied correctly
	data, err := os.ReadFile(filepath.Join(clone.AbsolutePath, "go.mod"))
	require.NoError(t, err)
	orig, err := os.ReadFile(filepath.Join(mod.AbsolutePath, "go.mod"))
	require.NoError(t, err)
	assert.Equal(t, string(orig), string(data))

	// Check that no other files or directories were copied
	entries, err := os.ReadDir(clone.AbsolutePath)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "Cloned module should only contain go.mod")
	assert.Equal(t, "go.mod", entries[0].Name())
}

func TestClonePackage(t *testing.T) {
	moduleRoot, cleanup := setupTestModule(t)
	defer cleanup()

	mod, err := NewModule(moduleRoot)
	require.NoError(t, err)

	// Load a package from the original module
	pkg, err := mod.LoadPackageByRelativeDir("pkga")
	require.NoError(t, err)

	clone, err := mod.CloneWithoutPackages()
	require.NoError(t, err)

	clonedPkg, err := clone.ClonePackage(pkg)
	require.NoError(t, err)
	assert.NotNil(t, clonedPkg)
	assert.Equal(t, clone, clonedPkg.Module)
	assert.NotEqual(t, pkg, clonedPkg)
	assert.Contains(t, clone.Packages, "example.com/testmodule/pkga")

	// Verify the file contents were copied correctly
	contents, err := os.ReadFile(filepath.Join(clonedPkg.Module.AbsolutePath, "pkga", "a.go"))
	require.NoError(t, err)
	orig, err := os.ReadFile(filepath.Join(mod.AbsolutePath, "pkga", "a.go"))
	require.NoError(t, err)
	assert.Equal(t, string(orig), string(contents))

	// Test error case: cloning into a non-cloned module
	_, err = mod.ClonePackage(pkg)
	assert.Error(t, err)
}

func TestModule_LoadPackageByRelativeDir(t *testing.T) {
	moduleRoot, cleanup := setupTestModule(t)
	defer cleanup()

	m, err := NewModule(moduleRoot)
	require.NoError(t, err)

	// Test loading root package
	pkg, err := m.LoadPackageByRelativeDir("")
	require.NoError(t, err)
	assert.NotNil(t, pkg)
	assert.Equal(t, "root", pkg.Name)
	assert.Equal(t, "", pkg.RelativeDir)
	assert.Equal(t, "example.com/testmodule", pkg.ImportPath)

	// Test loading a sub-package
	pkgA, err := m.LoadPackageByRelativeDir("pkga")
	require.NoError(t, err)
	assert.NotNil(t, pkgA)
	assert.Equal(t, "pkga", pkgA.Name)
	assert.Equal(t, "pkga", pkgA.RelativeDir)
	assert.Equal(t, "example.com/testmodule/pkga", pkgA.ImportPath)

	// Test caching by loading the same package again
	cachedPkgA, err := m.LoadPackageByRelativeDir("pkga")
	require.NoError(t, err)
	assert.Same(t, pkgA, cachedPkgA, "Expected to get the same package instance from cache")

	// Test loading a non-existent directory
	_, err = m.LoadPackageByRelativeDir("nonexistent")
	assert.Error(t, err)
}

func TestModule_LoadPackageByImportPath(t *testing.T) {
	moduleRoot, cleanup := setupTestModule(t)
	defer cleanup()

	m, err := NewModule(moduleRoot)
	require.NoError(t, err)
	require.Equal(t, "example.com/testmodule", m.Name)

	// Test loading root package by import path
	pkg, err := m.LoadPackageByImportPath("example.com/testmodule")
	require.NoError(t, err)
	assert.NotNil(t, pkg)
	assert.Equal(t, "root", pkg.Name)
	assert.Equal(t, "", pkg.RelativeDir)
	assert.Equal(t, "example.com/testmodule", pkg.ImportPath)

	// Test loading a sub-package by import path
	pkgA, err := m.LoadPackageByImportPath("example.com/testmodule/pkga")
	require.NoError(t, err)
	assert.NotNil(t, pkgA)
	assert.Equal(t, "pkga", pkgA.Name)
	assert.Equal(t, "pkga", pkgA.RelativeDir)
	assert.Equal(t, "example.com/testmodule/pkga", pkgA.ImportPath)

	// Test caching by loading the same package again
	cachedPkgA, err := m.LoadPackageByImportPath("example.com/testmodule/pkga")
	require.NoError(t, err)
	assert.Same(t, pkgA, cachedPkgA, "Expected to get the same package instance from cache")

	// Test loading an import path not in the module
	_, err = m.LoadPackageByImportPath("example.com/anothermodule")
	assert.ErrorIs(t, err, ErrImportNotInModule)

	// Test loading a completely different import path
	_, err = m.LoadPackageByImportPath("github.com/foo/bar")
	assert.ErrorIs(t, err, ErrImportNotInModule)
}

func TestModule_ResolvePackageByRelativeDir(t *testing.T) {
	moduleRoot, cleanup := setupTestModule(t)
	defer cleanup()

	m, err := NewModule(moduleRoot)
	require.NoError(t, err)

	t.Run("root", func(t *testing.T) {
		moduleAbsDir, packageAbsDir, packageRelDir, fqImportPath, err := m.ResolvePackageByRelativeDir("")
		require.NoError(t, err)
		assert.Equal(t, moduleRoot, moduleAbsDir)
		assert.Equal(t, moduleRoot, packageAbsDir)
		assert.Equal(t, ".", packageRelDir)
		assert.Equal(t, "example.com/testmodule", fqImportPath)
	})

	t.Run("root_dot", func(t *testing.T) {
		moduleAbsDir, packageAbsDir, packageRelDir, fqImportPath, err := m.ResolvePackageByRelativeDir(".")
		require.NoError(t, err)
		assert.Equal(t, moduleRoot, moduleAbsDir)
		assert.Equal(t, moduleRoot, packageAbsDir)
		assert.Equal(t, ".", packageRelDir)
		assert.Equal(t, "example.com/testmodule", fqImportPath)
	})

	t.Run("root_dot_slash", func(t *testing.T) {
		moduleAbsDir, packageAbsDir, packageRelDir, fqImportPath, err := m.ResolvePackageByRelativeDir("./")
		require.NoError(t, err)
		assert.Equal(t, moduleRoot, moduleAbsDir)
		assert.Equal(t, moduleRoot, packageAbsDir)
		assert.Equal(t, ".", packageRelDir)
		assert.Equal(t, "example.com/testmodule", fqImportPath)
	})

	t.Run("subpkg", func(t *testing.T) {
		moduleAbsDir, packageAbsDir, packageRelDir, fqImportPath, err := m.ResolvePackageByRelativeDir("pkga")
		require.NoError(t, err)
		assert.Equal(t, moduleRoot, moduleAbsDir)
		assert.Equal(t, filepath.Join(moduleRoot, "pkga"), packageAbsDir)
		assert.Equal(t, "pkga", packageRelDir)
		assert.Equal(t, "example.com/testmodule/pkga", fqImportPath)
	})

	t.Run("subpkg_dot_slash", func(t *testing.T) {
		moduleAbsDir, packageAbsDir, packageRelDir, fqImportPath, err := m.ResolvePackageByRelativeDir("./pkga")
		require.NoError(t, err)
		assert.Equal(t, moduleRoot, moduleAbsDir)
		assert.Equal(t, filepath.Join(moduleRoot, "pkga"), packageAbsDir)
		assert.Equal(t, "pkga", packageRelDir)
		assert.Equal(t, "example.com/testmodule/pkga", fqImportPath)
	})

	t.Run("not_found", func(t *testing.T) {
		_, _, _, _, err := m.ResolvePackageByRelativeDir("doesnotexist")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrResolveNotFound)
	})

	t.Run("escapes_module_root", func(t *testing.T) {
		_, _, _, _, err := m.ResolvePackageByRelativeDir("../pkga")
		require.Error(t, err)
		assert.False(t, errors.Is(err, ErrResolveNotFound))
	})

	t.Run("nested_module_not_found", func(t *testing.T) {
		nestedRoot := filepath.Join(moduleRoot, "nestedmod")
		require.NoError(t, os.MkdirAll(filepath.Join(nestedRoot, "pkg"), 0755))

		require.NoError(t, os.WriteFile(filepath.Join(nestedRoot, "go.mod"), []byte("module example.com/nestedmod\n"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(nestedRoot, "pkg", "pkg.go"), []byte("package pkg\n"), 0644))

		_, _, _, _, err := m.ResolvePackageByRelativeDir("nestedmod/pkg")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrResolveNotFound)
	})
}

func TestModule_ResolvePackageByImport(t *testing.T) {
	moduleRoot, cleanup := setupTestModule(t)
	defer cleanup()

	m, err := NewModule(moduleRoot)
	require.NoError(t, err)

	t.Run("module_root", func(t *testing.T) {
		moduleAbsDir, packageAbsDir, packageRelDir, fqImportPath, err := m.ResolvePackageByImport("example.com/testmodule")
		require.NoError(t, err)
		assert.Equal(t, moduleRoot, moduleAbsDir)
		assert.Equal(t, moduleRoot, packageAbsDir)
		assert.Equal(t, ".", packageRelDir)
		assert.Equal(t, "example.com/testmodule", fqImportPath)
	})

	t.Run("module_subpkg", func(t *testing.T) {
		moduleAbsDir, packageAbsDir, packageRelDir, fqImportPath, err := m.ResolvePackageByImport("example.com/testmodule/pkgb")
		require.NoError(t, err)
		assert.Equal(t, moduleRoot, moduleAbsDir)
		assert.Equal(t, filepath.Join(moduleRoot, "pkgb"), packageAbsDir)
		assert.Equal(t, "pkgb", packageRelDir)
		assert.Equal(t, "example.com/testmodule/pkgb", fqImportPath)
	})

	t.Run("stdlib", func(t *testing.T) {
		moduleAbsDir, packageAbsDir, packageRelDir, fqImportPath, err := m.ResolvePackageByImport("fmt")
		require.NoError(t, err)
		assert.Empty(t, moduleAbsDir)
		assert.NotEmpty(t, packageAbsDir)
		assert.Equal(t, "fmt", filepath.Base(packageAbsDir))
		assert.Empty(t, packageRelDir)
		assert.Equal(t, "fmt", fqImportPath)
	})

	t.Run("not_found_in_module", func(t *testing.T) {
		_, _, _, _, err := m.ResolvePackageByImport("example.com/testmodule/doesnotexist")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrResolveNotFound)
	})

	t.Run("reject_relative_import", func(t *testing.T) {
		_, _, _, _, err := m.ResolvePackageByImport("./pkga")
		require.Error(t, err)
		assert.False(t, errors.Is(err, ErrResolveNotFound))
	})

	t.Run("reject_pattern", func(t *testing.T) {
		_, _, _, _, err := m.ResolvePackageByImport("example.com/testmodule/...")
		require.Error(t, err)
		assert.False(t, errors.Is(err, ErrResolveNotFound))
	})
}
