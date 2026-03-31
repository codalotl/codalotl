# catalog

In-memory product catalog used by the integration fixture repo.

## Public API

```go
type StorageClass string

const (
	StorageAmbient StorageClass = "ambient"
	StorageCold    StorageClass = "cold"
	StorageFrozen  StorageClass = "frozen"
)

type Product struct {
	SKU            string
	Name           string
	BasePriceCents int
	WeightGrams    int
	Storage        StorageClass
	Tags           []string
}

type Catalog struct{}

func New(products ...Product) *Catalog
func (c *Catalog) Lookup(sku string) (Product, bool)
func (c *Catalog) Count() int
func (c *Catalog) ProductsWithTag(tag string) []Product
func (c *Catalog) TotalWeight(skus []string) int
```
