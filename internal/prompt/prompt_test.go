package prompt

import (
	"github.com/codalotl/codalotl/internal/llmmodel"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetPrompt(t *testing.T) {

	prompt := GetFullPrompt("Codalotl", llmmodel.ModelID("gpt-5"))
	assert.Contains(t, prompt, "Codalotl")
	assert.Contains(t, prompt, "# Sandbox, Approvals, and Safety")
	assert.Contains(t, prompt, "# Delivering your Final Message to the User")
}
