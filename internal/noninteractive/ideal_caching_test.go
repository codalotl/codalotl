package noninteractive

import (
	"os"
	"testing"

	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/stretchr/testify/require"
)

func TestReportIdealCachingEnabled_UsesLookupEnvPresence(t *testing.T) {
	require.False(t, reportIdealCachingEnabled())

	t.Setenv("REPORT_IDEAL_CACHING", "1")
	require.True(t, reportIdealCachingEnabled())

	// Presence matters, not truthiness.
	t.Setenv("REPORT_IDEAL_CACHING", "")
	require.True(t, reportIdealCachingEnabled())

	require.NoError(t, os.Unsetenv("REPORT_IDEAL_CACHING"))
	require.False(t, reportIdealCachingEnabled())
}

func TestIdealCachingForProviderTurns_RecalculatesCachedInputAsPreviousTurnInputPlusOutput(t *testing.T) {
	t.Parallel()

	turns := []llmstream.Turn{
		{Role: llmstream.RoleSystem},
		{Role: llmstream.RoleUser},
		{
			Role:       llmstream.RoleAssistant,
			ProviderID: "resp_1",
			Usage: llmstream.TokenUsage{
				TotalInputTokens:  10,
				CachedInputTokens: 999, // should be overwritten
				TotalOutputTokens: 1,
				ReasoningTokens:   2,
			},
		},
		{
			Role:       llmstream.RoleAssistant,
			ProviderID: "resp_2",
			Usage: llmstream.TokenUsage{
				TotalInputTokens:  14,
				CachedInputTokens: 0, // should become 10
				TotalOutputTokens: 2,
			},
		},
		{
			Role:       llmstream.RoleAssistant,
			ProviderID: "resp_3",
			Usage: llmstream.TokenUsage{
				TotalInputTokens:  7, // prompt shrank; clamp cached to 7
				CachedInputTokens: 123,
				TotalOutputTokens: 3,
			},
		},
		{
			Role:       llmstream.RoleAssistant,
			ProviderID: "", // not a provider request; should be ignored
			Usage: llmstream.TokenUsage{
				TotalInputTokens: 100,
			},
		},
	}

	filtered := providerAssistantTurns(turns)
	require.Len(t, filtered, 3)

	idealTurns, usage := idealCachingForProviderTurns(filtered)
	require.Len(t, idealTurns, 3)

	require.EqualValues(t, 0, idealTurns[0].Usage.CachedInputTokens)
	require.EqualValues(t, 11, idealTurns[1].Usage.CachedInputTokens) // 10 input + 1 output from previous turn
	require.EqualValues(t, 7, idealTurns[2].Usage.CachedInputTokens)

	require.EqualValues(t, 31, usage.TotalInputTokens)  // 10+14+7
	require.EqualValues(t, 18, usage.CachedInputTokens) // 0+11+7
	require.EqualValues(t, 6, usage.TotalOutputTokens)  // 1+2+3
	require.EqualValues(t, 2, usage.ReasoningTokens)    // only first turn set it
	require.EqualValues(t, "resp_1", idealTurns[0].ProviderID)
	require.EqualValues(t, "resp_2", idealTurns[1].ProviderID)
	require.EqualValues(t, "resp_3", idealTurns[2].ProviderID)
}
