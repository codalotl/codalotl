package cmdrunner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManifestDirWithLangInput(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	projectDir := filepath.Join(root, "project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "pkg"), 0o755))
	writeFile(t, filepath.Join(projectDir, "pyproject.toml"), "")

	resolver := newManifestDirResolver(root, map[string]any{"Lang": "py"})
	got, err := resolver.manifestDir(filepath.Join(projectDir, "pkg"))
	require.NoError(t, err)
	require.Equal(t, projectDir, got)
}

func TestManifestDirDetectsLanguageFromFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	moduleDir := filepath.Join(root, "module")
	serverDir := filepath.Join(moduleDir, "cmd", "server")
	require.NoError(t, os.MkdirAll(serverDir, 0o755))

	writeFile(t, filepath.Join(moduleDir, "go.mod"), "module example.com/module\n")
	writeFile(t, filepath.Join(serverDir, "main.go"), "package main\n")

	resolver := newManifestDirResolver(root, nil)
	got, err := resolver.manifestDir(filepath.Join(serverDir, "main.go"))
	require.NoError(t, err)
	require.Equal(t, moduleDir, got)
}

func TestManifestDirWalksUpWhenDirHasNoFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	projectDir := filepath.Join(root, "project")
	innerDir := filepath.Join(projectDir, "inner", "deep")

	require.NoError(t, os.MkdirAll(innerDir, 0o755))
	writeFile(t, filepath.Join(projectDir, "main.py"), "print('hello')\n")
	writeFile(t, filepath.Join(projectDir, "pyproject.toml"), "")

	resolver := newManifestDirResolver(root, nil)
	got, err := resolver.manifestDir(innerDir)
	require.NoError(t, err)
	require.Equal(t, projectDir, got)
}

func TestManifestDirReturnsRootWhenUnknownLanguage(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	docsDir := filepath.Join(root, "docs")
	require.NoError(t, os.MkdirAll(docsDir, 0o755))
	writeFile(t, filepath.Join(docsDir, "README"), "some docs\n")

	resolver := newManifestDirResolver(root, nil)
	got, err := resolver.manifestDir(docsDir)
	require.NoError(t, err)
	require.Equal(t, root, got)
}

func TestManifestDirReturnsRootWhenManifestMissing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	goDir := filepath.Join(root, "go")
	require.NoError(t, os.MkdirAll(goDir, 0o755))
	writeFile(t, filepath.Join(goDir, "main.go"), "package main\n")

	resolver := newManifestDirResolver(root, map[string]any{"Lang": "go"})
	got, err := resolver.manifestDir(goDir)
	require.NoError(t, err)
	require.Equal(t, root, got)
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
}
