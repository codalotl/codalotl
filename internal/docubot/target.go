package docubot

import (
	"fmt"
	"github.com/codalotl/codalotl/internal/gocodecontext"
	"sort"
)

// groupWithCost pairs an identifier group with its token cost when considering it for inclusion in an LLM context.
type groupWithCost struct {
	group *gocodecontext.IdentifierGroup // The identifier group being measured for inclusion.
	cost  int                            // Estimated tokens required to add the group to the context.
}

// prioritizeGroupsForDocumentation filters for undocumented groups and sorts them by "easiness/helpfulness" for documentation. A small function that everything
// uses (few undocumented deps, high fan-in, small size) should come first, enabling its callers to be documented. Note: algorithms using this do not depend on a
// particular sort; better ordering can simply make them faster or pick better targets.
func prioritizeGroupsForDocumentation(groups []*gocodecontext.IdentifierGroup) []*gocodecontext.IdentifierGroup {
	var groupsNeedingDocs []*gocodecontext.IdentifierGroup
	for _, group := range groups {
		if !group.IsDocumented {
			groupsNeedingDocs = append(groupsNeedingDocs, group)
		}
	}

	// To avoid re-calculating sorting criteria O(n*log(n)) times, we pre-calculate them once.
	type sortableGroup struct {
		group     *gocodecontext.IdentifierGroup
		undocDeps int
		fanIn     int
	}

	sortable := make([]sortableGroup, len(groupsNeedingDocs))
	for i, group := range groupsNeedingDocs {
		undocDeps := 0
		for _, dep := range group.DirectDeps {
			if !dep.IsDocumented {
				undocDeps++
			}
		}
		sortable[i] = sortableGroup{
			group:     group,
			undocDeps: undocDeps,
			fanIn:     len(group.UsedByDeps),
		}
	}

	// Sort the groups that need documentation. Prioritize groups that are both easy and impactful:
	// - fewer undocumented dependencies (easier to document)
	// - higher fan-in (more helpful: widely used)
	// - smaller token size (cheaper)
	sort.Slice(sortable, func(i, j int) bool {
		groupA := sortable[i]
		groupB := sortable[j]

		// 1. Number of undocumented direct dependencies (ascending)
		if groupA.undocDeps != groupB.undocDeps {
			return groupA.undocDeps < groupB.undocDeps
		}

		// 2. Fan-in (number of usedByFullDeps) (descending)
		if groupA.fanIn != groupB.fanIn {
			return groupA.fanIn > groupB.fanIn
		}

		// 3. Full body cost (bodyTokens) (ascending)
		if groupA.group.BodyTokens != groupB.group.BodyTokens {
			return groupA.group.BodyTokens < groupB.group.BodyTokens
		}

		// 4. First ID for deterministic sorting (alphabetical)
		return groupA.group.IDs[0] < groupB.group.IDs[0]
	})

	// Update groupsNeedingDocs with the sorted order.
	for i, s := range sortable {
		groupsNeedingDocs[i] = s.group
	}

	return groupsNeedingDocs
}

// countTokens estimates the token count of code.
var countTokens = func(code []byte) int {
	return len(code) / 4 // placeholder calculation, can update later
}

// formatTokenCount renders tokenCount in a compact, human-readable form for logs:
//   - <1000: "N toks"
//   - 1000..under 30k: "X.Yk toks" (1 decimal)
//   - >=30k: "Xk toks" (no decimals)
//
// Rounding follows fmt's rules (ex: 1,400 -> "1.4k toks"; 30,500 -> "31k toks").
func formatTokenCount(tokenCount int) string {
	if tokenCount < 1000 {
		return fmt.Sprintf("%d toks", tokenCount)
	}

	// Convert to float for division and rounding
	k := float64(tokenCount) / 1000.0

	// Round to 1 decimal place if needed
	if k < 30 {
		return fmt.Sprintf("%.1fk toks", k)
	}

	// For larger values, use no decimal places
	return fmt.Sprintf("%.0fk toks", k)
}
