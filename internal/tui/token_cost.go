package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/q/termformat"
)

func tokensCostLines(info llmmodel.ModelInfo, usage llmstream.TokenUsage, contextPercentUsed int) []string {
	totalTokens, inputTokens, cachedTokens, outputTokens := summarizeTokenCounts(info, usage)
	contextTokensUsed := inputTokens + cachedTokens
	contextText := formatContextUsage(contextPercentUsed, contextTokensUsed)
	costText := formatCostLine(usage, info)

	first := fmt.Sprintf("Context: %s   |   Cost: %s", contextText, costText)
	second := fmt.Sprintf("Tokens: %s (input: %s, cached: %s, output: %s)",
		formatTokenCount(totalTokens),
		formatTokenCount(inputTokens),
		formatTokenCount(cachedTokens),
		formatTokenCount(outputTokens),
	)

	return []string{
		termformat.Sanitize(first, 4),
		termformat.Sanitize(second, 4),
	}
}

func summarizeTokenCounts(info llmmodel.ModelInfo, usage llmstream.TokenUsage) (total, input, cached, output int64) {
	input = inputTokensForDisplay(usage)
	cached = clamp64(usage.CachedInputTokens)
	output = outputTokensForDisplay(info, usage)
	total = input + cached + output
	return
}

func formatContextUsage(percentUsed int, usedTokens int64) string {
	switch {
	case percentUsed < 0:
		return "unknown"
	case percentUsed == 0:
		if usedTokens <= 0 {
			return "100% left"
		}
		return "unknown"
	case percentUsed >= 100:
		return "0% left"
	}

	left := 100 - percentUsed
	if left < 0 {
		left = 0
	}
	if left > 100 {
		left = 100
	}
	return fmt.Sprintf("%d%% left", left)
}

func formatCostLine(usage llmstream.TokenUsage, info llmmodel.ModelInfo) string {
	if cost, ok := estimateUsageCostUSD(usage, info); ok {
		return fmt.Sprintf("$%.2f", cost)
	}
	return "unavailable"
}

func estimateUsageCostUSD(usage llmstream.TokenUsage, info llmmodel.ModelInfo) (float64, bool) {
	if info.ID == llmmodel.ModelIDUnknown {
		return 0, false
	}

	const million = 1_000_000.0

	uncached := uncachedInputTokens(usage)
	cacheCreation := clamp64(usage.CacheCreationInputTokens)

	var (
		totalCost float64
		missing   bool
	)

	if uncached > 0 {
		if info.CostPer1MIn > 0 {
			totalCost += (float64(uncached) / million) * info.CostPer1MIn
		} else {
			missing = true
		}
	}

	if usage.CachedInputTokens > 0 {
		rate := info.CostPer1MInCached
		if rate <= 0 {
			rate = info.CostPer1MIn
		}
		if rate > 0 {
			totalCost += (float64(usage.CachedInputTokens) / million) * rate
		} else {
			missing = true
		}
	}

	if cacheCreation > 0 {
		rate := info.CostPer1MInSaveToCache
		if rate <= 0 {
			rate = info.CostPer1MIn
		}
		if rate > 0 {
			totalCost += (float64(cacheCreation) / million) * rate
		} else {
			missing = true
		}
	}

	if usage.TotalOutputTokens > 0 {
		if info.CostPer1MOut > 0 {
			totalCost += (float64(usage.TotalOutputTokens) / million) * info.CostPer1MOut
		} else {
			missing = true
		}
	}

	if missing {
		return 0, false
	}
	if totalCost == 0 && (usage.TotalInputTokens > 0 || usage.TotalOutputTokens > 0) {
		return 0, false
	}
	return totalCost, true
}

func formatTokenCount(tokens int64) string {
	value := tokens
	if value < 0 {
		value = 0
	}

	switch {
	case value >= 1_000_000_000:
		return formatScaledTokenCount(value, 1_000_000_000, "B")
	case value >= 1_000_000:
		return formatScaledTokenCount(value, 1_000_000, "M")
	case value >= 1_000:
		return formatScaledTokenCount(value, 1_000, "k")
	default:
		return fmt.Sprintf("%d", value)
	}
}

func formatScaledTokenCount(value int64, base int64, suffix string) string {
	scaled := float64(value) / float64(base)
	precision := 0
	if scaled < 10 {
		precision = 1
	}
	text := strconv.FormatFloat(scaled, 'f', precision, 64)
	text = strings.TrimSuffix(text, ".0")
	return text + suffix
}

func clamp64(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

func inputTokensForDisplay(usage llmstream.TokenUsage) int64 {
	// "input" display includes everything not billed as a cache read:
	// base input + cache creation writes.
	return uncachedInputTokens(usage) + clamp64(usage.CacheCreationInputTokens)
}

func outputTokensForDisplay(info llmmodel.ModelInfo, usage llmstream.TokenUsage) int64 {
	output := clamp64(usage.TotalOutputTokens)
	// OpenAI's reasoning tokens are an output-token breakdown, so including them
	// again would double-count displayed totals.
	if info.ProviderID == llmmodel.ProviderIDOpenAI {
		return output
	}
	return output + clamp64(usage.ReasoningTokens)
}

func uncachedInputTokens(usage llmstream.TokenUsage) int64 {
	uncached := usage.TotalInputTokens - usage.CachedInputTokens - usage.CacheCreationInputTokens
	if uncached < 0 {
		return clamp64(usage.TotalInputTokens)
	}
	return clamp64(uncached)
}
