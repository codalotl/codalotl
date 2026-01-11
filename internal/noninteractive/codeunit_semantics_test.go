package noninteractive

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/stretchr/testify/require"
)

func TestBuildAuthorizerForTools_CodeUnitSemanticsMatchTUI(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	pkgRelPath := "mypkg"
	pkgAbsPath := filepath.Join(sandbox, filepath.FromSlash(pkgRelPath))

	require.NoError(t, os.MkdirAll(pkgAbsPath, 0o755))

	// Base dir can contain Go files; nested dirs containing Go files should be excluded.
	require.NoError(t, os.WriteFile(filepath.Join(pkgAbsPath, "main.go"), []byte("package mypkg\n"), 0o644))

	fixturesDir := filepath.Join(pkgAbsPath, "fixtures")
	require.NoError(t, os.MkdirAll(fixturesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fixturesDir, "input.txt"), []byte("hello\n"), 0o644))

	subpkgDir := filepath.Join(pkgAbsPath, "subpkg")
	require.NoError(t, os.MkdirAll(subpkgDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(subpkgDir, "subpkg.go"), []byte("package subpkg\n"), 0o644))

	// TUI semantics: testdata is included even if it contains *.go fixtures.
	testdataDir := filepath.Join(pkgAbsPath, "testdata")
	require.NoError(t, os.MkdirAll(testdataDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(testdataDir, "fixture.go"), []byte("package testdata\n"), 0o644))

	// But testdata under excluded dirs must remain excluded.
	subpkgTestdataDir := filepath.Join(subpkgDir, "testdata")
	require.NoError(t, os.MkdirAll(subpkgTestdataDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(subpkgTestdataDir, "fixture.go"), []byte("package testdata\n"), 0o644))

	otherDir := filepath.Join(sandbox, "otherdir")
	require.NoError(t, os.MkdirAll(otherDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(otherDir, "outside.txt"), []byte("outside\n"), 0o644))

	sandboxAuthorizer, _, err := authdomain.NewPermissiveSandboxAuthorizer(sandbox, nil)
	require.NoError(t, err)

	a, err := buildAuthorizerForTools(true, pkgRelPath, pkgAbsPath, sandboxAuthorizer, "", nil)
	require.NoError(t, err)
	require.NotNil(t, a)
	defer a.Close()

	require.True(t, a.IsCodeUnitDomain())

	// Allow supporting data dirs that contain no Go packages.
	require.NoError(t, a.IsAuthorizedForRead(false, "", "read_file", filepath.Join(fixturesDir, "input.txt")))

	// Exclude nested Go packages (dir contains *.go).
	require.Error(t, a.IsAuthorizedForRead(false, "", "read_file", filepath.Join(subpkgDir, "subpkg.go")))

	// Include package testdata, even if it contains *.go fixture files.
	require.NoError(t, a.IsAuthorizedForRead(false, "", "read_file", filepath.Join(testdataDir, "fixture.go")))

	// testdata under excluded dirs remains excluded.
	require.Error(t, a.IsAuthorizedForRead(false, "", "read_file", filepath.Join(subpkgTestdataDir, "fixture.go")))

	// Non-existent writes should be permitted if the parent dir is included.
	require.NoError(t, a.IsAuthorizedForWrite(false, "", "apply_patch", filepath.Join(fixturesDir, "new.txt")))
	require.Error(t, a.IsAuthorizedForWrite(false, "", "apply_patch", filepath.Join(subpkgDir, "new.txt")))

	// Confirm the code unit boundary is still enforced at least at the base-dir level.
	require.Error(t, a.IsAuthorizedForRead(false, "", "read_file", filepath.Join(otherDir, "outside.txt")))
}
