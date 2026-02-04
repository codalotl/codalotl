package simplelogger

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLog_WritesAndAppends(t *testing.T) {
	t.Setenv("CODALOTL_LOG_FILE", filepath.Join(t.TempDir(), "codalotl.log"))

	Log("hello %s", "world")
	Log(" %d", 123)

	b, err := os.ReadFile(os.Getenv("CODALOTL_LOG_FILE"))
	require.NoError(t, err)
	require.Equal(t, "hello world\n 123\n", string(b))
}

func TestLog_NoOpWhenUnset(t *testing.T) {
	t.Setenv("CODALOTL_LOG_FILE", "")
	Log("should not %s", "panic")
}

func TestLog_NoOpWhenPathIsDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODALOTL_LOG_FILE", dir)

	Log("ignored %d", 1)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries)
}
