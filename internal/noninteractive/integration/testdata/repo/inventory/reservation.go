package inventory

import (
	"sort"

	"example.com/clarifyintegration/catalog"
)

// Reserve aggregates duplicate request lines, does not mutate the snapshot,
// and records non-ambient confirmed items in ColdChainSKUs.
func (s Snapshot) Reserve(cat *catalog.Catalog, requests []Request) Reservation {
	merged := mergeRequests(requests)
	reservation := Reservation{}

	for _, request := range merged {
		available := s.Available(request.SKU)
		confirmed := request.Quantity
		if confirmed > available {
			confirmed = available
		}

		if confirmed > 0 {
			reservation.Confirmed = append(reservation.Confirmed, Request{
				SKU:      request.SKU,
				Quantity: confirmed,
			})
		}
		if available < request.Quantity {
			reservation.Shortages = append(reservation.Shortages, Shortage{
				SKU:       request.SKU,
				Requested: request.Quantity,
				Available: available,
			})
		}

		if confirmed == 0 || cat == nil {
			continue
		}
		product, ok := cat.Lookup(request.SKU)
		if !ok || product.Storage == catalog.StorageAmbient {
			continue
		}
		reservation.ColdChainSKUs = appendIfMissing(reservation.ColdChainSKUs, request.SKU)
	}

	sort.Strings(reservation.ColdChainSKUs)
	return reservation
}

func mergeRequests(requests []Request) []Request {
	positions := make(map[string]int, len(requests))
	merged := make([]Request, 0, len(requests))

	for _, request := range requests {
		if request.SKU == "" || request.Quantity <= 0 {
			continue
		}

		if idx, ok := positions[request.SKU]; ok {
			merged[idx].Quantity += request.Quantity
			continue
		}

		positions[request.SKU] = len(merged)
		merged = append(merged, Request{
			SKU:      request.SKU,
			Quantity: request.Quantity,
		})
	}

	return merged
}

func appendIfMissing(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
