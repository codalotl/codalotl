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
