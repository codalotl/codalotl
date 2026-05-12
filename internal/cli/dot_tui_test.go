package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/gocas"
	"github.com/codalotl/codalotl/internal/tui"
	"github.com/stretchr/testify/require"
)

func TestRun_DotArg_LaunchesTUI(t *testing.T) {
	isolateUserConfig(t)
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	require.NoError(t, os.Mkdir(filepath.Join(tmp, ".git"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "p"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "p", "p.go"), []byte("package p\n"), 0644))
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(filepath.Join(tmp, "p")))
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	// On macOS, temp dirs are often exposed via a /var -> /private/var symlink.
	// Normalize to the "real" path so we can do a stable string comparison.
	tmpReal, err := filepath.EvalSymlinks(tmp)
	require.NoError(t, err)
	wantCASRoot := filepath.Join(tmpReal, ".codalotl", "cas")
	t.Setenv(gocas.EnvCASDB, wantCASRoot)

	var called bool
	orig := runTUIWithConfig
	runTUIWithConfig = func(cfg tui.Config) error {
		called = true
		// The CLI always supplies a PersistModelID hook so the TUI can request it.
		require.NotNil(t, cfg.PersistModelID)
		require.NotNil(t, cfg.CASDB)
		require.Equal(t, wantCASRoot, cfg.CASDB.AbsRoot)
		require.True(t, strings.HasSuffix(cfg.CASDB.AbsRoot, filepath.Join(".codalotl", "cas")))
		return nil
	}
	t.Cleanup(func() { runTUIWithConfig = orig })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "."}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.True(t, called)
	_, err = os.Stat(wantCASRoot)
	require.Error(t, err)
	require.True(t, os.IsNotExist(err))
}

func TestRun_PathArg_IsStillUsageError(t *testing.T) {
	isolateUserConfig(t)

	orig := runTUIWithConfig
	runTUIWithConfig = func(cfg tui.Config) error {
		return nil
	}
	t.Cleanup(func() { runTUIWithConfig = orig })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "./internal/cli"}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 2, code)
	require.NotEmpty(t, errOut.String())
}
