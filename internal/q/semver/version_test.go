package semver_test

import (
	"testing"

	"github.com/codalotl/codalotl/internal/q/semver"

	"github.com/stretchr/testify/require"
)

func TestCompare(t *testing.T) {
	tests := []struct {
		left     string
		right    string
		expected int
	}{
		{left: "1.0.0", right: "1.0.1", expected: -1},
		{left: "1.0.1", right: "1.0.0", expected: 1},
		{left: "1.0.0", right: "1.0.0", expected: 0},
		{left: "1.0.0-alpha", right: "1.0.0", expected: -1},
		{left: "1.0.0-alpha", right: "1.0.0-alpha.1", expected: -1},
		{left: "1.0.0-alpha.1", right: "1.0.0-alpha.beta", expected: -1},
		{left: "1.0.0-alpha.beta", right: "1.0.0-beta", expected: -1},
		{left: "1.0.0-beta", right: "1.0.0-beta.2", expected: -1},
		{left: "1.0.0-beta.2", right: "1.0.0-beta.11", expected: -1},
		{left: "1.0.0-beta.11", right: "1.0.0-rc.1", expected: -1},
		{left: "1.0.0-rc.1", right: "1.0.0", expected: -1},
	}

	for _, tc := range tests {
		a, err := semver.ParseStrict(tc.left)
		require.NoError(t, err)
		b, err := semver.ParseStrict(tc.right)
		require.NoError(t, err)
		require.Equal(t, tc.expected, semver.Compare(a, b))
		require.Equal(t, -tc.expected, semver.Compare(b, a))
	}
}

func TestOrderingHelpersAndMethods(t *testing.T) {
	a, err := semver.ParseStrict("1.0.1")
	require.NoError(t, err)
	b, err := semver.ParseStrict("1.0.0")
	require.NoError(t, err)
	pre, err := semver.ParseStrict("1.0.0-alpha")
	require.NoError(t, err)

	// Top-level helpers
	require.True(t, semver.GreaterThan(a, b))
	require.False(t, semver.GreaterThan(b, a))
	require.True(t, semver.LessThan(pre, b))
	require.False(t, semver.LessThan(b, pre))

	// Version methods
	require.True(t, a.GreaterThan(b))
	require.True(t, b.LessThan(a))
	require.Equal(t, 1, a.Compare(b))
	require.Equal(t, -1, b.Compare(a))
}

func TestEqual(t *testing.T) {
	a, err := semver.ParseStrict("1.0.0-alpha+build.1")
	require.NoError(t, err)
	b, err := semver.ParseStrict("1.0.0-alpha+build.1")
	require.NoError(t, err)
	require.True(t, a.Equal(b))
	require.True(t, b.Equal(a))

	c, err := semver.ParseStrict("1.0.0-alpha+build.2")
	require.NoError(t, err)
	require.False(t, a.Equal(c))
}

func TestCompatible(t *testing.T) {
	tests := []struct {
		left     string
		right    string
		expected bool
	}{
		{left: "1.2.3", right: "1.5.0", expected: true},
		{left: "1.2.3", right: "2.0.0", expected: false},
		{left: "0.5.0", right: "0.5.1", expected: true},
		{left: "0.5.0", right: "0.6.0", expected: false},
		{left: "0.5.0", right: "1.0.0", expected: false},
	}

	for _, tc := range tests {
		a, err := semver.ParseStrict(tc.left)
		require.NoError(t, err)
		b, err := semver.ParseStrict(tc.right)
		require.NoError(t, err)
		require.Equal(t, tc.expected, semver.Compatible(a, b))
		require.Equal(t, tc.expected, b.CompatibleWith(a))
	}
}
