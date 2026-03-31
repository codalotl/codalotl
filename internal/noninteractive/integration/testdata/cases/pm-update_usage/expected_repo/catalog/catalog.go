package catalog

import (
	"fmt"
	"sort"
)

// StorageClass describes the shipping/storage constraints for a product.
type StorageClass string

const (
	StorageAmbient StorageClass = "ambient"
	StorageCold    StorageClass = "cold"
	StorageFrozen  StorageClass = "frozen"
)

// Product describes a sellable item in the catalog.
type Product struct {
	SKU            string
	Name           string
	BasePriceCents int
	WeightGrams    int
	Storage        StorageClass
	Tags           []string
}

// MatchesTag reports whether the product includes tag.
func (p Product) MatchesTag(tag string) bool {
	for _, existing := range p.Tags {
		if existing == tag {
			return true
		}
	}
	return false
}

// Catalog is an in-memory product index keyed by SKU.
type Catalog struct {
	products map[string]Product
}

// New constructs a catalog and normalizes tag slices for stable lookups.
func New(products ...Product) *Catalog {
	index := make(map[string]Product, len(products))
	for _, product := range products {
		copied := product
		copied.Tags = normalizedTags(product.Tags)
		index[product.SKU] = copied
	}
	return &Catalog{products: index}
}

// Lookup returns a defensive copy of the product for sku.
func (c *Catalog) Lookup(sku string) (Product, bool) {
	if c == nil {
		return Product{}, false
	}
	product, ok := c.products[sku]
	if !ok {
		return Product{}, false
	}
	product.Tags = append([]string(nil), product.Tags...)
	return product, true
}

// MustLookup returns the product for sku and panics when sku does not exist.
func (c *Catalog) MustLookup(sku string) Product {
	product, ok := c.Lookup(sku)
	if !ok {
		panic(fmt.Sprintf("catalog: unknown sku %q", sku))
	}
	return product
}

// All returns every product sorted by SKU.
func (c *Catalog) All() []Product {
	if c == nil {
		return nil
	}
	skus := make([]string, 0, len(c.products))
	for sku := range c.products {
		skus = append(skus, sku)
	}
	sort.Strings(skus)

	products := make([]Product, 0, len(skus))
	for _, sku := range skus {
		products = append(products, c.MustLookup(sku))
	}
	return products
}

func normalizedTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}

	seen := make(map[string]bool, len(tags))
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}
