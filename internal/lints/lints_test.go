package lints

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/q/cmdrunner"
	"github.com/stretchr/testify/require"
)

func TestResolveSteps_Defaults(t *testing.T) {
	steps, err := ResolveSteps(nil, 0)
	require.NoError(t, err)
	require.Len(t, steps, 1)
	require.Equal(t, "gofmt", steps[0].ID)

	require.Equal(t, "{{ .moduleDir }}", steps[0].Check.CWD)
	require.Contains(t, steps[0].Check.Args, "{{ .relativePackageDir }}")
}

func TestResolveSteps_ExtendDuplicateID(t *testing.T) {
	cfg := &Lints{
		Mode: ConfigModeExtend,
		Steps: []Step{
			{
				ID: "gofmt",
				Check: &cmdrunner.Command{
					Command: "anything",
				},
			},
		},
	}
	_, err := ResolveSteps(cfg, 120)
	require.Error(t, err)
}

func TestResolveSteps_ReplaceEmptyDisablesAll(t *testing.T) {
	cfg := &Lints{
		Mode:  ConfigModeReplace,
		Steps: nil,
	}
	steps, err := ResolveSteps(cfg, 120)
	require.NoError(t, err)
	require.Len(t, steps, 0)
}

func TestResolveSteps_Disable(t *testing.T) {
	cfg := &Lints{
		Disable: []string{"gofmt"},
	}
	steps, err := ResolveSteps(cfg, 120)
	require.NoError(t, err)
	require.Len(t, steps, 0)
}

func TestResolveSteps_ExtendCanAddPreconfiguredReflowByID(t *testing.T) {
	cfg := &Lints{
		Mode: ConfigModeExtend,
		Steps: []Step{
			{ID: "reflow"},
		},
	}

	steps, err := ResolveSteps(cfg, 123)
	require.NoError(t, err)
	require.Len(t, steps, 2)
	require.Equal(t, "gofmt", steps[0].ID)
	require.Equal(t, "reflow", steps[1].ID)

	require.Equal(t, "{{ .moduleDir }}", steps[1].Check.CWD)
	require.Contains(t, steps[1].Check.Args, "{{ .relativePackageDir }}")
	require.Contains(t, steps[1].Check.Args, "--check")
	require.Contains(t, steps[1].Check.Args, "--width=123")
	require.NotContains(t, steps[1].Fix.Args, "--check")
	require.Contains(t, steps[1].Fix.Args, "--width=123")

	// The preconfigured reflow step is intentionally excluded from initial.
	require.NotContains(t, steps[1].Situations, SituationInitial)
}

func TestResolveSteps_AllowsDuplicateUnsetID(t *testing.T) {
	cfg := &Lints{
		Mode: ConfigModeReplace,
		Steps: []Step{
			{ID: "", Check: helperCmd("", 0, true)},
			{ID: "", Check: helperCmd("", 0, true)},
		},
	}
	_, err := ResolveSteps(cfg, 120)
	require.NoError(t, err)
}

func TestResolveSteps_ReflowWidthNormalization(t *testing.T) {
	cfg := &Lints{
		Mode: ConfigModeReplace,
		Steps: []Step{
			{
				ID: "reflow",
				Check: &cmdrunner.Command{
					Command: "codalotl",
					Args:    []string{"docs", "reflow"},
				},
			},
		},
	}
	steps, err := ResolveSteps(cfg, 123)
	require.NoError(t, err)
	require.Len(t, steps, 1)
	require.Contains(t, steps[0].Check.Args, "--width=123")
}

func TestResolveSteps_ReflowWidthNotDuplicated(t *testing.T) {
	cfg := &Lints{
		Mode: ConfigModeReplace,
		Steps: []Step{
			{
				ID: "reflow",
				Check: &cmdrunner.Command{
					Command: "codalotl",
					Args:    []string{"docs", "reflow", "--width=99"},
				},
			},
		},
	}
	steps, err := ResolveSteps(cfg, 123)
	require.NoError(t, err)
	require.Len(t, steps, 1)
	require.Contains(t, steps[0].Check.Args, "--width=99")
	require.NotContains(t, steps[0].Check.Args, "--width=123")
}

func TestResolveSteps_ReflowWidthErrorsOnMultiple(t *testing.T) {
	cfg := &Lints{
		Mode: ConfigModeReplace,
		Steps: []Step{
			{
				ID: "reflow",
				Check: &cmdrunner.Command{
					Command: "codalotl",
					Args:    []string{"docs", "reflow", "--width=99", "--width=100"},
				},
			},
		},
	}
	_, err := ResolveSteps(cfg, 123)
	require.Error(t, err)
}

func TestRun_NoSteps(t *testing.T) {
	out, err := Run(context.Background(), t.TempDir(), t.TempDir(), nil, SituationCheck)
	require.NoError(t, err)
	require.Equal(t, `<lint-status ok="true" message="no linters"></lint-status>`, out)
}

func TestRun_SkipsReflowDuringInitial(t *testing.T) {
	out, err := Run(context.Background(), t.TempDir(), t.TempDir(), []Step{{ID: "reflow"}}, SituationInitial)
	require.NoError(t, err)
	require.Equal(t, `<lint-status ok="true" message="no linters"></lint-status>`, out)
}

func writeTempModule(t *testing.T) (sandboxDir string, targetPkgAbsDir string, relativePackageDir string) {
	t.Helper()

	sandboxDir = t.TempDir()
	err := os.WriteFile(
		filepath.Join(sandboxDir, "go.mod"),
		[]byte("module example.com/temp\n\ngo 1.22\n"),
		0o644,
	)
	require.NoError(t, err)

	relativePackageDir = filepath.ToSlash(filepath.Join("internal", "tgt"))
	targetPkgAbsDir = filepath.Join(sandboxDir, filepath.FromSlash(relativePackageDir))
	require.NoError(t, os.MkdirAll(targetPkgAbsDir, 0o755))
	return sandboxDir, targetPkgAbsDir, relativePackageDir
}

func TestRun_CheckModeRunsAllSteps(t *testing.T) {
	t.Setenv("CODALOTL_LINTS_HELPER_PROCESS", "1")

	sandboxDir, target, relativePackageDir := writeTempModule(t)

	steps := []Step{
		{
			ID:    "a",
			Check: helperCmd("issue", 0, true),
		},
		{
			ID:    "b",
			Check: helperCmd("{{ .relativePackageDir }}", 0, false),
		},
	}

	out, err := Run(context.Background(), sandboxDir, target, steps, SituationCheck)
	require.NoError(t, err)

	require.Contains(t, out, `lint-status ok="false"`)
	require.Equal(t, 2, strings.Count(out, "<command"))
	require.Contains(t, out, `mode="check"`)
	require.Contains(t, out, `<command ok="false"`)
	require.Contains(t, out, `<command ok="true"`)
	require.NotContains(t, out, "{{ .relativePackageDir }}")
	require.Contains(t, out, relativePackageDir)
}

func TestRun_FixModeUsesFixWhenAvailable(t *testing.T) {
	t.Setenv("CODALOTL_LINTS_HELPER_PROCESS", "1")

	sandboxDir, target, _ := writeTempModule(t)

	steps := []Step{
		{
			ID:    "a",
			Check: helperCmd("issue", 0, true),
			Fix:   helperCmd("{{ .moduleDir }}", 0, false),
		},
		{
			ID:    "b",
			Check: helperCmd("", 0, true),
		},
	}

	out, err := Run(context.Background(), sandboxDir, target, steps, SituationFix)
	require.NoError(t, err)

	require.Contains(t, out, `lint-status ok="true"`)
	require.Contains(t, out, `mode="fix"`)
	require.Contains(t, out, `mode="check"`)
	require.NotContains(t, out, "{{ .moduleDir }}")
	require.Contains(t, out, sandboxDir)
}

func TestRun_FiltersBySituation(t *testing.T) {
	t.Setenv("CODALOTL_LINTS_HELPER_PROCESS", "1")

	sandboxDir, target, _ := writeTempModule(t)

	steps := []Step{
		{
			ID:         "check-only",
			Situations: []Situation{SituationCheck},
			Check:      helperCmd("", 0, true),
		},
		{
			ID:         "fix-only",
			Situations: []Situation{SituationFix},
			Check:      helperCmd("", 0, true),
			Fix:        helperCmd("", 0, true),
		},
	}

	outCheck, err := Run(context.Background(), sandboxDir, target, steps, SituationCheck)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(outCheck, "\n$ "))

	outFix, err := Run(context.Background(), sandboxDir, target, steps, SituationFix)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(outFix, "\n$ "))
}

func helperCmd(stdout string, exitCode int, failIfAnyOutput bool) *cmdrunner.Command {
	return &cmdrunner.Command{
		Command: os.Args[0],
		Args: []string{
			"-test.run=^TestLintsHelperProcess$",
			"--",
			"stdout=" + stdout,
			"exit=" + strconv.Itoa(exitCode),
		},
		OutcomeFailIfAnyOutput: failIfAnyOutput,
	}
}

func TestLintsHelperProcess(t *testing.T) {
	if os.Getenv("CODALOTL_LINTS_HELPER_PROCESS") != "1" {
		return
	}

	args := os.Args
	delimiter := -1
	for i, a := range args {
		if a == "--" {
			delimiter = i
			break
		}
	}
	if delimiter == -1 {
		os.Exit(2)
	}

	var stdout string
	exitCode := 0

	for _, a := range args[delimiter+1:] {
		if strings.HasPrefix(a, "stdout=") {
			stdout = strings.TrimPrefix(a, "stdout=")
			continue
		}
		if strings.HasPrefix(a, "exit=") {
			n, err := strconv.Atoi(strings.TrimPrefix(a, "exit="))
			if err != nil {
				fmt.Fprint(os.Stderr, "bad exit code")
				os.Exit(2)
			}
			exitCode = n
			continue
		}
	}

	if stdout != "" {
		fmt.Fprint(os.Stdout, stdout)
	}
	os.Exit(exitCode)
}
