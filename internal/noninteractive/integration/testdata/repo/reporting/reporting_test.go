package reporting

import (
	"reflect"
	"testing"

	orders "example.com/clarifyintegration"
	"example.com/clarifyintegration/pricing"
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

	if got := summary; !reflect.DeepEqual(got, Summary{
		Orders:        3,
		Ready:         1,
		RevenueCents:  3600,
		DiscountCents: 150,
		TopIssue:      "requires cold-chain shipping",
	}) {
		t.Fatalf("expected summary %+v, got %+v", Summary{
			Orders:        3,
			Ready:         1,
			RevenueCents:  3600,
			DiscountCents: 150,
			TopIssue:      "requires cold-chain shipping",
		}, got)
	}
}

func TestPromotionCountsAndFormatSummary(t *testing.T) {
	counts := PromotionCounts([]pricing.Quote{
		{Applied: []string{"tea-sale", "preferred-loyalty"}},
		{Applied: []string{"tea-sale"}},
	})

	if got := counts; !reflect.DeepEqual(got, map[string]int{
		"preferred-loyalty": 1,
		"tea-sale":          2,
	}) {
		t.Fatalf("expected promotion counts %v, got %v", map[string]int{
			"preferred-loyalty": 1,
			"tea-sale":          2,
		}, got)
	}

	rendered := FormatSummary(Summary{
		Orders:        2,
		Ready:         1,
		RevenueCents:  2250,
		DiscountCents: 375,
	})

	if rendered != "2 orders, 1 ready, revenue $22.50, discounts $3.75, top issue: none" {
		t.Fatalf("expected rendered summary %q, got %q", "2 orders, 1 ready, revenue $22.50, discounts $3.75, top issue: none", rendered)
	}
}
