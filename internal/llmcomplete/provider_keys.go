package llmcomplete

import (
	"github.com/codalotl/codalotl/internal/llmcomplete/internal/modellist"
	"strings"
)

// configuredProviderKeys holds map of provider -> actual api key, set with ConfigureProviderKey.
// Not thread safe.
var configuredProviderKeys = map[ProviderID]string{}

func ConfigureProviderKey(providerID ProviderID, key string) {
	configuredProviderKeys[providerID] = key
}

// HasDefaultKey returns true if the current env has a value the provider's default key.
func HasDefaultKey(providerID ProviderID) bool {
	provider := findProvider(modellist.GetProviders(), providerID)
	if provider == nil {
		return false
	}
	return getEnvWithPossibleDollar(provider.APIKeyEnv) != ""
}

// ProviderKeyEnvVars returns a map of provider id to env var (without $) for all providers in AllProvidersIDs.
func ProviderKeyEnvVars() map[ProviderID]string {
	envVars := make(map[ProviderID]string, len(AllProvidersIDs))
	providers := modellist.GetProviders()

	for _, providerID := range AllProvidersIDs {
		provider := findProvider(providers, providerID)
		if provider != nil {
			envVars[providerID] = strings.TrimLeft(provider.APIKeyEnv, "$")
		}
	}

	return envVars
}
