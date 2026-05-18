package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/gocas"
	"github.com/stretchr/testify/require"
)

func TestRun_CAS_StoreAndRetrieve_RoundTrip(t *testing.T) {
	isolateUserConfig(t)
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	require.NoError(t, os.Mkdir(filepath.Join(tmp, ".git"), 0755))
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
		code, err := Run([]string{"codalotl", "cas", "set", "docs-fix", "./p", `{"result":"ok"}`}, &RunOptions{Out: &out, Err: &errOut})
		require.NoError(t, err)
		require.Equal(t, 0, code)
		require.Empty(t, errOut.String())
		require.Empty(t, out.String())
	}
	{
		var out bytes.Buffer
		var errOut bytes.Buffer
		code, err := Run([]string{"codalotl", "cas", "get", "docs-fix", "./p"}, &RunOptions{Out: &out, Err: &errOut})
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
func TestRun_CAS_Retrieve_Miss_PrintsNothingAndExit1(t *testing.T) {
	isolateUserConfig(t)
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	pkgDir := filepath.Join(tmp, "p")
	require.NoError(t, os.MkdirAll(pkgDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "p.go"), []byte("package p\n\nfunc P() {}\n"), 0644))
	dbRoot := filepath.Join(tmp, "casdb")
	t.Setenv(gocas.EnvCASDB, dbRoot)
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "get", "docs-fix", "./p"}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 1, code)
	require.Empty(t, errOut.String())
	require.Empty(t, out.String())
	_, err = os.Stat(dbRoot)
	require.Error(t, err)
	require.True(t, os.IsNotExist(err))
}
func TestRun_CAS_Store_UsesCODALOTL_CAS_DB(t *testing.T) {
	isolateUserConfig(t)
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	// If CODALOTL_CAS_DB is not set, the CLI would store under the nearest git dir.
	require.NoError(t, os.Mkdir(filepath.Join(tmp, ".git"), 0755))
	pkgDir := filepath.Join(tmp, "p")
	require.NoError(t, os.MkdirAll(pkgDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "p.go"), []byte("package p\n\nfunc P() {}\n"), 0644))
	dbRoot := filepath.Join(tmp, "casdb")
	t.Setenv(gocas.EnvCASDB, dbRoot)
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "set", "docs-fix", "./p", `"OK"`}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.Empty(t, out.String())
	// Store should create the namespace dir within the configured CAS root.
	_, err = os.Stat(filepath.Join(dbRoot, "docs-fix-1"))
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
	require.NoError(t, os.Mkdir(filepath.Join(tmp, ".git"), 0755))
	pkgDir := filepath.Join(tmp, "p")
	require.NoError(t, os.MkdirAll(pkgDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "p.go"), []byte("package p\n\nfunc P() {}\n"), 0644))
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "set", "docs-fix", "./p", `"OK"`}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.Empty(t, out.String())
	_, err = os.Stat(filepath.Join(tmp, ".codalotl", "cas", "docs-fix-1"))
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
	code, err := Run([]string{"codalotl", "cas", "set", "docs-fix", ".", "not-json"}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 2, code)
	require.NotEmpty(t, errOut.String())
}

func TestRun_CAS_UnknownNamespace_IsUsageError(t *testing.T) {
	isolateUserConfig(t)
	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "get", "unknown-ns", "."}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 2, code)
	require.Contains(t, errOut.String(), `unknown CAS namespace "unknown-ns"`)
}

func TestRun_CAS_LSNamespaces_ListsRegisteredNamespaces(t *testing.T) {
	isolateUserConfig(t)
	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "ls-namespaces"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Contains(t, lines, "clarify-public-api 1")
	require.Contains(t, lines, "docs-fix 1")
	require.Contains(t, lines, "specconforms 1")
	require.IsIncreasing(t, lines)
}

func TestRun_CAS_LSUnset_ListsPackagesMissingNamespace(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))

	p1Dir := filepath.Join(tmp, "p1")
	require.NoError(t, os.MkdirAll(p1Dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(p1Dir, "p1.go"), []byte("package p1\n\nfunc P1() {}\n"), 0644))

	p2Dir := filepath.Join(tmp, "p2")
	require.NoError(t, os.MkdirAll(p2Dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(p2Dir, "p2.go"), []byte("package p2\n\nfunc P2() {}\n"), 0644))

	t.Setenv(gocas.EnvCASDB, filepath.Join(tmp, "casdb"))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	{
		var out bytes.Buffer
		var errOut bytes.Buffer
		code, err := Run([]string{"codalotl", "cas", "set", "docs-fix", "./p1", `"OK"`}, &RunOptions{Out: &out, Err: &errOut})
		require.NoError(t, err)
		require.Equal(t, 0, code)
		require.Empty(t, errOut.String())
		require.Empty(t, out.String())
	}

	{
		var out bytes.Buffer
		var errOut bytes.Buffer
		code, err := Run([]string{"codalotl", "cas", "ls-unset", "docs-fix"}, &RunOptions{Out: &out, Err: &errOut})
		require.NoError(t, err)
		require.Equal(t, 0, code)
		require.Empty(t, errOut.String())

		got := map[string]bool{}
		for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				got[line] = true
			}
		}

		require.True(t, got["./p2"])
		require.False(t, got["./p1"])
	}
}
