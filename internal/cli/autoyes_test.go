package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/codalotl/codalotl/internal/noninteractive"
	"github.com/codalotl/codalotl/internal/tui"
	"github.com/stretchr/testify/require"
)

func writeProjectConfig(t *testing.T, root string, contents string) {
	t.Helper()
	cfgPath := filepath.Join(root, ".codalotl", "config.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0o755))
	require.NoError(t, os.WriteFile(cfgPath, []byte(contents), 0o644))
}

func chdirForTest(t *testing.T, dir string) {
	t.Helper()
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origWD) })
}

func TestLoadConfig_AutoYes(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	writeProjectConfig(t, tmp, "{\n  \"autoyes\": true\n}\n")
	chdirForTest(t, tmp)

	cfg, err := loadConfig()
	require.NoError(t, err)
	require.True(t, cfg.AutoYes)
}

func TestRun_TUI_ForwardsConfigAutoYes(t *testing.T) {
	isolateUserConfig(t)
	t.Setenv("CODALOTL_CAS_DB", "")

	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, ".git"), []byte(""), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "p"), 0o755))
	writeProjectConfig(t, tmp, "{\n  \"autoyes\": true\n}\n")
	chdirForTest(t, filepath.Join(tmp, "p"))

	orig := runTUIWithConfig
	runTUIWithConfig = func(cfg tui.Config) error {
		require.True(t, cfg.AutoYes)
		return nil
	}
	t.Cleanup(func() { runTUIWithConfig = orig })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "."}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
}

func TestRun_Exec_UsesConfigAutoYes(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	writeProjectConfig(t, tmp, "{\n  \"autoyes\": true\n}\n")
	chdirForTest(t, tmp)

	origRunNoninteractiveExec := runNoninteractiveExec
	t.Cleanup(func() { runNoninteractiveExec = origRunNoninteractiveExec })

	runNoninteractiveExec = func(userPrompt string, opts noninteractive.Options) error {
		require.Equal(t, "hello world", userPrompt)
		require.True(t, opts.AutoYes)
		return nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "exec", "hello world"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
}

func TestRun_Exec_FlagEnablesAutoYes(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	chdirForTest(t, tmp)

	origRunNoninteractiveExec := runNoninteractiveExec
	t.Cleanup(func() { runNoninteractiveExec = origRunNoninteractiveExec })

	runNoninteractiveExec = func(userPrompt string, opts noninteractive.Options) error {
		require.Equal(t, "hello world", userPrompt)
		require.True(t, opts.AutoYes)
		return nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "exec", "-y", "hello world"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
}
