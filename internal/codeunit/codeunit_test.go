package codeunit_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codalotl/codalotl/internal/codeunit"
)

func TestHappyPathExample(t *testing.T) {
	base := t.TempDir()

	writeFile := func(path string) {
		t.Helper()
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte("payload"), 0o644))
	}

	writeFile(filepath.Join(base, "main.go"))
	writeFile(filepath.Join(base, "README.md"))

	writeFile(filepath.Join(base, "providers", "openai", "provider.go"))
	writeFile(filepath.Join(base, "providers", "anthropic", "provider.go"))

	writeFile(filepath.Join(base, "docs", "overview.txt"))
	writeFile(filepath.Join(base, "docs", "extra", "notes.txt"))

	writeFile(filepath.Join(base, "testdata", "sample.go"))
	writeFile(filepath.Join(base, "testdata", "fixtures", "data.json"))

	writeFile(filepath.Join(base, "server", "server.go"))
	writeFile(filepath.Join(base, "client", "go.mod"))
	writeFile(filepath.Join(base, "client", "internal", "helper.go"))

	unit, err := codeunit.NewCodeUnit("package example", base)
	require.NoError(t, err)

	require.NoError(t, unit.IncludeSubtreeUnlessContains("*.go", "go.mod"))
	require.NoError(t, unit.IncludeDir("testdata", true))
	unit.PruneEmptyDirs()

	assert.True(t, unit.Includes("docs"))
	assert.True(t, unit.Includes(filepath.Join(base, "docs"))) // absolute paths work
	assert.True(t, unit.Includes("docs/overview.txt"))
	assert.True(t, unit.Includes("docs/extra"))
	assert.True(t, unit.Includes("testdata"))
	assert.True(t, unit.Includes("testdata/fixtures"))
	assert.True(t, unit.Includes("testdata/fixtures/data.json"))
	assert.True(t, unit.Includes("testdata/sample.go"))
	assert.True(t, unit.Includes("README.md"))

	assert.False(t, unit.Includes("providers"))
	assert.False(t, unit.Includes("providers/openai"))
	assert.False(t, unit.Includes("providers/openai/provider.go"))
	assert.False(t, unit.Includes("server"))
	assert.False(t, unit.Includes("client"))
	assert.False(t, unit.Includes("client/go.mod"))
	assert.False(t, unit.Includes("client/internal"))
	assert.False(t, unit.Includes("client/internal/helper.go"))

	expected := []string{
		base,
		filepath.Join(base, "README.md"),
		filepath.Join(base, "docs"),
		filepath.Join(base, "docs", "extra"),
		filepath.Join(base, "docs", "extra", "notes.txt"),
		filepath.Join(base, "docs", "overview.txt"),
		filepath.Join(base, "main.go"),
		filepath.Join(base, "testdata"),
		filepath.Join(base, "testdata", "fixtures"),
		filepath.Join(base, "testdata", "fixtures", "data.json"),
		filepath.Join(base, "testdata", "sample.go"),
	}
	slices.Sort(expected)

	assert.Equal(t, expected, unit.IncludedFiles())
}

func TestNewCodeUnitValidation(t *testing.T) {
	t.Parallel()

	temp := t.TempDir()
	filePath := filepath.Join(temp, "file.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("x"), 0o644))

	_, err := codeunit.NewCodeUnit("name", "relative/path")
	assert.ErrorContains(t, err, "base directory must be absolute")

	_, err = codeunit.NewCodeUnit("name", filePath)
	assert.ErrorContains(t, err, "base path is not a directory")

	missingDir := filepath.Join(temp, "missing")
	_, err = codeunit.NewCodeUnit("name", missingDir)
	assert.ErrorContains(t, err, "stat base directory")
}

func TestIncludeDirRequiresParent(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(base, "child", "grandchild"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(base, "child", "grandchild", "data.txt"), []byte("payload"), 0o644))

	unit, err := codeunit.NewCodeUnit("package include", base)
	require.NoError(t, err)

	err = unit.IncludeDir(filepath.Join("child", "grandchild"), false)
	assert.ErrorContains(t, err, "parent directory")

	require.NoError(t, unit.IncludeDir("child", false))
	require.NoError(t, unit.IncludeDir(filepath.Join("child", "grandchild"), false))

	assert.True(t, unit.Includes(filepath.Join("child", "grandchild")))
	assert.True(t, unit.Includes(filepath.Join("child", "grandchild", "data.txt")))
}

func TestIncludesNonExistentPath(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	unit, err := codeunit.NewCodeUnit("package missing paths", base)
	require.NoError(t, err)

	assert.True(t, unit.Includes("future.txt"))

	childAbs := filepath.Join(base, "child")
	require.NoError(t, os.MkdirAll(childAbs, 0o755))
	require.NoError(t, unit.IncludeDir("child", false))

	assert.True(t, unit.Includes(filepath.Join("child", "newfile.txt")))
	assert.True(t, unit.Includes(filepath.Join("child", "newdir")))

	otherAbs := filepath.Join(base, "other")
	require.NoError(t, os.MkdirAll(otherAbs, 0o755))

	assert.False(t, unit.Includes(filepath.Join("other", "planned.txt")))
}

func TestIncludeSubtreeUnlessContainsAndPrune(t *testing.T) {
	t.Parallel()

	base := t.TempDir()

	writeFile := func(rel string) {
		t.Helper()
		path := filepath.Join(base, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte("payload"), 0o644))
	}

	require.NoError(t, os.MkdirAll(filepath.Join(base, "empty", "nested"), 0o755))

	writeFile(filepath.Join("pkg1", "data.txt"))
	writeFile(filepath.Join("pkg1", "nested", "keep.txt"))
	writeFile(filepath.Join("pkg2", "main.go"))
	writeFile(filepath.Join("pkg2", "other.txt"))

	unit, err := codeunit.NewCodeUnit("package subtree", base)
	require.NoError(t, err)

	require.NoError(t, unit.IncludeSubtreeUnlessContains("*.go"))

	assert.True(t, unit.Includes("pkg1"))
	assert.True(t, unit.Includes(filepath.Join("pkg1", "nested")))
	assert.True(t, unit.Includes(filepath.Join("pkg1", "nested", "keep.txt")))

	assert.False(t, unit.Includes("pkg2"))
	assert.False(t, unit.Includes(filepath.Join("pkg2", "main.go")))
	assert.False(t, unit.Includes(filepath.Join("pkg2", "other.txt")))

	assert.True(t, unit.Includes("empty"))
	assert.True(t, unit.Includes(filepath.Join("empty", "nested")))

	unit.PruneEmptyDirs()

	assert.False(t, unit.Includes("empty"))
	assert.False(t, unit.Includes(filepath.Join("empty", "nested")))
	assert.True(t, unit.Includes("pkg1"))
}

func TestCodeUnitName(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	unit, err := codeunit.NewCodeUnit("package foo", base)
	require.NoError(t, err)

	assert.Equal(t, "package foo", unit.Name())
}

func TestCodeUnitNameDefault(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	unit, err := codeunit.NewCodeUnit("", base)
	require.NoError(t, err)

	assert.Equal(t, "code unit", unit.Name())
}
