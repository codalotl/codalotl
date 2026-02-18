package docubot

import (
	"github.com/codalotl/codalotl/internal/gocode"
	"math"
	"sort"
	"strings"
	"unicode/utf8"
)

// DetectReflowWidth examines code in the codebase and determines what a good value for MaxWidth is when reflowing (most [but not all] comments fit in this width).
// It returns (max width, is confident, error).
//
// If no good values can be found (e.g., we're not confident in a value), (0, false, nil) will be returned. This can happen if there's low or no data points. If
// a good value can be found, (observed max width, true, nil) will be returned.
//
// If an error occurs, (0, false, err) will be returned.
func DetectReflowWidth(pkg *gocode.Package) (int, bool, error) {
	// Approach: measure all top-level doc comments only, on a line-by-line basis.
	// Return 75% percentile, rounded to a nearby 10 (roundMaxWidthToNearbyTen).
	// NOTE: don't use indented comments (with tabs), because we don't know user's tab width
	// NOTE2: in theory we can also use end-of-line comments for unindended code

	// Use EachPackageWithIdentifiers only to iterate over package + test package:
	filterOptions := gocode.FilterIdentifiersOptionsAllNonGenerated
	var allWidths []int
	err := gocode.EachPackageWithIdentifiers(pkg, nil, filterOptions, filterOptions, func(p *gocode.Package, ids []string, onlyTests bool) error {
		ws := getPackageMaxWidths(p, onlyTests)
		if len(ws) > 0 {
			allWidths = append(allWidths, ws...)
		}
		return nil
	})
	if err != nil {
		return 0, false, err
	}

	if len(allWidths) == 0 {
		return 0, false, nil
	}

	p75 := calcPercentile(allWidths, 80)

	if len(allWidths) < 5 {
		// NOTE: we could potentially also calc the stddev for confidence.
		return 0, false, nil
	}

	return roundMaxWidthToNearbyTen(p75), true, nil
}

// calcPercentile returns the value at the given percentile (e.g., 75) from vals. If vals is empty, 0 is returned. The input slice is not modified.
func calcPercentile(vals []int, percentile int) int {
	if len(vals) == 0 {
		return 0
	}

	// Clamp percentile to [0, 100]
	if percentile < 0 {
		percentile = 0
	} else if percentile > 100 {
		percentile = 100
	}

	sort.Ints(vals)

	// Compute ceil-based index like in DetectReflowWidth, then clamp
	p := float64(percentile) / 100.0
	idx := int(math.Ceil(p*float64(len(vals)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(vals) {
		idx = len(vals) - 1
	}

	return vals[idx]
}

// getPackageMaxWidths iterates over pkg's AST and returns all comment widths for unindented doc comments
func getPackageMaxWidths(pkg *gocode.Package, onlyTests bool) []int {
	var widths []int

	// Package docs per file
	for _, ps := range pkg.PackageDocSnippets {
		if ps == nil {
			continue
		}
		if ps.Test() != onlyTests {
			continue
		}
		widths = append(widths, widthsFromDoc(ps.Doc)...)
	}

	// Function/method docs
	for _, fn := range pkg.FuncSnippets {
		if fn == nil || fn.Doc == "" {
			continue
		}
		if fn.Test() != onlyTests {
			continue
		}
		widths = append(widths, widthsFromDoc(fn.Doc)...)
	}

	// Value (var/const) docs: include block doc once and unique per-identifier docs; skip duplicates
	for _, vs := range pkg.ValueSnippets {
		if vs == nil {
			continue
		}
		if vs.Test() != onlyTests {
			continue
		}
		if vs.BlockDoc != "" {
			widths = append(widths, widthsFromDoc(vs.BlockDoc)...)
		}
		if len(vs.IdentifierDocs) > 0 {
			seen := make(map[string]struct{}, len(vs.IdentifierDocs))
			for _, doc := range vs.IdentifierDocs {
				if doc == "" {
					continue
				}
				if _, ok := seen[doc]; ok {
					continue
				}
				seen[doc] = struct{}{}
				widths = append(widths, widthsFromDoc(doc)...)
			}
		}
	}

	// Type docs: include block doc once and unique per-identifier docs; skip field docs entirely
	for _, ts := range pkg.TypeSnippets {
		if ts == nil {
			continue
		}
		if ts.Test() != onlyTests {
			continue
		}
		if ts.BlockDoc != "" {
			widths = append(widths, widthsFromDoc(ts.BlockDoc)...)
		}
		if len(ts.IdentifierDocs) > 0 {
			seen := make(map[string]struct{}, len(ts.IdentifierDocs))
			for _, doc := range ts.IdentifierDocs {
				if doc == "" {
					continue
				}
				if _, ok := seen[doc]; ok {
					continue
				}
				seen[doc] = struct{}{}
				widths = append(widths, widthsFromDoc(doc)...)
			}
		}
	}

	// Unattached top-level comments: include only non-pragma '//' style, exclude those above package clause, and filter by tests via filename
	for _, uc := range pkg.UnattachedComments {
		if uc == nil || uc.Comment == "" {
			continue
		}
		if uc.AbovePackage {
			continue // likely license or build tags; skip
		}
		isTestFile := strings.HasSuffix(uc.FileName, "_test.go")
		if isTestFile != onlyTests {
			continue
		}
		widths = append(widths, widthsFromDoc(uc.Comment)...)
	}

	return widths
}

// widthsFromDoc extracts per-line widths from a raw doc/comment string. It only considers "//" style lines, skips blank lines, pragmas ("//go:"), and lines that
// start with a tab after the slashes (code blocks), and measures the visual width as rune count after removing the leading slashes and at most one space.
func widthsFromDoc(doc string) []int {
	var widths []int
	if doc == "" {
		return widths
	}
	lines := strings.Split(doc, "\n")
	for _, line := range lines {
		trimmedLeft := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(trimmedLeft, "//") {
			continue // ignore block comments and non-comment lines
		}
		after := strings.TrimPrefix(trimmedLeft, "//")
		if strings.HasPrefix(after, "\t") {
			continue // code line inside comment; indented with tab
		}
		// Skip Go pragma lines like //go:generate
		if strings.HasPrefix(strings.TrimLeft(after, " "), "go:") {
			continue
		}
		// Remove at most one leading space
		after = strings.TrimPrefix(after, " ")
		text := strings.TrimRight(after, "\r")
		if text == "" {
			continue
		}
		widths = append(widths, utf8.RuneCountInString(text))
	}
	// If the last line is half or less of the previous line, drop it
	if len(widths) >= 2 {
		last := widths[len(widths)-1]
		prev := widths[len(widths)-2]
		if last*2 <= prev {
			widths = widths[:len(widths)-1]
		}
	}
	return widths
}

// roundMaxWidthToNearbyTen rounds width to a reasonable value divisible by 10, taking into account this is max width, which has some cultural norms. In particular,
// 80 is common and is a floor. If the value is like 81 or 82, they probably intended 80. But 89 will be rounded to 90. Besides these general guidelines, callers
// should assume any reasonable impl, but no specific rules are given.
func roundMaxWidthToNearbyTen(width int) int {

	// Clamp value between 80 and 200
	if width < 80 {
		return 80
	}
	if width > 200 {
		return 200
	}

	remainder := width % 10

	// already multiple of 10: we good
	if remainder == 0 {
		return width
	}

	// 3 or less: round down
	if remainder <= 3 {
		return width - remainder
	}

	// 4+: round up
	return width + (10 - remainder)
}
