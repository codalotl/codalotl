package llmcomplete

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAnthropic(t *testing.T) {
	if !runIntegrationTest(t, ProviderKeyEnvVars()[ProviderIDAnthropic]) {
		return
	}

	withCustomModel(t, ModelIDClaude35Haiku, ProviderIDAnthropic, "claude-3-5-haiku-20241022", ModelOverrides{}, func() {
		c := NewConversation(ModelIDClaude35Haiku, "Follow user instructions.")
		userMessage := c.AddUserMessage("Say apple")

		messages := c.Messages()
		if assert.Len(t, messages, 2) {
			assert.Equal(t, "Follow user instructions.", messages[0].Text)
			assert.Equal(t, RoleSystem, messages[0].Role)
			assert.Equal(t, "Say apple", messages[1].Text)
			assert.Equal(t, RoleUser, messages[1].Role)
		}

		m, err := c.Send()

		assert.NoError(t, err)
		messages = c.Messages()
		assert.Len(t, messages, 3)
		assert.True(t, strings.HasPrefix(userMessage.chosenModel, string(ModelIDClaude35Haiku)))

		if assert.NotNil(t, m) {
			assert.Equal(t, m, messages[2])

			assert.Equal(t, "", m.chosenModel)
			assert.Contains(t, strings.ToLower(m.Text), "apple")
			assert.Equal(t, RoleAssistant, m.Role)
			assert.Len(t, m.Errors, 0)
			if assert.NotNil(t, m.ResponseMetadata) {
				// Request IDs are opaque for Anthropic; just assert non-empty
				assert.NotEmpty(t, m.ResponseMetadata.RequestID)
				assert.Contains(t, m.ResponseMetadata.Model, string(ModelIDClaude35Haiku))

				// Usage fields should now be populated
				assert.True(t, m.ResponseMetadata.TotalTokens >= m.ResponseMetadata.InputTokens)
				assert.True(t, m.ResponseMetadata.TotalTokens >= m.ResponseMetadata.OutputTokens)
			}
		}
	})
}
