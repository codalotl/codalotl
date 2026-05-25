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

func newTestDB(baseDir, casRoot string) *DB {
	return &DB{
		BaseDir: baseDir,
		DB: cas.DB{
			AbsRoot: casRoot,
		},
	}
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
		Kind:           casRecordKind,
		Metadata:       json.RawMessage(`{"ok":true}`),
		AdditionalInfo: additionalInfo,
	})
}

func currentPackageHash(t *testing.T, db *DB, pkg *gocode.Package, spec NamespaceSpec) string {
	t.Helper()

	hasher, _, err := db.hasherForPackageSpec(pkg, spec)
	require.NoError(t, err)
	return hasher.Hash()
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()

	return runGitEnv(t, dir, nil, args...)
}

func runGitEnv(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()

	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not available")
	}

	cmd := exec.Command(gitPath, args...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return string(out)
}

func initTestGitRepo(t *testing.T, dir string) {
	t.Helper()

	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")
}

func commitGit(t *testing.T, dir, pathspec, msg string) {
	t.Helper()

	runGit(t, dir, "add", pathspec)
	runGit(t, dir, "commit", "-m", msg)
}

func commitGitAt(t *testing.T, dir, msg string, at time.Time) {
	t.Helper()

	date := at.Format(time.RFC3339)
	runGitEnv(t, dir, []string{
		"GIT_AUTHOR_DATE=" + date,
		"GIT_COMMITTER_DATE=" + date,
	}, "commit", "-m", msg)
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

func TestRootDirForBaseDir_EnvOverrideEmptyErrors(t *testing.T) {
	t.Setenv(EnvCASDB, "")

	_, err := RootDirForBaseDir(t.TempDir())
	require.Error(t, err)
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

func TestRootDirForBaseDir_EmptyBaseDirErrors(t *testing.T) {
	unsetCASDB(t)

	_, err := RootDirForBaseDir("")
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

func TestNewDBForBaseDir_EmptyBaseDirErrors(t *testing.T) {
	_, err := NewDBForBaseDir("")
	require.Error(t, err)
}

func TestNamespaceSpecNamespace(t *testing.T) {
	spec := NamespaceSpec{Name: "specconforms", Version: 1, HashMode: HashModeCodeUnit}

	require.Equal(t, Namespace("specconforms-1"), spec.Namespace())
}

func TestPublicMethodsValidateNilInputs(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()
	pkg := writeTestModuleWithPackage(t, baseDir)
	db := newTestDB(baseDir, casRoot)

	err := db.Store(nil, testPackageNamespace, testPayload{N: 1})
	require.Error(t, err)

	var target testPayload
	ok, _, err := db.Retrieve(nil, testPackageNamespace, &target)
	require.Error(t, err)
	require.False(t, ok)

	ok, _, err = db.Retrieve(pkg, testPackageNamespace, nil)
	require.Error(t, err)
	require.False(t, ok)

	_, err = db.SummarizePackage(nil, testPackageNamespace)
	require.Error(t, err)

	_, err = db.RecertifyPackage(nil, testPackageNamespace)
	require.Error(t, err)

	_, err = db.Prune([]NamespaceSpec{testPackageNamespace}, []*gocode.Package{nil}, PruneOptions{})
	require.Error(t, err)

	err = db.Delete(nil, testPackageNamespace)
	require.Error(t, err)
}

func TestStore_ValidatesNamespaceSpec(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()
	pkg := writeTestModuleWithPackage(t, baseDir)
	db := newTestDB(baseDir, casRoot)

	tests := []struct {
		name string
		spec NamespaceSpec
	}{
		{
			name: "empty name",
			spec: NamespaceSpec{
				Version:  1,
				HashMode: HashModePackage,
			},
		},
		{
			name: "name contains path separator",
			spec: NamespaceSpec{
				Name:     "bad/name",
				Version:  1,
				HashMode: HashModePackage,
			},
		},
		{
			name: "non-positive version",
			spec: NamespaceSpec{
				Name:     "bad",
				Version:  0,
				HashMode: HashModePackage,
			},
		},
		{
			name: "unsupported hash mode",
			spec: NamespaceSpec{
				Name:     "bad",
				Version:  1,
				HashMode: HashMode("other"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := db.Store(pkg, tt.spec, testPayload{N: 1})
			require.Error(t, err)
		})
	}
}

func TestStoreAndRetrieve_PackageHashRoundTrip(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)

	db := newTestDB(baseDir, casRoot)

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

	db := newTestDB(baseDir, casRoot)

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

	db := newTestDB(baseDir, casRoot)

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

	db := newTestDB(baseDir, casRoot)

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

	db := newTestDB(baseDir, casRoot)

	err := db.Store(pkg, testCodeUnitNamespace, testPayload{N: 17})
	require.NoError(t, err)

	err = db.Delete(pkg, testCodeUnitNamespace)
	require.NoError(t, err)

	var got testPayload
	ok, _, err := db.Retrieve(pkg, testCodeUnitNamespace, &got)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestDelete_MissingValueIsNoOp(t *testing.T) {
	tests := []struct {
		name    string
		casRoot func(t *testing.T) string
	}{
		{
			name: "existing CAS root",
			casRoot: func(t *testing.T) string {
				t.Helper()
				return t.TempDir()
			},
		},
		{
			name: "missing CAS root",
			casRoot: func(t *testing.T) string {
				t.Helper()
				return filepath.Join(t.TempDir(), "cas")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseDir := t.TempDir()
			pkg := writeTestModuleWithPackage(t, baseDir)
			db := newTestDB(baseDir, tt.casRoot(t))

			err := db.Delete(pkg, testCodeUnitNamespace)
			require.NoError(t, err)
		})
	}
}

func TestRetrieve_MissDoesNotMutateTarget(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)

	db := newTestDB(baseDir, casRoot)

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

	db := newTestDB(baseDir, casRoot)

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

	db := newTestDB(baseDir, casRoot)

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

	db := newTestDB(baseDir, casRoot)

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

func TestSummarizePackage_CurrentRecordUsesMetadataTimeWhenAddCommitMissing(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)

	db := newTestDB(baseDir, casRoot)

	err := db.Store(pkg, testPackageNamespace, testPayload{N: 7})
	require.NoError(t, err)

	summary, err := db.SummarizePackage(pkg, testPackageNamespace)
	require.NoError(t, err)
	require.NotNil(t, summary.Current)

	recordPath := storedRecordPath(t, db, testPackageNamespace.Namespace(), summary.Current.Hash)
	record := readStoredRecord(t, recordPath)
	metadataTime := time.Unix(1234, 0)
	record.AdditionalInfo.UnixTimestamp = int(metadataTime.Unix())
	writeStoredRecord(t, recordPath, record)

	mtime := time.Unix(5678, 0)
	err = os.Chtimes(recordPath, mtime, mtime)
	require.NoError(t, err)

	summary, err = db.SummarizePackage(pkg, testPackageNamespace)
	require.NoError(t, err)
	require.NotNil(t, summary.Current)
	require.Equal(t, metadataTime, summary.Current.Time)
}

func TestSummarizePackage_CurrentRecordUsesCASAddCommitTimeFromSubdirectoryModule(t *testing.T) {
	repoDir := t.TempDir()
	baseDir := filepath.Join(repoDir, "subdir", "module")
	casRoot := filepath.Join(repoDir, ".codalotl", "cas")

	pkg := writeTestModuleWithPackage(t, baseDir)
	initTestGitRepo(t, repoDir)
	runGit(t, repoDir, "add", ".")
	commitGitAt(t, repoDir, "initial", time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC))

	db := newTestDB(baseDir, casRoot)

	err := db.Store(pkg, testPackageNamespace, testPayload{N: 7})
	require.NoError(t, err)

	summary, err := db.SummarizePackage(pkg, testPackageNamespace)
	require.NoError(t, err)
	require.NotNil(t, summary.Current)

	recordPath := storedRecordPath(t, db, testPackageNamespace.Namespace(), summary.Current.Hash)
	record := readStoredRecord(t, recordPath)
	metadataTime := time.Date(2010, 3, 4, 5, 6, 7, 0, time.UTC)
	record.AdditionalInfo.UnixTimestamp = int(metadataTime.Unix())
	writeStoredRecord(t, recordPath, record)

	addCommitTime := time.Date(2004, 5, 6, 7, 8, 9, 0, time.UTC)
	runGit(t, repoDir, "add", ".codalotl")
	commitGitAt(t, repoDir, "add cas", addCommitTime)

	mtime := time.Date(2015, 6, 7, 8, 9, 10, 0, time.UTC)
	err = os.Chtimes(recordPath, mtime, mtime)
	require.NoError(t, err)

	summary, err = db.SummarizePackage(pkg, testPackageNamespace)
	require.NoError(t, err)
	require.NotNil(t, summary.Current)
	require.WithinDuration(t, addCommitTime, summary.Current.Time, time.Second)
}

func TestSummarizePackage_PriorInvalidatedRecordMatchesAbsoluteStoredPathsAndSkipsNoise(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)

	db := newTestDB(baseDir, casRoot)

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
	initTestGitRepo(t, baseDir)
	commitGit(t, baseDir, ".", "initial")

	db := newTestDB(baseDir, casRoot)

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
	initTestGitRepo(t, baseDir)
	commitGit(t, baseDir, ".", "initial")

	db := newTestDB(baseDir, casRoot)

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
	initTestGitRepo(t, baseDir)
	commitGit(t, baseDir, ".", "initial")

	db := newTestDB(baseDir, casRoot)

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

func TestSummarizePackage_ChurnCanUseCommitThatAddedCASRecord(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T) (repoDir, baseDir, casRoot string)
	}{
		{
			name: "root module",
			setup: func(t *testing.T) (string, string, string) {
				t.Helper()
				baseDir := t.TempDir()
				return baseDir, baseDir, filepath.Join(baseDir, ".codalotl", "cas")
			},
		},
		{
			name: "subdirectory module",
			setup: func(t *testing.T) (string, string, string) {
				t.Helper()
				repoDir := t.TempDir()
				baseDir := filepath.Join(repoDir, "subdir", "module")
				return repoDir, baseDir, filepath.Join(repoDir, ".codalotl", "cas")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoDir, baseDir, casRoot := tt.setup(t)
			pkg := writeTestModuleWithPackage(t, baseDir)
			initTestGitRepo(t, repoDir)
			commitGit(t, repoDir, ".", "initial")

			db := newTestDB(baseDir, casRoot)

			err := db.Store(pkg, testPackageNamespace, testPayload{N: 7})
			require.NoError(t, err)
			initialHash := currentPackageHash(t, db, pkg, testPackageNamespace)

			recordPath := storedRecordPath(t, db, testPackageNamespace.Namespace(), initialHash)
			record := readStoredRecord(t, recordPath)
			record.AdditionalInfo.GitCommit = ""
			record.AdditionalInfo.GitClean = false
			writeStoredRecord(t, recordPath, record)

			commitGit(t, repoDir, ".codalotl", "add cas")

			err = os.WriteFile(filepath.Join(baseDir, "foo", "foo.go"), []byte("package foo\n\nfunc A() {}\nfunc B() {}\n"), 0o644)
			require.NoError(t, err)

			summary, err := db.SummarizePackage(pkg, testPackageNamespace)
			require.NoError(t, err)
			require.Nil(t, summary.Current)
			require.NotNil(t, summary.PriorInvalidated)
			require.Equal(t, initialHash, summary.PriorInvalidated.Hash)
			require.NotNil(t, summary.ChurnPercent)
			require.Greater(t, *summary.ChurnPercent, 0.0)
		})
	}
}

func TestRecertifyPackage_CurrentRecordIsNoOp(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)

	db := newTestDB(baseDir, casRoot)

	err := db.Store(pkg, testPackageNamespace, testPayload{N: 7})
	require.NoError(t, err)

	currentHash := currentPackageHash(t, db, pkg, testPackageNamespace)
	recordPath := storedRecordPath(t, db, testPackageNamespace.Namespace(), currentHash)
	before, err := os.ReadFile(recordPath)
	require.NoError(t, err)

	result, err := db.RecertifyPackage(pkg, testPackageNamespace)
	require.NoError(t, err)
	require.Equal(t, PackageRecertificationStatusCurrent, result.Status)
	require.Equal(t, currentHash, result.CurrentHash)
	require.Empty(t, result.SourceHash)
	require.Empty(t, result.SourceRecord)
	require.Empty(t, result.Warnings)

	after, err := os.ReadFile(recordPath)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestRecertifyPackage_NoPriorIsNormalMiss(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := filepath.Join(t.TempDir(), "cas")

	pkg := writeTestModuleWithPackage(t, baseDir)

	db := newTestDB(baseDir, casRoot)

	hasher, _, err := db.hasherForPackageSpec(pkg, testPackageNamespace)
	require.NoError(t, err)

	result, err := db.RecertifyPackage(pkg, testPackageNamespace)
	require.NoError(t, err)
	require.Equal(t, PackageRecertificationStatusNoPrior, result.Status)
	require.Equal(t, hasher.Hash(), result.CurrentHash)
	require.Empty(t, result.SourceHash)
	require.Empty(t, result.SourceRecord)
	require.Empty(t, result.Warnings)

	_, err = os.Stat(db.namespaceDir(testPackageNamespace.Namespace()))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestRecertifyPackage_CopiesPayloadAndProvenance(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)

	db := newTestDB(baseDir, casRoot)

	err := db.Store(pkg, testPackageNamespace, testPayload{N: 7})
	require.NoError(t, err)

	sourceSummary, err := db.SummarizePackage(pkg, testPackageNamespace)
	require.NoError(t, err)
	require.NotNil(t, sourceSummary.Current)
	sourceHash := sourceSummary.Current.Hash
	sourceRecordID, ok := recordID(testPackageNamespace.Namespace(), sourceHash)
	require.True(t, ok)
	sourceRecordPath := storedRecordPath(t, db, testPackageNamespace.Namespace(), sourceHash)
	sourceRecordBefore := readStoredRecord(t, sourceRecordPath)
	sourceBytesBefore, err := os.ReadFile(sourceRecordPath)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(baseDir, "foo", "foo.go"), []byte("package foo\n\nfunc A() {}\nfunc B() {}\n"), 0o644)
	require.NoError(t, err)

	result, err := db.RecertifyPackage(pkg, testPackageNamespace)
	require.NoError(t, err)
	require.Equal(t, PackageRecertificationStatusRecertified, result.Status)
	require.NotEmpty(t, result.CurrentHash)
	require.NotEqual(t, sourceHash, result.CurrentHash)
	require.Equal(t, sourceHash, result.SourceHash)
	require.Equal(t, sourceRecordID, result.SourceRecord)

	var got testPayload
	ok, ai, err := db.Retrieve(pkg, testPackageNamespace, &got)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, testPayload{N: 7}, got)
	require.True(t, ai.Recertified)
	require.Equal(t, sourceHash, ai.RecertifiedFromHash)
	require.Equal(t, sourceRecordID, ai.RecertifiedFromRecord)
	require.Equal(t, []string{"foo/SPEC.md", "foo/foo.go", "foo/foo_test.go"}, ai.Paths)

	currentRecordPath := storedRecordPath(t, db, testPackageNamespace.Namespace(), result.CurrentHash)
	currentRecord := readStoredRecord(t, currentRecordPath)
	require.Equal(t, sourceRecordBefore.Metadata, currentRecord.Metadata)
	require.True(t, currentRecord.AdditionalInfo.Recertified)
	require.Equal(t, sourceHash, currentRecord.AdditionalInfo.RecertifiedFromHash)
	require.Equal(t, sourceRecordID, currentRecord.AdditionalInfo.RecertifiedFromRecord)

	sourceBytesAfter, err := os.ReadFile(sourceRecordPath)
	require.NoError(t, err)
	require.Equal(t, sourceBytesBefore, sourceBytesAfter)
}

func TestRecertifyPackage_WarnsForRiskyRecertification(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)
	initTestGitRepo(t, baseDir)
	commitGit(t, baseDir, ".", "initial")

	db := newTestDB(baseDir, casRoot)

	err := db.Store(pkg, testPackageNamespace, testPayload{N: 7})
	require.NoError(t, err)

	sourceSummary, err := db.SummarizePackage(pkg, testPackageNamespace)
	require.NoError(t, err)
	require.NotNil(t, sourceSummary.Current)

	sourceRecordPath := storedRecordPath(t, db, testPackageNamespace.Namespace(), sourceSummary.Current.Hash)
	sourceRecord := readStoredRecord(t, sourceRecordPath)
	sourceRecord.AdditionalInfo.UnixTimestamp = int(time.Now().Add(-31 * 24 * time.Hour).Unix())
	writeStoredRecord(t, sourceRecordPath, sourceRecord)

	err = os.WriteFile(filepath.Join(baseDir, "foo", "foo.go"), []byte("package foo\n\nfunc A() {}\nfunc B() {}\nfunc C() {}\nfunc D() {}\n"), 0o644)
	require.NoError(t, err)

	result, err := db.RecertifyPackage(pkg, testPackageNamespace)
	require.NoError(t, err)
	require.Equal(t, PackageRecertificationStatusRecertified, result.Status)
	require.Equal(t, []string{
		"current git worktree is dirty",
		"large churn (>=20%)",
		"source record is >=30 days old",
	}, result.Warnings)
}

func TestPrune_RemovesPriorNamespaceVersionsAndOldSupersededRecords(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)
	db := newTestDB(baseDir, casRoot)
	activeSpec := NamespaceSpec{
		Name:     "gocas-test",
		Version:  2,
		HashMode: HashModePackage,
	}

	err := db.Store(pkg, activeSpec, testPayload{N: 1})
	require.NoError(t, err)
	oldActiveHash := currentPackageHash(t, db, pkg, activeSpec)
	oldActivePath := storedRecordPath(t, db, activeSpec.Namespace(), oldActiveHash)
	oldRecord := readStoredRecord(t, oldActivePath)
	oldRecord.AdditionalInfo.UnixTimestamp = int(time.Now().Add(-40 * 24 * time.Hour).Unix())
	writeStoredRecord(t, oldActivePath, oldRecord)

	err = os.WriteFile(filepath.Join(baseDir, "foo", "foo.go"), []byte("package foo\n\nfunc A() {}\nfunc B() {}\n"), 0o644)
	require.NoError(t, err)
	err = db.Store(pkg, activeSpec, testPayload{N: 2})
	require.NoError(t, err)
	currentHash := currentPackageHash(t, db, pkg, activeSpec)
	currentPath := storedRecordPath(t, db, activeSpec.Namespace(), currentHash)

	priorNamespace := Namespace("gocas-test-1")
	writeCASRecordAtHash(t, db, priorNamespace, "aa11", cas.AdditionalInfo{})
	writeCASRecordAtHash(t, db, priorNamespace, "bb22", cas.AdditionalInfo{})
	priorPath := storedRecordPath(t, db, priorNamespace, "aa11")

	result, err := db.Prune([]NamespaceSpec{activeSpec}, []*gocode.Package{pkg}, PruneOptions{SupersededAgeDays: 30})
	require.NoError(t, err)
	require.Equal(t, 2, result.DeletedPriorVersionRecords)
	require.Equal(t, 1, result.DeletedSupersededRecords)

	_, err = os.Stat(priorPath)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(oldActivePath)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(currentPath)
	require.NoError(t, err)
}

func TestPrune_PriorNamespaceVersionSkipsCorruptOrUnrecognizedRecords(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)
	db := newTestDB(baseDir, casRoot)
	activeSpec := NamespaceSpec{
		Name:     "gocas-test",
		Version:  2,
		HashMode: HashModePackage,
	}
	priorNamespace := Namespace("gocas-test-1")

	writeCASRecordAtHash(t, db, priorNamespace, "aa11", cas.AdditionalInfo{})
	validPath := storedRecordPath(t, db, priorNamespace, "aa11")

	corruptPath := storedRecordPath(t, db, priorNamespace, "bb22")
	err := os.MkdirAll(filepath.Dir(corruptPath), 0o755)
	require.NoError(t, err)
	err = os.WriteFile(corruptPath, []byte("{"), 0o644)
	require.NoError(t, err)

	unrecognizedPath := storedRecordPath(t, db, priorNamespace, "cc33")
	err = os.MkdirAll(filepath.Dir(unrecognizedPath), 0o755)
	require.NoError(t, err)
	err = os.WriteFile(unrecognizedPath, []byte(`{"kind":"unknown","metadata":{"ok":true},"additional_info":{}}`), 0o644)
	require.NoError(t, err)

	missingMetadataPath := storedRecordPath(t, db, priorNamespace, "dd44")
	err = os.MkdirAll(filepath.Dir(missingMetadataPath), 0o755)
	require.NoError(t, err)
	err = os.WriteFile(missingMetadataPath, []byte(`{"kind":"cas-record-v1","additional_info":{}}`), 0o644)
	require.NoError(t, err)

	result, err := db.Prune([]NamespaceSpec{activeSpec}, []*gocode.Package{pkg}, PruneOptions{})
	require.NoError(t, err)
	require.Equal(t, 1, result.DeletedPriorVersionRecords)
	require.Equal(t, 0, result.DeletedSupersededRecords)

	_, err = os.Stat(validPath)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(corruptPath)
	require.NoError(t, err)
	_, err = os.Stat(unrecognizedPath)
	require.NoError(t, err)
	_, err = os.Stat(missingMetadataPath)
	require.NoError(t, err)
}

func TestPrune_PreservesCurrentRecertifiedSourceAndSkipsCorruptActiveRecords(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)
	db := newTestDB(baseDir, casRoot)

	err := db.Store(pkg, testPackageNamespace, testPayload{N: 7})
	require.NoError(t, err)
	sourceHash := currentPackageHash(t, db, pkg, testPackageNamespace)
	sourcePath := storedRecordPath(t, db, testPackageNamespace.Namespace(), sourceHash)
	sourceRecord := readStoredRecord(t, sourcePath)
	sourceRecord.AdditionalInfo.UnixTimestamp = int(time.Now().Add(-40 * 24 * time.Hour).Unix())
	writeStoredRecord(t, sourcePath, sourceRecord)

	err = os.WriteFile(filepath.Join(baseDir, "foo", "foo.go"), []byte("package foo\n\nfunc A() {}\nfunc B() {}\n"), 0o644)
	require.NoError(t, err)
	recertification, err := db.RecertifyPackage(pkg, testPackageNamespace)
	require.NoError(t, err)
	require.Equal(t, PackageRecertificationStatusRecertified, recertification.Status)
	currentPath := storedRecordPath(t, db, testPackageNamespace.Namespace(), recertification.CurrentHash)

	outdatedHash := "dd33"
	writeCASRecordAtHash(t, db, testPackageNamespace.Namespace(), outdatedHash, cas.AdditionalInfo{
		UnixTimestamp: int(time.Now().Add(-50 * 24 * time.Hour).Unix()),
		Paths:         []string{"foo/foo.go"},
	})
	outdatedPath := storedRecordPath(t, db, testPackageNamespace.Namespace(), outdatedHash)

	corruptPath := storedRecordPath(t, db, testPackageNamespace.Namespace(), "ee44")
	err = os.MkdirAll(filepath.Dir(corruptPath), 0o755)
	require.NoError(t, err)
	err = os.WriteFile(corruptPath, []byte("{"), 0o644)
	require.NoError(t, err)

	result, err := db.Prune([]NamespaceSpec{testPackageNamespace}, []*gocode.Package{pkg}, PruneOptions{SupersededAgeDays: 30})
	require.NoError(t, err)
	require.Equal(t, 0, result.DeletedPriorVersionRecords)
	require.Equal(t, 1, result.DeletedSupersededRecords)

	_, err = os.Stat(sourcePath)
	require.NoError(t, err)
	_, err = os.Stat(currentPath)
	require.NoError(t, err)
	_, err = os.Stat(corruptPath)
	require.NoError(t, err)
	_, err = os.Stat(outdatedPath)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestPrune_PreservesOldRecordWithoutNewerRecord(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)
	db := newTestDB(baseDir, casRoot)

	oldHash := "aa11"
	writeCASRecordAtHash(t, db, testPackageNamespace.Namespace(), oldHash, cas.AdditionalInfo{
		UnixTimestamp: int(time.Now().Add(-40 * 24 * time.Hour).Unix()),
		Paths:         []string{"foo/foo.go"},
	})
	oldPath := storedRecordPath(t, db, testPackageNamespace.Namespace(), oldHash)

	result, err := db.Prune([]NamespaceSpec{testPackageNamespace}, []*gocode.Package{pkg}, PruneOptions{SupersededAgeDays: 30})
	require.NoError(t, err)
	require.Equal(t, PruneResult{}, result)

	_, err = os.Stat(oldPath)
	require.NoError(t, err)
}

func TestPrune_RejectsOverflowingSupersededAgeWithoutDeletingRecords(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)
	db := newTestDB(baseDir, casRoot)
	activeSpec := NamespaceSpec{
		Name:     "gocas-test",
		Version:  2,
		HashMode: HashModePackage,
	}

	err := db.Store(pkg, activeSpec, testPayload{N: 1})
	require.NoError(t, err)
	oldActiveHash := currentPackageHash(t, db, pkg, activeSpec)
	oldActivePath := storedRecordPath(t, db, activeSpec.Namespace(), oldActiveHash)
	oldRecord := readStoredRecord(t, oldActivePath)
	oldRecord.AdditionalInfo.UnixTimestamp = int(time.Now().Add(-40 * 24 * time.Hour).Unix())
	writeStoredRecord(t, oldActivePath, oldRecord)

	err = os.WriteFile(filepath.Join(baseDir, "foo", "foo.go"), []byte("package foo\n\nfunc A() {}\nfunc B() {}\n"), 0o644)
	require.NoError(t, err)
	err = db.Store(pkg, activeSpec, testPayload{N: 2})
	require.NoError(t, err)
	currentHash := currentPackageHash(t, db, pkg, activeSpec)
	currentPath := storedRecordPath(t, db, activeSpec.Namespace(), currentHash)

	priorNamespace := Namespace("gocas-test-1")
	writeCASRecordAtHash(t, db, priorNamespace, "aa11", cas.AdditionalInfo{})
	priorPath := storedRecordPath(t, db, priorNamespace, "aa11")

	result, err := db.Prune([]NamespaceSpec{activeSpec}, []*gocode.Package{pkg}, PruneOptions{SupersededAgeDays: 200000})
	require.Error(t, err)
	require.Contains(t, err.Error(), "superseded age days is too large")
	require.Equal(t, PruneResult{}, result)

	_, err = os.Stat(priorPath)
	require.NoError(t, err)
	_, err = os.Stat(oldActivePath)
	require.NoError(t, err)
	_, err = os.Stat(currentPath)
	require.NoError(t, err)
}

func TestPackageHasherStableAcrossDifferentAbsoluteBaseDirs(t *testing.T) {
	baseDir1 := t.TempDir()
	baseDir2 := t.TempDir()
	casRoot := t.TempDir()

	pkg1 := writeTestModuleWithPackage(t, baseDir1)
	pkg2 := writeTestModuleWithPackage(t, baseDir2)

	db1 := newTestDB(baseDir1, casRoot)
	db2 := newTestDB(baseDir2, casRoot)

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

	db := newTestDB(baseDir, casRoot)

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

	db := newTestDB(baseDir, casRoot)

	err = db.Store(pkg, testPackageNamespace, testPayload{N: 1})
	require.Error(t, err)
}

func TestStoreAndRetrieve_PackageHashSpecMdAffectsKey(t *testing.T) {
	baseDir := t.TempDir()
	casRoot := t.TempDir()

	pkg := writeTestModuleWithPackage(t, baseDir)

	db := newTestDB(baseDir, casRoot)

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
