// Package semver provides minimal-allocation semantic version parsing and comparison utilities.
package semver

import (
	"strconv"
	"strings"
)

// Identifier models a single pre-release identifier. Numeric identifiers are flagged via the Numeric field and expose their
// numeric value through Number.
//
// The Value field reuses the original input substring. Callers should treat it as read-only.
type Identifier struct {
	Value   string // Original substring for the identifier; aliases the input and must be treated as read-only.
	Numeric bool   // Reports whether the identifier is digits-only and valid per SemVer (no leading zeros).
	Number  uint64 // Parsed numeric value when Numeric is true; undefined otherwise.
}

// Version represents a semantic version according to the SemVer 2.0.0 specification. The zero value represents version 0.0.0.
// Use [Parse] or [ParseStrict] to construct instances.
type Version struct {
	Major uint64       // Major version number.
	Minor uint64       // Minor version number.
	Patch uint64       // Patch version number.
	pre   []Identifier // Pre-release identifiers in order; exposed via PreRelease(). Treat as read-only.
	build []string     // Build metadata identifiers in order; exposed via Build(). Treat as read-only.
}

// Compare returns an integer comparing two versions according to SemVer's precedence rules. The result will be -1 if a <
// b, 1 if a > b, and 0 if they are equal.
func Compare(a, b Version) int {
	return a.Compare(b)
}

// LessThan returns true if a < b.
func LessThan(a, b Version) bool {
	return Compare(a, b) < 0
}

// GreaterThan returns true if a > b.
func GreaterThan(a, b Version) bool {
	return Compare(a, b) > 0
}

// Compatible reports whether versions a and b should be considered compatible. Versions with different major versions are
// incompatible. When both versions are still in the 0.y.z pre-release range, compatibility additionally requires matching
// minor versions.
func Compatible(a, b Version) bool {
	return a.CompatibleWith(b)
}

// Compare returns an integer comparing the receiver to the provided version according to SemVer's precedence rules. The
// result will be -1 if v < other, 1 if v > other, and 0 if they are equal.
func (v Version) Compare(other Version) int {
	if v.Major != other.Major {
		if v.Major < other.Major {
			return -1
		}
		return 1
	}
	if v.Minor != other.Minor {
		if v.Minor < other.Minor {
			return -1
		}
		return 1
	}
	if v.Patch != other.Patch {
		if v.Patch < other.Patch {
			return -1
		}
		return 1
	}
	if len(v.pre) == 0 && len(other.pre) > 0 {
		return 1
	}
	if len(v.pre) > 0 && len(other.pre) == 0 {
		return -1
	}
	for i := 0; i < len(v.pre) && i < len(other.pre); i++ {
		l, r := v.pre[i], other.pre[i]
		if l.Numeric && r.Numeric {
			if l.Number < r.Number {
				return -1
			}
			if l.Number > r.Number {
				return 1
			}
			continue
		}
		if l.Numeric {
			return -1
		}
		if r.Numeric {
			return 1
		}
		if l.Value < r.Value {
			return -1
		}
		if l.Value > r.Value {
			return 1
		}
	}
	if len(v.pre) < len(other.pre) {
		return -1
	}
	if len(v.pre) > len(other.pre) {
		return 1
	}
	return 0
}

// Equal reports whether the receiver and the other version are identical.
func (v Version) Equal(other Version) bool {
	if v.Major != other.Major || v.Minor != other.Minor || v.Patch != other.Patch {
		return false
	}
	if len(v.pre) != len(other.pre) || len(v.build) != len(other.build) {
		return false
	}
	for i := range v.pre {
		lp, rp := v.pre[i], other.pre[i]
		if lp.Value != rp.Value || lp.Numeric != rp.Numeric || lp.Number != rp.Number {
			return false
		}
	}
	for i := range v.build {
		if v.build[i] != other.build[i] {
			return false
		}
	}
	return true
}

// LessThan reports whether v is less than other according to SemVer precedence rules.
func (v Version) LessThan(other Version) bool {
	return v.Compare(other) < 0
}

// GreaterThan reports whether v is greater than other according to SemVer precedence rules.
func (v Version) GreaterThan(other Version) bool {
	return v.Compare(other) > 0
}

// CompatibleWith reports whether the receiver should be considered compatible with the other version. Versions with different
// major versions are incompatible. When both versions are still in the 0.y.z pre-release range, compatibility additionally
// requires matching minor versions.
func (v Version) CompatibleWith(other Version) bool {
	if v.Major != other.Major {
		return false
	}
	if v.Major == 0 {
		return v.Minor == other.Minor
	}
	return true
}

// PreRelease returns the pre-release identifiers. The returned slice aliases internal storage and should be treated as read-only.
func (v Version) PreRelease() []Identifier {
	if len(v.pre) == 0 {
		return nil
	}
	return v.pre
}

// Build returns the build metadata identifiers. The returned slice aliases internal storage and should be treated as read-only.
func (v Version) Build() []string {
	if len(v.build) == 0 {
		return nil
	}
	return v.build
}

// String formats the version using canonical SemVer formatting.
func (v Version) String() string {
	var b strings.Builder
	// Minimal grow hint: major+minor+patch digits and separators.
	b.Grow(16)
	b.WriteString(strconv.FormatUint(v.Major, 10))
	b.WriteByte('.')
	b.WriteString(strconv.FormatUint(v.Minor, 10))
	b.WriteByte('.')
	b.WriteString(strconv.FormatUint(v.Patch, 10))
	if len(v.pre) > 0 {
		b.WriteByte('-')
		for i, ident := range v.pre {
			if i > 0 {
				b.WriteByte('.')
			}
			b.WriteString(ident.Value)
		}
	}
	if len(v.build) > 0 {
		b.WriteByte('+')
		for i, ident := range v.build {
			if i > 0 {
				b.WriteByte('.')
			}
			b.WriteString(ident)
		}
	}
	return b.String()
}
