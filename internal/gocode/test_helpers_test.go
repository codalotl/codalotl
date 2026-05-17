package gocode

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

func newTestPackageFromFiles(t *testing.T, files map[string]string) (*Package, string) {
	t.Helper()

	tempDir := t.TempDir()
	fileNames := writeTestPackageFiles(t, tempDir, files)

	module := &Module{
		Name:         "testmodule",
		AbsolutePath: tempDir,
		Packages:     make(map[string]*Package),
	}
	pkg, err := NewPackage("", tempDir, fileNames, module)
	require.NoError(t, err)
	require.NotNil(t, pkg)

	return pkg, tempDir
}

func writeTestPackageFiles(t *testing.T, dir string, files map[string]string) []string {
	t.Helper()

	fileNames := make([]string, 0, len(files))
	for fileName, content := range files {
		filePath := filepath.Join(dir, fileName)
		require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0o755))
		require.NoError(t, os.WriteFile(filePath, []byte(content), 0o644))
		fileNames = append(fileNames, fileName)
	}
	sort.Strings(fileNames)
	return fileNames
}
