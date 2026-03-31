package reporting

import (
	"fmt"

	"example.com/clarifyintegration/pricing"
)

// FormatSummary renders Summary in a stable single-line format.
func FormatSummary(summary Summary) string {
	topIssue := summary.TopIssue
	if topIssue == "" {
		topIssue = "none"
	}
	return fmt.Sprintf(
		"%d orders, %d ready, revenue %s, discounts %s, top issue: %s",
		summary.Orders,
		summary.Ready,
		pricing.FormatCents(summary.RevenueCents),
		pricing.FormatCents(summary.DiscountCents),
		topIssue,
	)
}
