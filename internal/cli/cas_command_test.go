package cli

import (
	"bytes"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestRun_CAS_StoreAndRetrieve_RoundTrip(t *testing.T) {
	isolateUserConfig(t)
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	pkgDir := filepath.Join(tmp, "p")
	require.NoError(t, os.MkdirAll(pkgDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "p.go"), []byte("package p\n\nfunc P() {}\n"), 0644))
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	{
		var out bytes.Buffer
		var errOut bytes.Buffer
		code, err := Run([]string{"codalotl", "cas", "set", "ns-1.0", "./p", `{"result":"ok"}`}, &RunOptions{Out: &out, Err: &errOut})
		require.NoError(t, err)
		require.Equal(t, 0, code)
		require.Empty(t, errOut.String())
		require.Empty(t, out.String())
	}
	{
		var out bytes.Buffer
		var errOut bytes.Buffer
		code, err := Run([]string{"codalotl", "cas", "get", "ns-1.0", "./p"}, &RunOptions{Out: &out, Err: &errOut})
		require.NoError(t, err)
		require.Equal(t, 0, code)
		require.Empty(t, errOut.String())
		var got map[string]any
		require.NoError(t, json.Unmarshal(out.Bytes(), &got))
		require.Equal(t, true, got["ok"])
		val := got["value"].(map[string]any)
		require.Equal(t, "ok", val["result"])
	}
}
func TestRun_CAS_Retrieve_Miss_PrintsOKFalse(t *testing.T) {
	isolateUserConfig(t)
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	pkgDir := filepath.Join(tmp, "p")
	require.NoError(t, os.MkdirAll(pkgDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "p.go"), []byte("package p\n\nfunc P() {}\n"), 0644))
	t.Setenv("CODALOTL_CAS_DB", filepath.Join(tmp, "casdb"))
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "get", "ns-1.0", "./p"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	var got map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	require.Equal(t, false, got["ok"])
	_, hasValue := got["value"]
	require.False(t, hasValue)
}
func TestRun_CAS_Store_UsesCODALOTL_CAS_DB(t *testing.T) {
	isolateUserConfig(t)
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	// If CODALOTL_CAS_DB is not set, the CLI would store under the nearest git dir.
	require.NoError(t, os.WriteFile(filepath.Join(tmp, ".git"), []byte(""), 0644))
	pkgDir := filepath.Join(tmp, "p")
	require.NoError(t, os.MkdirAll(pkgDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "p.go"), []byte("package p\n\nfunc P() {}\n"), 0644))
	dbRoot := filepath.Join(tmp, "casdb")
	t.Setenv("CODALOTL_CAS_DB", dbRoot)
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "set", "ns-1.0", "./p", `"OK"`}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.Empty(t, out.String())
	// Store should create the namespace dir within the configured CAS root.
	_, err = os.Stat(filepath.Join(dbRoot, "ns-1.0"))
	require.NoError(t, err)
	// Ensure we did not fall back to the git-dir-based location.
	_, err = os.Stat(filepath.Join(tmp, ".codalotl", "cas"))
	require.Error(t, err)
	require.True(t, os.IsNotExist(err))
}
func TestRun_CAS_Store_UsesNearestGitDir_WhenEnvUnset(t *testing.T) {
	isolateUserConfig(t)
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, ".git"), []byte(""), 0644))
	pkgDir := filepath.Join(tmp, "p")
	require.NoError(t, os.MkdirAll(pkgDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "p.go"), []byte("package p\n\nfunc P() {}\n"), 0644))
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "set", "ns-1.0", "./p", `"OK"`}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.Empty(t, out.String())
	_, err = os.Stat(filepath.Join(tmp, ".codalotl", "cas", "ns-1.0"))
	require.NoError(t, err)
}
func TestRun_CAS_Store_InvalidNamespace_IsUsageError(t *testing.T) {
	isolateUserConfig(t)
	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "set", "no/slashes", ".", `"OK"`}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 2, code)
	require.NotEmpty(t, errOut.String())
}
func TestRun_CAS_Store_InvalidValue_IsUsageError(t *testing.T) {
	isolateUserConfig(t)
	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "set", "ns-1.0", ".", "not-json"}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 2, code)
	require.NotEmpty(t, errOut.String())
}
