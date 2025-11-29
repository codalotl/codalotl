package gocode

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, dir, name, contents string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(contents), 0o644))
	return p
}

func TestGoFilesInDirForConfig_IncludesTestFiles(t *testing.T) {
	tdir := t.TempDir()
	writeFile(t, tdir, "a.go", "package p\n")
	writeFile(t, tdir, "a_test.go", "package p\n")

	files, err := goFilesInDirForConfig(tdir, "", "", nil)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"a.go", "a_test.go"}, files)
}

func TestGoFilesInDirForConfig_GOOSFiltering(t *testing.T) {
	tdir := t.TempDir()
	writeFile(t, tdir, "b_linux.go", "package p\n")
	writeFile(t, tdir, "b_darwin.go", "package p\n")

	filesLinux, err := goFilesInDirForConfig(tdir, "amd64", "linux", nil)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"b_linux.go"}, filesLinux)

	filesDarwin, err := goFilesInDirForConfig(tdir, "arm64", "darwin", nil)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"b_darwin.go"}, filesDarwin)
}

func TestGoFilesInDirForConfig_BuildTagsParam(t *testing.T) {
	tdir := t.TempDir()
	writeFile(t, tdir, "c.go", "//go:build mytag\n\npackage p\n")

	// Without tag, excluded
	filesNoTag, err := goFilesInDirForConfig(tdir, runtime.GOARCH, runtime.GOOS, nil)
	require.NoError(t, err)
	require.Empty(t, filesNoTag)

	// With tag, included
	filesWithTag, err := goFilesInDirForConfig(tdir, runtime.GOARCH, runtime.GOOS, []string{"mytag"})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"c.go"}, filesWithTag)
}

func TestGoFilesInDirForConfig_GOFLAGS_MergesTags(t *testing.T) {
	tdir := t.TempDir()
	writeFile(t, tdir, "d.go", "//go:build tag2\n\npackage p\n")

	old := os.Getenv("GOFLAGS")
	defer os.Setenv("GOFLAGS", old)

	// Provide tag2 via GOFLAGS, and another tag via buildTags to ensure merge works.
	require.NoError(t, os.Setenv("GOFLAGS", "-tags=tag1,tag2"))
	files, err := goFilesInDirForConfig(tdir, runtime.GOARCH, runtime.GOOS, []string{"othertag"})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"d.go"}, files)

	// Also support space-separated form
	require.NoError(t, os.Setenv("GOFLAGS", "-tags tag2"))
	files2, err := goFilesInDirForConfig(tdir, runtime.GOARCH, runtime.GOOS, nil)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"d.go"}, files2)
}
