package orders

import (
	"testing"

	"example.com/clarifyintegration/catalog"
	"example.com/clarifyintegration/inventory"
	"example.com/clarifyintegration/pricing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	require.NoError(t, err)
	assert.Equal(t, "preferred", plan.LoyaltyTier)
	assert.Equal(t, 2, len(plan.Issues))
	assert.Equal(t, 1, plan.Reservation.ConfirmedQuantity("tea-earl-grey"))
	assert.Equal(t, 1, plan.Reservation.ConfirmedQuantity("gel-pack"))
	assert.Equal(t, 1215, plan.Quote.TotalCents)
	assert.Equal(t, 180, plan.TotalWeightGrams)
	assert.False(t, plan.ReadyToShip())
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

	require.NoError(t, err)
	assert.True(t, plan.ReadyToShip())
	assert.Equal(t, "2 items, total $15.20, issues: 0", plan.Summary())
}
