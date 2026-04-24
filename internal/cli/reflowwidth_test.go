package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/noninteractive"
	"github.com/codalotl/codalotl/internal/tui"
	"github.com/stretchr/testify/require"
)

func writeReflowWidthConfig(t *testing.T, root string, width int) {
	t.Helper()
	writeProjectConfig(t, root, fmt.Sprintf(`{
  "reflowwidth": %d,
  "lints": {
    "steps": [
      { "id": "reflow" }
    ]
  }
}
`, width))
}

func requireResolvedReflowWidth(t *testing.T, steps []lints.Step, width int) {
	t.Helper()
	widthArg := fmt.Sprintf("--width=%d", width)

	var reflow *lints.Step
	var specFmt *lints.Step
	for i := range steps {
		switch steps[i].ID {
		case "reflow":
			reflow = &steps[i]
		case "spec-fmt":
			specFmt = &steps[i]
		}
	}

	require.NotNil(t, reflow)
	require.NotNil(t, reflow.Check)
	require.NotNil(t, reflow.Fix)
	require.Contains(t, reflow.Check.Args, widthArg)
	require.Contains(t, reflow.Fix.Args, widthArg)

	require.NotNil(t, specFmt)
	require.NotNil(t, specFmt.Fix)
	require.Contains(t, specFmt.Fix.Args, widthArg)
}

func TestRun_TUI_UsesConfigReflowWidthInResolvedLintSteps(t *testing.T) {
	isolateUserConfig(t)
	t.Setenv("CODALOTL_CAS_DB", "")

	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, ".git"), []byte(""), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "p"), 0o755))
	writeReflowWidthConfig(t, tmp, 88)
	chdirForTest(t, filepath.Join(tmp, "p"))

	called := false
	orig := runTUIWithConfig
	runTUIWithConfig = func(cfg tui.Config) error {
		called = true
		requireResolvedReflowWidth(t, cfg.LintSteps, 88)
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

func TestRun_Exec_UsesConfigReflowWidthInResolvedLintSteps(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	writeReflowWidthConfig(t, tmp, 90)
	chdirForTest(t, tmp)

	orig := runNoninteractiveExec
	runNoninteractiveExec = func(userPrompt string, opts noninteractive.Options) error {
		require.Equal(t, "hello world", userPrompt)
		requireResolvedReflowWidth(t, opts.LintSteps, 90)
		return nil
	}
	t.Cleanup(func() { runNoninteractiveExec = orig })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "exec", "hello world"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
}

func TestRun_Iterate_UsesConfigReflowWidthInResolvedLintSteps(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	writeReflowWidthConfig(t, tmp, 92)
	chdirForTest(t, tmp)

	session := &fakeIterateSession{
		t: t,
		results: []noninteractive.Result{{
			TerminalEventType:   agent.EventTypeDoneSuccess,
			FinalAssistantText:  "STOP_ITERATION",
			ContextUsagePercent: 5,
		}},
	}
	stubNewNoninteractiveSession(t, func(opts noninteractive.Options) (iterateSession, error) {
		requireResolvedReflowWidth(t, opts.LintSteps, 92)
		return session, nil
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "iterate", "hello world"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
}
