package termformat

import (
	"strings"

	"github.com/codalotl/codalotl/internal/q/uni"
)

// Cut removes `left` terminal-cell width from the start of `s` and `right`
// terminal-cell width from the end of `s`, returning the remaining substring.
//
// `s` must not contain newlines. `s` may contain ANSI escape sequences.
// Recognized escape sequences are not counted toward width.
//
// Width removal is grapheme-cluster-aware: if a grapheme cluster has width 2 and
// only 1 width is to be removed, the entire cluster is removed.
//
// Cut preserves ANSI SGR styling: every remaining printable grapheme is rendered
// with the same SGR state (bold/italic/underline/reverse, fg/bg color) that it
// had at that position in the original `s`, even if the SGR sequences that
// establish that state were entirely within the removed left/right portions.
//
// The returned string is self-contained: if the result ends with a non-default
// SGR state, Cut appends `ANSIReset` so styles don’t leak into subsequent output.
//
// If `left` or `right` is negative, it is treated as 0. If `left+right` removes
// all width, Cut returns "".
//
// Examples (using tags for readability; real strings contain ANSI escape codes):
//   - Cut("hello", 1, 1) -> "ell"
//   - Cut("<red>hello<reset>", 2, 1) -> "<red>ll<reset>"
//   - Cut("<red>he<reset>llo", 1, 1) -> "<red>e<reset>ll"
//   - Cut("界!", 1, 0) -> "!"
//   - Cut("界!", 2, 0) -> "!"
func Cut(s string, left, right int) string {
	if s == "" {
		return ""
	}

	if left < 0 {
		left = 0
	}
	if right < 0 {
		right = 0
	}

	clusterWidths := make([]int, 0, len(s)) // rough upper bound
	totalWidth := 0

	for i := 0; i < len(s); {
		if s[i] == '\x1b' {
			seqLen := ansiSequenceLength(s[i:])
			if seqLen == 0 {
				i++
			} else {
				i += seqLen
			}
			continue
		}

		nextEsc := strings.IndexByte(s[i:], '\x1b')
		segmentEnd := len(s)
		if nextEsc >= 0 {
			segmentEnd = i + nextEsc
		}
		segment := s[i:segmentEnd]

		iter := uni.NewGraphemeIterator(segment, nil)
		for iter.Next() {
			w := iter.TextWidth()
			clusterWidths = append(clusterWidths, w)
			totalWidth += w
		}

		i = segmentEnd
	}

	if totalWidth == 0 {
		return ""
	}

	leftDropped := 0
	leftIdx := 0
	for leftIdx < len(clusterWidths) && leftDropped < left {
		leftDropped += clusterWidths[leftIdx]
		leftIdx++
	}

	rightDropped := 0
	rightIdx := len(clusterWidths)
	for rightIdx > leftIdx && rightDropped < right {
		rightIdx--
		rightDropped += clusterWidths[rightIdx]
	}

	keepLen := totalWidth - leftDropped - rightDropped
	if keepLen <= 0 {
		return ""
	}

	split := splitLineByWidth(s, leftDropped, keepLen)

	var b strings.Builder
	b.Grow(len(split.middle) + 16) // rough guess

	if leftDropped > 0 {
		if prefix := buildStateTransition(split.startState); prefix != "" {
			b.WriteString(prefix)
		}
	}
	b.WriteString(split.middle)

	if !split.endState.isDefault() {
		b.WriteString(ANSIReset)
	}

	return b.String()
}

