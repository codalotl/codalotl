package catalog

import (
	"reflect"
	"testing"
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
	if !ok {
		t.Fatal("expected product lookup to succeed")
	}

	product.Tags[0] = "changed"

	again, ok := cat.Lookup("tea-earl-grey")
	if !ok {
		t.Fatal("expected product lookup to succeed")
	}
	if !reflect.DeepEqual([]string{"black-tea", "tea"}, again.Tags) {
		t.Fatalf("expected tags %v, got %v", []string{"black-tea", "tea"}, again.Tags)
	}
}

func TestProductsWithTagSortedBySKU(t *testing.T) {
	cat := New(
		Product{SKU: "b", Name: "B", Tags: []string{"tea"}},
		Product{SKU: "a", Name: "A", Tags: []string{"tea"}},
		Product{SKU: "c", Name: "C", Tags: []string{"coffee"}},
	)

	products := cat.ProductsWithTag("tea")

	if len(products) != 2 {
		t.Fatalf("expected 2 products, got %d", len(products))
	}
	if products[0].SKU != "a" {
		t.Fatalf("expected first SKU %q, got %q", "a", products[0].SKU)
	}
	if products[1].SKU != "b" {
		t.Fatalf("expected second SKU %q, got %q", "b", products[1].SKU)
	}
}

func TestProductsWithTagEmptyTagReturnsNil(t *testing.T) {
	cat := New(
		Product{SKU: "a", Name: "A", Tags: []string{"", "tea"}},
		Product{SKU: "b", Name: "B"},
	)

	products := cat.ProductsWithTag("")

	if products != nil {
		t.Fatalf("expected nil products, got %v", products)
	}
}

func TestTotalWeightIgnoresMissingSKUs(t *testing.T) {
	cat := New(
		Product{SKU: "tea-earl-grey", WeightGrams: 120},
		Product{SKU: "tea-jasmine", WeightGrams: 90},
	)

	total := cat.TotalWeight([]string{"tea-earl-grey", "missing", "tea-jasmine"})

	if total != 210 {
		t.Fatalf("expected total weight %d, got %d", 210, total)
	}
}
