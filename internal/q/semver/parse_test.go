package semver_test

import (
	"errors"
	"testing"

	"github.com/codalotl/codalotl/internal/q/semver"

	"github.com/stretchr/testify/require"
)

func TestParseStrictValid(t *testing.T) {
	tests := []struct {
		name  string
		input string
		major uint64
		minor uint64
		patch uint64
		pre   []semver.Identifier
		build []string
	}{
		{
			name:  "basic",
			input: "1.2.3",
			major: 1,
			minor: 2,
			patch: 3,
		},
		{
			name:  "with prerelease and build",
			input: "1.2.3-alpha.1+build.5",
			major: 1,
			minor: 2,
			patch: 3,
			pre:   []semver.Identifier{{Value: "alpha"}, {Value: "1", Numeric: true, Number: 1}},
			build: []string{"build", "5"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v, err := semver.ParseStrict(tc.input)
			require.NoError(t, err)
			require.Equal(t, tc.major, v.Major)
			require.Equal(t, tc.minor, v.Minor)
			require.Equal(t, tc.patch, v.Patch)
			require.Equal(t, tc.pre, v.PreRelease())
			require.Equal(t, tc.build, v.Build())
			require.Equal(t, tc.input, v.String())
		})
	}
}

func TestParseStrictInvalid(t *testing.T) {
	cases := []string{
		"",
		"1",
		"v1",
		"1.2",
		"01.2.3",
		"1.02.3",
		"1.2.3-",
		"1.2.3+bad$",
	}

	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			_, err := semver.ParseStrict(input)
			require.Error(t, err)
			var pe *semver.ParseError
			require.True(t, errors.As(err, &pe))
		})
	}
}

func TestParseOverflow(t *testing.T) {
	_, err := semver.ParseStrict("18446744073709551616.0.0")
	require.Error(t, err)
	var pe *semver.ParseError
	require.True(t, errors.As(err, &pe))
	require.Contains(t, pe.Error(), "overflow")
}

func TestParseNonStrict(t *testing.T) {
	tests := []struct {
		input     string
		major     uint64
		minor     uint64
		patch     uint64
		pre       []string
		build     []string
		canonical string
	}{
		{input: "1", major: 1, minor: 0, patch: 0, canonical: "1.0.0"},
		{input: "v2", major: 2, minor: 0, patch: 0, canonical: "2.0.0"},
		{input: "V2", major: 2, minor: 0, patch: 0, canonical: "2.0.0"},
		{input: "v0.1", major: 0, minor: 1, patch: 0, canonical: "0.1.0"},
		{input: "v1.2", major: 1, minor: 2, patch: 0, canonical: "1.2.0"},
		{input: " 1.2.3 ", major: 1, minor: 2, patch: 3, canonical: "1.2.3"},
		{input: "V3.4.0-beta", major: 3, minor: 4, patch: 0, pre: []string{"beta"}, canonical: "3.4.0-beta"},
		{input: "0.0+build", major: 0, minor: 0, patch: 0, build: []string{"build"}, canonical: "0.0.0+build"},
	}

	for _, tc := range tests {
		v, err := semver.Parse(tc.input)
		require.NoError(t, err, tc.input)
		require.Equal(t, tc.major, v.Major)
		require.Equal(t, tc.minor, v.Minor)
		require.Equal(t, tc.patch, v.Patch)
		pre := v.PreRelease()
		if len(tc.pre) == 0 {
			require.Nil(t, pre)
		} else {
			require.Len(t, pre, len(tc.pre))
			for i, p := range pre {
				require.Equal(t, tc.pre[i], p.Value)
			}
		}
		if len(tc.build) == 0 {
			require.Nil(t, v.Build())
		} else {
			require.Equal(t, tc.build, v.Build())
		}
		require.Equal(t, tc.canonical, v.String())
	}
}

func TestNonStrictRejectsGarbage(t *testing.T) {
	cases := []string{
		"version1",
		"v1..2",
		"v1.2.3-beta$",
		"v.1.2",
	}

	for _, input := range cases {
		_, err := semver.Parse(input)
		require.Error(t, err, input)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "", want: "semver: empty input"},
		{input: "   ", want: "semver: empty input"},
		{input: ".", want: "semver: expected digit at position 0"},
		{input: "v", want: "semver: expected digit at position 1"},
		{input: "v.", want: "semver: expected digit at position 1"},
		{input: "01.2.3", want: "semver: numeric identifier with leading zero at position 0"},
		{input: "1.02.3", want: "semver: numeric identifier with leading zero at position 2"},
		{input: "1.2.3-", want: "semver: incomplete pre-release at position 6"},
		{input: "1.2.3-.", want: "semver: empty pre-release identifier at position 6"},
		{input: "1.2.3-01", want: "semver: numeric identifier with leading zero at position 6"},
		{input: "1.2.3-beta$", want: "semver: invalid character in pre-release at position 10"},
		{input: "1.2.3+", want: "semver: incomplete build metadata at position 6"},
		{input: "1.2.3+.", want: "semver: empty build identifier at position 6"},
		{input: "1.2.3+bad$", want: "semver: invalid character in build metadata at position 9"},
		{input: "1.2.3b", want: "semver: unexpected trailing data at position 5"},
		{input: "v1..2", want: "semver: expected digit at position 3"},
		{input: "version1", want: "semver: expected digit at position 1"},
		{input: "1.2.3-alpha.", want: "semver: incomplete pre-release at position 12"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			_, err := semver.Parse(tc.input)
			require.Error(t, err)
			require.Equal(t, tc.want, err.Error())
		})
	}
}

func TestParseErrorNilReceiver(t *testing.T) {
	var pe *semver.ParseError
	// Ensure calling Error on a nil receiver is safe and returns a sentinel string.
	require.Equal(t, "<nil>", pe.Error())
}
