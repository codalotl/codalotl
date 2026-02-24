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
