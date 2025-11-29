package termformat

import (
	"strings"
	"unicode/utf8"
)

const hexDigits = "0123456789ABCDEF"

// Sanitize sanitizes user input s for display in a terminal.
//   - If tabWidth > 0, it replaces \t with tabWidth spaces. Otherwise, \t is left as-is.
//   - \r and \n are left as-is.
//   - Except for above, all non-visible ASCII characters <= 0x1F and 0x7F replaced with "\\xXX" (ex: []byte{'\', 'x', '1', 'B'} for ESC).
//   - Invalid UTF-8 is replaced by U+FFFD.
func Sanitize(s string, tabWidth int) string {
	if s == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			b.WriteRune('\uFFFD')
			i++
			continue
		}
		i += size

		switch r {
		case '\t':
			if tabWidth > 0 {
				for j := 0; j < tabWidth; j++ {
					b.WriteByte(' ')
				}
			} else {
				b.WriteRune('\t')
			}
		case '\n', '\r':
			b.WriteRune(r)
		default:
			if r <= 0x7F && (r < 0x20 || r == 0x7F) {
				code := byte(r)
				b.WriteByte('\\')
				b.WriteByte('x')
				b.WriteByte(hexDigits[code>>4])
				b.WriteByte(hexDigits[code&0x0F])
				continue
			}
			b.WriteRune(r)
		}
	}

	return b.String()
}
