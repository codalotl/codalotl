package authdomain

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShellAllowedCommandsCheck(t *testing.T) {
	t.Parallel()

	absolute := "/usr/bin/rm"
	if runtime.GOOS == "windows" {
		absolute = `C:\Windows\System32\cmd.exe`
	}
	parentEscape := filepath.Join("..", "..", "bin", "rm")

	goTestMatcher := CommandMatcher{Command: "go", CommandArgsPrefix: []string{"test"}}

	setupSafeGo := func(s *ShellAllowedCommands) {
		s.AddSafe(goTestMatcher)
	}

	setupSafePrecedence := func(s *ShellAllowedCommands) {
		setupSafeGo(s)
		s.AddBlocked(goTestMatcher)
	}

	setupBlockedRm := func(s *ShellAllowedCommands) {
		s.AddBlocked(CommandMatcher{Command: "rm"})
	}

	setupDangerousNpm := func(s *ShellAllowedCommands) {
		s.AddDangerous(CommandMatcher{
			Command:           "npm",
			CommandArgsPrefix: []string{"install"},
			Flags:             []string{"--global"},
		})
	}

	setupDangerousPnpm := func(s *ShellAllowedCommands) {
		s.AddDangerous(CommandMatcher{
			Command:           "pnpm",
			CommandArgsPrefix: []string{"add"},
			Flags:             []string{"--global", "--recursive"},
		})
	}

	setupSafeDangerous := func(s *ShellAllowedCommands) {
		s.AddSafe(goTestMatcher)
		s.AddDangerous(goTestMatcher)
	}

	setupBlockedPython := func(s *ShellAllowedCommands) {
		s.AddBlocked(CommandMatcher{Command: "python", CommandArgsPrefix: []string{"script.py"}})
	}

	tests := []struct {
		name       string
		setup      func(*ShellAllowedCommands)
		args       []string
		wantResult CommandCheckResult
		wantErr    error
	}{
		{
			name:    "RejectsEmptyCommandNil",
			args:    nil,
			wantErr: ErrEmptyCommand,
		},
		{
			name:    "RejectsEmptyCommandSlice",
			args:    []string{},
			wantErr: ErrEmptyCommand,
		},
		{
			name:       "SafePrecedence",
			setup:      setupSafePrecedence,
			args:       []string{"go", "test", "./..."},
			wantResult: CommandCheckResultSafe,
		},
		{
			name:       "BlockedCommand",
			setup:      setupBlockedRm,
			args:       []string{"rm"},
			wantResult: CommandCheckResultBlocked,
		},
		{
			name:       "DangerousMatcherWithSpaceSeparatedFlag",
			setup:      setupDangerousNpm,
			args:       []string{"npm", "install", "--global", "typescript"},
			wantResult: CommandCheckResultDangerous,
		},
		{
			name:       "DangerousMatcherWithEqualsFlag",
			setup:      setupDangerousNpm,
			args:       []string{"npm", "install", "--global=typescript"},
			wantResult: CommandCheckResultDangerous,
		},
		{
			name:       "ShellWrapperSafeZsh",
			setup:      setupSafeGo,
			args:       []string{"zsh", "-lc", "go test ./..."},
			wantResult: CommandCheckResultSafe,
		},
		{
			name:       "ShellWrapperSafeBashLogin",
			setup:      setupSafeGo,
			args:       []string{"bash", "--login", "-c", "go test ./..."},
			wantResult: CommandCheckResultSafe,
		},
		{
			name:       "ShellWrapperDangerous",
			setup:      setupDangerousNpm,
			args:       []string{"bash", "-c", "npm install --global typescript"},
			wantResult: CommandCheckResultDangerous,
		},
		{
			name:       "SafeOverridesDangerous",
			setup:      setupSafeDangerous,
			args:       []string{"go", "test", "./..."},
			wantResult: CommandCheckResultSafe,
		},
		{
			name:       "InscrutablePipeline",
			args:       []string{"ls", "|", "grep"},
			wantResult: CommandCheckResultInscrutable,
		},
		{
			name:       "InscrutableXargsRm",
			args:       []string{"xargs", "rm"},
			wantResult: CommandCheckResultInscrutable,
		},
		{
			name:       "ShellWrapperInscrutableQuotes",
			args:       []string{"zsh", "-lc", "echo 'hello world'"},
			wantResult: CommandCheckResultInscrutable,
		},
		{
			name:       "ShellWrapperInscrutableExtraArgs",
			args:       []string{"sh", "-c", "go test ./...", "arg0"},
			wantResult: CommandCheckResultInscrutable,
		},
		{
			name:    "ShellWrapperEmptyCommandString",
			args:    []string{"bash", "-c", "   "},
			wantErr: ErrEmptyCommand,
		},
		{
			name:    "ShellWrapperMissingCommandString",
			args:    []string{"bash", "-c"},
			wantErr: ErrEmptyCommand,
		},
		{
			name:       "InscrutableCommandSubstitution",
			args:       []string{"echo", "$(whoami)"},
			wantResult: CommandCheckResultInscrutable,
		},
		{
			name:       "DangerousDueToAbsolutePath",
			args:       []string{absolute},
			wantResult: CommandCheckResultDangerous,
		},
		{
			name:       "DangerousDueToParentEscape",
			args:       []string{parentEscape},
			wantResult: CommandCheckResultDangerous,
		},
		{
			name:       "DangerousDueToDotDotCommand",
			args:       []string{".."},
			wantResult: CommandCheckResultDangerous,
		},
		{
			name:       "NoneWhenNoMatch",
			args:       []string{"echo", "hello"},
			wantResult: CommandCheckResultNone,
		},
		{
			name:       "DangerousWhenAllFlagsMatch",
			setup:      setupDangerousPnpm,
			args:       []string{"pnpm", "add", "--global", "--recursive", "lodash"},
			wantResult: CommandCheckResultDangerous,
		},
		{
			name:       "NoneWhenMissingRequiredFlag",
			setup:      setupDangerousPnpm,
			args:       []string{"pnpm", "add", "--global", "lodash"},
			wantResult: CommandCheckResultNone,
		},
		{
			name:       "BlockedCommandDoesNotTriggerOutsideSandboxHeuristic",
			setup:      setupBlockedPython,
			args:       []string{"python", "script.py"},
			wantResult: CommandCheckResultBlocked,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := &ShellAllowedCommands{}
			if tt.setup != nil {
				tt.setup(s)
			}

			result, err := s.Check(tt.args)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.wantResult, result)
		})
	}
}
