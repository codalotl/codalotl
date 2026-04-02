package orders

import (
	"testing"

	"example.com/clarifyintegration/catalog"
	"example.com/clarifyintegration/inventory"
	"example.com/clarifyintegration/pricing"
)

func TestBuildPlanReturnsIssuesForShortagesAndColdShipping(t *testing.T) {
	cat := catalog.New(
		catalog.Product{
			SKU:            "tea-earl-grey",
			BasePriceCents: 1200,
			WeightGrams:    120,
			Storage:        catalog.StorageAmbient,
			Tags:           []string{"tea"},
		},
		catalog.Product{
			SKU:            "gel-pack",
			BasePriceCents: 250,
			WeightGrams:    60,
			Storage:        catalog.StorageCold,
		},
	)
	stock := inventory.NewSnapshot(map[string]inventory.Record{
		"tea-earl-grey": {OnHand: 1},
		"gel-pack":      {OnHand: 1},
	})

	plan, err := BuildPlan(cat, stock, Request{
		Customer:             Customer{ID: "cust-1", MonthlySpendCents: 25000},
		Items:                []pricing.LineItem{{SKU: "tea-earl-grey", Quantity: 2}, {SKU: "gel-pack", Quantity: 1}},
		DisallowColdShipping: true,
	}, []pricing.Rule{
		{Label: "tea-sale", Tag: "tea", FlatOffCents: 100},
	})

	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if plan.LoyaltyTier != "preferred" {
		t.Fatalf("expected loyalty tier %q, got %q", "preferred", plan.LoyaltyTier)
	}
	if len(plan.Issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(plan.Issues))
	}
	if got := plan.Reservation.ConfirmedQuantity("tea-earl-grey"); got != 1 {
		t.Fatalf("expected tea-earl-grey quantity %d, got %d", 1, got)
	}
	if got := plan.Reservation.ConfirmedQuantity("gel-pack"); got != 1 {
		t.Fatalf("expected gel-pack quantity %d, got %d", 1, got)
	}
	if plan.Quote.TotalCents != 1215 {
		t.Fatalf("expected total cents %d, got %d", 1215, plan.Quote.TotalCents)
	}
	if plan.TotalWeightGrams != 180 {
		t.Fatalf("expected total weight %d, got %d", 180, plan.TotalWeightGrams)
	}
	if plan.ReadyToShip() {
		t.Fatal("expected plan not to be ready to ship")
	}
}

func TestSummaryUsesConfirmedItems(t *testing.T) {
	cat := catalog.New(
		catalog.Product{
			SKU:            "tea-jasmine",
			BasePriceCents: 800,
			WeightGrams:    90,
			Storage:        catalog.StorageAmbient,
		},
	)
	stock := inventory.NewSnapshot(map[string]inventory.Record{
		"tea-jasmine": {OnHand: 3},
	})

	plan, err := BuildPlan(cat, stock, Request{
		Customer: Customer{ID: "cust-2", MonthlySpendCents: 6000},
		Items:    []pricing.LineItem{{SKU: "tea-jasmine", Quantity: 2}},
	}, nil)

	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !plan.ReadyToShip() {
		t.Fatal("expected plan to be ready to ship")
	}
	if plan.Summary() != "2 items, total $15.20, issues: 0" {
		t.Fatalf("expected summary %q, got %q", "2 items, total $15.20, issues: 0", plan.Summary())
	}
}
