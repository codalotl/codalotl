package mypkg

import "strings"

// NormalizeName trims whitespace and title-cases the app or worker name.
func NormalizeName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	parts := strings.Fields(s)
	for i := range parts {
		if len(parts[i]) == 1 {
			parts[i] = strings.ToUpper(parts[i])
			continue
		}
		parts[i] = strings.ToUpper(parts[i][:1]) + strings.ToLower(parts[i][1:])
	}
	return strings.Join(parts, " ")
}

// Clamp clamps n between min and max inclusive.
func Clamp(n, min, max int) int {
	if min > max {
		min, max = max, min
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}
