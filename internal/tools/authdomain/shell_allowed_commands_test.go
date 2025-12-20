package authdomain

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewShellAllowedCommandsPopulatesDefaults(t *testing.T) {
	t.Parallel()

	s := NewShellAllowedCommands()

	blocked := s.BlockedCommandMatchers()
	dangerous := s.DangerousCommandMatchers()
	safe := s.SafeCommandMatchers()

	expectedBlocked := cloneMatchers(defaultBlockedCommandMatchers)
	sort.Slice(expectedBlocked, func(i, j int) bool {
		return matcherLess(expectedBlocked[i], expectedBlocked[j])
	})
	expectedDangerous := cloneMatchers(defaultDangerousCommandMatchers)
	sort.Slice(expectedDangerous, func(i, j int) bool {
		return matcherLess(expectedDangerous[i], expectedDangerous[j])
	})
	expectedSafe := cloneMatchers(defaultSafeCommandMatchers)
	sort.Slice(expectedSafe, func(i, j int) bool {
		return matcherLess(expectedSafe[i], expectedSafe[j])
	})

	require.Equal(t, expectedBlocked, blocked)
	require.Equal(t, expectedDangerous, dangerous)
	require.Equal(t, expectedSafe, safe)
}

func TestDefaultBlockedCommandMatchers(t *testing.T) {
	t.Parallel()

	s := &ShellAllowedCommands{}
	got := s.DefaultBlockedCommandMatchers()
	want := cloneMatchers(defaultBlockedCommandMatchers)
	require.Equal(t, want, got)

	// ensure callers receive an isolated copy
	if len(got) > 0 {
		original := got[0].Command
		got[0].Command = "mutated"
		fresh := s.DefaultBlockedCommandMatchers()
		require.Equal(t, original, fresh[0].Command)
	}
}

func TestDefaultDangerousCommandMatchers(t *testing.T) {
	t.Parallel()

	s := &ShellAllowedCommands{}
	got := s.DefaultDangerousCommandMatchers()
	want := cloneMatchers(defaultDangerousCommandMatchers)
	require.Equal(t, want, got)
}

func TestDefaultSafeCommandMatchers(t *testing.T) {
	t.Parallel()

	s := &ShellAllowedCommands{}
	got := s.DefaultSafeCommandMatchers()
	want := cloneMatchers(defaultSafeCommandMatchers)
	require.Equal(t, want, got)
}

func TestAddAndRemoveBlocked(t *testing.T) {
	t.Parallel()

	s := &ShellAllowedCommands{}
	matcher := CommandMatcher{Command: "go", CommandArgsPrefix: []string{"build"}}

	s.AddBlocked(matcher)
	require.Equal(t, []CommandMatcher{matcher}, s.BlockedCommandMatchers())

	// no-op on duplicate
	s.AddBlocked(matcher)
	require.Equal(t, []CommandMatcher{matcher}, s.BlockedCommandMatchers())

	require.NoError(t, s.RemoveBlocked(matcher))
	require.Empty(t, s.BlockedCommandMatchers())

	require.ErrorIs(t, s.RemoveBlocked(matcher), ErrCommandMatcherNotFound)
}

func TestAddAndRemoveDangerous(t *testing.T) {
	t.Parallel()

	s := &ShellAllowedCommands{}
	matcher := CommandMatcher{Command: "go", CommandArgsPrefix: []string{"run"}}

	s.AddDangerous(matcher)
	require.Equal(t, []CommandMatcher{matcher}, s.DangerousCommandMatchers())

	s.AddDangerous(matcher)
	require.Equal(t, []CommandMatcher{matcher}, s.DangerousCommandMatchers())

	require.NoError(t, s.RemoveDangerous(matcher))
	require.Empty(t, s.DangerousCommandMatchers())

	require.ErrorIs(t, s.RemoveDangerous(matcher), ErrCommandMatcherNotFound)
}

func TestAddAndRemoveSafe(t *testing.T) {
	t.Parallel()

	s := &ShellAllowedCommands{}
	matcher := CommandMatcher{Command: "go", CommandArgsPrefix: []string{"fmt"}}

	s.AddSafe(matcher)
	require.Equal(t, []CommandMatcher{matcher}, s.SafeCommandMatchers())

	s.AddSafe(matcher)
	require.Equal(t, []CommandMatcher{matcher}, s.SafeCommandMatchers())

	require.NoError(t, s.RemoveSafe(matcher))
	require.Empty(t, s.SafeCommandMatchers())

	require.ErrorIs(t, s.RemoveSafe(matcher), ErrCommandMatcherNotFound)
}

func TestBlockedCommands(t *testing.T) {
	t.Parallel()

	s := &ShellAllowedCommands{}
	s.AddBlocked(CommandMatcher{Command: "go"})
	s.AddBlocked(CommandMatcher{Command: "go", CommandArgsPrefix: []string{"build"}}) // should be ignored
	s.AddBlocked(CommandMatcher{Command: "npm"})

	got := s.BlockedCommands()
	want := []string{"go", "npm"}
	sort.Strings(want)

	require.Equal(t, want, got)
}

func TestCloneHelpersIsolated(t *testing.T) {
	t.Parallel()

	original := []CommandMatcher{{Command: "go"}, {Command: "npm", Flags: []string{"--global"}}}
	cloned := cloneMatchers(original)

	cloned[0].Command = "mutated"
	cloned[1].Flags[0] = "changed"

	require.Equal(t, "go", original[0].Command)
	require.Equal(t, "--global", original[1].Flags[0])
}

func TestMatcherKeyUniqueness(t *testing.T) {
	t.Parallel()

	m1 := CommandMatcher{Command: "go", CommandArgsPrefix: []string{"test"}, Flags: []string{"-run"}}
	m2 := CommandMatcher{Command: "go", CommandArgsPrefix: []string{"test"}, Flags: []string{"-run"}}
	m3 := CommandMatcher{Command: "go", CommandArgsPrefix: []string{"test"}, Flags: []string{"-timeout"}}

	key1 := matcherKey(m1)
	key2 := matcherKey(m2)
	key3 := matcherKey(m3)

	require.Equal(t, key1, key2)
	require.NotEqual(t, key1, key3)
}

func TestRemoveFromEmptyCollections(t *testing.T) {
	t.Parallel()

	s := &ShellAllowedCommands{}
	matcher := CommandMatcher{Command: "go"}

	require.ErrorIs(t, s.RemoveBlocked(matcher), ErrCommandMatcherNotFound)
	require.ErrorIs(t, s.RemoveDangerous(matcher), ErrCommandMatcherNotFound)
	require.ErrorIs(t, s.RemoveSafe(matcher), ErrCommandMatcherNotFound)
}
