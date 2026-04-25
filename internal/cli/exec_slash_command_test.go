package cli

import (
	"bytes"
	"testing"

	"github.com/codalotl/codalotl/internal/noninteractive"
	"github.com/stretchr/testify/require"
)

func stubRunNoninteractiveExec(t *testing.T, fn func(string, noninteractive.Options) error) {
	t.Helper()

	orig := runNoninteractiveExec
	runNoninteractiveExec = fn
	t.Cleanup(func() { runNoninteractiveExec = orig })
}

func TestRun_Exec_HelpMentionsSlashCommand(t *testing.T) {
	isolateUserConfig(t)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "exec", "--help"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Contains(t, out.String(), "--slash-command")
	require.Contains(t, out.String(), "orchestrate")
	require.Empty(t, errOut.String())
}

func TestRun_Exec_SlashCommandOrchestrate_AllowsEmptyPrompt(t *testing.T) {
	isolateUserConfig(t)
	chdirForTest(t, t.TempDir())

	var gotPrompt string
	var gotOpts noninteractive.Options
	stubRunNoninteractiveExec(t, func(userPrompt string, opts noninteractive.Options) error {
		gotPrompt = userPrompt
		gotOpts = opts
		return nil
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "exec", "--slash-command=orchestrate"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, gotPrompt)
	require.Equal(t, "orchestrate", gotOpts.SlashCommand)
	require.Empty(t, errOut.String())
}

func TestRun_Exec_SlashCommandSlashOrchestrate_AllowsEmptyPrompt(t *testing.T) {
	isolateUserConfig(t)
	chdirForTest(t, t.TempDir())

	var gotPrompt string
	var gotOpts noninteractive.Options
	stubRunNoninteractiveExec(t, func(userPrompt string, opts noninteractive.Options) error {
		gotPrompt = userPrompt
		gotOpts = opts
		return nil
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "exec", "--slash-command=/orchestrate"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, gotPrompt)
	require.Equal(t, "/orchestrate", gotOpts.SlashCommand)
	require.Empty(t, errOut.String())
}

func TestRun_Exec_WithoutPromptAndWithoutOrchestrateSlashCommand_IsUsageError(t *testing.T) {
	isolateUserConfig(t)
	chdirForTest(t, t.TempDir())

	called := false
	stubRunNoninteractiveExec(t, func(userPrompt string, opts noninteractive.Options) error {
		called = true
		return nil
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "exec"}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 2, code)
	require.False(t, called)
	require.NotEmpty(t, errOut.String())
}

func TestRun_Exec_SlashCommand_ForwardsPromptAndExistingFlags(t *testing.T) {
	isolateUserConfig(t)
	chdirForTest(t, t.TempDir())

	var gotPrompt string
	var gotOpts noninteractive.Options
	stubRunNoninteractiveExec(t, func(userPrompt string, opts noninteractive.Options) error {
		gotPrompt = userPrompt
		gotOpts = opts
		return nil
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{
		"codalotl",
		"exec",
		"--package=.",
		"--yes",
		"--no-color",
		"--json",
		"--model=gpt-5.5-high",
		"--slash-command=orchestrate",
		"hello",
		"world",
	}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Equal(t, "hello world", gotPrompt)
	require.Equal(t, ".", gotOpts.PackagePath)
	require.Equal(t, "orchestrate", gotOpts.SlashCommand)
	require.True(t, gotOpts.AutoYes)
	require.True(t, gotOpts.NoFormatting)
	require.True(t, gotOpts.OutputJSON)
	require.Equal(t, "gpt-5.5-high", string(gotOpts.ModelID))
	require.Empty(t, errOut.String())
}
