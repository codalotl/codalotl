package prompt

import (
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/llmmodel"

	"github.com/stretchr/testify/assert"
)

func TestGetPrompt(t *testing.T) {
	SetAgentName("Codalotl")
	SetModel(llmmodel.DefaultModel)

	prompt := GetFullPrompt()
	assert.Contains(t, prompt, "Codalotl")
	assert.Contains(t, prompt, "# Sandbox, Approvals, and Safety")
	assert.Contains(t, prompt, "# Delivering your Final Message to the User")
}

func TestGetGoPackageModeModePrompt_ExtendsFullPrompt(t *testing.T) {
	agentName := "Codalotl"
	modelID := llmmodel.DefaultModel

	SetAgentName(agentName)
	SetModel(modelID)

	base := GetFullPrompt()
	got := GetGoPackageModeModePrompt(GoPackageModePromptKindFull)

	data := map[string]any{
		"AgentName": agentName,
		"ModelName": modelDisplayName(modelID),
	}
	wantSuffix := renderFragment(strings.TrimSpace(goPackageModeSection), data)
	want := base + "\n\n" + wantSuffix

	assert.True(t, strings.HasPrefix(got, base))
	assert.Equal(t, want, got)
}
