package cascade

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// withHome temporarily points the process's home directory to a new temporary directory and calls callback with that path. It ensures the path has no trailing separator.
// On non-Windows systems, the original HOME is restored after the callback returns. On Windows, USERPROFILE is set during the callback and restored after it returns,
// while HOME is unchanged. The test fails if the environment cannot be set.
func withHome(t *testing.T, callback func(home string)) {
	osHomeEnv := "HOME"
	if runtime.GOOS == "windows" {
		osHomeEnv = "USERPROFILE"
	}

	origHomeEnv := os.Getenv(osHomeEnv)
	defer func() {
		if origHomeEnv == "" {
			_ = os.Unsetenv(osHomeEnv)
		} else {
			_ = os.Setenv(osHomeEnv, origHomeEnv)
		}
	}()

	// Point HOME to a temp dir to make behavior deterministic:
	tempHome := t.TempDir()

	// Ensure path has no trailing separator for predictable joins
	if strings.HasSuffix(tempHome, string(filepath.Separator)) {
		tempHome = strings.TrimRight(tempHome, string(filepath.Separator))
	}
	require.NoError(t, os.Setenv(osHomeEnv, tempHome))

	callback(tempHome)
}

// withEnv sets the provided environment variables for the duration of callback and then restores their prior values (unsetting those that were previously unset).
// The test fails if any variable cannot be set.
func withEnv(t *testing.T, vars map[string]string, callback func()) {
	type original struct {
		value  string
		exists bool
	}
	originals := map[string]original{}

	// Capture originals for all keys we are about to set
	for k := range vars {
		v, ok := os.LookupEnv(k)
		originals[k] = original{value: v, exists: ok}
	}

	// Ensure we restore the original environment after the callback
	defer func() {
		for k, o := range originals {
			if o.exists {
				_ = os.Setenv(k, o.value)
			} else {
				_ = os.Unsetenv(k)
			}
		}
	}()

	// Apply requested environment variables
	for k, v := range vars {
		require.NoError(t, os.Setenv(k, v))
	}

	callback()
}

func TestExpandPath(t *testing.T) {

	withHome(t, func(tempHome string) {
		// Empty string stays empty
		require.Equal(t, "", ExpandPath(""))

		// Plain relative becomes absolute
		absRel := ExpandPath("foo/bar")
		require.True(t, filepath.IsAbs(absRel))

		// Leading ~ expands to HOME exactly
		require.Equal(t, tempHome, ExpandPath("~"))
		require.Equal(t, tempHome, ExpandPath("~/"))

		// ~/subdir expands under HOME with correct separator handling
		expanded := ExpandPath("~/sub/dir")
		require.True(t, strings.HasPrefix(expanded, tempHome))
		require.Equal(t, filepath.Join(tempHome, "sub", "dir"), expanded)

		// Mixed backslash after ~ should also work: the remainder is preserved verbatim.
		expandedBS := ExpandPath(`~\sub\\dir`)
		require.Equal(t, filepath.Join(tempHome, `sub\\dir`), expandedBS)

		// Absolute path input remains absolute (unchanged semantically)
		absInput := filepath.Join(tempHome, "already", "abs")
		require.Equal(t, absInput, ExpandPath(absInput))

		// Ensure not accidentally keeping literal tilde when HOME set
		require.NotContains(t, ExpandPath("~/x"), "~")
	})
}

func TestInUserConfigDirectory(t *testing.T) {
	withHome(t, func(tempHome string) {
		if runtime.GOOS == "windows" {
			expected := filepath.Join(ExpandPath("~/AppData/Local"), "my", "app.json")
			require.Equal(t, expected, InUserConfigDirectory("my/app.json"))
		} else {
			expected := filepath.Join(ExpandPath("~"), "my", "app.json")
			require.Equal(t, expected, InUserConfigDirectory("my/app.json"))
		}
	})
}
