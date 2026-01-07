package tui

import (
	"testing"

	"github.com/codalotl/codalotl/internal/llmmodel"
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

func TestNewSession_ModelSelection_DefaultWhenUnset(t *testing.T) {
	s, err := newSession(sessionConfig{})
	require.NoError(t, err)
	t.Cleanup(s.Close)
	require.Equal(t, defaultModelID, s.modelID)
}

func TestNewSession_ModelSelection_UsesProvidedModelID(t *testing.T) {
	customID := llmmodel.ModelID("test-session-model-id")
	err := llmmodel.AddCustomModel(customID, llmmodel.ProviderIDOpenAI, string(llmmodel.DefaultModel), llmmodel.ModelOverrides{})
	require.NoError(t, err)

	s, err := newSession(sessionConfig{modelID: customID})
	require.NoError(t, err)
	t.Cleanup(s.Close)
	require.Equal(t, customID, s.modelID)
}
