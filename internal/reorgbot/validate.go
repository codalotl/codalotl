package reorgbot

import (
	"fmt"
	"github.com/codalotl/codalotl/internal/gocode"
	"sort"
	"strings"
)

// fileSortIsValid validates that proposedSortedIDs is a permutation of ids. It returns an error describing missing, extra, or duplicate ids if invalid.
func fileSortIsValid(ids []string, proposedSortedIDs []string) error {
	// Build expected set
	expected := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		expected[id] = struct{}{}
	}

	// Count seen and detect immediate extras
	seenCounts := make(map[string]int, len(proposedSortedIDs))
	var extra []string
	for _, id := range proposedSortedIDs {
		if _, ok := expected[id]; !ok {
			extra = append(extra, id)
		}
		seenCounts[id]++
	}

	// Missing are expected that were never seen
	var missing []string
	for id := range expected {
		if seenCounts[id] == 0 {
			missing = append(missing, id)
		}
	}

	// Duplicates are any seen with count > 1
	var duplicates []string
	for id, count := range seenCounts {
		if count > 1 {
			duplicates = append(duplicates, id)
		}
	}

	if len(missing) == 0 && len(extra) == 0 && len(duplicates) == 0 {
		return nil
	}

	sort.Strings(missing)
	sort.Strings(extra)
	sort.Strings(duplicates)

	var parts []string
	if len(missing) > 0 {
		parts = append(parts, fmt.Sprintf("missing: %v", missing))
	}
	if len(extra) > 0 {
		parts = append(parts, fmt.Sprintf("extra: %v", extra))
	}
	if len(duplicates) > 0 {
		parts = append(parts, fmt.Sprintf("duplicates: %v", duplicates))
	}
	return fmt.Errorf("invalid file sort; %s", strings.Join(parts, "; "))
}

// orgIsValid ensures the set of IDs present across all groups in org exactly matches the set of canonical IDs present in snippetsByCanonicalID. It also rejects
// duplicate assignments of the same ID to multiple groups.
//
// If isTests is true, all filenames in org must end with "_test.go". If false, none of the filenames may end with "_test.go".
func orgIsValid(org map[string][]string, snippetsByCanonicalID map[string]gocode.Snippet, isTests bool) error {
	// Expected IDs from snippets map
	expected := make(map[string]struct{}, len(snippetsByCanonicalID))
	for id := range snippetsByCanonicalID {
		expected[id] = struct{}{}
	}

	// Seen IDs from org and track duplicates
	seenCounts := make(map[string]int, len(expected))
	for k, ids := range org {
		if isTests {
			if !strings.HasSuffix(k, "_test.go") {
				return fmt.Errorf("invalid test filename: %s (must end with _test.go)", k)
			}
		} else {
			if strings.HasSuffix(k, "_test.go") {
				return fmt.Errorf("invalid non-test filename: %s (must not end with _test.go)", k)
			}
		}
		if len(ids) == 0 {
			return fmt.Errorf("empty files not allowed: %s", k)
		}
		for _, id := range ids {
			seenCounts[id]++
		}
	}

	var missing []string
	for id := range expected {
		if _, ok := seenCounts[id]; !ok {
			missing = append(missing, id)
		}
	}

	var extra []string
	for id := range seenCounts {
		if _, ok := expected[id]; !ok {
			extra = append(extra, id)
		}
	}

	var duplicates []string
	for id, count := range seenCounts {
		if count > 1 {
			duplicates = append(duplicates, id)
		}
	}

	if len(missing) == 0 && len(extra) == 0 && len(duplicates) == 0 {
		return nil
	}

	// Sort for stable error messages
	sort.Strings(missing)
	sort.Strings(extra)
	sort.Strings(duplicates)

	var parts []string
	if len(missing) > 0 {
		parts = append(parts, fmt.Sprintf("missing: %v", missing))
	}
	if len(extra) > 0 {
		parts = append(parts, fmt.Sprintf("extra: %v", extra))
	}
	if len(duplicates) > 0 {
		parts = append(parts, fmt.Sprintf("duplicates: %v", duplicates))
	}
	return fmt.Errorf("invalid organization; %s", strings.Join(parts, "; "))
}
