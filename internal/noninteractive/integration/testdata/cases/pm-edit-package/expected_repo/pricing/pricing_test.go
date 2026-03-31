package pricing

import (
	"testing"

	"example.com/clarifyintegration/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	require.NoError(t, err)
	assert.Equal(t, 2400, quote.SubtotalCents)
	assert.Equal(t, 375, quote.DiscountCents)
	assert.Equal(t, 2025, quote.TotalCents)
	assert.Equal(t, []string{"tea-sale", "preferred-loyalty"}, quote.Applied)
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

	require.NoError(t, err)
	assert.Equal(t, 100, quote.SubtotalCents)
	assert.Equal(t, 100, quote.DiscountCents)
	assert.Equal(t, 0, quote.TotalCents)
}

func TestQuoteOrderReturnsZeroQuoteForNilCatalogAndEmptyItems(t *testing.T) {
	quote, err := QuoteOrder(nil, nil, nil)

	require.NoError(t, err)
	assert.Equal(t, Quote{}, quote)
}

func TestLoyaltyTierThresholds(t *testing.T) {
	assert.Equal(t, "starter", LoyaltyTier(4999))
	assert.Equal(t, "regular", LoyaltyTier(5000))
	assert.Equal(t, "preferred", LoyaltyTier(20000))
	assert.Equal(t, "-$1.50", FormatCents(-150))
}
