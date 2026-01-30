package modellist

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetProviders(t *testing.T) {
	providers := GetProviders()
	require.NotEmpty(t, providers)

	// Make sure openAI is there with sensible basic data:
	found := false
	for _, p := range providers {
		if p.ID == "openai" {
			found = true

			assert.Equal(t, "https://api.openai.com/v1", p.APIEndpointURL)
			assert.True(t, len(p.Models) > 0)
		}
	}
	require.True(t, found, "could not find openai")

	// Ensure cache works and returns the same slice reference
	providers2 := GetProviders()
	require.Equal(t, len(providers), len(providers2))
	require.True(t, &providers[0] == &providers2[0])
}

func TestGetProviderNames(t *testing.T) {
	got := GetProviderNames()
	require.NotEmpty(t, got)

	// Ensure names are unique and match the embedded configs order (best-effort)
	seen := make(map[string]struct{}, len(got))
	for _, name := range got {
		require.NotEmpty(t, name)
		if _, ok := seen[name]; ok {
			require.Fail(t, "duplicate provider name: %s", name)
		}
		seen[name] = struct{}{}
	}
}
