package tui

import (
	"os"
	"testing"

	"github.com/codalotl/codalotl/internal/agent"
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

func TestNewSession_OrchestrateMode(t *testing.T) {
	s, err := newSession(sessionConfig{agentName: orchestrateAgentName})
	require.NoError(t, err)
	t.Cleanup(s.Close)

	require.True(t, s.config.orchestrateMode())
	require.False(t, s.config.packageMode())
}

func TestNewSession_AutoYesDisablesUserRequests(t *testing.T) {
	s, err := newSession(sessionConfig{autoYes: true})
	require.NoError(t, err)
	t.Cleanup(s.Close)

	require.Nil(t, s.UserRequests())
}

func TestNewSession_ZDREnvControlsNoStoreRootAgentCreator(t *testing.T) {
	oldCreator := newRootAgentCreator
	t.Cleanup(func() {
		newRootAgentCreator = oldCreator
	})

	for _, tc := range []struct {
		name        string
		envValue    *string
		wantOptions []agent.NewOptions
	}{
		{name: "unset"},
		{name: "false", envValue: ptr("false")},
		{name: "uppercase true", envValue: ptr("TRUE")},
		{name: "exact true", envValue: ptr("true"), wantOptions: []agent.NewOptions{{NoStore: true}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setCODALOTLZDR(t, tc.envValue)

			var gotOptions []agent.NewOptions
			newRootAgentCreator = func(options ...agent.NewOptions) agent.AgentCreator {
				gotOptions = append([]agent.NewOptions(nil), options...)
				return oldCreator(options...)
			}

			s, err := newSession(sessionConfig{})
			require.NoError(t, err)
			t.Cleanup(s.Close)

			require.Equal(t, tc.wantOptions, gotOptions)
		})
	}
}

func ptr[T any](v T) *T {
	return &v
}

func setCODALOTLZDR(t *testing.T, value *string) {
	t.Helper()

	oldValue, hadOldValue := os.LookupEnv("CODALOTL_ZDR")
	t.Cleanup(func() {
		if hadOldValue {
			require.NoError(t, os.Setenv("CODALOTL_ZDR", oldValue))
			return
		}
		require.NoError(t, os.Unsetenv("CODALOTL_ZDR"))
	})

	if value == nil {
		require.NoError(t, os.Unsetenv("CODALOTL_ZDR"))
		return
	}
	require.NoError(t, os.Setenv("CODALOTL_ZDR", *value))
}
