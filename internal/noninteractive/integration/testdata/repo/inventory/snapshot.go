package inventory

// Record captures the current stock state for a SKU.
type Record struct {
	OnHand   int
	Reserved int
}

// Request asks to reserve Quantity units of SKU.
type Request struct {
	SKU      string
	Quantity int
}

// Shortage describes an item that could not be fully reserved.
type Shortage struct {
	SKU       string
	Requested int
	Available int
}

// Reservation contains the reservation outcome for a batch of requests.
type Reservation struct {
	Confirmed     []Request
	Shortages     []Shortage
	ColdChainSKUs []string
}

// HasShortage reports whether any requested line could not be fully reserved.
func (r Reservation) HasShortage() bool {
	return len(r.Shortages) > 0
}

// ConfirmedQuantity returns the reserved quantity for sku.
func (r Reservation) ConfirmedQuantity(sku string) int {
	for _, request := range r.Confirmed {
		if request.SKU == sku {
			return request.Quantity
		}
	}
	return 0
}

// Snapshot is an immutable stock view used for planning.
type Snapshot struct {
	records map[string]Record
}

// NewSnapshot copies records and clamps negative values to zero.
func NewSnapshot(records map[string]Record) Snapshot {
	copied := make(map[string]Record, len(records))
	for sku, record := range records {
		if record.OnHand < 0 {
			record.OnHand = 0
		}
		if record.Reserved < 0 {
			record.Reserved = 0
		}
		copied[sku] = record
	}
	return Snapshot{records: copied}
}

// Available returns the immediately reservable quantity for sku.
func (s Snapshot) Available(sku string) int {
	record, ok := s.records[sku]
	if !ok {
		return 0
	}
	available := record.OnHand - record.Reserved
	if available < 0 {
		return 0
	}
	return available
}
