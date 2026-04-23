package gocas

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/codalotl/codalotl/internal/codeunit"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/q/cas"
	"github.com/stretchr/testify/require"
)

type testPayload struct {
	N int `json:"n"`
}

const testNamespace Namespace = "gocas-test"

func writeTestFile(t *testing.T, path string, contents []byte) {
	t.Helper()

	err := os.MkdirAll(filepath.Dir(path), 0o755)
	require.NoError(t, err)

	err = os.WriteFile(path, contents, 0o644)
	require.NoError(t, err)
}

func writeTestModuleWithPackage(t *testing.T, modDir string) *gocode.Package {
	t.Helper()

	writeTestFile(t, filepath.Join(modDir, "go.mod"), []byte("module example.com/tmp\n\ngo 1.22\n"))

	pkgDir := filepath.Join(modDir, "foo")
	err := os.MkdirAll(pkgDir, 0o755)
	require.NoError(t, err)

	writeTestFile(t, filepath.Join(pkgDir, "SPEC.md"), []byte("# foo\n\npackage spec\n"))
	writeTestFile(t, filepath.Join(pkgDir, "foo.go"), []byte("package foo\n\nfunc A() {}\n"))

	// Ensure we cover pkg.TestPackage hashing as well.
	writeTestFile(t, filepath.Join(pkgDir, "foo_test.go"), []byte("package foo_test\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n"))

	m, err := gocode.NewModule(modDir)
	require.NoError(t, err)

	pkg, err := m.LoadPackageByRelativeDir("foo")
	require.NoError(t, err)
	return pkg
}

func TestStoreOnPackageAndRetrieveOnPackage_RoundTrip(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)

	db := &DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err := db.StoreOnPackage(pkg, testNamespace, testPayload{N: 7})
	require.NoError(t, err)

	var got testPayload
	ok, ai, err := db.RetrieveOnPackage(pkg, testNamespace, &got)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 7, got.N)
	require.Greater(t, ai.UnixTimestamp, 0)
	require.Equal(t, []string{"foo/SPEC.md", "foo/foo.go", "foo/foo_test.go"}, ai.Paths)
}

func TestStoreOnCodeUnitAndRetrieveOnCodeUnit_RoundTrip(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	writeTestModuleWithPackage(t, baseDir)
	writeTestFile(t, filepath.Join(baseDir, "foo", "data", "config.yml"), []byte("name: demo\n"))
	writeTestFile(t, filepath.Join(baseDir, "foo", ".hidden", "ignored.txt"), []byte("ignored\n"))

	unit, err := codeunit.DefaultGoCodeUnit(filepath.Join(baseDir, "foo"))
	require.NoError(t, err)

	db := &DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err = db.StoreOnCodeUnit(unit, testNamespace, testPayload{N: 11})
	require.NoError(t, err)

	var got testPayload
	ok, ai, err := db.RetrieveOnCodeUnit(unit, testNamespace, &got)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 11, got.N)
	require.Greater(t, ai.UnixTimestamp, 0)
	require.Equal(t, []string{
		"foo/SPEC.md",
		"foo/data/config.yml",
		"foo/foo.go",
		"foo/foo_test.go",
	}, ai.Paths)
}

func TestStoreOnCodeUnitAndRetrieveOnCodeUnit_SupportFileAffectsKey(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	writeTestModuleWithPackage(t, baseDir)
	supportFile := filepath.Join(baseDir, "foo", "data", "config.yml")
	writeTestFile(t, supportFile, []byte("name: before\n"))

	unit, err := codeunit.DefaultGoCodeUnit(filepath.Join(baseDir, "foo"))
	require.NoError(t, err)

	db := &DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err = db.StoreOnCodeUnit(unit, testNamespace, testPayload{N: 5})
	require.NoError(t, err)

	err = os.WriteFile(supportFile, []byte("name: after\n"), 0o644)
	require.NoError(t, err)

	var got testPayload
	ok, _, err := db.RetrieveOnCodeUnit(unit, testNamespace, &got)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestDeleteOnCodeUnit_RemovesStoredValue(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	writeTestModuleWithPackage(t, baseDir)

	unit, err := codeunit.DefaultGoCodeUnit(filepath.Join(baseDir, "foo"))
	require.NoError(t, err)

	db := &DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err = db.StoreOnCodeUnit(unit, testNamespace, testPayload{N: 17})
	require.NoError(t, err)

	err = db.DeleteOnCodeUnit(unit, testNamespace)
	require.NoError(t, err)

	var got testPayload
	ok, _, err := db.RetrieveOnCodeUnit(unit, testNamespace, &got)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestDeleteOnCodeUnit_MissingRecordIsNoOp(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	writeTestModuleWithPackage(t, baseDir)

	unit, err := codeunit.DefaultGoCodeUnit(filepath.Join(baseDir, "foo"))
	require.NoError(t, err)

	db := &DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err = db.DeleteOnCodeUnit(unit, testNamespace)
	require.NoError(t, err)

	err = db.DeleteOnCodeUnit(unit, testNamespace)
	require.NoError(t, err)
}

func TestRetrieveOnPackage_MissDoesNotMutateTarget(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)

	db := &DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	target := testPayload{N: 123}
	ok, _, err := db.RetrieveOnPackage(pkg, testNamespace, &target)
	require.NoError(t, err)
	require.False(t, ok)
	require.Equal(t, 123, target.N)
}

func TestPackageHasherStableAcrossDifferentAbsoluteBaseDirs(t *testing.T) {
	baseDir1 := t.TempDir()
	baseDir2 := t.TempDir()
	casRoot := t.TempDir()

	pkg1 := writeTestModuleWithPackage(t, baseDir1)
	pkg2 := writeTestModuleWithPackage(t, baseDir2)

	db1 := &DB{
		BaseDir: baseDir1,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}
	db2 := &DB{
		BaseDir: baseDir2,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err := db1.StoreOnPackage(pkg1, testNamespace, testPayload{N: 9})
	require.NoError(t, err)

	var got testPayload
	ok, _, err := db2.RetrieveOnPackage(pkg2, testNamespace, &got)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 9, got.N)
}

func TestStoreOnPackage_IgnoresCodeUnitSupportFiles(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)
	supportFile := filepath.Join(baseDir, "foo", "data", "config.yml")
	writeTestFile(t, supportFile, []byte("name: before\n"))

	db := &DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err := db.StoreOnPackage(pkg, testNamespace, testPayload{N: 13})
	require.NoError(t, err)

	err = os.WriteFile(supportFile, []byte("name: after\n"), 0o644)
	require.NoError(t, err)

	var got testPayload
	ok, _, err := db.RetrieveOnPackage(pkg, testNamespace, &got)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 13, got.N)
}

func TestStoreOnPackage_ErrOnUnreadableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission-based unreadable file test is not reliable on windows")
	}
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)

	p := filepath.Join(baseDir, "foo", "foo.go")
	err := os.Chmod(p, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })

	db := &DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err = db.StoreOnPackage(pkg, testNamespace, testPayload{N: 1})
	require.Error(t, err)
}

func TestStoreOnPackageAndRetrieveOnPackage_SpecMdAffectsKey(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)

	db := &DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err := db.StoreOnPackage(pkg, testNamespace, testPayload{N: 7})
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(baseDir, "foo", "SPEC.md"), []byte("# foo\n\nchanged\n"), 0o644)
	require.NoError(t, err)

	var got testPayload
	ok, _, err := db.RetrieveOnPackage(pkg, testNamespace, &got)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestStoreOnPackage_ErrOnRelativeBaseDir(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)

	db := &DB{
		BaseDir: filepath.Base(baseDir), // intentionally relative
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err := db.StoreOnPackage(pkg, testNamespace, testPayload{N: 1})
	require.Error(t, err)
}
