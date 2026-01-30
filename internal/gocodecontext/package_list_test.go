package gocodecontext

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPackageList_ModuleOnly(t *testing.T) {
	modDir := t.TempDir()

	writeFile(t, filepath.Join(modDir, "go.mod"), "module example.com/m\n\ngo 1.20\n")
	writeFile(t, filepath.Join(modDir, "main.go"), "package main\n\nfunc main() {}\n")
	writeFile(t, filepath.Join(modDir, "foo", "foo.go"), "package foo\n")
	writeFile(t, filepath.Join(modDir, "bar", "bar_test.go"), "package bar_test\n")

	pkgs, ctx, err := PackageList(filepath.Join(modDir, "foo"), "", false)
	require.NoError(t, err)

	assert.Contains(t, pkgs, "example.com/m")
	assert.Contains(t, pkgs, "example.com/m/foo")
	assert.Contains(t, pkgs, "example.com/m/bar")
	assert.NotContains(t, ctx, "Defined in ")
	assert.Contains(t, ctx, "These packages are defined in the current module (example.com/m):")
	assert.Contains(t, ctx, "- example.com/m/foo")
	assert.Contains(t, ctx, "- example.com/m/bar")
}

func TestPackageList_SearchFilter(t *testing.T) {
	modDir := t.TempDir()

	writeFile(t, filepath.Join(modDir, "go.mod"), "module example.com/m\n\ngo 1.20\n")
	writeFile(t, filepath.Join(modDir, "main.go"), "package main\n\nfunc main() {}\n")
	writeFile(t, filepath.Join(modDir, "foo", "foo.go"), "package foo\n")
	writeFile(t, filepath.Join(modDir, "bar", "bar_test.go"), "package bar_test\n")

	pkgs, _, err := PackageList(modDir, `^example\.com/m/(foo|bar)$`, false)
	require.NoError(t, err)

	assert.Equal(t, []string{"example.com/m/bar", "example.com/m/foo"}, pkgs)
}

func TestPackageList_DirectDepsOnlyAndNoInternalDeps(t *testing.T) {
	root := t.TempDir()

	mainDir := filepath.Join(root, "main")
	depDir := filepath.Join(root, "dep")
	indirectDir := filepath.Join(root, "indirect")

	require.NoError(t, os.MkdirAll(mainDir, 0o755))
	require.NoError(t, os.MkdirAll(depDir, 0o755))
	require.NoError(t, os.MkdirAll(indirectDir, 0o755))

	writeFile(t, filepath.Join(depDir, "go.mod"), "module example.com/dep\n\ngo 1.20\n")
	writeFile(t, filepath.Join(depDir, "pkg", "pkg.go"), "package pkg\n")
	writeFile(t, filepath.Join(depDir, "internal", "secret", "secret.go"), "package secret\n")

	writeFile(t, filepath.Join(indirectDir, "go.mod"), "module example.com/indirect\n\ngo 1.20\n")
	writeFile(t, filepath.Join(indirectDir, "z", "z.go"), "package z\n")

	goMod := strings.Join([]string{
		"module example.com/main",
		"",
		"go 1.20",
		"",
		"require (",
		"\texample.com/dep v0.0.0",
		"\texample.com/indirect v0.0.0 // indirect",
		")",
		"",
		"replace example.com/dep => ../dep",
		"replace example.com/indirect => ../indirect",
		"",
	}, "\n")
	writeFile(t, filepath.Join(mainDir, "go.mod"), goMod)
	writeFile(t, filepath.Join(mainDir, "main.go"), "package main\n\nfunc main() {}\n")
	writeFile(t, filepath.Join(mainDir, "p", "p.go"), "package p\n")

	pkgs, ctx, err := PackageList(mainDir, "", true)
	require.NoError(t, err)

	assert.Contains(t, pkgs, "example.com/main")
	assert.Contains(t, pkgs, "example.com/main/p")

	assert.Contains(t, pkgs, "example.com/dep/pkg")
	assert.NotContains(t, pkgs, "example.com/dep/internal/secret")

	assert.NotContains(t, pkgs, "example.com/indirect/z")
	assert.Contains(t, ctx, "Defined in example.com/dep:")
	assert.Contains(t, ctx, "- example.com/dep/pkg")
	assert.NotContains(t, ctx, "example.com/dep/internal/secret")
}

func TestPackageList_ContextCollapsesLargeSubtree(t *testing.T) {
	modDir := t.TempDir()

	writeFile(t, filepath.Join(modDir, "go.mod"), "module example.com/coll\n\ngo 1.20\n")
	writeFile(t, filepath.Join(modDir, "main.go"), "package main\n\nfunc main() {}\n")
	writeFile(t, filepath.Join(modDir, "other", "other.go"), "package other\n")

	for i := 0; i < 70; i++ {
		p := filepath.Join(modDir, "internal", "gen", fmt.Sprintf("p%03d", i), "x.go")
		writeFile(t, p, "package p\n")
	}

	pkgs, ctx, err := PackageList(modDir, "", false)
	require.NoError(t, err)

	assert.Contains(t, pkgs, "example.com/coll/internal/gen/p000")
	assert.Contains(t, ctx, "example.com/coll/internal/gen/... (")
	assert.NotContains(t, ctx, "- example.com/coll/internal/gen/p000")
}

func TestModuleInfo_ReturnsGoMod(t *testing.T) {
	modDir := t.TempDir()

	goMod := strings.Join([]string{
		"module example.com/m",
		"",
		"go 1.22",
		"",
		"require example.com/dep v0.0.0",
		"",
	}, "\n")
	writeFile(t, filepath.Join(modDir, "go.mod"), goMod)
	writeFile(t, filepath.Join(modDir, "p", "p.go"), "package p\n")

	gotFromDir, err := ModuleInfo(filepath.Join(modDir, "p"))
	require.NoError(t, err)
	assert.Equal(t, strings.TrimSpace(goMod), gotFromDir)

	gotFromFile, err := ModuleInfo(filepath.Join(modDir, "p", "p.go"))
	require.NoError(t, err)
	assert.Equal(t, strings.TrimSpace(goMod), gotFromFile)
}

func TestModuleInfo_NoGoMod(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "p", "p.go"), "package p\n")

	_, err := ModuleInfo(filepath.Join(dir, "p"))
	require.Error(t, err)
}

func writeFile(t *testing.T, path string, contents string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
}
