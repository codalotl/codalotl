package applypatch

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyPatch_CodexCompatibility_LeadingWhitespaceAroundPatchIsAccepted(t *testing.T) {
	td := t.TempDir()

	changes, err := ApplyPatch(td, "\n\n*** Begin Patch\n*** Add File: hello.txt\n+hi\n*** End Patch\n\n")
	require.NoError(t, err)
	require.Equal(t, []FileChange{
		{Path: "hello.txt", Kind: FileChangeAdded},
	}, changes)

	got, readErr := snapshotDir(td)
	require.NoError(t, readErr)
	require.Equal(t, map[string]string{"hello.txt": "hi\n"}, got)
}

func TestApplyPatch_CodexCompatibility_HeredocWrappedPatchIsAccepted(t *testing.T) {
	td := t.TempDir()

	changes, err := ApplyPatch(td, "<<EOF\n*** Begin Patch\n*** Add File: hello.txt\n+hi\n*** End Patch\nEOF\n")
	require.NoError(t, err)
	require.Equal(t, []FileChange{
		{Path: "hello.txt", Kind: FileChangeAdded},
	}, changes)

	got, readErr := snapshotDir(td)
	require.NoError(t, readErr)
	require.Equal(t, map[string]string{"hello.txt": "hi\n"}, got)
}

func TestApplyPatch_CodexCompatibility_BlankLineBetweenChunksIsAccepted(t *testing.T) {
	td := t.TempDir()
	require.NoError(t, writeFiles(td, map[string]string{
		"multi.txt": "line1\nline2\nline3\nline4\n",
	}))

	_, err := ApplyPatch(td, `*** Begin Patch
*** Update File: multi.txt
@@
-line2
+changed2

@@
-line4
+changed4
*** End Patch
`)
	require.NoError(t, err)

	got, readErr := snapshotDir(td)
	require.NoError(t, readErr)
	require.Equal(t, map[string]string{
		"multi.txt": "line1\nchanged2\nline3\nchanged4\n",
	}, got)
}

func TestApplyPatch_CodexCompatibility_BlankContextLineIsAccepted(t *testing.T) {
	td := t.TempDir()
	require.NoError(t, writeFiles(td, map[string]string{
		"file.txt": "alpha\n\nbeta\n",
	}))

	_, err := ApplyPatch(td, `*** Begin Patch
*** Update File: file.txt
@@
 alpha

-beta
+gamma
*** End Patch
`)
	require.NoError(t, err)

	got, readErr := snapshotDir(td)
	require.NoError(t, readErr)
	require.Equal(t, map[string]string{
		"file.txt": "alpha\n\ngamma\n",
	}, got)
}

func TestApplyPatch_CodexCompatibility_EmptyPatchReturnsError(t *testing.T) {
	td := t.TempDir()

	changes, err := ApplyPatch(td, `*** Begin Patch
*** End Patch
`)
	require.Error(t, err)
	require.Nil(t, changes)

	got, readErr := snapshotDir(td)
	require.NoError(t, readErr)
	require.Empty(t, got)
}

func TestApplyPatch_CodexCompatibility_MissingDeleteReturnsError(t *testing.T) {
	td := t.TempDir()

	changes, err := ApplyPatch(td, `*** Begin Patch
*** Delete File: missing.txt
*** End Patch
`)
	require.Error(t, err)
	require.Nil(t, changes)

	got, readErr := snapshotDir(td)
	require.NoError(t, readErr)
	require.Empty(t, got)
}

func TestApplyPatch_CodexCompatibility_EmptyUpdateHunkReturnsError(t *testing.T) {
	td := t.TempDir()
	require.NoError(t, writeFiles(td, map[string]string{
		"foo.txt": "hello\n",
	}))

	changes, err := ApplyPatch(td, `*** Begin Patch
*** Update File: foo.txt
*** End Patch
`)
	require.Error(t, err)
	require.Nil(t, changes)

	data, readErr := os.ReadFile(filepath.Join(td, "foo.txt"))
	require.NoError(t, readErr)
	require.Equal(t, "hello\n", string(data))
}

func TestApplyPatch_CodexCompatibility_MoveOnlyUpdateReturnsError(t *testing.T) {
	td := t.TempDir()
	require.NoError(t, writeFiles(td, map[string]string{
		"old/name.txt": "hello\n",
	}))

	changes, err := ApplyPatch(td, `*** Begin Patch
*** Update File: old/name.txt
*** Move to: new/name.txt
*** End Patch
`)
	require.Error(t, err)
	require.Nil(t, changes)

	got, readErr := snapshotDir(td)
	require.NoError(t, readErr)
	require.Equal(t, map[string]string{
		"old/name.txt": "hello\n",
	}, got)
}

func TestApplyPatch_CodexCompatibility_DeleteDirectoryReturnsError(t *testing.T) {
	td := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(td, "dir"), 0o755))

	changes, err := ApplyPatch(td, `*** Begin Patch
*** Delete File: dir
*** End Patch
`)
	require.Error(t, err)
	require.Nil(t, changes)

	info, statErr := os.Stat(filepath.Join(td, "dir"))
	require.NoError(t, statErr)
	require.True(t, info.IsDir())
}
