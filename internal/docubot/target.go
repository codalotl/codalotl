package docubot

import (
	"fmt"
	"sort"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodecontext"
)

// groupWithCost pairs an identifier group with its token cost when considering it for inclusion in an LLM context.
type groupWithCost struct {
	group *gocodecontext.IdentifierGroup // The identifier group being measured for inclusion.
	cost  int                            // Estimated tokens required to add the group to the context.
}

// prioritizeGroupsForDocumentation filters for groups with identifiers still targeted for documentation and sorts them by "easiness/helpfulness" for documentation.
// A small function that everything uses (few undocumented deps, high fan-in, small size) should come first, enabling its callers to be documented. Note: algorithms
// using this do not depend on a particular sort; better ordering can simply make them faster or pick better targets.
func prioritizeGroupsForDocumentation(groups []*gocodecontext.IdentifierGroup, idents *Identifiers) []*gocodecontext.IdentifierGroup {
	var groupsNeedingDocs []*gocodecontext.IdentifierGroup
	for _, group := range groups {
		if groupHasIdentifierNeedingDocs(group, idents) {
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
			if groupHasIdentifierNeedingDocs(dep, idents) {
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

func groupHasIdentifierNeedingDocs(group *gocodecontext.IdentifierGroup, idents *Identifiers) bool {
	if group.IsExternal {
		return false
	}
	for _, id := range group.IDs {
		if identifierNeedsDocs(group, id, idents) {
			return true
		}
	}
	return false
}

func identifierNeedsDocs(group *gocodecontext.IdentifierGroup, id string, idents *Identifiers) bool {
	if !identifierTrackedByIdentifiers(id, idents) {
		return false
	}
	if _, ok := idents.withDocs[id]; ok {
		return false
	}
	snippet := group.GetSnippet(id)
	if snippet != nil {
		if fs, ok := snippet.(*gocode.FuncSnippet); ok && fs.IsTestFunc() {
			return false
		}
	}
	return true
}

func identifierTrackedByIdentifiers(id string, idents *Identifiers) bool {
	if id == gocode.PackageIdentifier {
		return !idents.isTestPkg
	}
	for _, candidate := range idents.allFuncs {
		if candidate == id {
			return true
		}
	}
	for _, candidate := range idents.allTypes {
		if candidate == id {
			return true
		}
	}
	for _, candidate := range idents.allValues {
		if candidate == id {
			return true
		}
	}
	return false
}

func allDirectDepsDocumentedForTargets(group *gocodecontext.IdentifierGroup, idents *Identifiers) bool {
	for _, dep := range group.DirectDeps {
		if groupHasIdentifierNeedingDocs(dep, idents) {
			return false
		}
	}
	return true
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
