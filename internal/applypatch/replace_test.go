package applypatch

import (
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestReplace_BasicLiteral(t *testing.T) {
	td := t.TempDir()
	path := filepath.Join(td, "file.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello old world\n"), 0o644))
	got, err := Replace(path, "old", "new", false)
	require.NoError(t, err)
	require.Equal(t, "hello new world\n", got)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "hello new world\n", string(data))
}
func TestReplace_AmbiguousSingleMatchReturnsInvalidPatch(t *testing.T) {
	td := t.TempDir()
	path := filepath.Join(td, "file.txt")
	require.NoError(t, os.WriteFile(path, []byte("x\nx\n"), 0o644))
	_, err := Replace(path, "x", "y", false)
	require.Error(t, err)
	require.True(t, IsInvalidPatch(err))
	require.Contains(t, err.Error(), "ambiguous")
}
func TestReplace_ReplaceAll(t *testing.T) {
	td := t.TempDir()
	path := filepath.Join(td, "file.txt")
	require.NoError(t, os.WriteFile(path, []byte("red blue red\n"), 0o644))
	got, err := Replace(path, "red", "green", true)
	require.NoError(t, err)
	require.Equal(t, "green blue green\n", got)
}
func TestReplace_NewlineNormalizedKeepsCRLFStyle(t *testing.T) {
	td := t.TempDir()
	path := filepath.Join(td, "file.txt")
	require.NoError(t, os.WriteFile(path, []byte("alpha\r\nbeta\r\n"), 0o644))
	got, err := Replace(path, "alpha\nbeta\n", "gamma\ndelta\n", false)
	require.NoError(t, err)
	require.Equal(t, "gamma\r\ndelta\r\n", got)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "gamma\r\ndelta\r\n", string(data))
}
func TestReplace_NotFoundReturnsInvalidPatch(t *testing.T) {
	td := t.TempDir()
	path := filepath.Join(td, "file.txt")
	require.NoError(t, os.WriteFile(path, []byte("abc\n"), 0o644))
	_, err := Replace(path, "missing", "x", false)
	require.Error(t, err)
	require.True(t, IsInvalidPatch(err))
	require.Contains(t, err.Error(), "not found")
}
func TestReplace_UnicodeNormalization(t *testing.T) {
	td := t.TempDir()
	path := filepath.Join(td, "file.txt")
	require.NoError(t, os.WriteFile(path, []byte("x — y\n"), 0o644))
	got, err := Replace(path, "x - y", "x - z", false)
	require.NoError(t, err)
	require.Equal(t, "x - z\n", got)
}
