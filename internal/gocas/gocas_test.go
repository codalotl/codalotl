package gocas

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/codalotl/codalotl/internal/gocode"

	"github.com/codalotl/codalotl/internal/codeunit"
	"github.com/codalotl/codalotl/internal/q/cas"
	"github.com/stretchr/testify/require"
)

type testPayload struct {
	N int `json:"n"`
}

const testNamespace Namespace = "gocas-test"

func writeTestModuleWithPackage(t *testing.T, modDir string) *gocode.Package {
	t.Helper()

	err := os.WriteFile(filepath.Join(modDir, "go.mod"), []byte("module example.com/tmp\n\ngo 1.22\n"), 0o644)
	require.NoError(t, err)

	pkgDir := filepath.Join(modDir, "foo")
	err = os.MkdirAll(pkgDir, 0o755)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(pkgDir, "foo.go"), []byte("package foo\n\nfunc A() {}\n"), 0o644)
	require.NoError(t, err)

	// Ensure we cover pkg.TestPackage hashing as well.
	err = os.WriteFile(filepath.Join(pkgDir, "foo_test.go"), []byte("package foo_test\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n"), 0o644)
	require.NoError(t, err)

	m, err := gocode.NewModule(modDir)
	require.NoError(t, err)

	pkg, err := m.LoadPackageByRelativeDir("foo")
	require.NoError(t, err)
	return pkg
}

func TestStoreOnCodeUnitAndRetrieve_RoundTrip(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	err := os.WriteFile(filepath.Join(baseDir, "a.txt"), []byte("hello"), 0o644)
	require.NoError(t, err)

	unit, err := codeunit.NewCodeUnit("unit", baseDir)
	require.NoError(t, err)

	db := &DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err = db.StoreOnCodeUnit(unit, testNamespace, testPayload{N: 7})
	require.NoError(t, err)

	var got testPayload
	ok, ai, err := db.RetrieveOnCodeUnit(unit, testNamespace, &got)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 7, got.N)
	require.Greater(t, ai.UnixTimestamp, 0)
	require.Equal(t, []string{"a.txt"}, ai.Paths)
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
	require.Equal(t, []string{"foo/foo.go", "foo/foo_test.go"}, ai.Paths)
}

func TestRetrieve_MissDoesNotMutateTarget(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	err := os.WriteFile(filepath.Join(baseDir, "a.txt"), []byte("hello"), 0o644)
	require.NoError(t, err)

	unit, err := codeunit.NewCodeUnit("unit", baseDir)
	require.NoError(t, err)

	db := &DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	target := testPayload{N: 123}
	ok, _, err := db.RetrieveOnCodeUnit(unit, testNamespace, &target)
	require.NoError(t, err)
	require.False(t, ok)
	require.Equal(t, 123, target.N)
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

func TestHasherStableAcrossDifferentAbsoluteBaseDirs(t *testing.T) {
	baseDir1 := t.TempDir()
	baseDir2 := t.TempDir()
	casRoot := t.TempDir()

	err := os.WriteFile(filepath.Join(baseDir1, "a.txt"), []byte("same"), 0o644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(baseDir2, "a.txt"), []byte("same"), 0o644)
	require.NoError(t, err)

	unit1, err := codeunit.NewCodeUnit("unit1", baseDir1)
	require.NoError(t, err)
	unit2, err := codeunit.NewCodeUnit("unit2", baseDir2)
	require.NoError(t, err)

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

	err = db1.StoreOnCodeUnit(unit1, testNamespace, testPayload{N: 9})
	require.NoError(t, err)

	var got testPayload
	ok, _, err := db2.RetrieveOnCodeUnit(unit2, testNamespace, &got)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 9, got.N)
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

func TestStoreOnCodeUnit_ErrOnUnreadableIncludedFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission-based unreadable file test is not reliable on windows")
	}
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	p := filepath.Join(baseDir, "a.txt")
	err := os.WriteFile(p, []byte("hello"), 0o644)
	require.NoError(t, err)

	unit, err := codeunit.NewCodeUnit("unit", baseDir)
	require.NoError(t, err)
	err = os.Chmod(p, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })

	// Keep the path present, but make it unreadable so the CAS hashing step fails.

	db := &DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err = db.StoreOnCodeUnit(unit, testNamespace, testPayload{N: 1})
	require.Error(t, err)
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

func TestStoreOnCodeUnit_ErrOnRelativeBaseDir(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	err := os.WriteFile(filepath.Join(baseDir, "a.txt"), []byte("hello"), 0o644)
	require.NoError(t, err)

	unit, err := codeunit.NewCodeUnit("unit", baseDir)
	require.NoError(t, err)

	db := &DB{
		BaseDir: filepath.Base(baseDir), // intentionally relative
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err = db.StoreOnCodeUnit(unit, testNamespace, testPayload{N: 1})
	require.Error(t, err)
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
