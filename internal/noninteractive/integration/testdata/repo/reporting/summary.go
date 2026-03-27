package reporting

import (
	orders "example.com/clarifyintegration"
	"example.com/clarifyintegration/pricing"
)

// Summary captures a compact aggregate view over a batch of plans.
type Summary struct {
	Orders        int
	Ready         int
	RevenueCents  int
	DiscountCents int
	TopIssue      string
}

// SummarizePlans aggregates revenue, discounts, readiness, and the most common issue.
func SummarizePlans(plans []orders.Plan) Summary {
	summary := Summary{Orders: len(plans)}
	issueCounts := map[string]int{}
	topIssueCount := 0

	for _, plan := range plans {
		if plan.ReadyToShip() {
			summary.Ready++
		}
		summary.RevenueCents += plan.Quote.TotalCents
		summary.DiscountCents += plan.Quote.DiscountCents

		for _, issue := range plan.Issues {
			issueCounts[issue.Message]++
			count := issueCounts[issue.Message]
			if count > topIssueCount || (count == topIssueCount && issue.Message < summary.TopIssue) {
				topIssueCount = count
				summary.TopIssue = issue.Message
			}
		}
	}

	return summary
}

// PromotionCounts counts how many quotes used each promotion label.
func PromotionCounts(quotes []pricing.Quote) map[string]int {
	counts := make(map[string]int)
	for _, quote := range quotes {
		for _, label := range quote.Applied {
			counts[label]++
		}
	}
	return counts
}
