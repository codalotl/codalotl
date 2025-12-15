package llmcomplete

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProvider_OpenRouter_Basic(t *testing.T) {
	if !runIntegrationTest(t, ProviderKeyEnvVars()[ProviderIDOpenRouter]) {
		return
	}

	mid := ModelID("my-model")
	vendorMid := "anthropic/claude-3.5-haiku-20241022"

	withCustomModel(t, mid, ProviderIDOpenRouter, vendorMid, ModelOverrides{}, func() {
		c := NewConversation(mid, "Follow user instructions.")
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
		assert.Equal(t, vendorMid, userMessage.chosenModel)

		if assert.NotNil(t, m) {
			assert.Equal(t, m, messages[2])
			assert.Contains(t, strings.ToLower(m.Text), "apple")
			assert.Equal(t, RoleAssistant, m.Role)
			assert.Len(t, m.Errors, 0)
		}
	})
}

func TestProvider_XAI(t *testing.T) {
	if !runIntegrationTest(t, ProviderKeyEnvVars()[ProviderIDXAI]) {
		return
	}

	c := NewConversation(ModelIDGrok4Fast, "Follow user instructions.")
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
	assert.Equal(t, string(ModelIDGrok4Fast), userMessage.chosenModel)

	if assert.NotNil(t, m) {
		assert.Equal(t, m, messages[2])

		assert.Equal(t, "", m.chosenModel) // chosenModel is only for user messages

		assert.Contains(t, strings.ToLower(m.Text), "apple")
		assert.Equal(t, RoleAssistant, m.Role)
		assert.Len(t, m.Errors, 0)
		if assert.NotNil(t, m.ResponseMetadata) {
			assert.GreaterOrEqual(t, strings.Count(m.ResponseMetadata.RequestID, "-"), 4) // "741e129f-443d-c411-2b0a-f526b4935da0" is example RequestID
			assert.Contains(t, m.ResponseMetadata.Model, string(ModelIDGrok4Fast))
			assert.Equal(t, "stop", m.ResponseMetadata.StopReason)

			assertIntBetween(t, 10, 500, m.ResponseMetadata.TotalTokens)     // 186,464 when testing
			assertIntBetween(t, 10, 300, m.ResponseMetadata.InputTokens)     // 123 when testing
			assertIntBetween(t, 1, 100, m.ResponseMetadata.OutputTokens)     // 1 when testing
			assertIntBetween(t, 10, 500, m.ResponseMetadata.ReasoningTokens) // 169,225,216,431 when testing

			// XAI as of 2025/08/06 does not support rate limit headers, so these are 0.
			// If they start, these might start getting populated, and this test will fail. Just delete this comment+assert.Equals, and uncomment the next 4 asserts.
			assert.Equal(t, 0, m.ResponseMetadata.RateLimits.RequestsLimit)
			assert.Equal(t, 0, m.ResponseMetadata.RateLimits.TokensLimit)

			// assert.True(t, m.ResponseMetadata.RateLimits.RequestsLimit > 0)
			// assert.True(t, m.ResponseMetadata.RateLimits.TokensLimit > 0)
			// assertIntBetween(t, 1, m.ResponseMetadata.RequestsLimit-1, m.ResponseMetadata.RateLimits.RequestsRemaining) // assume that we actually use one from our limit
			// assertIntBetween(t, 1, m.ResponseMetadata.TokensLimit-1, m.ResponseMetadata.RateLimits.TokensRemaining)     // NOTE: instead of 1, i subtracted m.ResponseMetadata.TotalTokens, which didn't work :shrug:

			assert.NotZero(t, m.ResponseMetadata.RateLimits.RequestsResetsAt)
			assert.NotZero(t, m.ResponseMetadata.RateLimits.TokensResetsAt)
		}
	}
}
