package tui

import (
	"testing"

	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokensCostLinesFormatsValues(t *testing.T) {
	info := llmmodel.ModelInfo{
		ID:                llmmodel.ModelID("fake"),
		ContextWindow:     200_000,
		CostPer1MIn:       10,
		CostPer1MOut:      20,
		CostPer1MInCached: 5,
	}
	usage := llmstream.TokenUsage{
		TotalInputTokens:  102_000,
		CachedInputTokens: 60_000,
		TotalOutputTokens: 21_000,
		ReasoningTokens:   1_000,
	}

	lines := tokensCostLines(info, usage, 51)
	require.Len(t, lines, 2)

	assert.Equal(t, "Context: 49% left   |   Cost: $1.14", lines[0])
	assert.Equal(t, "Tokens: 124k (input: 42k, cached: 60k, output: 22k)", lines[1])
}

func TestTokensCostLinesHandlesUnknowns(t *testing.T) {
	info := llmmodel.ModelInfo{ID: llmmodel.ModelID("no-pricing")}
	usage := llmstream.TokenUsage{TotalInputTokens: 1_000}

	lines := tokensCostLines(info, usage, 0)
	require.Len(t, lines, 2)
	assert.Equal(t, "Context: unknown   |   Cost: unavailable", lines[0])
	assert.Equal(t, "Tokens: 1k (input: 1k, cached: 0, output: 0)", lines[1])
}

func TestFormatTokenCount(t *testing.T) {
	assert.Equal(t, "313", formatTokenCount(313))
	assert.Equal(t, "1.4k", formatTokenCount(1_400))
	assert.Equal(t, "520k", formatTokenCount(520_000))
	assert.Equal(t, "1.2M", formatTokenCount(1_200_000))
	assert.Equal(t, "3B", formatTokenCount(3_000_000_000))
}

func TestEstimateUsageCostUSD_AccountsForCacheCreationWrites(t *testing.T) {
	info := llmmodel.ModelInfo{
		ID:                     llmmodel.ModelID("fake"),
		CostPer1MIn:            10,
		CostPer1MInCached:      2,
		CostPer1MInSaveToCache: 20,
		CostPer1MOut:           30,
	}
	usage := llmstream.TokenUsage{
		TotalInputTokens:         1_000,
		CachedInputTokens:        200,
		CacheCreationInputTokens: 300,
		TotalOutputTokens:        500,
	}

	cost, ok := estimateUsageCostUSD(usage, info)
	require.True(t, ok)
	assert.InDelta(t, 0.0264, cost, 0.0000001)
}

func TestTokensCostLines_InputIncludesCacheWrites(t *testing.T) {
	info := llmmodel.ModelInfo{
		ID:                     llmmodel.ModelID("fake"),
		ContextWindow:          200_000,
		CostPer1MIn:            10,
		CostPer1MOut:           20,
		CostPer1MInCached:      5,
		CostPer1MInSaveToCache: 15,
	}
	usage := llmstream.TokenUsage{
		TotalInputTokens:         100_000,
		CachedInputTokens:        40_000,
		CacheCreationInputTokens: 20_000,
		TotalOutputTokens:        10_000,
	}

	lines := tokensCostLines(info, usage, 50)
	require.Len(t, lines, 2)

	// input = uncached (100k - 40k - 20k = 40k) + cache writes (20k) = 60k
	assert.Equal(t, "Tokens: 110k (input: 60k, cached: 40k, output: 10k)", lines[1])
}

func TestTokensCostLines_OpenAIDoesNotDoubleCountReasoningTokens(t *testing.T) {
	info := llmmodel.ModelInfo{
		ID:                llmmodel.ModelID("fake-openai"),
		ProviderID:        llmmodel.ProviderIDOpenAI,
		ContextWindow:     200_000,
		CostPer1MIn:       10,
		CostPer1MOut:      20,
		CostPer1MInCached: 5,
	}
	usage := llmstream.TokenUsage{
		TotalInputTokens:  102_000,
		CachedInputTokens: 60_000,
		TotalOutputTokens: 21_000,
		ReasoningTokens:   1_000,
	}

	lines := tokensCostLines(info, usage, 51)
	require.Len(t, lines, 2)

	assert.Equal(t, "Tokens: 123k (input: 42k, cached: 60k, output: 21k)", lines[1])
}

func TestTokensCostLines_AnthropicPricingUsesCacheReadAndWriteRates(t *testing.T) {
	info := llmmodel.GetModelInfo(llmmodel.ModelID("sonnet-4.6"))
	require.Equal(t, llmmodel.ProviderIDAnthropic, info.ProviderID)

	usage := llmstream.TokenUsage{
		TotalInputTokens:         100_000,
		CachedInputTokens:        20_000,
		CacheCreationInputTokens: 10_000,
		TotalOutputTokens:        5_000,
	}

	lines := tokensCostLines(info, usage, 50)
	require.Len(t, lines, 2)

	// Cost math for sonnet-4.6:
	// uncached input (70k) @ $3/M + cached read (20k) @ $0.3/M +
	// cache writes (10k) @ $3.75/M + output (5k) @ $15/M = $0.3285 -> $0.33.
	assert.Equal(t, "Context: 50% left   |   Cost: $0.33", lines[0])
	assert.Equal(t, "Tokens: 105k (input: 80k, cached: 20k, output: 5k)", lines[1])
}
