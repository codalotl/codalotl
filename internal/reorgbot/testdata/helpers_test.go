package mypkg

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeName(t *testing.T) {
	require.Equal(t, "Hello World", NormalizeName("  hello   world  "))
	require.Equal(t, "A", NormalizeName("a"))
	require.Equal(t, "", NormalizeName("   "))
}

func TestClamp(t *testing.T) {
	require.Equal(t, 5, Clamp(5, 1, 10))
	require.Equal(t, 1, Clamp(-100, 1, 10))
	require.Equal(t, 10, Clamp(100, 1, 10))
	// reversed min/max should still work
	require.Equal(t, 7, Clamp(7, 10, 1))
}
