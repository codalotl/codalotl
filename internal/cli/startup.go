package cli

import (
	"fmt"
	"strings"

	"github.com/codalotl/codalotl/internal/goclitools"
	"github.com/codalotl/codalotl/internal/llmmodel"
)

type startupValidationError struct {
	MissingTools []goclitools.ToolStatus
	MissingLLM   bool
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
		b.WriteString("\nNo LLM provider API key is configured.\n")

		relevant := e.LLMEnvVars
		if len(relevant) > 0 {
			b.WriteString("\nTo fix, set one of these ENV variables (recommended):\n")
			for _, ev := range relevant {
				b.WriteString("- ")
				b.WriteString(ev)
				b.WriteString("\n")
			}
		}

		b.WriteString("\nOr add a config file:\n")
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

func validateStartup(cfg Config, requiredTools []goclitools.ToolRequirement) error {
	toolStatuses := goclitools.CheckTools(requiredTools)
	var missingTools []goclitools.ToolStatus
	for _, st := range toolStatuses {
		if strings.TrimSpace(st.Path) == "" {
			missingTools = append(missingTools, st)
		}
	}

	missingLLM := len(llmmodel.AvailableModelIDsWithAPIKey()) == 0

	if len(missingTools) == 0 && !missingLLM {
		return nil
	}
	return startupValidationError{
		MissingTools: missingTools,
		MissingLLM:   missingLLM,
		LLMEnvVars:   llmProviderEnvVarsForDisplay(cfg),
	}
}
