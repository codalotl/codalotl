package llmmodel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderSubscriptionMakesProviderModelsAvailable(t *testing.T) {
	for _, pid := range AllProviderIDs {
		ConfigureProviderKey(pid, "")
		ClearProviderSubscription(pid)
		if env := ProviderKeyEnvVars()[pid]; env != "" {
			t.Setenv(env, "")
		}
	}
	t.Cleanup(func() {
		for _, pid := range AllProviderIDs {
			ClearProviderSubscription(pid)
		}
	})

	before := AvailableModelIDsWithAPIKey()

	SetProviderSubscription(ProviderIDOpenAI, ProviderSubscription{
		AccessToken:    "access-token",
		APIEndpointURL: "https://chatgpt.com/backend-api/codex",
	})

	ids := AvailableModelIDsWithAPIKey()
	require.NotEmpty(t, ids)
	assert.Greater(t, len(ids), len(before))
	for _, id := range newModelIDs(ids, before) {
		assert.Equal(t, ProviderIDOpenAI, GetModelInfo(id).ProviderID)
	}

	ClearProviderSubscription(ProviderIDOpenAI)
	assert.ElementsMatch(t, before, AvailableModelIDsWithAPIKey())
}

func newModelIDs(after, before []ModelID) []ModelID {
	seen := make(map[ModelID]bool, len(before))
	for _, id := range before {
		seen[id] = true
	}
	var out []ModelID
	for _, id := range after {
		if !seen[id] {
			out = append(out, id)
		}
	}
	return out
}
