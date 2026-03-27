package catalog

// ProductsWithTag returns all products with tag, sorted by SKU.
func (c *Catalog) ProductsWithTag(tag string) []Product {
	if tag == "" {
		return nil
	}
	products := c.All()
	filtered := make([]Product, 0, len(products))
	for _, product := range products {
		if product.HasTag(tag) {
			filtered = append(filtered, product)
		}
	}
	return filtered
}

// TotalWeight sums the weights of the provided SKUs and ignores unknown products.
func (c *Catalog) TotalWeight(skus []string) int {
	total := 0
	for _, sku := range skus {
		product, ok := c.Lookup(sku)
		if !ok {
			continue
		}
		total += product.WeightGrams
	}
	return total
}
