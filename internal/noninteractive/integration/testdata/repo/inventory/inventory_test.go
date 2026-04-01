package inventory

import (
	"reflect"
	"testing"

	"example.com/clarifyintegration/catalog"
)

func TestAvailableClampsNegativeToZero(t *testing.T) {
	snapshot := NewSnapshot(map[string]Record{
		"tea-earl-grey": {OnHand: 2, Reserved: 5},
	})

	if got := snapshot.Available("tea-earl-grey"); got != 0 {
		t.Fatalf("expected available quantity %d, got %d", 0, got)
	}
}

func TestReserveAggregatesDuplicateRequestsAndCapturesColdChain(t *testing.T) {
	cat := catalog.New(
		catalog.Product{SKU: "tea-earl-grey", Storage: catalog.StorageAmbient},
		catalog.Product{SKU: "gel-pack", Storage: catalog.StorageCold},
	)
	snapshot := NewSnapshot(map[string]Record{
		"tea-earl-grey": {OnHand: 5, Reserved: 1},
		"gel-pack":      {OnHand: 1},
	})

	reservation := snapshot.Reserve(cat, []Request{
		{SKU: "tea-earl-grey", Quantity: 2},
		{SKU: "tea-earl-grey", Quantity: 3},
		{SKU: "gel-pack", Quantity: 1},
	})

	if len(reservation.Confirmed) != 2 {
		t.Fatalf("expected 2 confirmed reservations, got %d", len(reservation.Confirmed))
	}
	if got := reservation.ConfirmedQuantity("tea-earl-grey"); got != 4 {
		t.Fatalf("expected tea-earl-grey quantity %d, got %d", 4, got)
	}
	if got := reservation.ConfirmedQuantity("gel-pack"); got != 1 {
		t.Fatalf("expected gel-pack quantity %d, got %d", 1, got)
	}
	if len(reservation.Shortages) != 1 {
		t.Fatalf("expected 1 shortage, got %d", len(reservation.Shortages))
	}
	if got := reservation.Shortages[0]; got != (Shortage{SKU: "tea-earl-grey", Requested: 5, Available: 4}) {
		t.Fatalf("expected shortage %+v, got %+v", Shortage{SKU: "tea-earl-grey", Requested: 5, Available: 4}, got)
	}
	if !reflect.DeepEqual([]string{"gel-pack"}, reservation.ColdChainSKUs) {
		t.Fatalf("expected cold chain SKUs %v, got %v", []string{"gel-pack"}, reservation.ColdChainSKUs)
	}
}
