package applypatch

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestIsInvalidPatch(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		require.False(t, IsInvalidPatch(nil))
	})
	t.Run("parse error", func(t *testing.T) {
		td := t.TempDir()
		_, err := ApplyPatch(td, "*** Update File: file.txt\n")
		require.Error(t, err)
		require.True(t, IsInvalidPatch(err))
	})
	t.Run("path escapes root", func(t *testing.T) {
		td := t.TempDir()
		_, err := ApplyPatch(td, fmt.Sprintf(`*** Begin Patch
*** Add File: %s
+nope
*** End Patch
`, filepath.ToSlash(filepath.Join(td, "..", "escape.txt"))))
		require.Error(t, err)
		require.True(t, IsInvalidPatch(err))
	})
	t.Run("hunk does not match", func(t *testing.T) {
		td := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(td, "file.txt"), []byte("alpha\nbeta\n"), 0o644))
		_, err := ApplyPatch(td, trimLeadingNewline(`
*** Begin Patch
*** Update File: file.txt
@@
-gamma
+delta
*** End Patch
`))
		require.Error(t, err)
		require.True(t, IsInvalidPatch(err))
	})
	t.Run("update file does not exist", func(t *testing.T) {
		td := t.TempDir()
		_, err := ApplyPatch(td, trimLeadingNewline(`
*** Begin Patch
*** Update File: missing.txt
@@
+hi
*** End Patch
`))
		require.Error(t, err)
		require.True(t, IsInvalidPatch(err))
	})
	t.Run("filesystem write failure is not an invalid patch", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("chmod-based permission tests are unreliable on windows")
		}
		td := t.TempDir()
		ro := filepath.Join(td, "ro")
		require.NoError(t, os.MkdirAll(ro, 0o777))
		require.NoError(t, os.Chmod(ro, 0o555))
		_, err := ApplyPatch(td, trimLeadingNewline(`
*** Begin Patch
*** Add File: ro/file.txt
+hi
*** End Patch
`))
		require.Error(t, err)
		require.False(t, IsInvalidPatch(err))
	})
	t.Run("invalid root arg is not an invalid patch", func(t *testing.T) {
		_, err := ApplyPatch("relative/root", trimLeadingNewline(`
*** Begin Patch
*** Add File: file.txt
+hi
*** End Patch
`))
		require.Error(t, err)
		require.False(t, IsInvalidPatch(err))
	})
}
