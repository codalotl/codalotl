package inventory

import (
	"testing"

	"example.com/clarifyintegration/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAvailableClampsNegativeToZero(t *testing.T) {
	snapshot := NewSnapshot(map[string]Record{
		"tea-earl-grey": {OnHand: 2, Reserved: 5},
	})

	assert.Equal(t, 0, snapshot.Available("tea-earl-grey"))
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

	require.Len(t, reservation.Confirmed, 2)
	assert.Equal(t, 4, reservation.ConfirmedQuantity("tea-earl-grey"))
	assert.Equal(t, 1, reservation.ConfirmedQuantity("gel-pack"))
	require.Len(t, reservation.Shortages, 1)
	assert.Equal(t, Shortage{SKU: "tea-earl-grey", Requested: 5, Available: 4}, reservation.Shortages[0])
	assert.Equal(t, []string{"gel-pack"}, reservation.ColdChainSKUs)
}
