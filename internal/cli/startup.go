package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/codalotl/codalotl/internal/goclitools"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/subscriptions/openaisub"
)

var refreshOpenAIDefaultProviderSubscription = openaisub.RefreshDefaultProviderSubscription

// A startupModelSelector returns model IDs that should be included in startup validation.
type startupModelSelector func(Config) []llmmodel.ModelID

// startupValidationError describes startup validation failures that should be shown to the user.
type startupValidationError struct {
	MissingTools                   []goclitools.ToolStatus // MissingTools lists required tools that were not found on PATH.
	MissingLLM                     bool                    // MissingLLM reports whether no usable LLM auth is available.
	OpenAISubscriptionAuthUnusable bool                    // OpenAISubscriptionAuthUnusable reports whether saved OpenAI subscription auth is unusable.
	OpenAISubscriptionRefreshError error                   // OpenAISubscriptionRefreshError is the saved OpenAI subscription auth refresh failure, if any.
	LLMEnvVars                     []string                // LLMEnvVars lists provider API key environment variables relevant to the config.
}

// Error returns the user-facing startup validation failure message.
func (e startupValidationError) Error() string {
	var b strings.Builder
	b.WriteString("codalotl startup validation failed.\n")

	if len(e.MissingTools) > 0 {
		b.WriteString("\nMissing required tools (must be on PATH):\n")
		for _, st := range e.MissingTools {
			name := strings.TrimSpace(st.Name)
			if name == "" {
				name = "(unknown)"
			}
			b.WriteString("- ")
			b.WriteString(name)
			b.WriteString("\n")
		}

		var hasGoInstall bool
		for _, st := range e.MissingTools {
			if strings.TrimSpace(st.InstallHint) != "" {
				hasGoInstall = true
				break
			}
		}
		if hasGoInstall {
			b.WriteString("\nInstall (tools available via `go install`):\n")
			for _, st := range e.MissingTools {
				hint := strings.TrimSpace(st.InstallHint)
				if hint == "" {
					continue
				}
				b.WriteString("- ")
				b.WriteString(hint)
				b.WriteString("\n")
			}
		}
		b.WriteString("\nOther tools must be installed via your system package manager.\n")
	}

	if e.MissingLLM {
		if e.OpenAISubscriptionAuthUnusable {
			b.WriteString("\nOpenAI ChatGPT subscription auth is configured but unusable for the selected OpenAI model.\n")

			if e.OpenAISubscriptionRefreshError != nil {
				b.WriteString("\nOpenAI subscription auth could not be loaded/refreshed:\n")
				b.WriteString("- ")
				b.WriteString(e.OpenAISubscriptionRefreshError.Error())
				b.WriteString("\n")
			}

			b.WriteString("\nTo fix, log in again:\n")
			b.WriteString("- codalotl auth openai login\n")

			b.WriteString("\nOr remove saved OpenAI subscription credentials to allow configured OpenAI API-key billing:\n")
			b.WriteString("- codalotl auth openai logout\n")
		} else {
			b.WriteString("\nNo usable LLM auth or credentials are configured.\n")

			if e.OpenAISubscriptionRefreshError != nil {
				b.WriteString("\nOpenAI subscription auth could not be loaded/refreshed:\n")
				b.WriteString("- ")
				b.WriteString(e.OpenAISubscriptionRefreshError.Error())
				b.WriteString("\n")
			}

			relevant := e.LLMEnvVars
			if len(relevant) > 0 {
				b.WriteString("\nTo fix, set one of these provider API key ENV variables:\n")
				for _, ev := range relevant {
					b.WriteString("- ")
					b.WriteString(ev)
					b.WriteString("\n")
				}
			}

			b.WriteString("\nOr log in with supported provider subscription auth:\n")
			b.WriteString("- codalotl auth openai login\n")

			b.WriteString("\nOr add an API key to a config file:\n")
			b.WriteString("- Global: ")
			b.WriteString(globalConfigPath())
			b.WriteString("\n")
			b.WriteString("- Project: .codalotl/config.json\n")

			// Keep this snippet aligned with the current ProviderKeys schema.
			if len(relevant) > 0 {
				b.WriteString("\nExample config.json:\n")
				exampleProvider := exampleProviderKeyID(relevant)
				if exampleProvider == "" {
					exampleProvider = "openai"
				}
				b.WriteString(fmt.Sprintf(`{
  "providerkeys": { "%s": "sk-..." }
}
`, exampleProvider))
			}
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

// exampleProviderKeyID returns the providerkeys config key to use in a missing-auth example. It chooses the first known provider exposed by ProviderKeys whose default
// API-key environment variable appears in relevantEnvVars, or "" if none match.
func exampleProviderKeyID(relevantEnvVars []string) string {
	if len(relevantEnvVars) == 0 {
		return ""
	}
	relevant := make(map[string]bool, len(relevantEnvVars))
	for _, ev := range relevantEnvVars {
		ev = strings.TrimSpace(ev)
		if ev == "" {
			continue
		}
		relevant[ev] = true
	}
	envVars := llmmodel.ProviderKeyEnvVars()
	for _, pid := range providerIDsExposedByProviderKeys() {
		if !isKnownProviderID(pid) {
			continue
		}
		ev := strings.TrimSpace(envVars[pid])
		if ev == "" || !relevant[ev] {
			continue
		}
		return string(pid)
	}
	return ""
}

// validateStartup checks required tools and usable LLM authentication before a command runs.
func validateStartup(ctx context.Context, cfg Config, requiredTools []goclitools.ToolRequirement, selectedModels ...llmmodel.ModelID) error {
	if ctx == nil {
		ctx = context.Background()
	}

	toolStatuses := goclitools.CheckTools(requiredTools)
	var missingTools []goclitools.ToolStatus
	for _, st := range toolStatuses {
		if strings.TrimSpace(st.Path) == "" {
			missingTools = append(missingTools, st)
		}
	}

	var refreshErr error
	if shouldRefreshOpenAISubscriptionForStartup(cfg, selectedModels...) || shouldRefreshOpenAISubscriptionBeforeMissingAuth() {
		refreshErr = refreshOpenAIDefaultProviderSubscription(ctx)
	}
	availableModels := llmmodel.AvailableModelIDsWithAuth()
	missingLLM := len(availableModels) == 0
	openAISubscriptionAuthUnusable := openAISubscriptionAuthRequiredButUnusableForStartup(cfg, selectedModels...)

	if len(missingTools) == 0 && !missingLLM && !openAISubscriptionAuthUnusable {
		return nil
	}
	return startupValidationError{
		MissingTools:                   missingTools,
		MissingLLM:                     missingLLM || openAISubscriptionAuthUnusable,
		OpenAISubscriptionAuthUnusable: openAISubscriptionAuthUnusable,
		OpenAISubscriptionRefreshError: refreshErr,
		LLMEnvVars:                     llmProviderEnvVarsForDisplay(cfg),
	}
}

func selectedStartupModels(cfg Config, selectedModels ...llmmodel.ModelID) []llmmodel.ModelID {
	models := make([]llmmodel.ModelID, 0, len(selectedModels))
	for _, id := range selectedModels {
		if strings.TrimSpace(string(id)) == "" {
			continue
		}
		models = append(models, id)
	}
	if len(models) == 0 {
		models = append(models, effectiveModel(cfg))
	}
	return models
}

func startupModelsFromSelectors(cfg Config, selectors []startupModelSelector) []llmmodel.ModelID {
	var models []llmmodel.ModelID
	for _, selector := range selectors {
		if selector == nil {
			continue
		}
		models = append(models, selector(cfg)...)
	}
	return selectedStartupModels(cfg, models...)
}

func shouldRefreshOpenAISubscriptionForStartup(cfg Config, selectedModels ...llmmodel.ModelID) bool {
	for _, id := range selectedStartupModels(cfg, selectedModels...) {
		if id.ProviderID() == llmmodel.ProviderIDOpenAI {
			return !llmmodel.ProviderHasSubscription(llmmodel.ProviderIDOpenAI)
		}
	}
	return false
}

func shouldRefreshOpenAISubscriptionBeforeMissingAuth() bool {
	return !llmmodel.ProviderHasSubscription(llmmodel.ProviderIDOpenAI) &&
		len(llmmodel.AvailableModelIDsWithAuth()) == 0
}

func openAISubscriptionAuthRequiredButUnusableForStartup(cfg Config, selectedModels ...llmmodel.ModelID) bool {
	for _, id := range selectedStartupModels(cfg, selectedModels...) {
		if !modelEligibleForOpenAISubscriptionAuth(id) {
			continue
		}
		return llmmodel.ProviderSubscriptionRequired(llmmodel.ProviderIDOpenAI) &&
			!llmmodel.ProviderHasSubscription(llmmodel.ProviderIDOpenAI)
	}
	return false
}

func modelEligibleForOpenAISubscriptionAuth(id llmmodel.ModelID) bool {
	info := llmmodel.GetModelInfo(id)
	return info.ID != llmmodel.ModelIDUnknown &&
		info.ProviderID == llmmodel.ProviderIDOpenAI &&
		strings.TrimSpace(info.APIActualKey) == "" &&
		!apiEnvKeyHasUsableValue(info.APIEnvKey) &&
		strings.TrimSpace(info.ModelOverrides.APIEndpointURL) == ""
}

func apiEnvKeyHasUsableValue(envKey string) bool {
	envKey = strings.TrimSpace(envKey)
	envKey = strings.TrimPrefix(envKey, "$")
	if envKey == "" {
		return false
	}
	return strings.TrimSpace(os.Getenv(envKey)) != ""
}
