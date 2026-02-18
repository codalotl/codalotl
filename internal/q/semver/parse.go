package semver

import "strconv"

// ParseError describes a failure to parse a semantic version.
type ParseError struct {
	Message string // Human-readable description of the parse failure.
	Offset  int    // Byte offset of the error within the input, or -1 if no specific position applies.
}

// Internal bounds used to detect uint64 overflow while parsing numeric identifiers.
const (
	maxUint64      = ^uint64(0)     // maxUint64 is the maximum unsigned 64-bit value.
	maxUint64Div10 = maxUint64 / 10 // maxUint64Div10 equals maxUint64/10.
	maxUint64Mod10 = maxUint64 % 10 // maxUint64Mod10 equals maxUint64%10.
)

var errEmpty = &ParseError{Message: "empty input", Offset: -1} // errEmpty indicates an empty version string (or only whitespace in non-strict mode).

// Parse parses a semantic version in non-strict mode. It accepts everything allowed by [ParseStrict] and also common variations such as an optional "v" prefix,
// leading or trailing ASCII whitespace, and missing minor or patch numbers (which are treated as zero). For example, "v2" is interpreted as 2.0.0.
func Parse(input string) (Version, error) {
	return parse(input, false)
}

// ParseStrict parses a semantic version that strictly conforms to SemVer 2.0.0. The input must match MAJOR.MINOR.PATCH with optional pre-release and build metadata
// sections.
func ParseStrict(input string) (Version, error) {
	return parse(input, true)
}

// Error implements the error interface.
func (e *ParseError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Offset >= 0 {
		return "semver: " + e.Message + " at position " + strconv.Itoa(e.Offset)
	}
	return "semver: " + e.Message
}

// parse parses a semantic version from input, with behavior controlled by strict. When strict is false, it also accepts a leading 'v'/'V', leading/trailing ASCII
// whitespace, and missing minor or patch components (treated as zero). On success it returns the parsed Version; on failure it returns a *ParseError whose Offset
// indicates the failing byte.
func parse(input string, strict bool) (Version, error) {
	if input == "" {
		return Version{}, errEmpty
	}
	start := 0
	end := len(input)
	if !strict {
		for start < end && isSpace(input[start]) {
			start++
		}
		for end > start && isSpace(input[end-1]) {
			end--
		}
		if start == end {
			return Version{}, errEmpty
		}
	}
	idx := start
	if !strict {
		if idx < end && (input[idx] == 'v' || input[idx] == 'V') {
			idx++
		}
	}
	var v Version
	var err error
	v.Major, idx, err = parseNumericIdentifier(input, idx, end)
	if err != nil {
		return Version{}, err
	}
	hasMinor := false
	hasPatch := false
	if idx < end && input[idx] == '.' {
		idx++
		hasMinor = true
		v.Minor, idx, err = parseNumericIdentifier(input, idx, end)
		if err != nil {
			return Version{}, err
		}
		if idx < end && input[idx] == '.' {
			idx++
			hasPatch = true
			v.Patch, idx, err = parseNumericIdentifier(input, idx, end)
			if err != nil {
				return Version{}, err
			}
		} else {
			if strict {
				return Version{}, &ParseError{Message: "expected patch version", Offset: idx}
			}
		}
	} else {
		if strict {
			return Version{}, &ParseError{Message: "expected minor version", Offset: idx}
		}
	}
	if !hasMinor {
		v.Minor = 0
	}
	if !hasPatch {
		v.Patch = 0
	}
	if idx < end && input[idx] == '-' {
		idx++
		v.pre = v.pre[:0]
		for {
			if idx >= end {
				return Version{}, &ParseError{Message: "incomplete pre-release", Offset: idx}
			}
			startIdent := idx
			numeric := true
			leadingZero := false
			digits := 0
			var number uint64
			for idx < end {
				c := input[idx]
				if c == '.' || c == '+' {
					break
				}
				if !isAlphaNumHyphen(c) {
					return Version{}, &ParseError{Message: "invalid character in pre-release", Offset: idx}
				}
				if c >= '0' && c <= '9' {
					if digits == 0 {
						leadingZero = c == '0'
						number = uint64(c - '0')
					} else {
						if leadingZero {
							return Version{}, &ParseError{Message: "numeric identifier with leading zero", Offset: startIdent}
						}
						digit := uint64(c - '0')
						if number > maxUint64Div10 || (number == maxUint64Div10 && digit > maxUint64Mod10) {
							return Version{}, &ParseError{Message: "numeric identifier overflow", Offset: startIdent}
						}
						number = number*10 + digit
					}
					digits++
				} else {
					numeric = false
				}
				idx++
			}
			if idx == startIdent {
				return Version{}, &ParseError{Message: "empty pre-release identifier", Offset: idx}
			}
			ident := Identifier{Value: input[startIdent:idx]}
			if numeric {
				if digits > 1 && leadingZero {
					return Version{}, &ParseError{Message: "numeric identifier with leading zero", Offset: startIdent}
				}
				ident.Numeric = true
				ident.Number = number
			}
			v.pre = append(v.pre, ident)
			if idx < end && input[idx] == '.' {
				idx++
				continue
			}
			break
		}
	}
	if idx < end && input[idx] == '+' {
		idx++
		v.build = v.build[:0]
		for {
			if idx >= end {
				return Version{}, &ParseError{Message: "incomplete build metadata", Offset: idx}
			}
			startIdent := idx
			for idx < end {
				c := input[idx]
				if c == '.' {
					break
				}
				if !isAlphaNumHyphen(c) {
					return Version{}, &ParseError{Message: "invalid character in build metadata", Offset: idx}
				}
				idx++
			}
			if idx == startIdent {
				return Version{}, &ParseError{Message: "empty build identifier", Offset: idx}
			}
			v.build = append(v.build, input[startIdent:idx])
			if idx < end && input[idx] == '.' {
				idx++
				continue
			}
			break
		}
	}
	if idx != end {
		return Version{}, &ParseError{Message: "unexpected trailing data", Offset: idx}
	}
	if !strict {
		for idx < len(input) {
			if !isSpace(input[idx]) {
				break
			}
			idx++
		}
		if idx != len(input) {
			return Version{}, &ParseError{Message: "unexpected trailing data", Offset: idx}
		}
	}
	return v, nil
}

// parseNumericIdentifier parses a SemVer numeric identifier from input[idx:end]. It requires at least one digit; leading zeros are rejected except for the single
// digit "0". On success it returns the parsed value and the index of the first non-digit byte. On failure it returns a *ParseError whose Offset indicates the position
// of the problem.
func parseNumericIdentifier(input string, idx, end int) (uint64, int, error) {
	if idx >= end {
		return 0, idx, &ParseError{Message: "expected digit", Offset: idx}
	}
	if input[idx] < '0' || input[idx] > '9' {
		return 0, idx, &ParseError{Message: "expected digit", Offset: idx}
	}
	if input[idx] == '0' {
		idx++
		if idx < end && input[idx] >= '0' && input[idx] <= '9' {
			return 0, idx, &ParseError{Message: "numeric identifier with leading zero", Offset: idx - 1}
		}
		return 0, idx, nil
	}
	var n uint64
	for idx < end {
		c := input[idx]
		if c < '0' || c > '9' {
			break
		}
		digit := uint64(c - '0')
		if n > maxUint64Div10 || (n == maxUint64Div10 && digit > maxUint64Mod10) {
			return 0, idx, &ParseError{Message: "numeric identifier overflow", Offset: idx}
		}
		n = n*10 + digit
		idx++
	}
	return n, idx, nil
}

// isSpace reports whether c is an ASCII whitespace byte: ' ', '\n', '\r', '\t', '\f', or '\v'.
func isSpace(c byte) bool {
	switch c {
	case ' ', '\n', '\r', '\t', '\f', '\v':
		return true
	}
	return false
}

// isAlphaNumHyphen reports whether c is an ASCII alphanumeric character or a hyphen ('-').
func isAlphaNumHyphen(c byte) bool {
	if c >= '0' && c <= '9' {
		return true
	}
	if c >= 'a' && c <= 'z' {
		return true
	}
	if c >= 'A' && c <= 'Z' {
		return true
	}
	return c == '-'
}
