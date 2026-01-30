package llmcomplete

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUsage(t *testing.T) {

	modelID := ModelIDGPT41Nano
	withCustomModel(t, modelID, ProviderIDOpenAI, string(ModelIDGPT41Nano), ModelOverrides{}, func() {
		c := NewConversation(modelID, "sys")
		c.AddUserMessage("hello")
		c.LastMessage().chosenModel = string(ModelIDGPT41Nano)

		// fabricate assistant message with metadata
		md := &ResponseMetadata{
			Model:           "gpt-4.1-nano-2025-04-14",
			TotalTokens:     20,
			InputTokens:     15,
			ReasoningTokens: 5,
			OutputTokens:    5,
		}
		c.(*conversation).messages = append(c.(*conversation).messages, &Message{Role: RoleAssistant, Text: "hi", ResponseMetadata: md})

		u := c.Usage()
		if assert.Len(t, u, 1) {
			assert.Equal(t, "gpt-4.1-nano-2025-04-14", u[0].Model)
			assert.Equal(t, 20, u[0].TotalTokens)
			assert.True(t, u[0].Cost > 0)
		}
	})
}

func TestPrintTotalUsageEmpty(t *testing.T) {
	PrintTotalUsage(nil)
}
