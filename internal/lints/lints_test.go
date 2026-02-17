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
	require.Len(t, steps, 2)
	require.Equal(t, "gofmt", steps[0].ID)
	require.Equal(t, "spec-diff", steps[1].ID)
	require.Equal(t, []Situation{SituationTests, SituationFix}, steps[1].Situations)
	require.NotNil(t, steps[1].Check)
	require.Nil(t, steps[1].Fix)
	require.Equal(t, "{{ .moduleDir }}", steps[1].Check.CWD)
	require.Contains(t, steps[1].Check.Args, "{{ .relativePackageDir }}")

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
	require.Len(t, steps, 1)
	require.Equal(t, "spec-diff", steps[0].ID)
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
	require.Len(t, steps, 3)
	require.Equal(t, "gofmt", steps[0].ID)
	require.Equal(t, "spec-diff", steps[1].ID)
	require.Equal(t, "reflow", steps[2].ID)
	reflow := steps[2]

	require.Equal(t, "{{ .moduleDir }}", reflow.Check.CWD)
	require.Contains(t, reflow.Check.Args, "{{ .relativePackageDir }}")
	require.Contains(t, reflow.Check.Args, "--check")
	require.Contains(t, reflow.Check.Args, "--width=123")
	require.NotContains(t, reflow.Fix.Args, "--check")
	require.Contains(t, reflow.Fix.Args, "--width=123")

	// The preconfigured reflow step is intentionally excluded from initial.
	require.NotContains(t, reflow.Situations, SituationInitial)
}

func TestResolveSteps_ExtendAllowsOverridingSituationsForPreconfiguredStep(t *testing.T) {
	cfg := &Lints{
		Mode: ConfigModeExtend,
		Steps: []Step{
			{ID: "reflow", Situations: []Situation{SituationFix}},
		},
	}

	steps, err := ResolveSteps(cfg, 123)
	require.NoError(t, err)
	require.Len(t, steps, 3)
	require.Equal(t, "reflow", steps[2].ID)
	require.Equal(t, []Situation{SituationFix}, steps[2].Situations)
}

func TestResolveSteps_ExtendCanAddPreconfiguredStaticcheckByID(t *testing.T) {
	cfg := &Lints{
		Mode: ConfigModeExtend,
		Steps: []Step{
			{ID: "staticcheck"},
		},
	}

	steps, err := ResolveSteps(cfg, 120)
	require.NoError(t, err)
	require.Len(t, steps, 3)
	require.Equal(t, "gofmt", steps[0].ID)
	require.Equal(t, "spec-diff", steps[1].ID)
	require.Equal(t, "staticcheck", steps[2].ID)
	require.Equal(t, "{{ .moduleDir }}", steps[2].Check.CWD)
	require.Contains(t, steps[2].Check.Args, "./{{ .relativePackageDir }}")
	require.Nil(t, steps[2].Situations)
	require.Nil(t, steps[2].Fix)
}

func TestResolveSteps_ExtendCanAddPreconfiguredGolangciLintByID(t *testing.T) {
	cfg := &Lints{
		Mode: ConfigModeExtend,
		Steps: []Step{
			{ID: "golangci-lint"},
		},
	}

	steps, err := ResolveSteps(cfg, 120)
	require.NoError(t, err)
	require.Len(t, steps, 3)
	require.Equal(t, "gofmt", steps[0].ID)
	require.Equal(t, "spec-diff", steps[1].ID)
	require.Equal(t, "golangci-lint", steps[2].ID)
	require.Equal(t, "{{ .moduleDir }}", steps[2].Check.CWD)
	require.Contains(t, steps[2].Check.Args, "./{{ .relativePackageDir }}")
	require.Contains(t, steps[2].Fix.Args, "--fix")
	require.Nil(t, steps[2].Situations)
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

func TestLints_Reflows(t *testing.T) {
	t.Run("default false", func(t *testing.T) {
		require.False(t, (Lints{}).Reflows())
	})

	t.Run("extend with reflow", func(t *testing.T) {
		cfg := Lints{
			Mode: ConfigModeExtend,
			Steps: []Step{
				{ID: "reflow"},
			},
		}
		require.True(t, cfg.Reflows())
	})

	t.Run("disabled", func(t *testing.T) {
		cfg := Lints{
			Mode:    ConfigModeExtend,
			Disable: []string{"reflow"},
			Steps: []Step{
				{ID: "reflow"},
			},
		}
		require.False(t, cfg.Reflows())
	})

	t.Run("situations empty means never runs", func(t *testing.T) {
		cfg := Lints{
			Mode: ConfigModeReplace,
			Steps: []Step{
				{ID: "reflow", Situations: []Situation{}},
			},
		}
		require.False(t, cfg.Reflows())
	})

	t.Run("situations initial only means never runs", func(t *testing.T) {
		cfg := Lints{
			Mode: ConfigModeReplace,
			Steps: []Step{
				{ID: "reflow", Situations: []Situation{SituationInitial}},
			},
		}
		require.False(t, cfg.Reflows())
	})

	t.Run("situations fix means runs", func(t *testing.T) {
		cfg := Lints{
			Mode: ConfigModeReplace,
			Steps: []Step{
				{ID: "reflow", Situations: []Situation{SituationFix}},
			},
		}
		require.True(t, cfg.Reflows())
	})

	t.Run("invalid config false", func(t *testing.T) {
		cfg := Lints{
			Mode: ConfigModeReplace,
			Steps: []Step{
				{ID: "reflow", Situations: []Situation{"bogus"}},
			},
		}
		require.False(t, cfg.Reflows())
	})
}

func TestRun_NoSteps(t *testing.T) {
	out, err := Run(context.Background(), t.TempDir(), t.TempDir(), nil, SituationTests)
	require.NoError(t, err)
	require.Equal(t, `<lint-status ok="true" message="no linters"></lint-status>`, out)
}

func TestRun_SkipsReflowDuringInitial(t *testing.T) {
	out, err := Run(context.Background(), t.TempDir(), t.TempDir(), []Step{{ID: "reflow"}}, SituationInitial)
	require.NoError(t, err)
	require.Equal(t, `<lint-status ok="true" message="no linters"></lint-status>`, out)
}

func TestRun_SpecDiffSkippedWhenNoSpecMD(t *testing.T) {
	sandboxDir, target, _ := writeTempModule(t)
	step, ok := preconfiguredStep("spec-diff", 0)
	require.True(t, ok)

	out, err := Run(context.Background(), sandboxDir, target, []Step{step}, SituationFix)
	require.NoError(t, err)
	require.Equal(t, `<lint-status ok="true" message="no linters"></lint-status>`, out)
}

func TestRun_SpecDiffRunsInProcess(t *testing.T) {
	t.Setenv("CODALOTL_LINTS_HELPER_PROCESS", "1")

	sandboxDir, target, relativePackageDir := writeTempModule(t)

	// Minimal implementation for the package so specmd can diff against it.
	err := os.WriteFile(filepath.Join(target, "tgt.go"), []byte("package tgt\n"), 0o644)
	require.NoError(t, err)

	// A SPEC with a declared public API that is missing in the implementation.
	spec := strings.Join([]string{
		"# spec",
		"",
		"## Public API",
		"",
		"```go {api}",
		"func Foo() {",
		"}",
		"```",
		"",
	}, "\n")
	err = os.WriteFile(filepath.Join(target, "SPEC.md"), []byte(spec), 0o644)
	require.NoError(t, err)

	// Even if the configured command is something else, `spec-diff` is executed
	// in-process.
	steps := []Step{{
		ID:         "spec-diff",
		Situations: []Situation{SituationFix},
		Check:      helperCmd("should-not-run", 0, false),
	}}

	out, err := Run(context.Background(), sandboxDir, target, steps, SituationFix)
	require.NoError(t, err)

	require.Contains(t, out, `lint-status ok="false"`)
	require.Contains(t, out, "\n$ codalotl spec diff "+relativePackageDir+"\n")
	require.Contains(t, out, `mode="check"`)
	require.NotContains(t, out, `mode="fix"`)
	require.Contains(t, out, "Foo")
	require.Contains(t, out, "Fixing SPEC diff failures")
	require.NotContains(t, out, "should-not-run")
}

func TestRunSpecDiff_NoInstructionsWhenNoDiffs(t *testing.T) {
	sandboxDir, target, relativePackageDir := writeTempModule(t)

	err := os.WriteFile(filepath.Join(target, "tgt.go"), []byte("package tgt\n\nfunc Foo() {}\n"), 0o644)
	require.NoError(t, err)

	spec := strings.Join([]string{
		"# spec",
		"",
		"## Public API",
		"",
		"```go {api}",
		"func Foo() {",
		"}",
		"```",
		"",
	}, "\n")
	err = os.WriteFile(filepath.Join(target, "SPEC.md"), []byte(spec), 0o644)
	require.NoError(t, err)

	cr := runSpecDiff(relativePackageDir, target)
	require.Equal(t, cmdrunner.OutcomeSuccess, cr.Outcome)
	require.Empty(t, cr.Output)
	require.NotContains(t, cr.Output, "Fixing SPEC diff failures")
	require.NoError(t, cr.ExecError)
	require.Equal(t, "codalotl", cr.Command)
	require.Equal(t, []string{"spec", "diff", relativePackageDir}, cr.Args)
	require.Equal(t, []string{"mode", "check"}, cr.Attrs)

	// Silence unused in case a future refactor removes a module-dir dependency.
	_ = sandboxDir
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

	out, err := Run(context.Background(), sandboxDir, target, steps, SituationTests)
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
			Situations: []Situation{SituationTests},
			Check:      helperCmd("", 0, true),
		},
		{
			ID:         "fix-only",
			Situations: []Situation{SituationFix},
			Check:      helperCmd("", 0, true),
			Fix:        helperCmd("", 0, true),
		},
	}

	outCheck, err := Run(context.Background(), sandboxDir, target, steps, SituationTests)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(outCheck, "\n$ "))

	outFix, err := Run(context.Background(), sandboxDir, target, steps, SituationFix)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(outFix, "\n$ "))
}

func TestRun_ConditionalStepInactiveIsSkipped(t *testing.T) {
	t.Setenv("CODALOTL_LINTS_HELPER_PROCESS", "1")
	sandboxDir, target, _ := writeTempModule(t)
	steps := []Step{
		{
			ID:     "a",
			Active: helperCmd("", 0, false), // inactive
			Check:  helperCmd("should-not-run", 0, false),
		},
	}
	out, err := Run(context.Background(), sandboxDir, target, steps, SituationTests)
	require.NoError(t, err)
	require.Equal(t, `<lint-status ok="true" message="no linters"></lint-status>`, out)
}

func TestRun_ConditionalStepWhitespaceOutputIsInactive(t *testing.T) {
	t.Setenv("CODALOTL_LINTS_HELPER_PROCESS", "1")
	sandboxDir, target, _ := writeTempModule(t)
	steps := []Step{
		{
			ID:     "a",
			Active: helperCmd("\n", 0, false), // treated as empty output
			Check:  helperCmd("should-not-run", 0, false),
		},
	}
	out, err := Run(context.Background(), sandboxDir, target, steps, SituationTests)
	require.NoError(t, err)
	require.Equal(t, `<lint-status ok="true" message="no linters"></lint-status>`, out)
}

func TestRun_ConditionalStepActiveRunsAndActiveOutputIsInvisible(t *testing.T) {
	t.Setenv("CODALOTL_LINTS_HELPER_PROCESS", "1")
	sandboxDir, target, _ := writeTempModule(t)
	steps := []Step{
		{
			ID:     "a",
			Active: helperCmd("SECRET", 0, false), // active
			Check:  helperCmd("ran", 0, false),
		},
	}
	out, err := Run(context.Background(), sandboxDir, target, steps, SituationTests)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(out, "\n$ "))
	require.Contains(t, out, "ran")
	require.NotContains(t, out, "SECRET")
}

func TestRun_ConditionalStepErrorIsTreatedActive(t *testing.T) {
	t.Setenv("CODALOTL_LINTS_HELPER_PROCESS", "1")
	sandboxDir, target, _ := writeTempModule(t)
	steps := []Step{
		{
			ID: "a",
			Active: &cmdrunner.Command{
				Command: "echo",
				Args: []string{
					"{{ .notARealVar }}",
				},
				CWD: "{{ .moduleDir }}",
			},
			Check: helperCmd("ran", 0, false),
		},
	}
	out, err := Run(context.Background(), sandboxDir, target, steps, SituationTests)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(out, "\n$ "))
	require.Contains(t, out, "ran")
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
