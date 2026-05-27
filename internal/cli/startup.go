package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/codalotl/codalotl/internal/goclitools"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/subscriptions/openaisub"
)

var refreshOpenAIDefaultProviderSubscription = openaisub.RefreshDefaultProviderSubscription

type startupValidationError struct {
	MissingTools []goclitools.ToolStatus
	MissingLLM   bool
	LLMAuthError error
	LLMEnvVars   []string
}

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
		b.WriteString("\nNo usable LLM auth or credentials are configured.\n")

		if e.LLMAuthError != nil {
			b.WriteString("\nOpenAI subscription auth could not be loaded/refreshed:\n")
			b.WriteString("- ")
			b.WriteString(e.LLMAuthError.Error())
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

	return strings.TrimRight(b.String(), "\n")
}

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

func validateStartup(ctx context.Context, cfg Config, requiredTools []goclitools.ToolRequirement) error {
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

	refreshErr := refreshOpenAIDefaultProviderSubscription(ctx)
	missingLLM := len(llmmodel.AvailableModelIDsWithAuth()) == 0

	if len(missingTools) == 0 && !missingLLM {
		return nil
	}
	return startupValidationError{
		MissingTools: missingTools,
		MissingLLM:   missingLLM,
		LLMAuthError: refreshErr,
		LLMEnvVars:   llmProviderEnvVarsForDisplay(cfg),
	}
}
