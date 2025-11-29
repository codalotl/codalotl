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
	totalTokens, inputTokens, cachedTokens, outputTokens := summarizeTokenCounts(usage)
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

func summarizeTokenCounts(usage llmstream.TokenUsage) (total, input, cached, output int64) {
	input = nonCachedInputTokens(usage)
	cached = clamp64(usage.CachedInputTokens)
	output = clamp64(usage.TotalOutputTokens + usage.ReasoningTokens)
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

	nonCached := nonCachedInputTokens(usage)

	var (
		totalCost float64
		missing   bool
	)

	if nonCached > 0 {
		if info.CostPer1MIn > 0 {
			totalCost += (float64(nonCached) / million) * info.CostPer1MIn
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

func nonCachedInputTokens(usage llmstream.TokenUsage) int64 {
	nonCached := usage.TotalInputTokens - usage.CachedInputTokens
	if nonCached < 0 {
		return clamp64(usage.TotalInputTokens)
	}
	return clamp64(nonCached)
}
