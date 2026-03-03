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

	prompt := GetBasicPrompt()
	assert.Contains(t, prompt, "Codalotl")
	assert.Contains(t, prompt, "# Sandbox, Approvals, and Safety")
	assert.Contains(t, prompt, "# Delivering your Final Message to the User")
	assert.Contains(t, prompt, "`apply_patch`")
}

func TestGetGoPackageModeModePrompt_ExtendsBasicPrompt(t *testing.T) {
	agentName := "Codalotl"
	modelID := llmmodel.DefaultModel

	SetAgentName(agentName)
	SetModel(modelID)

	base := GetBasicPrompt()
	got := GetGoPackageModeModePrompt(GoPackageModePromptKindFull)

	data := promptTemplateData(agentName, modelID)
	wantSuffix := renderFragment(strings.TrimSpace(packageModeDefault), data)
	want := base + "\n\n" + wantSuffix

	assert.True(t, strings.HasPrefix(got, base))
	assert.Equal(t, want, got)
}
func TestGetPrompt_NonOpenAIUsesEditWriteDelete(t *testing.T) {
	SetAgentName("Codalotl")
	SetModel(llmmodel.ModelID("non-openai-model"))
	prompt := GetBasicPrompt()
	assert.Contains(t, prompt, "`edit`, `write`, and `delete`")
	assert.NotContains(t, prompt, "use `apply_patch`")
}

func TestDecideFileEditTools(t *testing.T) {
	openAI := decideFileEditTools(llmmodel.DefaultModel)
	assert.Equal(t, "`apply_patch`", openAI.list)
	assert.Equal(t, "`apply_patch`", openAI.afterEach)
	other := decideFileEditTools(llmmodel.ModelID("non-openai-model"))
	assert.Equal(t, "`edit`, `write`, and `delete`", other.list)
	assert.Equal(t, "file-edit tool call (`edit`, `write`, or `delete`)", other.afterEach)
}
