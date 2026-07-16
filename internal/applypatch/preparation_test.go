package applypatch

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyPatch_ResolvesAllTargetsBeforeMutation(t *testing.T) {
	root := t.TempDir()
	patch := "*** Begin Patch\n" +
		"*** Add File: valid.txt\n" +
		"+new\n" +
		"*** Add File: ../outside.txt\n" +
		"+new\n" +
		"*** End Patch\n"

	changes, err := ApplyPatch(root, patch)
	require.Error(t, err)
	require.Nil(t, changes)

	got, err := snapshotDir(root)
	require.NoError(t, err)
	require.Empty(t, got)
}
