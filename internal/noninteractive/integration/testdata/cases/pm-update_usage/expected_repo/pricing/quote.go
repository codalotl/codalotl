package pricing

import (
	"errors"

	"example.com/clarifyintegration/catalog"
)

// LineItem represents a priced quantity of a SKU.
type LineItem struct {
	SKU      string
	Quantity int
}

// Rule applies either a flat or percentage discount to products with Tag.
// An empty Tag matches every product in the order.
type Rule struct {
	Label        string
	Tag          string
	FlatOffCents int
	PercentOff   int
}

// Quote is the priced outcome for a set of line items.
type Quote struct {
	SubtotalCents int
	DiscountCents int
	TotalCents    int
	Applied       []string
}

// QuoteOrder prices an order from catalog base prices. Matching flat discounts
// are applied once per line before any percentage discounts, and discounts for
// a line are capped at that line's subtotal.
func QuoteOrder(cat *catalog.Catalog, items []LineItem, rules []Rule) (Quote, error) {
	if cat == nil {
		return Quote{}, errors.New("catalog is required")
	}

	quote := Quote{}
	applied := make(map[string]bool, len(rules))

	for _, item := range items {
		if item.Quantity <= 0 {
			return Quote{}, errors.New("line item quantities must be positive")
		}

		product, ok := cat.Lookup(item.SKU)
		if !ok {
			return Quote{}, errors.New("unknown sku: " + item.SKU)
		}

		lineSubtotal := product.BasePriceCents * item.Quantity
		quote.SubtotalCents += lineSubtotal

		matched := matchingRules(product, rules)
		remaining := lineSubtotal

		for _, rule := range matched {
			if rule.FlatOffCents <= 0 {
				continue
			}
			discount := rule.FlatOffCents
			if discount > remaining {
				discount = remaining
			}
			if discount == 0 {
				continue
			}
			remaining -= discount
			quote.DiscountCents += discount
			if !applied[rule.Label] {
				applied[rule.Label] = true
				quote.Applied = append(quote.Applied, rule.Label)
			}
		}

		for _, rule := range matched {
			if rule.PercentOff <= 0 {
				continue
			}
			discount := remaining * rule.PercentOff / 100
			if discount == 0 {
				continue
			}
			remaining -= discount
			quote.DiscountCents += discount
			if !applied[rule.Label] {
				applied[rule.Label] = true
				quote.Applied = append(quote.Applied, rule.Label)
			}
		}
	}

	quote.TotalCents = quote.SubtotalCents - quote.DiscountCents
	return quote, nil
}

func matchingRules(product catalog.Product, rules []Rule) []Rule {
	matched := make([]Rule, 0, len(rules))
	for _, rule := range rules {
		if rule.Tag == "" || product.MatchesTag(rule.Tag) {
			matched = append(matched, rule)
		}
	}
	return matched
}
