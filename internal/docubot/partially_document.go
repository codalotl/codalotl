package docubot

import (
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/updatedocs"
	"fmt"
	"sort"
	"strings"
)

// errSomeSnippetsFailed indicates that at least one generated documentation snippet could not be applied. It is non-fatal; callers should inspect the returned package and updated file
// set for any successful changes.
var errSomeSnippetsFailed = fmt.Errorf("some snippets failed")

// successfulSnippets returns the snippets that were successfully applied. A snippet is still considered successful if it was partially applied. Future improvement: if a snippet is
// fully rejected, it should not be considered successful.
func successfulSnippets(snippets []string, snippetErrors []updatedocs.SnippetError) []string {
	// Create a map of failed snippets for O(1) lookup, excluding partially rejected ones
	failedSnippets := make(map[string]struct{})
	for _, err := range snippetErrors {
		// Only consider it failed if it wasn't partially applied
		if !err.PartiallyRejected {
			failedSnippets[err.Snippet] = struct{}{}
		}
	}

	// Build list of successful snippets
	var successful []string
	for _, snippet := range snippets {
		if _, failed := failedSnippets[snippet]; !failed {
			successful = append(successful, snippet)
		}
	}

	return successful
}

// sliceToSet returns a new set containing all elements of arr, represented as map[string]struct{}. Duplicates in arr are collapsed; a nil or empty arr yields an empty, non-nil map.
func sliceToSet(arr []string) map[string]struct{} {
	set := map[string]struct{}{}

	for _, a := range arr {
		set[a] = struct{}{}
	}

	return set
}

// setToSlice returns the keys of set as a lexicographically sorted slice. Sorting ensures deterministic output across runs.
func setToSlice(set map[string]struct{}) []string {
	slice := make([]string, 0, len(set))
	for k := range set {
		slice = append(slice, k)
	}
	sort.Strings(slice) // determinism
	return slice
}

// llmInstructionsForIdentifiers returns a human-readable instruction block listing the identifiers to document, suitable for appending to the LLM request.
//
// The output begins with "Document these identifiers:" followed by one "- " line per identifier in the order provided by targetIdentifiers. If an identifier equals gocode.PackageIdentifier,
// the line targets the package doc as "package <pkg.Name> (overall package documentation)". If targetFields[id] is non-empty, the line notes the fields to document for that identifier.
func llmInstructionsForIdentifiers(pkg *gocode.Package, targetIdentifiers []string, targetFields map[string][]string) string {
	var b strings.Builder

	b.WriteString("Document these identifiers:\n")
	for _, id := range targetIdentifiers {
		b.WriteString("- ")
		if id == gocode.PackageIdentifier {
			b.WriteString("package ")
			b.WriteString(pkg.Name)
			b.WriteString(" (overall package documentation)")
		} else {
			b.WriteString(id)

			// If this type has undocumented fields, list them
			if fields, hasFields := targetFields[id]; hasFields {
				b.WriteString(" (including fields: ")
				for i, field := range fields {
					if i > 0 {
						b.WriteString(", ")
					}
					b.WriteString(field)
				}
				b.WriteString(" - documeneted on the field")
				b.WriteString(")")
			}
		}
		b.WriteString("\n")
	}

	return b.String()
}
