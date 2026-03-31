package orders

import (
	"errors"
	"fmt"

	"example.com/clarifyintegration/catalog"
	"example.com/clarifyintegration/inventory"
	"example.com/clarifyintegration/pricing"
)

// Customer captures the requester's loyalty data.
type Customer struct {
	ID                string
	MonthlySpendCents int
}

// Request is the input for order planning.
type Request struct {
	Customer             Customer
	Items                []pricing.LineItem
	DisallowColdShipping bool
}

// Issue is a non-fatal planning concern that blocks shipment.
type Issue struct {
	SKU     string
	Message string
}

// Plan is the combined reservation and pricing result for a request.
type Plan struct {
	LoyaltyTier      string
	Reservation      inventory.Reservation
	Quote            pricing.Quote
	Issues           []Issue
	TotalWeightGrams int
}

// BuildPlan validates the request, reserves available stock, and prices only
// the confirmed quantities. Unknown SKUs and invalid quantities are returned as
// errors; stock shortages and cold-shipping conflicts are recorded in Issues.
func BuildPlan(cat *catalog.Catalog, stock inventory.Snapshot, req Request, baseRules []pricing.Rule) (Plan, error) {
	if cat == nil {
		return Plan{}, errors.New("catalog is required")
	}
	if len(req.Items) == 0 {
		return Plan{}, errors.New("request must contain at least one item")
	}

	inventoryRequests := make([]inventory.Request, 0, len(req.Items))
	for _, item := range req.Items {
		if item.Quantity <= 0 {
			return Plan{}, fmt.Errorf("sku %q has non-positive quantity", item.SKU)
		}
		if _, ok := cat.Lookup(item.SKU); !ok {
			return Plan{}, fmt.Errorf("unknown sku %q", item.SKU)
		}
		inventoryRequests = append(inventoryRequests, inventory.Request{
			SKU:      item.SKU,
			Quantity: item.Quantity,
		})
	}

	reservation := stock.Reserve(cat, inventoryRequests)

	confirmedItems := make([]pricing.LineItem, 0, len(reservation.Confirmed))
	weightSKUs := make([]string, 0)
	for _, confirmed := range reservation.Confirmed {
		confirmedItems = append(confirmedItems, pricing.LineItem{
			SKU:      confirmed.SKU,
			Quantity: confirmed.Quantity,
		})
		for range confirmed.Quantity {
			weightSKUs = append(weightSKUs, confirmed.SKU)
		}
	}

	loyaltyTier := pricing.LoyaltyTier(req.Customer.MonthlySpendCents)
	rules := append([]pricing.Rule(nil), baseRules...)
	switch loyaltyTier {
	case "preferred":
		rules = append(rules, pricing.Rule{Label: "preferred-loyalty", PercentOff: 10})
	case "regular":
		rules = append(rules, pricing.Rule{Label: "regular-loyalty", PercentOff: 5})
	}

	quote, err := pricing.QuoteOrder(cat, confirmedItems, rules)
	if err != nil {
		return Plan{}, err
	}

	issues := make([]Issue, 0, len(reservation.Shortages)+len(reservation.ColdChainSKUs))
	for _, shortage := range reservation.Shortages {
		issues = append(issues, Issue{
			SKU:     shortage.SKU,
			Message: fmt.Sprintf("requested %d but only %d available", shortage.Requested, shortage.Available),
		})
	}
	if req.DisallowColdShipping {
		for _, sku := range reservation.ColdChainSKUs {
			issues = append(issues, Issue{
				SKU:     sku,
				Message: "requires cold-chain shipping",
			})
		}
	}

	return Plan{
		LoyaltyTier:      loyaltyTier,
		Reservation:      reservation,
		Quote:            quote,
		Issues:           issues,
		TotalWeightGrams: cat.TotalWeight(weightSKUs),
	}, nil
}

// ReadyToShip reports whether the plan has no blocking issues.
func (p Plan) ReadyToShip() bool {
	return len(p.Issues) == 0
}

// Summary renders a compact human-readable order summary.
func (p Plan) Summary() string {
	totalItems := 0
	for _, confirmed := range p.Reservation.Confirmed {
		totalItems += confirmed.Quantity
	}
	return fmt.Sprintf("%d items, total %s, issues: %d", totalItems, pricing.FormatCents(p.Quote.TotalCents), len(p.Issues))
}
