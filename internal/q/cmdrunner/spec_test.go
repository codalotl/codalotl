package cmdrunner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunnerUsageExample(t *testing.T) {
	t.Parallel()

	inputSchema := map[string]InputType{
		"path":        InputTypePathDir,
		"verbose":     InputTypeBool,
		"namePattern": InputTypeString,
	}
	requiredInputs := []string{"path"}

	runner := NewRunner(inputSchema, requiredInputs)
	runner.AddCommand(Command{
		Command: "go",
		Args: []string{
			"test",
			"{{ if eq .path (manifestDir .path) }}.{{ else }}./{{ relativeTo .path (manifestDir .path) }}{{ end }}",
			"{{ if .verbose }}-v{{ end }}",
			"{{ if ne .namePattern \"\" }}-run={{ .namePattern }}{{ end }}",
		},
		CWD: "{{ manifestDir .path }}",
	})

	root := t.TempDir()

	projectDir := filepath.Join(root, "project")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "pass"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "fail"), 0o755))

	writeScenarioFile(t, filepath.Join(projectDir, "go.mod"), `module example.com/spec

go 1.24.4
`)
	writeScenarioFile(t, filepath.Join(projectDir, "project.go"), `package project

func RootValue() int { return 42 }
`)
	writeScenarioFile(t, filepath.Join(projectDir, "project_test.go"), `package project

import "testing"

func TestRootValue(t *testing.T) {
	if RootValue() != 42 {
		t.Fatalf("unexpected value: got %d", RootValue())
	}
}
`)
	writeScenarioFile(t, filepath.Join(projectDir, "pass", "pass.go"), `package pass

func Sum(a, b int) int { return a + b }
`)
	writeScenarioFile(t, filepath.Join(projectDir, "pass", "pass_test.go"), `package pass

import "testing"

func TestPassAlpha(t *testing.T) {
	if Sum(1, 2) != 3 {
		t.Fatalf("Sum mismatch")
	}
}

func TestPassBeta(t *testing.T) {
	if Sum(2, 3) != 5 {
		t.Fatalf("Sum mismatch")
	}
}
`)
	writeScenarioFile(t, filepath.Join(projectDir, "fail", "fail.go"), `package fail

func AlwaysTrue() bool { return true }
`)
	writeScenarioFile(t, filepath.Join(projectDir, "fail", "fail_test.go"), `package fail

import "testing"

func TestFailAlpha(t *testing.T) {
	t.Fatalf("intentional failure")
}

func TestFailBeta(t *testing.T) {
	if !AlwaysTrue() {
		t.Fatalf("expected AlwaysTrue to return true")
	}
}
`)

	tests := []struct {
		name                  string
		path                  string
		verbose               bool
		namePattern           string
		wantArgs              []string
		wantSuccess           bool
		wantOutcome           Outcome
		wantExitCode          int
		wantExecError         bool
		wantOutputContains    []string
		wantOutputNotContains []string
	}{
		{
			name:          "module root verbose",
			path:          "project",
			verbose:       true,
			namePattern:   "",
			wantArgs:      []string{"test", ".", "-v"},
			wantSuccess:   true,
			wantOutcome:   OutcomeSuccess,
			wantExitCode:  0,
			wantExecError: false,
			wantOutputContains: []string{
				"--- PASS: TestRootValue",
			},
		},
		{
			name:          "pass package default flags",
			path:          filepath.Join("project", "pass"),
			verbose:       false,
			namePattern:   "",
			wantArgs:      []string{"test", "./pass"},
			wantSuccess:   true,
			wantOutcome:   OutcomeSuccess,
			wantExitCode:  0,
			wantExecError: false,
		},
		{
			name:          "fail package default flags",
			path:          filepath.Join("project", "fail"),
			verbose:       false,
			namePattern:   "",
			wantArgs:      []string{"test", "./fail"},
			wantSuccess:   false,
			wantOutcome:   OutcomeFailed,
			wantExitCode:  1,
			wantExecError: true,
			wantOutputContains: []string{
				"FAIL",
				"TestFailAlpha",
			},
		},
		{
			name:          "fail package filtered to passing test",
			path:          filepath.Join("project", "fail"),
			verbose:       false,
			namePattern:   "Beta",
			wantArgs:      []string{"test", "./fail", "-run=Beta"},
			wantSuccess:   true,
			wantOutcome:   OutcomeSuccess,
			wantExitCode:  0,
			wantExecError: false,
			wantOutputNotContains: []string{
				"TestFailAlpha",
			},
		},
	}

	for _, tc := range tests {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			result, err := runner.Run(context.Background(), root, map[string]any{
				"path":        tc.path,
				"verbose":     tc.verbose,
				"namePattern": tc.namePattern,
			})
			require.NoError(t, err)
			require.Len(t, result.Results, 1)

			commandResult := result.Results[0]

			require.Equal(t, "go", commandResult.Command)
			require.Equal(t, tc.wantArgs, commandResult.Args)
			require.Equal(t, projectDir, commandResult.CWD)
			require.Equal(t, ExecStatusCompleted, commandResult.ExecStatus)
			require.Equal(t, tc.wantOutcome, commandResult.Outcome)
			require.Equal(t, tc.wantExitCode, commandResult.ExitCode)
			if tc.wantExecError {
				require.Error(t, commandResult.ExecError)
			} else {
				require.NoError(t, commandResult.ExecError)
			}
			require.Equal(t, tc.wantSuccess, result.Success())

			for _, contains := range tc.wantOutputContains {
				require.Contains(t, commandResult.Output, contains)
			}
			for _, notContains := range tc.wantOutputNotContains {
				require.NotContains(t, commandResult.Output, notContains)
			}
		})
	}
}

func writeScenarioFile(t *testing.T, path, contents string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
}
