package cmdrunner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunnerRunValidation(t *testing.T) {
	t.Parallel()

	baseSchema := map[string]InputType{
		"path": InputTypePathDir,
		"flag": InputTypeBool,
		"name": InputTypeString,
	}
	required := []string{"path"}

	tests := []struct {
		name      string
		schema    map[string]InputType
		required  []string
		inputs    map[string]any
		setup     func(t *testing.T, root string)
		wantErr   string
		wantErrIs error
	}{
		{
			name:     "missing required input",
			schema:   baseSchema,
			required: required,
			inputs:   nil,
			wantErr:  `missing required input "path"`,
		},
		{
			name:     "unknown input key",
			schema:   baseSchema,
			required: required,
			inputs: map[string]any{
				"path":  "module",
				"flag":  true,
				"extra": "oops",
			},
			setup: func(t *testing.T, root string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(root, "module"), 0o755))
			},
			wantErr: `unknown input "extra"`,
		},
		{
			name:     "path type mismatch",
			schema:   baseSchema,
			required: required,
			inputs: map[string]any{
				"path": 123,
				"flag": true,
				"name": "demo",
			},
			wantErr: `input "path": expected string for path`,
		},
		{
			name:     "bool type mismatch",
			schema:   baseSchema,
			required: required,
			inputs: map[string]any{
				"path": "module",
				"flag": "true",
				"name": "demo",
			},
			setup: func(t *testing.T, root string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(root, "module"), 0o755))
			},
			wantErr: `input "flag": expected bool`,
		},
		{
			name:     "string type mismatch",
			schema:   baseSchema,
			required: required,
			inputs: map[string]any{
				"path": "module",
				"flag": true,
				"name": 42,
			},
			setup: func(t *testing.T, root string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(root, "module"), 0o755))
			},
			wantErr: `input "name": expected string`,
		},
		{
			name:     "path does not exist",
			schema:   baseSchema,
			required: required,
			inputs: map[string]any{
				"path": "missing",
				"flag": true,
				"name": "demo",
			},
			wantErr: `input "path": path does not exist`,
		},
		{
			name:     "path dir input allows file",
			schema:   baseSchema,
			required: required,
			inputs: map[string]any{
				"path": "module/file.txt",
				"flag": true,
				"name": "demo",
			},
			setup: func(t *testing.T, root string) {
				t.Helper()
				dir := filepath.Join(root, "module")
				require.NoError(t, os.MkdirAll(dir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("data"), 0o644))
			},
		},
		{
			name: "path unchecked allows missing",
			schema: map[string]InputType{
				"path": InputTypePathUnchecked,
			},
			required: []string{"path"},
			inputs: map[string]any{
				"path": "missing/path",
			},
		},
		{
			name:     "valid inputs pass validation",
			schema:   baseSchema,
			required: required,
			inputs: map[string]any{
				"path": "module",
				"flag": true,
				"name": "demo",
			},
			setup: func(t *testing.T, root string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(root, "module"), 0o755))
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			if tc.setup != nil {
				tc.setup(t, root)
			}

			runner := NewRunner(tc.schema, tc.required)
			result, err := runner.Run(context.Background(), root, tc.inputs)

			switch {
			case tc.wantErr != "":
				require.Error(t, err)
				require.Empty(t, result.Results)
				require.ErrorContains(t, err, tc.wantErr)
			case tc.wantErrIs != nil:
				require.ErrorIs(t, err, tc.wantErrIs)
				require.Empty(t, result.Results)
			default:
				require.NoError(t, err)
				require.Empty(t, result.Results)
			}
		})
	}
}

func TestRunnerRunPathDirInputFromFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	moduleDir := filepath.Join(root, "module")
	require.NoError(t, os.MkdirAll(moduleDir, 0o755))
	filePath := filepath.Join(moduleDir, "file.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("data"), 0o644))

	runner := NewRunner(map[string]InputType{
		"path": InputTypePathDir,
	}, []string{"path"})
	runner.AddCommand(Command{
		Command: "__missing_command__",
		Args: []string{
			"{{ .path }}",
		},
	})

	result, err := runner.Run(context.Background(), root, map[string]any{
		"path": filepath.Join("module", "file.txt"),
	})
	require.NoError(t, err)
	require.Len(t, result.Results, 1)

	commandResult := result.Results[0]
	require.Equal(t, "__missing_command__", commandResult.Command)
	require.Equal(t, []string{moduleDir}, commandResult.Args)
	require.Equal(t, ExecStatusFailedToStart, commandResult.ExecStatus)
	require.Error(t, commandResult.ExecError)
}

func TestRunnerRunTemplating(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	pkgRel := filepath.Join("module", "pkg")
	pkgAbs := filepath.Join(root, pkgRel)
	require.NoError(t, os.MkdirAll(pkgAbs, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgAbs, "main.go"), []byte("package pkg\n"), 0o644))
	moduleDir := filepath.Dir(pkgAbs)
	require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte("module example.com/test\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))

	schema := map[string]InputType{
		"binary":  InputTypeString,
		"pkg":     InputTypePathDir,
		"verbose": InputTypeBool,
		"pattern": InputTypeString,
	}
	required := []string{"binary", "pkg"}

	runner := NewRunner(schema, required)
	runner.AddCommand(Command{
		Command: "{{ .binary }}",
		Args: []string{
			"list",
			"{{ if .verbose }}-json{{ end }}",
			"{{ if ne .pattern \"\" }}-run={{ .pattern }}{{ end }}",
			"   ./...   ",
			"",
		},
		CWD: "{{ manifestDir .pkg }}",
	})
	runner.AddCommand(Command{
		Command: "{{ .binary }}",
		Args: []string{
			"env",
			"GOMOD",
		},
		CWD: "{{ repoDir .pkg }}",
	})
	runner.AddCommand(Command{
		Command: "__missing_command__",
		Args: []string{
			"{{ relativeTo .pkg (manifestDir .pkg) }}",
			"{{ manifestDir .pkg }}",
			"{{ repoDir .pkg }}",
		},
	})

	inputs := map[string]any{
		"binary":  "go",
		"pkg":     pkgRel,
		"verbose": true,
		"pattern": "",
	}

	result, err := runner.Run(context.Background(), root, inputs)
	require.NoError(t, err)
	require.Len(t, result.Results, 3)

	first := result.Results[0]
	require.Equal(t, "go", first.Command)
	require.Equal(t, []string{"list", "-json", "./..."}, first.Args)
	require.Equal(t, moduleDir, first.CWD)
	require.Equal(t, ExecStatusCompleted, first.ExecStatus)
	require.Equal(t, 0, first.ExitCode)
	require.Equal(t, OutcomeSuccess, first.Outcome)
	require.Contains(t, first.Output, "module/pkg")

	second := result.Results[1]
	require.Equal(t, "go", second.Command)
	require.Equal(t, []string{"env", "GOMOD"}, second.Args)
	require.Equal(t, filepath.Clean(root), second.CWD)
	require.Equal(t, ExecStatusCompleted, second.ExecStatus)
	require.Equal(t, 0, second.ExitCode)
	require.Equal(t, OutcomeSuccess, second.Outcome)
	require.Equal(t, os.DevNull, strings.TrimSpace(second.Output))

	third := result.Results[2]
	require.Equal(t, "__missing_command__", third.Command)
	require.Equal(t, []string{"pkg", moduleDir, root}, third.Args)
	require.Equal(t, filepath.Clean(root), third.CWD)
	require.Equal(t, ExecStatusFailedToStart, third.ExecStatus)
	require.Equal(t, -1, third.ExitCode)
	require.Equal(t, OutcomeFailed, third.Outcome)
	require.NotNil(t, third.ExecError)
}

func TestRunnerRunTemplatingMissingKey(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	runner := NewRunner(map[string]InputType{
		"path": InputTypePathDir,
	}, []string{"path"})
	runner.AddCommand(Command{
		Command: "{{ .missing }}",
	})

	require.NoError(t, os.MkdirAll(filepath.Join(root, "dir"), 0o755))

	result, err := runner.Run(context.Background(), root, map[string]any{
		"path": "dir",
	})
	require.Error(t, err)
	require.Empty(t, result.Results)
	require.ErrorContains(t, err, `command[0] command template`)
	require.ErrorContains(t, err, `missing`)
}

func TestRunnerRunExecutionNonZeroExit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	runner := NewRunner(map[string]InputType{
		"binary": InputTypeString,
	}, []string{"binary"})
	runner.AddCommand(Command{
		Command: "{{ .binary }}",
		Args:    []string{"tool", "doesnotexist"},
	})

	result, err := runner.Run(context.Background(), root, map[string]any{"binary": "go"})
	require.NoError(t, err)
	require.Len(t, result.Results, 1)

	cr := result.Results[0]
	require.Equal(t, ExecStatusCompleted, cr.ExecStatus)
	require.Greater(t, cr.ExitCode, 0)
	require.Equal(t, OutcomeFailed, cr.Outcome)
	require.Error(t, cr.ExecError)
	require.Contains(t, cr.Output, "no such tool")
}

func TestRunnerRunOutcomeFailIfAnyOutput(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	runner := NewRunner(map[string]InputType{
		"binary": InputTypeString,
	}, []string{"binary"})
	runner.AddCommand(Command{
		Command:                "{{ .binary }}",
		Args:                   []string{"env", "GOROOT"},
		OutcomeFailIfAnyOutput: true,
	})

	result, err := runner.Run(context.Background(), root, map[string]any{"binary": "go"})
	require.NoError(t, err)
	require.Len(t, result.Results, 1)

	cr := result.Results[0]
	require.Equal(t, ExecStatusCompleted, cr.ExecStatus)
	require.Equal(t, 0, cr.ExitCode)
	require.Equal(t, OutcomeFailed, cr.Outcome)
	require.NotEqual(t, "", strings.TrimSpace(cr.Output))
}

func TestRunnerRunContextCanceled(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runner := NewRunner(map[string]InputType{
		"binary": InputTypeString,
	}, []string{"binary"})
	runner.AddCommand(Command{
		Command: "{{ .binary }}",
		Args:    []string{"env", "GOROOT"},
	})

	result, err := runner.Run(ctx, root, map[string]any{"binary": "go"})
	require.NoError(t, err)
	require.Len(t, result.Results, 1)

	cr := result.Results[0]
	require.Equal(t, ExecStatusCanceled, cr.ExecStatus)
	require.Equal(t, OutcomeFailed, cr.Outcome)
	require.Equal(t, -1, cr.ExitCode)
	require.Error(t, cr.ExecError)
	require.True(t, errors.Is(cr.ExecError, context.Canceled))
}

func TestRunnerRunContextTimeout(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	command := "sleep"
	args := []string{"10"}
	if runtime.GOOS == "windows" {
		command = "powershell"
		args = []string{"-Command", "Start-Sleep -Seconds 10"}
	}

	schema := map[string]InputType{
		"binary": InputTypeString,
	}

	argTemplates := make([]string, len(args))
	for i := range args {
		key := fmt.Sprintf("arg%d", i)
		schema[key] = InputTypeString
		argTemplates[i] = fmt.Sprintf("{{ .%s }}", key)
	}

	runner := NewRunner(schema, []string{"binary"})
	runner.AddCommand(Command{
		Command: "{{ .binary }}",
		Args:    argTemplates,
	})

	inputs := map[string]any{
		"binary": command,
	}
	for i, arg := range args {
		inputs[fmt.Sprintf("arg%d", i)] = arg
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result, err := runner.Run(ctx, root, inputs)
	require.NoError(t, err)
	require.Len(t, result.Results, 1)

	cr := result.Results[0]
	require.Equal(t, ExecStatusTimedOut, cr.ExecStatus)
	require.Equal(t, OutcomeFailed, cr.Outcome)
	require.Error(t, cr.ExecError)
	require.True(t, errors.Is(cr.ExecError, context.DeadlineExceeded))
}

func TestRunnerRunCommandAttrsMustBeEvenLength(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	runner := NewRunner(nil, nil)
	runner.AddCommand(Command{
		Command: "echo",
		Args:    []string{"hello"},
		Attrs:   []string{"key-without-value"},
	})

	result, err := runner.Run(context.Background(), root, nil)
	require.Error(t, err)
	require.Empty(t, result.Results)
	require.ErrorContains(t, err, "attrs must have even length")
	require.ErrorContains(t, err, "command[0]")
}
