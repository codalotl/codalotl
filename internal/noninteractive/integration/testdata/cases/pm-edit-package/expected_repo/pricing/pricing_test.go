package pricing

import (
	"reflect"
	"testing"

	"example.com/clarifyintegration/catalog"
)

func TestQuoteOrderAppliesFlatBeforePercent(t *testing.T) {
	cat := catalog.New(
		catalog.Product{
			SKU:            "tea-earl-grey",
			BasePriceCents: 1200,
			Tags:           []string{"tea"},
		},
	)

	quote, err := QuoteOrder(cat, []LineItem{
		{SKU: "tea-earl-grey", Quantity: 2},
	}, []Rule{
		{Label: "tea-sale", Tag: "tea", FlatOffCents: 150},
		{Label: "preferred-loyalty", PercentOff: 10},
	})

	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if quote.SubtotalCents != 2400 {
		t.Fatalf("expected subtotal %d, got %d", 2400, quote.SubtotalCents)
	}
	if quote.DiscountCents != 375 {
		t.Fatalf("expected discount %d, got %d", 375, quote.DiscountCents)
	}
	if quote.TotalCents != 2025 {
		t.Fatalf("expected total %d, got %d", 2025, quote.TotalCents)
	}
	if !reflect.DeepEqual([]string{"tea-sale", "preferred-loyalty"}, quote.Applied) {
		t.Fatalf("expected applied rules %v, got %v", []string{"tea-sale", "preferred-loyalty"}, quote.Applied)
	}
}

func TestQuoteOrderCapsDiscountAtSubtotal(t *testing.T) {
	cat := catalog.New(
		catalog.Product{
			SKU:            "sampler",
			BasePriceCents: 100,
		},
	)

	quote, err := QuoteOrder(cat, []LineItem{
		{SKU: "sampler", Quantity: 1},
	}, []Rule{
		{Label: "oversized-flat", FlatOffCents: 250},
		{Label: "extra-percent", PercentOff: 50},
	})

	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if quote.SubtotalCents != 100 {
		t.Fatalf("expected subtotal %d, got %d", 100, quote.SubtotalCents)
	}
	if quote.DiscountCents != 100 {
		t.Fatalf("expected discount %d, got %d", 100, quote.DiscountCents)
	}
	if quote.TotalCents != 0 {
		t.Fatalf("expected total %d, got %d", 0, quote.TotalCents)
	}
}

func TestQuoteOrderReturnsZeroQuoteForNilCatalogAndEmptyItems(t *testing.T) {
	quote, err := QuoteOrder(nil, nil, nil)

	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !reflect.DeepEqual(Quote{}, quote) {
		t.Fatalf("expected zero quote, got %#v", quote)
	}
}

func TestLoyaltyTierThresholds(t *testing.T) {
	if got := LoyaltyTier(4999); got != "starter" {
		t.Fatalf("expected loyalty tier %q, got %q", "starter", got)
	}
	if got := LoyaltyTier(5000); got != "regular" {
		t.Fatalf("expected loyalty tier %q, got %q", "regular", got)
	}
	if got := LoyaltyTier(20000); got != "preferred" {
		t.Fatalf("expected loyalty tier %q, got %q", "preferred", got)
	}
	if got := FormatCents(-150); got != "-$1.50" {
		t.Fatalf("expected formatted cents %q, got %q", "-$1.50", got)
	}
}
