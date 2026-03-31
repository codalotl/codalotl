package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLookupReturnsDefensiveCopy(t *testing.T) {
	cat := New(Product{
		SKU:            "tea-earl-grey",
		Name:           "Earl Grey",
		BasePriceCents: 1299,
		WeightGrams:    120,
		Storage:        StorageAmbient,
		Tags:           []string{"tea", "black-tea"},
	})

	product, ok := cat.Lookup("tea-earl-grey")
	require.True(t, ok)

	product.Tags[0] = "changed"

	again, ok := cat.Lookup("tea-earl-grey")
	require.True(t, ok)
	assert.Equal(t, []string{"black-tea", "tea"}, again.Tags)
}

func TestProductsWithTagSortedBySKU(t *testing.T) {
	cat := New(
		Product{SKU: "b", Name: "B", Tags: []string{"tea"}},
		Product{SKU: "a", Name: "A", Tags: []string{"tea"}},
		Product{SKU: "c", Name: "C", Tags: []string{"coffee"}},
	)

	products := cat.ProductsWithTag("tea")

	require.Len(t, products, 2)
	assert.Equal(t, "a", products[0].SKU)
	assert.Equal(t, "b", products[1].SKU)
}

func TestTotalWeightIgnoresMissingSKUs(t *testing.T) {
	cat := New(
		Product{SKU: "tea-earl-grey", WeightGrams: 120},
		Product{SKU: "tea-jasmine", WeightGrams: 90},
	)

	total := cat.TotalWeight([]string{"tea-earl-grey", "missing", "tea-jasmine"})

	assert.Equal(t, 210, total)
}
