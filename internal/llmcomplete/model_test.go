package llmcomplete

import (
	"github.com/codalotl/codalotl/internal/llmcomplete/internal/modellist"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ModelID constants used in tests
const (
	ModelIDGPT5          ModelID = "gpt-5"
	ModelIDGPT5Nano      ModelID = "gpt-5-nano"
	ModelIDGPT41Nano     ModelID = "gpt-4.1-nano"
	ModelIDGrok4         ModelID = "grok-4"
	ModelIDGrok4Fast     ModelID = "grok-4-fast"
	ModelIDClaude35Haiku ModelID = "claude-3-5-haiku"
	ModelIDClaudeSonnet4 ModelID = "claude-sonnet-4-5"
	ModelIDO3            ModelID = "o3"
)

// withCustomModel adds a custom model, runs fn, then restores availableModels.
func withCustomModel(t *testing.T, id ModelID, providerID ProviderID, providerModelID string, overrides ModelOverrides, fn func()) {
	t.Helper()
	// Snapshot and restore global state for isolation
	saved := append([]model(nil), availableModels...)
	defer func() { availableModels = saved }()

	require.NoError(t, AddCustomModel(id, providerID, providerModelID, overrides))
	fn()
}

func TestProviders(t *testing.T) {
	// Sanity check: make sure some openai, anthropic, xai, and openrouter are in AllProviderIDs.
	assert.Contains(t, AllProvidersIDs, ProviderIDOpenAI, "openai should be in AllProvidersIDs")
	assert.Contains(t, AllProvidersIDs, ProviderIDAnthropic, "anthropic should be in AllProvidersIDs")
	assert.Contains(t, AllProvidersIDs, ProviderIDXAI, "xai should be in AllProvidersIDs")
	assert.Contains(t, AllProvidersIDs, ProviderIDOpenRouter, "openrouter should be in AllProvidersIDs")

	// Ensure all ids in AllProviderIDs are in ProviderKeyEnvVars, and then do sanity check to make sure we have expected values for our sanity test set.
	providerKeyEnvVars := ProviderKeyEnvVars()

	// All providers in AllProvidersIDs should have corresponding env vars
	for _, providerID := range AllProvidersIDs {
		assert.Contains(t, providerKeyEnvVars, providerID, "provider %s should have an env var mapping", providerID)
	}

	// Sanity check that we have expected values for our sanity test set
	assert.Equal(t, "OPENAI_API_KEY", providerKeyEnvVars[ProviderIDOpenAI])
	assert.Equal(t, "ANTHROPIC_API_KEY", providerKeyEnvVars[ProviderIDAnthropic])
	assert.Equal(t, "XAI_API_KEY", providerKeyEnvVars[ProviderIDXAI])
	assert.Equal(t, "OPENROUTER_API_KEY", providerKeyEnvVars[ProviderIDOpenRouter])
}

func TestAddCustomModel(t *testing.T) {
	withCustomModel(t, "test-model", ProviderIDOpenAI, "gpt-test", ModelOverrides{ReasoningEffort: "low"}, func() {
		// Verify the model was added
		require.True(t, ModelIDIsValid("test-model"), "Model should be valid after adding")

		// Verify provider ID
		require.Equal(t, ProviderIDOpenAI, ProviderIDForModelID("test-model"))

		// Test duplicate ID error
		err := AddCustomModel("test-model", ProviderIDOpenAI, "gpt-test-2", ModelOverrides{})
		require.Error(t, err)
	})

	// Test empty ID error
	err := AddCustomModel("", ProviderIDOpenAI, "gpt-test", ModelOverrides{})
	require.Error(t, err)

	// Test empty provider ID error
	err = AddCustomModel("test-model-3", ProviderID(""), "gpt-test", ModelOverrides{})
	require.Error(t, err)

	// Test non-existent provider error
	err = AddCustomModel("test-model-4", ProviderID("nonexistent"), "gpt-test", ModelOverrides{})
	require.Error(t, err)

	// Test empty model ID error
	err = AddCustomModel("test-model-5", ProviderIDOpenAI, "", ModelOverrides{})
	require.Error(t, err)
}

func TestAddCustomModelWithExistingModel(t *testing.T) {
	// Test adding a custom model that references an existing model in the provider
	// Get a real model from openai
	providers := modellist.GetProviders()
	var existingModelID string
	for _, p := range providers {
		if p.ID == "openai" {
			if len(p.Models) > 0 {
				existingModelID = p.Models[0].ID
				break
			}
		}
	}

	if existingModelID == "" {
		t.Skip("No existing OpenAI models found to test with")
	}

	withCustomModel(t, "alias-model", ProviderIDOpenAI, existingModelID, ModelOverrides{ReasoningEffort: "high"}, func() {
		require.True(t, ModelIDIsValid("alias-model"), "Aliased model should be valid")
	})
}

func TestAddCustomModelOverridesAndDefault(t *testing.T) {
	customID := ModelID("test-addcustom-params")
	params := ModelOverrides{ReasoningEffort: "medium"}

	withCustomModel(t, customID, ProviderIDOpenAI, "gpt-test-params", params, func() {
		// Locate the newly added model and assert fields
		var found *model
		for i := range availableModels {
			if availableModels[i].id == customID {
				found = &availableModels[i]
				break
			}
		}
		require.NotNil(t, found, "did not find model %q in availableModels", customID)
		assert.False(t, found.isDefault, "expected isDefault=false for custom model")
		assert.Equal(t, params, found.ModelOverrides)
	})
}
