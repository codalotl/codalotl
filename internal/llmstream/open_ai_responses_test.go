package llmstream

import (
	"testing"

	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponesBuildResponse_SetsRoleAssistant(t *testing.T) {
	turn := openaiResponesBuildResponse(responses.Response{
		ID:     "resp_123",
		Status: "completed",
	})
	require.NotNil(t, turn)
	assert.Equal(t, RoleAssistant, turn.Role)
}

func TestOpenAIResponesConvertUsage_TotalOutputIncludesReasoning(t *testing.T) {
	usage := responses.ResponseUsage{
		InputTokens: 12,
		InputTokensDetails: responses.ResponseUsageInputTokensDetails{
			CachedTokens: 3,
		},
		OutputTokens: 40,
		OutputTokensDetails: responses.ResponseUsageOutputTokensDetails{
			ReasoningTokens: 7,
		},
	}

	got := openaiResponesConvertUsage(usage)

	assert.EqualValues(t, 12, got.TotalInputTokens)
	assert.EqualValues(t, 3, got.CachedInputTokens)
	assert.EqualValues(t, 7, got.ReasoningTokens)
	assert.EqualValues(t, 40, got.TotalOutputTokens)
	assert.GreaterOrEqual(t, got.TotalOutputTokens, got.ReasoningTokens)
}
