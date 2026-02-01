package cli

import (
	"bytes"
	"testing"

	"github.com/codalotl/codalotl/internal/tui"
	"github.com/stretchr/testify/require"
)

func TestRun_DotArg_LaunchesTUI(t *testing.T) {
	isolateUserConfig(t)

	var called bool
	orig := runTUIWithConfig
	runTUIWithConfig = func(cfg tui.Config) error {
		called = true
		// The CLI always supplies a PersistModelID hook so the TUI can request it.
		require.NotNil(t, cfg.PersistModelID)
		return nil
	}
	t.Cleanup(func() { runTUIWithConfig = orig })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "."}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.True(t, called)
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
