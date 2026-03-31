package reporting

import (
	"testing"

	orders "example.com/clarifyintegration"
	"example.com/clarifyintegration/pricing"
	"github.com/stretchr/testify/assert"
)

func TestSummarizePlansAggregatesTopIssue(t *testing.T) {
	plans := []orders.Plan{
		{
			Quote:  pricing.Quote{TotalCents: 1200, DiscountCents: 100},
			Issues: []orders.Issue{{SKU: "gel-pack", Message: "requires cold-chain shipping"}},
		},
		{
			Quote:  pricing.Quote{TotalCents: 900, DiscountCents: 50},
			Issues: []orders.Issue{{SKU: "gel-pack", Message: "requires cold-chain shipping"}},
		},
		{
			Quote: pricing.Quote{TotalCents: 1500, DiscountCents: 0},
		},
	}

	summary := SummarizePlans(plans)

	assert.Equal(t, Summary{
		Orders:        3,
		Ready:         1,
		RevenueCents:  3600,
		DiscountCents: 150,
		TopIssue:      "requires cold-chain shipping",
	}, summary)
}

func TestPromotionCountsAndFormatSummary(t *testing.T) {
	counts := PromotionCounts([]pricing.Quote{
		{Applied: []string{"tea-sale", "preferred-loyalty"}},
		{Applied: []string{"tea-sale"}},
	})

	assert.Equal(t, map[string]int{
		"preferred-loyalty": 1,
		"tea-sale":          2,
	}, counts)

	rendered := FormatSummary(Summary{
		Orders:        2,
		Ready:         1,
		RevenueCents:  2250,
		DiscountCents: 375,
	})

	assert.Equal(t, "2 orders, 1 ready, revenue $22.50, discounts $3.75, top issue: none", rendered)
}
