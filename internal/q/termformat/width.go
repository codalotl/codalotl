package termformat

import "github.com/codalotl/codalotl/internal/q/uni"

// TextWidthWithANSICodes returns the text width of str for monospace fonts in terminals while ignoring ANSI codes. Ex: color formatting codes don't
// contribute to the width and so are ignored. In other words, if rendered to a terminal, how many cells does str occupy?
func TextWidthWithANSICodes(str string) int {
	if str == "" {
		return 0
	}

	width := 0
	segmentStart := 0

	for i := 0; i < len(str); {
		if str[i] != '\x1b' {
			i++
			continue
		}

		if segmentStart < i {
			width += uni.TextWidth(str[segmentStart:i], nil)
		}

		seqLen := ansiSequenceLength(str[i:])
		if seqLen == 0 {
			i++
		} else {
			i += seqLen
		}
		segmentStart = i
	}

	if segmentStart < len(str) {
		width += uni.TextWidth(str[segmentStart:], nil)
	}

	return width
}

func ansiSequenceLength(s string) int {
	if len(s) == 0 || s[0] != '\x1b' {
		return 0
	}
	if len(s) == 1 {
		return 1
	}

	switch s[1] {
	case '[':
		for i := 2; i < len(s); i++ {
			final := s[i]
			if final >= 0x40 && final <= 0x7e { // Final byte of a CSI sequence
				return i + 1
			}
		}
		return 0
	case ']':
		for i := 2; i < len(s); i++ {
			if s[i] == '\a' { // BEL terminator
				return i + 1
			}
			if s[i] == '\\' && s[i-1] == '\x1b' { // ST terminator (ESC \)
				return i + 1
			}
		}
		return 0
	case 'P', '^', '_':
		for i := 2; i < len(s); i++ {
			if s[i] == '\\' && s[i-1] == '\x1b' {
				return i + 1
			}
		}
		return 0
	default:
		return 2 // ESC followed by a single-character control sequence
	}
}
