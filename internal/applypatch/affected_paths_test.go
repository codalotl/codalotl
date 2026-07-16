package applypatch

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAffectedPaths_AllOperationsInFirstSeenOrder(t *testing.T) {
	root := t.TempDir()
	patch := strings.Join([]string{
		"  ",
		"  *** Begin Patch  ",
		"  *** Add File: added.txt  ",
		"+new",
		"  *** Delete File: deleted.txt  ",
		"  *** Update File: updated.txt  ",
		"@@",
		"-old",
		"+new",
		"  *** Update File: moved.txt  ",
		"  *** Move to: destination.txt  ",
		"@@",
		"-before",
		"+after",
		"  *** End Patch  ",
		"",
	}, "\n")

	paths, err := AffectedPaths(root, patch)
	require.NoError(t, err)
	require.Equal(t, []string{
		filepath.Join(root, "added.txt"),
		filepath.Join(root, "deleted.txt"),
		filepath.Join(root, "updated.txt"),
		filepath.Join(root, "moved.txt"),
		filepath.Join(root, "destination.txt"),
	}, paths)
}

func TestAffectedPaths_PreservesLeadingPathWhitespace(t *testing.T) {
	root := t.TempDir()
	patch := `*** Begin Patch
*** Add File:  leading.txt
+new
*** End Patch
`

	paths, err := AffectedPaths(root, patch)
	require.NoError(t, err)
	require.Equal(t, []string{filepath.Join(root, " leading.txt")}, paths)

	changes, err := ApplyPatch(root, patch)
	require.NoError(t, err)
	require.Equal(t, []FileChange{{Path: " leading.txt", Kind: FileChangeAdded}}, changes)
}

func TestAffectedPaths_DeduplicatesResolvedAliases(t *testing.T) {
	root := t.TempDir()
	absolute := filepath.Join(root, "same.txt")
	patch := fmt.Sprintf(`*** Begin Patch
*** Add File: same.txt
+first
*** Add File: ./same.txt
+second
*** Add File: %s
+third
*** End Patch
`, filepath.ToSlash(absolute))

	paths, err := AffectedPaths(root, patch)
	require.NoError(t, err)
	require.Equal(t, []string{absolute}, paths)
}

func TestAffectedPaths_RejectsAnyInvalidTarget(t *testing.T) {
	root := t.TempDir()
	patch := `*** Begin Patch
*** Add File: valid.txt
+new
*** Add File: ../outside.txt
+new
*** End Patch
`

	paths, err := AffectedPaths(root, patch)
	require.Error(t, err)
	require.Nil(t, paths)
	require.True(t, IsInvalidPatch(err))
}
