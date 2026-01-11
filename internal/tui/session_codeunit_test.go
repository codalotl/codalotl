package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewSession_PackageMode_IncludesDataDirsButNotNestedPackages(t *testing.T) {
	sandboxDir := t.TempDir()

	// Make the temp dir a minimal Go module so package-mode tooling has a sane environment.
	require.NoError(t, os.WriteFile(filepath.Join(sandboxDir, "go.mod"), []byte("module example.com/test\n\ngo 1.22\n"), 0o644))

	// Base package.
	pkgAbsDir := filepath.Join(sandboxDir, "foo", "bar")
	require.NoError(t, os.MkdirAll(pkgAbsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgAbsDir, "bar.go"), []byte("package bar\n"), 0o644))

	// Supporting data dir (no .go files).
	bobDir := filepath.Join(pkgAbsDir, "bob")
	require.NoError(t, os.MkdirAll(bobDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(bobDir, "data.txt"), []byte("hello\n"), 0o644))

	// Nested Go package (should be excluded from the code unit).
	subpkgDir := filepath.Join(pkgAbsDir, "subpkg")
	require.NoError(t, os.MkdirAll(subpkgDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(subpkgDir, "sub.go"), []byte("package subpkg\n"), 0o644))

	// testdata under an included dir is included, even if it contains .go fixture files.
	bobTestdataDir := filepath.Join(bobDir, "testdata")
	require.NoError(t, os.MkdirAll(bobTestdataDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(bobTestdataDir, "fixture.go"), []byte("package main\n"), 0o644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(sandboxDir))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	s, err := newSession(sessionConfig{packagePath: "foo/bar"})
	require.NoError(t, err)
	t.Cleanup(s.Close)

	require.NoError(t, s.authorizer.IsAuthorizedForRead(false, "", "read_file", filepath.Join(bobDir, "data.txt")))
	require.NoError(t, s.authorizer.IsAuthorizedForWrite(false, "", "apply_patch", filepath.Join(bobDir, "new.txt")))
	require.NoError(t, s.authorizer.IsAuthorizedForRead(false, "", "read_file", filepath.Join(bobTestdataDir, "fixture.go")))

	require.Error(t, s.authorizer.IsAuthorizedForRead(false, "", "read_file", filepath.Join(subpkgDir, "sub.go")))
}
