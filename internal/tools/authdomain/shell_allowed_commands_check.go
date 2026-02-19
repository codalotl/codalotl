package authdomain

import (
	"errors"
	"path/filepath"
	"strings"
)

// ErrEmptyCommand is returned when Check is invoked with an empty argv.
var ErrEmptyCommand = errors.New("shell command argv is empty")

// Check checks argv lexically against the blocked/dangerous/safe commands. It returns a CommandCheckResult (eg, blocked, safe, dangerous, inscrutable, none), or
// an error.
//   - Precedence: safe > blocked > dangerous. So a match on the safe list overrules everything.
//   - This implies there's no super-clean way to specify "allow go commands, except for go install". You'd need to either explicitly enumerate safe go commands,
//     or not add go to the safe list.
//   - inscrutable is returned if we detect argv contains a set of pipes, subshells, xargs, or various other non-simple elements, which we don't support reasoning
//     about.
//   - a command is marked as dangerous if it does not match any list and argv[0] is a path-qualified command (absolute or uses ".."); this is treated as "outside
//     sandbox" heuristically.
//   - a scrutable command that is on no list is 'none'.
func (s *ShellAllowedCommands) Check(argv []string) (CommandCheckResult, error) {
	if len(argv) == 0 {
		return CommandCheckResultNone, ErrEmptyCommand
	}

	if unwrapped, handled, inscrutable := unwrapShellCommand(argv); handled {
		if inscrutable {
			return CommandCheckResultInscrutable, nil
		}
		argv = unwrapped
		if len(argv) == 0 {
			return CommandCheckResultNone, ErrEmptyCommand
		}
	}

	if isInscrutableCommand(argv) {
		return CommandCheckResultInscrutable, nil
	}

	s.mu.RLock()
	safeMatch := matchAny(argv, s.safe)
	blockedMatch := matchAny(argv, s.blocked)
	dangerousMatch := matchAny(argv, s.dangerous)
	s.mu.RUnlock()

	switch {
	case safeMatch:
		return CommandCheckResultSafe, nil
	case blockedMatch:
		return CommandCheckResultBlocked, nil
	case dangerousMatch:
		return CommandCheckResultDangerous, nil
	}

	if isOutsideSandboxCommand(argv[0]) {
		return CommandCheckResultDangerous, nil
	}

	return CommandCheckResultNone, nil
}

func matchAny(argv []string, matchers map[string]CommandMatcher) bool {
	if len(argv) == 0 || len(matchers) == 0 {
		return false
	}
	for _, matcher := range matchers {
		if commandMatches(matcher, argv) {
			return true
		}
	}
	return false
}

func commandMatches(m CommandMatcher, argv []string) bool {
	if len(argv) == 0 || m.Command != argv[0] {
		return false
	}

	if len(m.CommandArgsPrefix) > len(argv)-1 {
		return false
	}
	for i, arg := range m.CommandArgsPrefix {
		if argv[i+1] != arg {
			return false
		}
	}

	if len(m.Flags) == 0 {
		return true
	}

	args := argv[1:]
	for _, flag := range m.Flags {
		if !flagPresent(args, flag) {
			return false
		}
	}
	return true
}

func flagPresent(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag {
			return true
		}
		if strings.HasPrefix(arg, flag) {
			rest := arg[len(flag):]
			if strings.HasPrefix(rest, "=") || strings.HasPrefix(rest, " ") {
				return true
			}
		}
	}
	return false
}

func isInscrutableCommand(argv []string) bool {
	inscrutableTokens := map[string]struct{}{
		"|":  {},
		"||": {},
		"&&": {},
		";":  {},
		"&":  {},
	}

	for _, arg := range argv {
		if arg == "" {
			continue
		}
		if _, ok := inscrutableTokens[arg]; ok {
			return true
		}
		if arg == "xargs" {
			return true
		}
		if strings.ContainsAny(arg, "|;&") {
			return true
		}
		if strings.Contains(arg, "$(") || strings.Contains(arg, "`") {
			return true
		}
		if strings.Contains(arg, "<(") || strings.Contains(arg, ">(") {
			return true
		}
	}
	return false
}

func isOutsideSandboxCommand(command string) bool {
	if command == "" {
		return false
	}
	if filepath.IsAbs(command) {
		return true
	}

	clean := filepath.Clean(command)
	if clean == ".." {
		return true
	}

	if strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return true
	}

	if strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "..\\") {
		return true
	}

	return false
}

func unwrapShellCommand(argv []string) ([]string, bool, bool) {
	if len(argv) < 2 {
		return nil, false, false
	}

	if !isShellWrapper(argv[0]) {
		return nil, false, false
	}

	commandIdx, handled, inscrutable := shellCommandStringIndex(argv)
	if !handled {
		return nil, false, false
	}
	if inscrutable {
		return nil, true, true
	}

	if commandIdx < len(argv)-1 {
		return nil, true, true
	}

	var commandString string
	if commandIdx < len(argv) {
		commandString = strings.TrimSpace(argv[commandIdx])
	}

	if commandString == "" {
		return []string{}, true, false
	}

	if strings.ContainsAny(commandString, "'\"\\") {
		return nil, true, true
	}
	if strings.ContainsAny(commandString, "\n\r\t") {
		return nil, true, true
	}

	fields := strings.Fields(commandString)
	return fields, true, false
}

func shellCommandStringIndex(argv []string) (int, bool, bool) {
	if len(argv) < 2 {
		return -1, false, false
	}

	i := 1
	for i < len(argv) {
		arg := argv[i]
		switch arg {
		case "-c", "-lc", "-cl":
			return i + 1, true, false
		case "-l", "--login":
			i++
			continue
		default:
			if strings.HasPrefix(arg, "-") {
				return -1, true, true
			}
			return -1, false, false
		}
	}

	return -1, false, false
}

func isShellWrapper(command string) bool {
	switch command {
	case "bash", "sh", "zsh":
		return true
	default:
		return false
	}
}
