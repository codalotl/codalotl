package llmstream

import (
	"bytes"
	"fmt"
	"text/tabwriter"
)

// UsageAndCaching returns a string intended for stdout printing, which contains a table of provider ids, and their usage. For each row, indicate:
//   - provider id (ex: "resp_123")
//   - response usage: input (uncached)
//   - response usage: cached input
//   - response usage: output
//   - cumulative versions of the above
//   - an indication of whether this response was likely "successful" in caching.
func UsageAndCaching(sc StreamingConversation) string {
	if sc == nil {
		return ""
	}

	// We can only infer "caching success" heuristically from TokenUsage:
	// - CachedInputTokens > 0 is a strong signal of a cache hit.
	// - CachedInputTokens == 0 is ambiguous for small prompts (may be ineligible),
	//   but for large prompts it suggests the provider didn't cache (or couldn't).
	cacheIndicator := func(rowIdx int, totalIn, cachedIn int64) string {
		// totalIn is typically inclusive of cached tokens (cached is a subset).
		if totalIn <= 0 {
			return "n/a"
		}

		if cachedIn <= 0 {
			// First provider response often can't hit cache, and small prompts may not be eligible.
			if rowIdx == 0 || totalIn < 256 {
				return "n/a"
			}
			// Above this threshold, a 0 cached count is more suspicious.
			if totalIn >= 1024 {
				return "miss 0%"
			}
			return "miss? 0%"
		}

		// Round to nearest percent to keep output stable and compact.
		pct := (cachedIn*100 + totalIn/2) / totalIn
		switch {
		case pct >= 50:
			return fmt.Sprintf("hit %d%%", pct)
		case pct >= 10:
			return fmt.Sprintf("partial %d%%", pct)
		default:
			return fmt.Sprintf("weak %d%%", pct)
		}
	}

	turns := sc.Turns()
	rows := make([]Turn, 0, len(turns))
	for _, t := range turns {
		// Only include turns that look like provider responses (i.e. have usage/provider ids).
		// User/system turns generally won't have these populated.
		if t.ProviderID != "" ||
			t.Usage.TotalInputTokens != 0 ||
			t.Usage.CachedInputTokens != 0 ||
			t.Usage.TotalOutputTokens != 0 {
			rows = append(rows, t)
		}
	}
	if len(rows) == 0 {
		return ""
	}

	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)

	fmt.Fprintln(tw, "turn\tprovider_id\tin_uncached\tin_cached\tout\tcum_in_uncached\tcum_in_cached\tcum_out\tcache")

	var cumUncachedIn int64
	var cumCachedIn int64
	var cumOut int64

	for i, t := range rows {
		totalIn := t.Usage.TotalInputTokens
		cachedIn := t.Usage.CachedInputTokens
		uncachedIn := totalIn - cachedIn
		if uncachedIn < 0 {
			uncachedIn = 0
		}
		out := t.Usage.TotalOutputTokens

		cumUncachedIn += uncachedIn
		cumCachedIn += cachedIn
		cumOut += out

		fmt.Fprintf(
			tw,
			"%d\t%s\t%d\t%d\t%d\t%d\t%d\t%d\t%s\n",
			i,
			t.ProviderID,
			uncachedIn,
			cachedIn,
			out,
			cumUncachedIn,
			cumCachedIn,
			cumOut,
			cacheIndicator(i, totalIn, cachedIn),
		)
	}

	fmt.Fprintf(tw, "\tTOTAL\t%d\t%d\t%d\t%d\t%d\t%d\t\n", cumUncachedIn, cumCachedIn, cumOut, cumUncachedIn, cumCachedIn, cumOut)

	_ = tw.Flush()
	return buf.String()
}
