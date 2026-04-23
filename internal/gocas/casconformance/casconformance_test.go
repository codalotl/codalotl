package casconformance

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codalotl/codalotl/internal/gocas"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/q/cas"
	"github.com/stretchr/testify/require"
)

func writeTestModuleWithPackage(t *testing.T, modDir string) *gocode.Package {
	t.Helper()

	err := os.WriteFile(filepath.Join(modDir, "go.mod"), []byte("module example.com/tmp\n\ngo 1.22\n"), 0o644)
	require.NoError(t, err)

	pkgDir := filepath.Join(modDir, "foo")
	err = os.MkdirAll(pkgDir, 0o755)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(pkgDir, "foo.go"), []byte("package foo\n\nfunc A() {}\n"), 0o644)
	require.NoError(t, err)

	m, err := gocode.NewModule(modDir)
	require.NoError(t, err)

	pkg, err := m.LoadPackageByRelativeDir("foo")
	require.NoError(t, err)
	return pkg
}

func writeTestDataFile(t *testing.T, pkg *gocode.Package, relPath string, content string) {
	t.Helper()

	absPath := filepath.Join(pkg.AbsolutePath(), relPath)
	err := os.MkdirAll(filepath.Dir(absPath), 0o755)
	require.NoError(t, err)

	err = os.WriteFile(absPath, []byte(content), 0o644)
	require.NoError(t, err)
}

func TestStoreAndRetrieve_RoundTrip(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)

	db := &gocas.DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err := Store(db, pkg, true)
	require.NoError(t, err)

	found, conforms, err := Retrieve(db, pkg)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, conforms)
}

func TestRetrieve_Miss(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)

	db := &gocas.DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	found, conforms, err := Retrieve(db, pkg)
	require.NoError(t, err)
	require.False(t, found)
	require.False(t, conforms)
}

func TestDelete_MissIsNoOp(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)

	db := &gocas.DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err := Delete(db, pkg)
	require.NoError(t, err)

	found, conforms, err := Retrieve(db, pkg)
	require.NoError(t, err)
	require.False(t, found)
	require.False(t, conforms)
}

func TestDelete_RemovesStoredMetadata(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)

	db := &gocas.DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err := Store(db, pkg, true)
	require.NoError(t, err)

	err = Delete(db, pkg)
	require.NoError(t, err)

	found, conforms, err := Retrieve(db, pkg)
	require.NoError(t, err)
	require.False(t, found)
	require.False(t, conforms)
}

func TestRetrieve_MissAfterTestdataChange(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)
	writeTestDataFile(t, pkg, filepath.Join("testdata", "fixture.txt"), "before\n")

	db := &gocas.DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err := Store(db, pkg, true)
	require.NoError(t, err)

	found, conforms, err := Retrieve(db, pkg)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, conforms)

	writeTestDataFile(t, pkg, filepath.Join("testdata", "fixture.txt"), "after\n")

	found, conforms, err = Retrieve(db, pkg)
	require.NoError(t, err)
	require.False(t, found)
	require.False(t, conforms)
}
