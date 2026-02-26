package applypatch

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// Replace replaces findText with replacementText in absPath (which must be an absolute path). It edits the file in place. If edits are made, the new file's contents
// are returned. If replaceAll is true, multiple replacements are made.
//
// A variety of heuristics are used to match findText. Replace does not merely do strict string replacement.
//
// An error is returned if:
//   - invalid inputs (ex: path is not absolute; findText is empty)
//   - file I/O errors and other Go errors
//   - if replaceAll is false and there are ambiguous matches at the selected heuristic level. IsInvalidPatch(err) will return true.
//   - if findText could not be found. IsInvalidPatch(err) will return true.
func Replace(absPath string, findText string, replacementText string, replaceAll bool) (string, error) {
	if !filepath.IsAbs(absPath) {
		return "", fmt.Errorf("absPath must be absolute: %q", absPath)
	}
	if findText == "" {
		return "", fmt.Errorf("findText must not be empty")
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}
	original := string(data)
	type level struct {
		name       string
		finder     func(content, find string) []textMatch
		nearSearch bool
	}
	levels := []level{
		{name: "literal", finder: findLiteralMatches},
		{name: "newline-normalized", finder: findNewlineNormalizedMatches},
		{name: "unicode-normalized", finder: findUnicodeNormalizedMatches},
		{name: "indentation-normalized", finder: findIndentationNormalizedMatches},
		{name: "horizontal-whitespace-relaxed", finder: findWhitespaceRelaxedMatches},
		{name: "near", finder: findNearMatches, nearSearch: true},
	}
	var (
		matches      []textMatch
		selectedName string
		nearLevel    bool
	)
	for _, level := range levels {
		matches = dedupeAndSortMatches(level.finder(original, findText))
		if len(matches) == 0 {
			continue
		}
		selectedName = level.name
		nearLevel = level.nearSearch
		break
	}
	if len(matches) == 0 {
		return "", invalidPatchError(fmt.Errorf("findText not found"))
	}
	if replaceAll {
		matches = selectNonOverlapping(matches)
	} else {
		if nearLevel {
			winner, ok := pickClearNearWinner(matches)
			if !ok {
				return "", invalidPatchError(fmt.Errorf("findText is ambiguous at %s level (%d matches)", selectedName, len(matches)))
			}
			matches = []textMatch{winner}
		} else if len(matches) > 1 {
			return "", invalidPatchError(fmt.Errorf("findText is ambiguous at %s level (%d matches)", selectedName, len(matches)))
		}
	}
	newline := preferredNewlineStyle(original)
	replacement := adaptReplacementNewlines(replacementText, newline)
	updated := applyTextMatches(original, matches, replacement)
	updated = preserveFinalNewlineStyle(original, updated, findText, replacementText, newline)
	if err := os.WriteFile(absPath, []byte(updated), 0o644); err != nil {
		return "", err
	}
	return updated, nil
}

type textMatch struct {
	Start int
	End   int
	Score float64
}

func dedupeAndSortMatches(matches []textMatch) []textMatch {
	if len(matches) == 0 {
		return nil
	}
	type key struct {
		start int
		end   int
	}
	best := make(map[key]textMatch, len(matches))
	for _, m := range matches {
		if m.End <= m.Start {
			continue
		}
		k := key{start: m.Start, end: m.End}
		if existing, ok := best[k]; !ok || m.Score > existing.Score {
			best[k] = m
		}
	}
	out := make([]textMatch, 0, len(best))
	for _, m := range best {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Start != out[j].Start {
			return out[i].Start < out[j].Start
		}
		if out[i].End != out[j].End {
			return out[i].End < out[j].End
		}
		return out[i].Score > out[j].Score
	})
	return out
}
func selectNonOverlapping(matches []textMatch) []textMatch {
	if len(matches) < 2 {
		return matches
	}
	out := make([]textMatch, 0, len(matches))
	nextStart := 0
	for _, m := range matches {
		if m.Start < nextStart {
			continue
		}
		out = append(out, m)
		nextStart = m.End
	}
	return out
}
func pickClearNearWinner(matches []textMatch) (textMatch, bool) {
	if len(matches) == 0 {
		return textMatch{}, false
	}
	if len(matches) == 1 {
		return matches[0], true
	}
	ordered := append([]textMatch(nil), matches...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Score != ordered[j].Score {
			return ordered[i].Score > ordered[j].Score
		}
		if ordered[i].Start != ordered[j].Start {
			return ordered[i].Start < ordered[j].Start
		}
		return ordered[i].End < ordered[j].End
	})
	top := ordered[0]
	second := ordered[1]
	if top.Score >= 0.96 && top.Score-second.Score >= 0.04 {
		return top, true
	}
	return textMatch{}, false
}
func applyTextMatches(content string, matches []textMatch, replacement string) string {
	if len(matches) == 0 {
		return content
	}
	var b strings.Builder
	b.Grow(len(content) + len(matches)*len(replacement))
	pos := 0
	for _, m := range matches {
		if m.Start < pos || m.End > len(content) {
			continue
		}
		b.WriteString(content[pos:m.Start])
		b.WriteString(replacement)
		pos = m.End
	}
	b.WriteString(content[pos:])
	return b.String()
}
func preferredNewlineStyle(content string) string {
	if strings.Contains(content, "\r\n") {
		return "\r\n"
	}
	return "\n"
}
func adaptReplacementNewlines(replacement string, preferred string) string {
	if preferred != "\r\n" {
		return replacement
	}
	if strings.Contains(replacement, "\r\n") {
		return replacement
	}
	if !strings.Contains(replacement, "\n") {
		return replacement
	}
	return strings.ReplaceAll(replacement, "\n", "\r\n")
}
func preserveFinalNewlineStyle(original string, updated string, findText string, replacementText string, preferred string) string {
	origFinal := hasFinalNewline(original)
	updatedFinal := hasFinalNewline(updated)
	if origFinal == updatedFinal {
		return updated
	}
	if hasFinalNewline(findText) || hasFinalNewline(replacementText) {
		return updated
	}
	if origFinal {
		return updated + preferred
	}
	return trimOneFinalNewline(updated)
}
func hasFinalNewline(s string) bool {
	return strings.HasSuffix(s, "\n")
}
func trimOneFinalNewline(s string) string {
	if strings.HasSuffix(s, "\r\n") {
		return s[:len(s)-2]
	}
	if strings.HasSuffix(s, "\n") {
		return s[:len(s)-1]
	}
	return s
}
func findLiteralMatches(content, find string) []textMatch {
	return directFindMatches(content, find)
}
func findNewlineNormalizedMatches(content, find string) []textMatch {
	return findUsingNormalizer(content, find, normalizeNewlinesOnly)
}
func findUnicodeNormalizedMatches(content, find string) []textMatch {
	return findUsingNormalizer(content, find, normalizeUnicodeAndInvisible)
}
func findWhitespaceRelaxedMatches(content, find string) []textMatch {
	return findUsingNormalizer(content, find, normalizeWhitespaceRelaxed)
}
func directFindMatches(content, find string) []textMatch {
	if find == "" {
		return nil
	}
	var matches []textMatch
	for offset := 0; offset <= len(content)-len(find); {
		idx := strings.Index(content[offset:], find)
		if idx < 0 {
			break
		}
		start := offset + idx
		matches = append(matches, textMatch{Start: start, End: start + len(find), Score: 1})
		offset = start + 1
	}
	return matches
}

type normalizedText struct {
	text   string
	starts []int
	ends   []int
}

func (n normalizedText) rawRange(start, end int) (int, int, bool) {
	if start < 0 || end > len(n.text) || start >= end {
		return 0, 0, false
	}
	return n.starts[start], n.ends[end-1], true
}
func findUsingNormalizer(content, find string, normalize func(string) normalizedText) []textMatch {
	h := normalize(content)
	n := normalize(find)
	if n.text == "" || len(n.text) > len(h.text) {
		return nil
	}
	var matches []textMatch
	for offset := 0; offset <= len(h.text)-len(n.text); {
		idx := strings.Index(h.text[offset:], n.text)
		if idx < 0 {
			break
		}
		startNorm := offset + idx
		endNorm := startNorm + len(n.text)
		startRaw, endRaw, ok := h.rawRange(startNorm, endNorm)
		if ok {
			matches = append(matches, textMatch{Start: startRaw, End: endRaw, Score: 1})
		}
		offset = startNorm + 1
	}
	return matches
}
func normalizeNewlinesOnly(s string) normalizedText {
	out := make([]byte, 0, len(s))
	starts := make([]int, 0, len(s))
	ends := make([]int, 0, len(s))
	for i := 0; i < len(s); {
		if s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n' {
			out = append(out, '\n')
			starts = append(starts, i)
			ends = append(ends, i+2)
			i += 2
			continue
		}
		out = append(out, s[i])
		starts = append(starts, i)
		ends = append(ends, i+1)
		i++
	}
	return normalizedText{text: string(out), starts: starts, ends: ends}
}
func normalizeUnicodeAndInvisible(s string) normalizedText {
	return normalizeWithOptions(s, false)
}
func normalizeWhitespaceRelaxed(s string) normalizedText {
	return normalizeWithOptions(s, true)
}
func normalizeWithOptions(s string, collapseHorizontalWhitespace bool) normalizedText {
	out := make([]byte, 0, len(s))
	starts := make([]int, 0, len(s))
	ends := make([]int, 0, len(s))
	appendByte := func(ch byte, start, end int) {
		out = append(out, ch)
		starts = append(starts, start)
		ends = append(ends, end)
	}
	for i := 0; i < len(s); {
		if s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n' {
			appendByte('\n', i, i+2)
			i += 2
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			appendByte(s[i], i, i+1)
			i++
			continue
		}
		if isInvisibleRune(r) {
			i += size
			continue
		}
		if collapseHorizontalWhitespace && isHorizontalWhitespaceRune(r) {
			runStart := i
			runEnd := i + size
			i += size
			for i < len(s) {
				if s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n' {
					break
				}
				nextRune, nextSize := utf8.DecodeRuneInString(s[i:])
				if nextRune == utf8.RuneError && nextSize == 1 {
					if s[i] == ' ' || s[i] == '\t' {
						runEnd = i + 1
						i++
						continue
					}
					break
				}
				if isInvisibleRune(nextRune) {
					i += nextSize
					continue
				}
				if !isHorizontalWhitespaceRune(nextRune) {
					break
				}
				runEnd = i + nextSize
				i += nextSize
			}
			appendByte(' ', runStart, runEnd)
			continue
		}
		if mapped, ok := unicodeReplaceRune(r); ok {
			appendByte(mapped, i, i+size)
			i += size
			continue
		}
		for k := 0; k < size; k++ {
			appendByte(s[i+k], i+k, i+k+1)
		}
		i += size
	}
	return normalizedText{text: string(out), starts: starts, ends: ends}
}
func unicodeReplaceRune(r rune) (byte, bool) {
	switch r {
	case '—', '–', '―':
		return '-', true
	case '“', '”':
		return '"', true
	case '‘', '’':
		return '\'', true
	case '•', '·':
		return '*', true
	case '×':
		return 'x', true
	case '…':
		return '.', true
	case '\u00A0', '\u1680', '\u2000', '\u2001', '\u2002', '\u2003', '\u2004', '\u2005', '\u2006', '\u2007', '\u2008', '\u2009', '\u200A', '\u202F', '\u205F', '\u3000':
		return ' ', true
	}
	return 0, false
}
func isInvisibleRune(r rune) bool {
	switch r {
	case '\u200B', '\u200C', '\u200D', '\u2060', '\uFEFF':
		return true
	}
	return false
}
func isHorizontalWhitespaceRune(r rune) bool {
	if r == ' ' || r == '\t' {
		return true
	}
	_, mapped := unicodeReplaceRune(r)
	return mapped && r != '\n' && r != '\r'
}

type lineSegment struct {
	text       string
	start      int
	end        int
	hasNewline bool
}

func findIndentationNormalizedMatches(content, find string) []textMatch {
	findNorm := strings.ReplaceAll(find, "\r\n", "\n")
	if !strings.Contains(findNorm, "\n") {
		return nil
	}
	if findNorm == "" {
		return nil
	}
	hayNorm := normalizeNewlinesOnly(content)
	findLines, findHasFinal := splitLines(findNorm)
	hayLines, _ := splitLines(hayNorm.text)
	if len(findLines) == 0 || len(findLines) > len(hayLines) {
		return nil
	}
	var matches []textMatch
	for i := 0; i+len(findLines) <= len(hayLines); i++ {
		candidate := hayLines[i : i+len(findLines)]
		if !linesMatchUniformIndent(findLines, candidate) {
			continue
		}
		startNorm := candidate[0].start
		endNorm := candidate[len(candidate)-1].end
		if findHasFinal {
			last := candidate[len(candidate)-1]
			if !last.hasNewline {
				continue
			}
			endNorm++
		}
		startRaw, endRaw, ok := hayNorm.rawRange(startNorm, endNorm)
		if !ok {
			continue
		}
		matches = append(matches, textMatch{Start: startRaw, End: endRaw, Score: 1})
	}
	return matches
}
func splitLines(s string) ([]lineSegment, bool) {
	if s == "" {
		return nil, false
	}
	lines := make([]lineSegment, 0, strings.Count(s, "\n")+1)
	start := 0
	for start < len(s) {
		if idx := strings.IndexByte(s[start:], '\n'); idx >= 0 {
			end := start + idx
			lines = append(lines, lineSegment{
				text:       s[start:end],
				start:      start,
				end:        end,
				hasNewline: true,
			})
			start = end + 1
			continue
		}
		lines = append(lines, lineSegment{
			text:       s[start:],
			start:      start,
			end:        len(s),
			hasNewline: false,
		})
		start = len(s)
	}
	return lines, strings.HasSuffix(s, "\n")
}
func linesMatchUniformIndent(expected []lineSegment, candidate []lineSegment) bool {
	if len(expected) != len(candidate) || len(expected) == 0 {
		return false
	}
	type delta struct {
		mode int
		text string
		set  bool
	}
	var want delta
	for i := range expected {
		eIndent, eRest := splitIndent(expected[i].text)
		cIndent, cRest := splitIndent(candidate[i].text)
		if eRest != cRest {
			return false
		}
		mode := 0
		diff := ""
		switch {
		case cIndent == eIndent:
			mode = 0
		case strings.HasPrefix(cIndent, eIndent):
			mode = 1
			diff = cIndent[len(eIndent):]
		case strings.HasPrefix(eIndent, cIndent):
			mode = -1
			diff = eIndent[len(cIndent):]
		default:
			return false
		}
		if !want.set {
			want = delta{mode: mode, text: diff, set: true}
			continue
		}
		if want.mode != mode || want.text != diff {
			return false
		}
	}
	return true
}
func findNearMatches(content, find string) []textMatch {
	h := normalizeWhitespaceRelaxed(content)
	n := normalizeWhitespaceRelaxed(find)
	target := n.text
	if len(target) < 4 || len(target) == 0 || len(h.text) == 0 {
		return nil
	}
	const maxDist = 2
	minLen := len(target) - maxDist
	if minLen < 1 {
		minLen = 1
	}
	maxLen := len(target) + maxDist
	type key struct {
		start int
		end   int
	}
	best := make(map[key]textMatch)
	for start := 0; start+minLen <= len(h.text); start++ {
		for l := minLen; l <= maxLen && start+l <= len(h.text); l++ {
			candidate := h.text[start : start+l]
			dist := levenshteinWithin(target, candidate, maxDist)
			if dist > maxDist {
				continue
			}
			denom := len(target)
			if l > denom {
				denom = l
			}
			score := 1 - (float64(dist) / float64(denom))
			if score < 0.92 {
				continue
			}
			startRaw, endRaw, ok := h.rawRange(start, start+l)
			if !ok {
				continue
			}
			k := key{start: startRaw, end: endRaw}
			m := textMatch{Start: startRaw, End: endRaw, Score: score}
			if existing, ok := best[k]; !ok || m.Score > existing.Score {
				best[k] = m
			}
		}
	}
	out := make([]textMatch, 0, len(best))
	for _, m := range best {
		out = append(out, m)
	}
	return out
}
func levenshteinWithin(a, b string, maxDist int) int {
	if abs(len(a)-len(b)) > maxDist {
		return maxDist + 1
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := 0; j <= len(b); j++ {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		rowMin := cur[0]
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			del := prev[j] + 1
			ins := cur[j-1] + 1
			sub := prev[j-1] + cost
			cur[j] = min3(del, ins, sub)
			if cur[j] < rowMin {
				rowMin = cur[j]
			}
		}
		if rowMin > maxDist {
			return maxDist + 1
		}
		prev, cur = cur, prev
	}
	if prev[len(b)] > maxDist {
		return maxDist + 1
	}
	return prev[len(b)]
}
func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
