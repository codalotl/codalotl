package llmcomplete

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProvider_Gemini(t *testing.T) {
	if !runIntegrationTest(t, ProviderKeyEnvVars()[ProviderIDGemini]) {
		return
	}

	defaultID := DefaultModelIDForProvider(ProviderIDGemini)
	if defaultID == ModelIDUnknown {
		t.Skip("No default Gemini model configured")
	}

	c := NewConversation(defaultID, "Follow user instructions.")
	userMessage := c.AddUserMessage("Say apple")

	messages := c.Messages()
	if assert.Len(t, messages, 2) {
		assert.Equal(t, "Follow user instructions.", messages[0].Text)
		assert.Equal(t, RoleSystem, messages[0].Role)
		assert.Equal(t, "Say apple", messages[1].Text)
		assert.Equal(t, RoleUser, messages[1].Role)
	}

	m, err := c.Send()

	require.NoError(t, err)
	messages = c.Messages()
	assert.Len(t, messages, 3)
	assert.Equal(t, string(defaultID), userMessage.chosenModel)

	if assert.NotNil(t, m) {
		assert.Equal(t, m, messages[2])

		assert.Equal(t, "", m.chosenModel) // chosenModel is only for user messages

		assert.Contains(t, strings.ToLower(m.Text), "apple")
		assert.Equal(t, RoleAssistant, m.Role)
		assert.Len(t, m.Errors, 0)
		if assert.NotNil(t, m.ResponseMetadata) {
			// Gemini minimal client does not provide a request id via our client wrapper; may be blank
			assert.Contains(t, m.ResponseMetadata.Model, string(defaultID))

			// StopReason may be provider-specific casing (e.g., "STOP"); don't assert exact

			// Tokens should be non-negative and usually >0 when a response is produced
			assert.True(t, m.ResponseMetadata.TotalTokens >= 0)
			assert.True(t, m.ResponseMetadata.InputTokens >= 0)
			assert.True(t, m.ResponseMetadata.OutputTokens >= 0)

			// Gemini does not expose ratelimit headers in this minimal client; limits remain zero.
			assert.Equal(t, 0, m.ResponseMetadata.RateLimits.RequestsLimit)
			assert.Equal(t, 0, m.ResponseMetadata.RateLimits.TokensLimit)
		}
	}
}
