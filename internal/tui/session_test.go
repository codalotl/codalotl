package tui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewSessionRecreatesAuthorizationObject(t *testing.T) {
	s1, err := newSession(sessionConfig{})
	require.NoError(t, err)
	t.Cleanup(s1.Close)

	s2, err := newSession(sessionConfig{})
	require.NoError(t, err)
	t.Cleanup(s2.Close)

	// authdomain constructors return a new user request channel per authorizer; if we ever
	// accidentally reuse the authorizer across sessions, this is the first thing that breaks.
	require.NotNil(t, s1.UserRequests())
	require.NotNil(t, s2.UserRequests())
	require.NotEqual(t, s1.UserRequests(), s2.UserRequests())
}
