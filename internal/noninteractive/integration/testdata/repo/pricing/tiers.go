package pricing

import "fmt"

// LoyaltyTier groups customers by recent spend for coarse discount rules.
func LoyaltyTier(monthlySpendCents int) string {
	switch {
	case monthlySpendCents >= 20000:
		return "preferred"
	case monthlySpendCents >= 5000:
		return "regular"
	default:
		return "starter"
	}
}

// FormatCents renders cents in stable dollar notation.
func FormatCents(cents int) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return fmt.Sprintf("%s$%d.%02d", sign, cents/100, cents%100)
}
