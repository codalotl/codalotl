package authdomain

import (
	"errors"
	"sort"
	"strings"
	"sync"
)

// CommandCheckResult captures how a command matched against the configured allow/deny lists.
type CommandCheckResult int

const (
	CommandCheckResultNone        CommandCheckResult = iota // CommandCheckResultNone indicates the command did not match any configured matcher.
	CommandCheckResultSafe                                  // CommandCheckResultSafe indicates the command matched the safe list.
	CommandCheckResultBlocked                               // CommandCheckResultBlocked indicates the command matched the blocked list.
	CommandCheckResultDangerous                             // CommandCheckResultDangerous indicates the command matched the dangerous list.
	CommandCheckResultInscrutable                           // CommandCheckResultInscrutable indicates the command could not be reasoned about lexically.
)

// ErrCommandMatcherNotFound is returned when attempting to remove a matcher that is not registered.
var ErrCommandMatcherNotFound = errors.New("command matcher not found")

// CommandMatcher describes how to identify a command invocation.
type CommandMatcher struct {
	// Command is the executable (argv[0]) to match. Example: "go".
	Command string

	// CommandArgsPrefix matches the subsequent arguments (argv[1:]) exactly up to len(CommandArgsPrefix). Example: []string{"test"} matches `go test ./...`, but not
	// `go help test`.
	CommandArgsPrefix []string

	// Flags matches any argument that equals one of the listed flags, supports "--flag=value" forms.
	Flags []string
}

// ShellAllowedCommands keeps track of blocked, dangerous, and safe shell commands. All methods are safe for concurrent use.
type ShellAllowedCommands struct {
	mu        sync.RWMutex
	blocked   map[string]CommandMatcher
	dangerous map[string]CommandMatcher
	safe      map[string]CommandMatcher
}

// NewShellAllowedCommands constructs a ShellAllowedCommands pre-populated with the default blocked, dangerous, and safe command matchers.
func NewShellAllowedCommands() *ShellAllowedCommands {
	s := &ShellAllowedCommands{
		blocked:   make(map[string]CommandMatcher, len(defaultBlockedCommandMatchers)),
		dangerous: make(map[string]CommandMatcher, len(defaultDangerousCommandMatchers)),
		safe:      make(map[string]CommandMatcher, len(defaultSafeCommandMatchers)),
	}

	for _, matcher := range defaultBlockedCommandMatchers {
		key := matcherKey(matcher)
		s.blocked[key] = cloneMatcher(matcher)
	}
	for _, matcher := range defaultDangerousCommandMatchers {
		key := matcherKey(matcher)
		s.dangerous[key] = cloneMatcher(matcher)
	}
	for _, matcher := range defaultSafeCommandMatchers {
		key := matcherKey(matcher)
		s.safe[key] = cloneMatcher(matcher)
	}

	return s
}

var defaultBlockedCommandMatchers = func() []CommandMatcher {
	commands := []string{
		// Network/Download tools
		"alias",
		"aria2c",
		"axel",
		"chrome",
		"curl",
		"curlie",
		"firefox",
		"http-prompt",
		"httpie",
		"links",
		"lynx",
		"nc",
		"safari",
		"scp",
		"ssh",
		"telnet",
		"w3m",
		"wget",
		"xh",

		// System administration
		"doas",
		"su",
		"sudo",

		// Package managers
		"apk",
		"apt",
		"apt-cache",
		"apt-get",
		"brew",
		"dnf",
		"dpkg",
		"emerge",
		"home-manager",
		"makepkg",
		"opkg",
		"pacman",
		"paru",
		"pkg",
		"pkg_add",
		"pkg_delete",
		"portage",
		"rpm",
		"yay",
		"yum",
		"zypper",

		// System modification
		"at",
		"batch",
		"chkconfig",
		"crontab",
		"diskutil",
		"fdisk",
		"halt",
		"mkfs",
		"mount",
		"parted",
		"poweroff",
		"reboot",
		"service",
		"shutdown",
		"systemctl",
		"umount",

		// Network configuration
		"firewall-cmd",
		"ifconfig",
		"ip",
		"iptables",
		"netstat",
		"pfctl",
		"route",
		"ufw",
	}

	seen := make(map[string]struct{}, len(commands))
	matchers := make([]CommandMatcher, 0, len(commands))
	for _, cmd := range commands {
		if _, ok := seen[cmd]; ok {
			continue
		}
		seen[cmd] = struct{}{}
		matchers = append(matchers, CommandMatcher{Command: cmd})
	}

	sort.Slice(matchers, func(i, j int) bool {
		return matchers[i].Command < matchers[j].Command
	})
	return matchers
}()

var defaultDangerousCommandMatchers = []CommandMatcher{
	{Command: "git", CommandArgsPrefix: []string{"push"}},
	{Command: "git", CommandArgsPrefix: []string{"pull"}},
	{Command: "git", CommandArgsPrefix: []string{"fetch"}},
	{Command: "git", CommandArgsPrefix: []string{"commit"}},
	{Command: "git", CommandArgsPrefix: []string{"checkout"}},
	{Command: "git", CommandArgsPrefix: []string{"reset"}},
	{Command: "git", CommandArgsPrefix: []string{"rm"}},
	{Command: "rm", CommandArgsPrefix: []string{"-f"}},
	{Command: "rm", CommandArgsPrefix: []string{"-rf"}},
	{Command: "docker"},
	{Command: "kubectl"},
	{Command: "cargo", CommandArgsPrefix: []string{"install"}},
	{Command: "gem", CommandArgsPrefix: []string{"install"}},
	{Command: "go", CommandArgsPrefix: []string{"install"}},
	{Command: "npm", CommandArgsPrefix: []string{"install"}, Flags: []string{"--global"}},
	{Command: "npm", CommandArgsPrefix: []string{"install"}, Flags: []string{"-g"}},
	{Command: "pip", CommandArgsPrefix: []string{"install"}, Flags: []string{"--user"}},
	{Command: "pip3", CommandArgsPrefix: []string{"install"}, Flags: []string{"--user"}},
	{Command: "pnpm", CommandArgsPrefix: []string{"add"}, Flags: []string{"--global"}},
	{Command: "pnpm", CommandArgsPrefix: []string{"add"}, Flags: []string{"-g"}},
	{Command: "yarn", CommandArgsPrefix: []string{"global", "add"}},
}

var defaultSafeCommandMatchers = []CommandMatcher{
	{Command: "cargo", CommandArgsPrefix: []string{"check"}},
	{Command: "cd"},
	{Command: "echo"},
	{Command: "false"},
	{Command: "git", CommandArgsPrefix: []string{"branch"}},
	{Command: "git", CommandArgsPrefix: []string{"log"}},
	{Command: "git", CommandArgsPrefix: []string{"show"}},
	{Command: "grep"},
	{Command: "find"},
	{Command: "head"},
	{Command: "ls"},
	{Command: "nl"},
	{Command: "pwd"},
	{Command: "cat"},
	{Command: "git", CommandArgsPrefix: []string{"status"}},
	{Command: "git", CommandArgsPrefix: []string{"diff"}},
	{Command: "go", CommandArgsPrefix: []string{"test"}},
	{Command: "tail"},
	{Command: "true"},
	{Command: "wc"},
	{Command: "which"},
}

// DefaultBlockedCommandMatchers returns a copy of the built-in blocked matchers.
func (s *ShellAllowedCommands) DefaultBlockedCommandMatchers() []CommandMatcher {
	return cloneMatchers(defaultBlockedCommandMatchers)
}

// DefaultDangerousCommandMatchers returns a copy of the built-in dangerous matchers.
func (s *ShellAllowedCommands) DefaultDangerousCommandMatchers() []CommandMatcher {
	return cloneMatchers(defaultDangerousCommandMatchers)
}

// DefaultSafeCommandMatchers returns a copy of the built-in safe matchers.
func (s *ShellAllowedCommands) DefaultSafeCommandMatchers() []CommandMatcher {
	return cloneMatchers(defaultSafeCommandMatchers)
}

// BlockedCommandMatchers returns the currently registered blocked matchers.
func (s *ShellAllowedCommands) BlockedCommandMatchers() []CommandMatcher {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return cloneAndSortMatchers(s.blocked)
}

// DangerousCommandMatchers returns the currently registered dangerous matchers.
func (s *ShellAllowedCommands) DangerousCommandMatchers() []CommandMatcher {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return cloneAndSortMatchers(s.dangerous)
}

// SafeCommandMatchers returns the currently registered safe matchers.
func (s *ShellAllowedCommands) SafeCommandMatchers() []CommandMatcher {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return cloneAndSortMatchers(s.safe)
}

// BlockedCommands returns simple command names that are entirely blocked regardless of arguments.
func (s *ShellAllowedCommands) BlockedCommands() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.blocked) == 0 {
		return nil
	}

	commands := make([]string, 0, len(s.blocked))
	seen := make(map[string]struct{}, len(s.blocked))
	for _, matcher := range s.blocked {
		if len(matcher.CommandArgsPrefix) != 0 || len(matcher.Flags) != 0 {
			continue
		}
		if _, ok := seen[matcher.Command]; ok {
			continue
		}
		seen[matcher.Command] = struct{}{}
		commands = append(commands, matcher.Command)
	}

	sort.Strings(commands)
	return commands
}

// AddBlocked adds a matcher to the blocked set.
func (s *ShellAllowedCommands) AddBlocked(m CommandMatcher) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ensureMapsLocked()
	key := matcherKey(m)
	if _, ok := s.blocked[key]; ok {
		return
	}
	s.blocked[key] = cloneMatcher(m)
}

// RemoveBlocked removes a matcher from the blocked set.
func (s *ShellAllowedCommands) RemoveBlocked(m CommandMatcher) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.blocked) == 0 {
		return ErrCommandMatcherNotFound
	}

	key := matcherKey(m)
	if _, ok := s.blocked[key]; !ok {
		return ErrCommandMatcherNotFound
	}
	delete(s.blocked, key)
	return nil
}

// AddDangerous adds a matcher to the dangerous set.
func (s *ShellAllowedCommands) AddDangerous(m CommandMatcher) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ensureMapsLocked()
	key := matcherKey(m)
	if _, ok := s.dangerous[key]; ok {
		return
	}
	s.dangerous[key] = cloneMatcher(m)
}

// RemoveDangerous removes a matcher from the dangerous set.
func (s *ShellAllowedCommands) RemoveDangerous(m CommandMatcher) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.dangerous) == 0 {
		return ErrCommandMatcherNotFound
	}

	key := matcherKey(m)
	if _, ok := s.dangerous[key]; !ok {
		return ErrCommandMatcherNotFound
	}
	delete(s.dangerous, key)
	return nil
}

// AddSafe adds a matcher to the safe set.
func (s *ShellAllowedCommands) AddSafe(m CommandMatcher) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ensureMapsLocked()
	key := matcherKey(m)
	if _, ok := s.safe[key]; ok {
		return
	}
	s.safe[key] = cloneMatcher(m)
}

// RemoveSafe removes a matcher from the safe set.
func (s *ShellAllowedCommands) RemoveSafe(m CommandMatcher) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.safe) == 0 {
		return ErrCommandMatcherNotFound
	}

	key := matcherKey(m)
	if _, ok := s.safe[key]; !ok {
		return ErrCommandMatcherNotFound
	}
	delete(s.safe, key)
	return nil
}

func (s *ShellAllowedCommands) ensureMapsLocked() {
	if s.blocked == nil {
		s.blocked = make(map[string]CommandMatcher)
	}
	if s.dangerous == nil {
		s.dangerous = make(map[string]CommandMatcher)
	}
	if s.safe == nil {
		s.safe = make(map[string]CommandMatcher)
	}
}

func cloneAndSortMatchers(m map[string]CommandMatcher) []CommandMatcher {
	if len(m) == 0 {
		return nil
	}
	matchers := make([]CommandMatcher, 0, len(m))
	for _, matcher := range m {
		matchers = append(matchers, cloneMatcher(matcher))
	}
	sort.Slice(matchers, func(i, j int) bool {
		return matcherLess(matchers[i], matchers[j])
	})
	return matchers
}

func cloneMatchers(matchers []CommandMatcher) []CommandMatcher {
	if len(matchers) == 0 {
		return nil
	}
	out := make([]CommandMatcher, len(matchers))
	for i, matcher := range matchers {
		out[i] = cloneMatcher(matcher)
	}
	return out
}

func cloneMatcher(m CommandMatcher) CommandMatcher {
	c := CommandMatcher{
		Command: m.Command,
	}
	if len(m.CommandArgsPrefix) > 0 {
		c.CommandArgsPrefix = append([]string(nil), m.CommandArgsPrefix...)
	}
	if len(m.Flags) > 0 {
		c.Flags = append([]string(nil), m.Flags...)
	}
	return c
}

func matcherKey(m CommandMatcher) string {
	var builder strings.Builder
	builder.WriteString(m.Command)
	builder.WriteByte('\x00')
	builder.WriteString(strings.Join(m.CommandArgsPrefix, "\x00"))
	builder.WriteByte('\x00')
	builder.WriteString(strings.Join(m.Flags, "\x00"))
	return builder.String()
}

func matcherLess(a, b CommandMatcher) bool {
	if a.Command != b.Command {
		return a.Command < b.Command
	}
	for i := 0; i < len(a.CommandArgsPrefix) && i < len(b.CommandArgsPrefix); i++ {
		if a.CommandArgsPrefix[i] != b.CommandArgsPrefix[i] {
			return a.CommandArgsPrefix[i] < b.CommandArgsPrefix[i]
		}
	}
	if len(a.CommandArgsPrefix) != len(b.CommandArgsPrefix) {
		return len(a.CommandArgsPrefix) < len(b.CommandArgsPrefix)
	}
	for i := 0; i < len(a.Flags) && i < len(b.Flags); i++ {
		if a.Flags[i] != b.Flags[i] {
			return a.Flags[i] < b.Flags[i]
		}
	}
	if len(a.Flags) != len(b.Flags) {
		return len(a.Flags) < len(b.Flags)
	}
	return false
}
