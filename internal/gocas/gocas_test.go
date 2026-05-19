package gocas

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/q/cas"
	"github.com/stretchr/testify/require"
)

type testPayload struct {
	N int `json:"n"`
}

var (
	testPackageNamespace = NamespaceSpec{
		Name:     "gocas-test",
		Version:  1,
		HashMode: HashModePackage,
	}
	testCodeUnitNamespace = NamespaceSpec{
		Name:     "gocas-test",
		Version:  1,
		HashMode: HashModeCodeUnit,
	}
)

func unsetCASDB(t *testing.T) {
	t.Helper()

	old, ok := os.LookupEnv(EnvCASDB)
	err := os.Unsetenv(EnvCASDB)
	require.NoError(t, err)

	t.Cleanup(func() {
		if ok {
			err := os.Setenv(EnvCASDB, old)
			require.NoError(t, err)
			return
		}
		err := os.Unsetenv(EnvCASDB)
		require.NoError(t, err)
	})
}

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

func loadTestPackage(t *testing.T, modDir string) *gocode.Package {
	t.Helper()

	m, err := gocode.NewModule(modDir)
	require.NoError(t, err)

	pkg, err := m.LoadPackageByRelativeDir("foo")
	require.NoError(t, err)
	return pkg
}

func storedRecordPath(t *testing.T, db *DB, namespace Namespace, hash string) string {
	t.Helper()

	p, ok := db.recordPath(namespace, hash)
	require.True(t, ok)
	return p
}

func readStoredRecord(t *testing.T, recordPath string) casRecordFile {
	t.Helper()

	b, err := os.ReadFile(recordPath)
	require.NoError(t, err)

	var record casRecordFile
	err = json.Unmarshal(b, &record)
	require.NoError(t, err)
	return record
}

func writeStoredRecord(t *testing.T, recordPath string, record casRecordFile) {
	t.Helper()

	b, err := json.Marshal(record)
	require.NoError(t, err)

	err = os.WriteFile(recordPath, b, 0o644)
	require.NoError(t, err)
}

func writeCASRecordAtHash(t *testing.T, db *DB, namespace Namespace, hash string, additionalInfo cas.AdditionalInfo) {
	t.Helper()

	recordPath := storedRecordPath(t, db, namespace, hash)
	err := os.MkdirAll(filepath.Dir(recordPath), 0o755)
	require.NoError(t, err)

	writeStoredRecord(t, recordPath, casRecordFile{
		Kind:           "cas-record-v1",
		Metadata:       json.RawMessage(`{"ok":true}`),
		AdditionalInfo: additionalInfo,
	})
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()

	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not available")
	}

	cmd := exec.Command(gitPath, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return string(out)
}

func TestRootDirForBaseDir_EnvOverride(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := filepath.Join(t.TempDir(), "cas")
	t.Setenv(EnvCASDB, casRoot)

	got, err := RootDirForBaseDir(baseDir)
	require.NoError(t, err)
	require.Equal(t, casRoot, got)

	_, err = os.Stat(casRoot)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestRootDirForBaseDir_EnvOverrideRelativeIsAbsolute(t *testing.T) {
	t.Setenv(EnvCASDB, "relative-cas-for-test")
	want, err := filepath.Abs("relative-cas-for-test")
	require.NoError(t, err)

	got, err := RootDirForBaseDir(t.TempDir())
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestRootDirForBaseDir_GitRootFallback(t *testing.T) {
	unsetCASDB(t)

	repoDir := t.TempDir()
	err := os.Mkdir(filepath.Join(repoDir, ".git"), 0o755)
	require.NoError(t, err)

	baseDir := filepath.Join(repoDir, "a", "b")
	err = os.MkdirAll(baseDir, 0o755)
	require.NoError(t, err)

	got, err := RootDirForBaseDir(baseDir)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(repoDir, ".codalotl", "cas"), got)
}

func TestRootDirForBaseDir_NearestGitRoot(t *testing.T) {
	unsetCASDB(t)

	outerRepoDir := t.TempDir()
	err := os.Mkdir(filepath.Join(outerRepoDir, ".git"), 0o755)
	require.NoError(t, err)

	innerRepoDir := filepath.Join(outerRepoDir, "inner")
	err = os.MkdirAll(filepath.Join(innerRepoDir, ".git"), 0o755)
	require.NoError(t, err)

	baseDir := filepath.Join(innerRepoDir, "pkg")
	err = os.MkdirAll(baseDir, 0o755)
	require.NoError(t, err)

	got, err := RootDirForBaseDir(baseDir)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(innerRepoDir, ".codalotl", "cas"), got)
}

func TestRootDirForBaseDir_NoGitRoot(t *testing.T) {
	unsetCASDB(t)

	_, err := RootDirForBaseDir(t.TempDir())
	require.Error(t, err)
}

func TestNewDBForBaseDir(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := filepath.Join(t.TempDir(), "cas")
	t.Setenv(EnvCASDB, casRoot)

	db, err := NewDBForBaseDir(baseDir)
	require.NoError(t, err)
	require.Equal(t, baseDir, db.BaseDir)
	require.Equal(t, casRoot, db.DB.AbsRoot)

	_, err = os.Stat(casRoot)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestNamespaceSpecNamespace(t *testing.T) {
	spec := NamespaceSpec{Name: "specconforms", Version: 1, HashMode: HashModeCodeUnit}

	require.Equal(t, Namespace("specconforms-1"), spec.Namespace())
}

func TestStoreAndRetrieve_PackageHashRoundTrip(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)

	db := &DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err := db.Store(pkg, testPackageNamespace, testPayload{N: 7})
	require.NoError(t, err)

	var got testPayload
	ok, ai, err := db.Retrieve(pkg, testPackageNamespace, &got)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 7, got.N)
	require.Greater(t, ai.UnixTimestamp, 0)
	require.Equal(t, []string{"foo/SPEC.md", "foo/foo.go", "foo/foo_test.go"}, ai.Paths)
}

func TestStore_CreatesMissingCASRoot(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := filepath.Join(t.TempDir(), "cas")

	pkg := writeTestModuleWithPackage(t, baseDir)

	db := &DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err := db.Store(pkg, testPackageNamespace, testPayload{N: 7})
	require.NoError(t, err)

	var got testPayload
	ok, _, err := db.Retrieve(pkg, testPackageNamespace, &got)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 7, got.N)
}

func TestStoreAndRetrieve_CodeUnitHashRoundTrip(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)
	writeTestFile(t, filepath.Join(baseDir, "foo", "data", "config.yml"), []byte("name: demo\n"))
	writeTestFile(t, filepath.Join(baseDir, "foo", ".hidden", "ignored.txt"), []byte("ignored\n"))

	db := &DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err := db.Store(pkg, testCodeUnitNamespace, testPayload{N: 11})
	require.NoError(t, err)

	var got testPayload
	ok, ai, err := db.Retrieve(pkg, testCodeUnitNamespace, &got)
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

func TestStoreAndRetrieve_CodeUnitHashSupportFileAffectsKey(t *testing.T) {
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

	err := db.Store(pkg, testCodeUnitNamespace, testPayload{N: 5})
	require.NoError(t, err)

	err = os.WriteFile(supportFile, []byte("name: after\n"), 0o644)
	require.NoError(t, err)

	var got testPayload
	ok, _, err := db.Retrieve(pkg, testCodeUnitNamespace, &got)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestDelete_CodeUnitHashRemovesStoredValue(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)

	db := &DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err := db.Store(pkg, testCodeUnitNamespace, testPayload{N: 17})
	require.NoError(t, err)

	err = db.Delete(pkg, testCodeUnitNamespace)
	require.NoError(t, err)

	var got testPayload
	ok, _, err := db.Retrieve(pkg, testCodeUnitNamespace, &got)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestDelete_MissingRecordIsNoOp(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)

	db := &DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err := db.Delete(pkg, testCodeUnitNamespace)
	require.NoError(t, err)

	err = db.Delete(pkg, testCodeUnitNamespace)
	require.NoError(t, err)
}

func TestDelete_MissingCASRootIsNoOp(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := filepath.Join(t.TempDir(), "cas")

	pkg := writeTestModuleWithPackage(t, baseDir)

	db := &DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err := db.Delete(pkg, testCodeUnitNamespace)
	require.NoError(t, err)

	err = db.Delete(pkg, testCodeUnitNamespace)
	require.NoError(t, err)
}

func TestRetrieve_MissDoesNotMutateTarget(t *testing.T) {
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
	ok, _, err := db.Retrieve(pkg, testPackageNamespace, &target)
	require.NoError(t, err)
	require.False(t, ok)
	require.Equal(t, 123, target.N)
}

func TestSummarizePackage_CurrentRecord(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)

	db := &DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err := db.Store(pkg, testPackageNamespace, testPayload{N: 7})
	require.NoError(t, err)

	summary, err := db.SummarizePackage(pkg, testPackageNamespace)
	require.NoError(t, err)
	require.NotNil(t, summary.Current)
	require.Nil(t, summary.PriorInvalidated)
	require.Nil(t, summary.ChurnPercent)
	require.NotEmpty(t, summary.Current.Hash)
	require.False(t, summary.Current.Time.IsZero())
	require.Equal(t, []string{"foo/SPEC.md", "foo/foo.go", "foo/foo_test.go"}, summary.Current.AdditionalInfo.Paths)
}

func TestSummarizePackage_MissingCASRootIsEmpty(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := filepath.Join(t.TempDir(), "cas")

	pkg := writeTestModuleWithPackage(t, baseDir)

	db := &DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	summary, err := db.SummarizePackage(pkg, testPackageNamespace)
	require.NoError(t, err)
	require.Nil(t, summary.Current)
	require.Nil(t, summary.PriorInvalidated)
	require.Nil(t, summary.ChurnPercent)
}

func TestSummarizePackage_CurrentRecordUsesFileMTimeWhenMetadataTimeMissing(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)

	db := &DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err := db.Store(pkg, testPackageNamespace, testPayload{N: 7})
	require.NoError(t, err)

	summary, err := db.SummarizePackage(pkg, testPackageNamespace)
	require.NoError(t, err)
	require.NotNil(t, summary.Current)

	recordPath := storedRecordPath(t, db, testPackageNamespace.Namespace(), summary.Current.Hash)
	record := readStoredRecord(t, recordPath)
	record.AdditionalInfo.UnixTimestamp = 0
	writeStoredRecord(t, recordPath, record)

	mtime := time.Unix(1234, 0)
	err = os.Chtimes(recordPath, mtime, mtime)
	require.NoError(t, err)

	summary, err = db.SummarizePackage(pkg, testPackageNamespace)
	require.NoError(t, err)
	require.NotNil(t, summary.Current)
	require.WithinDuration(t, mtime, summary.Current.Time, time.Second)
}

func TestSummarizePackage_PriorInvalidatedRecordMatchesAbsoluteStoredPathsAndSkipsNoise(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)

	db := &DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err := db.Store(pkg, testPackageNamespace, testPayload{N: 7})
	require.NoError(t, err)

	summary, err := db.SummarizePackage(pkg, testPackageNamespace)
	require.NoError(t, err)
	require.NotNil(t, summary.Current)
	priorHash := summary.Current.Hash
	priorRecordPath := storedRecordPath(t, db, testPackageNamespace.Namespace(), priorHash)

	record := readStoredRecord(t, priorRecordPath)
	for i, relPath := range record.AdditionalInfo.Paths {
		record.AdditionalInfo.Paths[i] = filepath.Join(baseDir, relPath)
	}
	writeStoredRecord(t, priorRecordPath, record)

	writeCASRecordAtHash(t, db, testPackageNamespace.Namespace(), "aabb", cas.AdditionalInfo{
		UnixTimestamp: record.AdditionalInfo.UnixTimestamp + 1000,
		Paths:         []string{"bar/bar.go"},
	})

	corruptPath := storedRecordPath(t, db, testPackageNamespace.Namespace(), "ccdd")
	err = os.MkdirAll(filepath.Dir(corruptPath), 0o755)
	require.NoError(t, err)
	err = os.WriteFile(corruptPath, []byte("{"), 0o644)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(baseDir, "foo", "foo.go"), []byte("package foo\n\nfunc A() {}\nfunc B() {}\n"), 0o644)
	require.NoError(t, err)

	summary, err = db.SummarizePackage(pkg, testPackageNamespace)
	require.NoError(t, err)
	require.Nil(t, summary.Current)
	require.NotNil(t, summary.PriorInvalidated)
	require.Equal(t, priorHash, summary.PriorInvalidated.Hash)
	require.True(t, filepath.IsAbs(summary.PriorInvalidated.AdditionalInfo.Paths[0]))
	require.Nil(t, summary.ChurnPercent)
}

func TestSummarizePackage_ChurnPercentFromGitMetadata(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)
	runGit(t, baseDir, "init")
	runGit(t, baseDir, "config", "user.email", "test@example.com")
	runGit(t, baseDir, "config", "user.name", "Test User")
	runGit(t, baseDir, "add", ".")
	runGit(t, baseDir, "commit", "-m", "initial")

	db := &DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err := db.Store(pkg, testPackageNamespace, testPayload{N: 7})
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(baseDir, "foo", "foo.go"), []byte("package foo\n\nfunc A() {}\nfunc B() {}\n"), 0o644)
	require.NoError(t, err)

	summary, err := db.SummarizePackage(pkg, testPackageNamespace)
	require.NoError(t, err)
	require.Nil(t, summary.Current)
	require.NotNil(t, summary.PriorInvalidated)
	require.NotNil(t, summary.ChurnPercent)
	require.Greater(t, *summary.ChurnPercent, 0.0)
}

func TestSummarizePackage_ChurnPercentCountsUntrackedCurrentFiles(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)
	runGit(t, baseDir, "init")
	runGit(t, baseDir, "config", "user.email", "test@example.com")
	runGit(t, baseDir, "config", "user.name", "Test User")
	runGit(t, baseDir, "add", ".")
	runGit(t, baseDir, "commit", "-m", "initial")

	db := &DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err := db.Store(pkg, testPackageNamespace, testPayload{N: 7})
	require.NoError(t, err)

	writeTestFile(t, filepath.Join(baseDir, "foo", "new.go"), []byte("package foo\n\nfunc B() {}\nfunc C() {}\n"))
	pkg = loadTestPackage(t, baseDir)

	summary, err := db.SummarizePackage(pkg, testPackageNamespace)
	require.NoError(t, err)
	require.Nil(t, summary.Current)
	require.NotNil(t, summary.PriorInvalidated)
	require.NotNil(t, summary.ChurnPercent)
	require.InEpsilon(t, (4.0/11.0)*100.0, *summary.ChurnPercent, 0.001)
}

func TestSummarizePackage_ChurnUsesOlderReliableRecordWhenNewestPriorIsDirty(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)
	runGit(t, baseDir, "init")
	runGit(t, baseDir, "config", "user.email", "test@example.com")
	runGit(t, baseDir, "config", "user.name", "Test User")
	runGit(t, baseDir, "add", ".")
	runGit(t, baseDir, "commit", "-m", "initial")

	db := &DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err := db.Store(pkg, testPackageNamespace, testPayload{N: 7})
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(baseDir, "foo", "foo.go"), []byte("package foo\n\nfunc A() {}\nfunc Dirty() {}\n"), 0o644)
	require.NoError(t, err)

	err = db.Store(pkg, testPackageNamespace, testPayload{N: 8})
	require.NoError(t, err)
	dirtyHasher, _, err := db.hasherForPackageSpec(pkg, testPackageNamespace)
	require.NoError(t, err)
	dirtyHash := dirtyHasher.Hash()

	dirtyRecordPath := storedRecordPath(t, db, testPackageNamespace.Namespace(), dirtyHash)
	dirtyRecord := readStoredRecord(t, dirtyRecordPath)
	require.False(t, dirtyRecord.AdditionalInfo.GitClean)
	dirtyRecord.AdditionalInfo.UnixTimestamp += 100
	writeStoredRecord(t, dirtyRecordPath, dirtyRecord)

	err = os.WriteFile(filepath.Join(baseDir, "foo", "foo.go"), []byte("package foo\n\nfunc A() {}\nfunc B() {}\n"), 0o644)
	require.NoError(t, err)

	summary, err := db.SummarizePackage(pkg, testPackageNamespace)
	require.NoError(t, err)
	require.Nil(t, summary.Current)
	require.NotNil(t, summary.PriorInvalidated)
	require.Equal(t, dirtyHash, summary.PriorInvalidated.Hash)
	require.NotNil(t, summary.ChurnPercent)
	require.Greater(t, *summary.ChurnPercent, 0.0)
}

func TestSummarizePackage_ChurnCanUseCommitThatAddedCASRecordFromSubmodule(t *testing.T) {
	repoDir := t.TempDir()
	baseDir := filepath.Join(repoDir, "subdir", "module")
	casRoot := filepath.Join(repoDir, ".codalotl", "cas")

	pkg := writeTestModuleWithPackage(t, baseDir)
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "config", "user.email", "test@example.com")
	runGit(t, repoDir, "config", "user.name", "Test User")
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "initial")

	db := &DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err := db.Store(pkg, testPackageNamespace, testPayload{N: 7})
	require.NoError(t, err)
	initialHasher, _, err := db.hasherForPackageSpec(pkg, testPackageNamespace)
	require.NoError(t, err)
	initialHash := initialHasher.Hash()

	recordPath := storedRecordPath(t, db, testPackageNamespace.Namespace(), initialHash)
	record := readStoredRecord(t, recordPath)
	record.AdditionalInfo.GitCommit = ""
	record.AdditionalInfo.GitClean = false
	writeStoredRecord(t, recordPath, record)

	runGit(t, repoDir, "add", ".codalotl")
	runGit(t, repoDir, "commit", "-m", "add cas")

	err = os.WriteFile(filepath.Join(baseDir, "foo", "foo.go"), []byte("package foo\n\nfunc A() {}\nfunc B() {}\n"), 0o644)
	require.NoError(t, err)

	summary, err := db.SummarizePackage(pkg, testPackageNamespace)
	require.NoError(t, err)
	require.Nil(t, summary.Current)
	require.NotNil(t, summary.PriorInvalidated)
	require.Equal(t, initialHash, summary.PriorInvalidated.Hash)
	require.NotNil(t, summary.ChurnPercent)
	require.Greater(t, *summary.ChurnPercent, 0.0)
}

func TestSummarizePackage_ChurnCanUseCommitThatAddedCASRecord(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := filepath.Join(baseDir, ".codalotl", "cas")

	pkg := writeTestModuleWithPackage(t, baseDir)
	runGit(t, baseDir, "init")
	runGit(t, baseDir, "config", "user.email", "test@example.com")
	runGit(t, baseDir, "config", "user.name", "Test User")
	runGit(t, baseDir, "add", ".")
	runGit(t, baseDir, "commit", "-m", "initial")

	db := &DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err := db.Store(pkg, testPackageNamespace, testPayload{N: 7})
	require.NoError(t, err)
	initialHasher, _, err := db.hasherForPackageSpec(pkg, testPackageNamespace)
	require.NoError(t, err)
	initialHash := initialHasher.Hash()

	recordPath := storedRecordPath(t, db, testPackageNamespace.Namespace(), initialHash)
	record := readStoredRecord(t, recordPath)
	record.AdditionalInfo.GitCommit = ""
	record.AdditionalInfo.GitClean = false
	writeStoredRecord(t, recordPath, record)

	runGit(t, baseDir, "add", ".codalotl")
	runGit(t, baseDir, "commit", "-m", "add cas")

	err = os.WriteFile(filepath.Join(baseDir, "foo", "foo.go"), []byte("package foo\n\nfunc A() {}\nfunc B() {}\n"), 0o644)
	require.NoError(t, err)

	summary, err := db.SummarizePackage(pkg, testPackageNamespace)
	require.NoError(t, err)
	require.Nil(t, summary.Current)
	require.NotNil(t, summary.PriorInvalidated)
	require.Equal(t, initialHash, summary.PriorInvalidated.Hash)
	require.NotNil(t, summary.ChurnPercent)
	require.Greater(t, *summary.ChurnPercent, 0.0)
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

	err := db1.Store(pkg1, testPackageNamespace, testPayload{N: 9})
	require.NoError(t, err)

	var got testPayload
	ok, _, err := db2.Retrieve(pkg2, testPackageNamespace, &got)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 9, got.N)
}

func TestStore_PackageHashIgnoresCodeUnitSupportFiles(t *testing.T) {
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

	err := db.Store(pkg, testPackageNamespace, testPayload{N: 13})
	require.NoError(t, err)

	err = os.WriteFile(supportFile, []byte("name: after\n"), 0o644)
	require.NoError(t, err)

	var got testPayload
	ok, _, err := db.Retrieve(pkg, testPackageNamespace, &got)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 13, got.N)
}

func TestStore_ErrOnUnreadableFile(t *testing.T) {
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

	err = db.Store(pkg, testPackageNamespace, testPayload{N: 1})
	require.Error(t, err)
}

func TestStoreAndRetrieve_PackageHashSpecMdAffectsKey(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)

	db := &DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err := db.Store(pkg, testPackageNamespace, testPayload{N: 7})
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(baseDir, "foo", "SPEC.md"), []byte("# foo\n\nchanged\n"), 0o644)
	require.NoError(t, err)

	var got testPayload
	ok, _, err := db.Retrieve(pkg, testPackageNamespace, &got)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestStore_ErrOnRelativeBaseDir(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)

	db := &DB{
		BaseDir: filepath.Base(baseDir), // intentionally relative
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}

	err := db.Store(pkg, testPackageNamespace, testPayload{N: 1})
	require.Error(t, err)
}
