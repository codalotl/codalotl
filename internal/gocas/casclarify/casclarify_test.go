package casclarify

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/codalotl/codalotl/internal/gocas"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	qcas "github.com/codalotl/codalotl/internal/q/cas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppendAndRetrieve(t *testing.T) {
	gocodetesting.WithCode(t, "func Exported() {}", func(pkg *gocode.Package) {
		db := testDB(t, pkg)
		first := Entry{
			OriginPackage: "mymodule/caller",
			TargetPackage: "mymodule/mypkg",
			Identifier:    "Exported",
			Question:      "What does it do?",
			Answer:        "It is exported.",
		}
		second := Entry{
			OriginPackage: "mymodule/other",
			TargetPackage: "mymodule/mypkg",
			Identifier:    "package",
			Question:      "What is the package for?",
			Answer:        "It is a test fixture.",
		}

		require.NoError(t, Append(db, pkg, first))
		require.NoError(t, Append(db, pkg, second))

		found, metadata, err := Retrieve(db, pkg)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, Metadata{Entries: []Entry{first, second}}, metadata)
	})
}

func TestRetrieveMissing(t *testing.T) {
	gocodetesting.WithCode(t, "func Exported() {}", func(pkg *gocode.Package) {
		found, metadata, err := Retrieve(testDB(t, pkg), pkg)

		require.NoError(t, err)
		assert.False(t, found)
		assert.Empty(t, metadata.Entries)
	})
}

func TestFindInPlayFindsUncommittedCurrentHashRecord(t *testing.T) {
	gocodetesting.WithCode(t, "func Exported() {}", func(pkg *gocode.Package) {
		initGitRepo(t, pkg.Module.AbsolutePath)
		db := repoDB(pkg)
		entry := testEntry(pkg)

		require.NoError(t, Append(db, pkg, entry))

		records, err := FindInPlay(db, pkg.Module)
		require.NoError(t, err)
		require.Len(t, records, 1)
		assert.True(t, filepath.IsAbs(records[0].Path))
		assert.Equal(t, pkg.ImportPath, records[0].TargetPackage)
		assert.Equal(t, Metadata{Entries: []Entry{entry}}, records[0].Metadata)

		require.NoError(t, records[0].Delete())
		_, err = os.Stat(records[0].Path)
		assert.ErrorIs(t, err, os.ErrNotExist)
	})
}

func TestFindInPlaySkipsUncommittedDriftedHashRecord(t *testing.T) {
	gocodetesting.WithCode(t, "func Exported() {}", func(pkg *gocode.Package) {
		initGitRepo(t, pkg.Module.AbsolutePath)
		db := repoDB(pkg)

		require.NoError(t, Append(db, pkg, testEntry(pkg)))
		require.NoError(t, os.WriteFile(filepath.Join(pkg.AbsolutePath(), "code.go"), []byte("package mypkg\n\nfunc Exported() int { return 1 }\n"), 0o644))

		records, err := FindInPlay(db, pkg.Module)
		require.NoError(t, err)
		assert.Empty(t, records)
	})
}

func TestFindInPlayFindsBranchAddedDriftedHashRecord(t *testing.T) {
	gocodetesting.WithCode(t, "func Exported() {}", func(pkg *gocode.Package) {
		initGitRepo(t, pkg.Module.AbsolutePath)
		git(t, pkg.Module.AbsolutePath, "checkout", "-b", "feature")
		db := repoDB(pkg)
		entry := testEntry(pkg)

		require.NoError(t, Append(db, pkg, entry))
		git(t, pkg.Module.AbsolutePath, "add", ".codalotl")
		git(t, pkg.Module.AbsolutePath, "commit", "-m", "add clarify record")
		require.NoError(t, os.WriteFile(filepath.Join(pkg.AbsolutePath(), "code.go"), []byte("package mypkg\n\nfunc Exported() int { return 1 }\n"), 0o644))

		records, err := FindInPlay(db, pkg.Module)
		require.NoError(t, err)
		require.Len(t, records, 1)
		assert.Equal(t, pkg.ImportPath, records[0].TargetPackage)
		assert.Equal(t, Metadata{Entries: []Entry{entry}}, records[0].Metadata)
	})
}

func TestFindInPlayReturnsNoRecordsWhenGitStateCannotBeRead(t *testing.T) {
	gocodetesting.WithCode(t, "func Exported() {}", func(pkg *gocode.Package) {
		db := repoDB(pkg)

		require.NoError(t, Append(db, pkg, testEntry(pkg)))

		records, err := FindInPlay(db, pkg.Module)
		require.NoError(t, err)
		assert.Empty(t, records)
	})
}

func testDB(t *testing.T, pkg *gocode.Package) *gocas.DB {
	t.Helper()

	return &gocas.DB{
		BaseDir: pkg.Module.AbsolutePath,
		DB: qcas.DB{
			AbsRoot: t.TempDir(),
		},
	}
}

func repoDB(pkg *gocode.Package) *gocas.DB {
	return &gocas.DB{
		BaseDir: pkg.Module.AbsolutePath,
		DB: qcas.DB{
			AbsRoot: filepath.Join(pkg.Module.AbsolutePath, ".codalotl", "cas"),
		},
	}
}

func testEntry(pkg *gocode.Package) Entry {
	return Entry{
		OriginPackage: "mymodule/caller",
		TargetPackage: pkg.ImportPath,
		Identifier:    "Exported",
		Question:      "What does it do?",
		Answer:        "It is exported.",
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}

	git(t, dir, "init")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "user.name", "Test User")
	git(t, dir, "checkout", "-B", "main")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "initial")
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}
