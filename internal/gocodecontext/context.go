package gocodecontext

import (
	"fmt"
	"github.com/codalotl/codalotl/internal/gocode"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A Context represents a bundle of code that can be sent to an LLM. The bundle describes K IdentifierGroups. An IdentifierGroup is described by Context if:
//   - The IdentifierGroup itself has full bytes, and
//   - All of IdentifierGroup's UsedByDeps have full bytes (so we can see how it is used in a function)
//   - All of IdentifierGroup's DirectDeps have either: A) full bytes, or B) are external and/or have documentation and partial snippet bytes (we either need a documented
//     function, or we need to see the entire function).
type Context struct {
	// originalGroups are the identifier groups explicitly added to the context via NewContext or AddGroup.
	originalGroups []*IdentifierGroup

	// groupsWithFullBytes stores 3 states per group:
	//
	//	_, ok = groupsWithFullBytes[g]; !ok -> g is not in Context at all.
	//	groupsWithFullBytes[g] == true -> Context has full bytes for g
	//	groupsWithFullBytes[g] == false -> Context has partial snippet bytes for g (be sure to check that g is in groupsWithFullBytes!)
	groupsWithFullBytes map[*IdentifierGroup]bool
}

// NewContext constructs a context describing the groups' identifiers. This context may also include other groups/identifiers, which can be checked with c.GroupsForFree().
//
// If groups need to be pruned to fit into a context budget, this should be done before creating a context.
func NewContext(groups []*IdentifierGroup) *Context {
	// TODO: if any group is external, error

	c := &Context{
		originalGroups:      groups,
		groupsWithFullBytes: calculateGroupsWithFullBytes(groups),
	}

	return c
}

// AddedGroups returns only those groups in c that were explicitly added (via NewContext or AddGroup).
func (c *Context) AddedGroups() []*IdentifierGroup {
	return c.originalGroups
}

// GroupsForFree returns groups that are referenced in DirectDeps/UsedByDeps and also have full context. In other words, they are "free" to add to a code context.
// This does NOT compute the full closure of groups for free.
//
// For example, consider len(c.AddedGroups())=1, where the group is type X. The context includes the full bodies of X's methods. If those methods are also fully
// described by X's context (ex: no one else calls them, and they only reference X), then we have full context for each of those methods. Each method for which this
// is true will be returned.
//
// Invariants:
//   - Any returned value is not in c.AddedGroups().
//   - c.Cost() does not change after adding each of c.GroupsForFree()
func (c *Context) GroupsForFree() []*IdentifierGroup {
	var groupsForFree []*IdentifierGroup

	// The only possibilities for free groups are c.hasFullBytes groups which are true.
	for g, hasFullBytes := range c.groupsWithFullBytes {
		if hasFullBytes {
			// ensure g isn't officially part of the context:
			if slices.Contains(c.originalGroups, g) {
				continue
			}

			// assume true, then set to false if we find a missing requirement. If this is still true at the end, we found a free group:
			allRequirementsMet := true

			// ensure all UsedByDeps have full bytes:
			for _, dep := range g.UsedByDeps {
				if !c.groupsWithFullBytes[dep] {
					allRequirementsMet = false
					break
				}
			}

			// ensure all DirectDeps are present. If the direct dep is undocumeneted, it also needs full bytes:
			if allRequirementsMet {
				for _, dep := range g.DirectDeps {
					hasFullBytes, present := c.groupsWithFullBytes[dep]
					if !present {
						allRequirementsMet = false
						break
					}
					if !dep.IsDocumented && !hasFullBytes {
						allRequirementsMet = false
						break
					}
				}
			}

			if allRequirementsMet {
				groupsForFree = append(groupsForFree, g)
			}
		}
	}

	return groupsForFree
}

// AllGroups returns the groups explicitly added to the context plus any additional groups that are available for free. It does not include snippet-only dependency
// groups that are part of the context's code/cost.
func (c *Context) AllGroups() []*IdentifierGroup {
	groupsForFree := c.GroupsForFree()
	allGroups := make([]*IdentifierGroup, 0, len(c.originalGroups)+len(groupsForFree))
	allGroups = append(allGroups, c.originalGroups...)
	allGroups = append(allGroups, groupsForFree...)
	return allGroups
}

// AddedIdentifiers returns all identifiers from the groups explicitly added to the context.
func (c *Context) AddedIdentifiers() []string {
	return identifiersInGroups(c.originalGroups)
}

// IdentifiersForFree returns the identifiers that c provides for free. See GroupsForFree.
func (c *Context) IdentifiersForFree() []string {
	return identifiersInGroups(c.GroupsForFree())
}

// AllIdentifiers returns all identifiers in c's groups, as well as the identifiers that c provides for free.
func (c *Context) AllIdentifiers() []string {
	return identifiersInGroups(c.AllGroups())
}

// AddGroup adds group to the context if it is not already present. If group was previously in c.GroupsForFree(), it will no longer be returned there.
func (c *Context) AddGroup(group *IdentifierGroup) {
	if slices.Contains(c.originalGroups, group) {
		return
	}
	c.originalGroups = append(c.originalGroups, group)
	c.groupsWithFullBytes = calculateGroupsWithFullBytes(c.originalGroups)
}

// Cost returns an estimate of the token count of Code(). Code() includes a few additional tokens, such as filename separators and newlines.
//
// Callers who require greater accuracy should count tokens in Code() directly.
func (c *Context) Cost() int {
	total := 0
	for g, hasFullBytes := range c.groupsWithFullBytes {
		// External groups are always included as snippets only.
		if g.IsExternal {
			total += g.SnippetTokens
			continue
		}
		if hasFullBytes {
			total += g.BodyTokens
		} else {
			total += g.SnippetTokens
		}
	}
	return total
}

// AdditionalCostForGroup returns the cost of adding a group (which is 0 if the group is already included in c or in c.GroupsForFree()).
func (c *Context) AdditionalCostForGroup(group *IdentifierGroup) int {
	if group == nil {
		return 0
	}

	// If the group was explicitly added already, no extra cost.
	if slices.Contains(c.originalGroups, group) {
		return 0
	}

	// Build a hypothetical context with the group added and return the delta in token cost.
	newOriginal := make([]*IdentifierGroup, 0, len(c.originalGroups)+1)
	newOriginal = append(newOriginal, c.originalGroups...)
	newOriginal = append(newOriginal, group)

	newCtx := NewContext(newOriginal)
	delta := newCtx.Cost() - c.Cost()
	if delta < 0 {
		return 0 // Safety guard; should not happen.
	}
	return delta
}

// HasFullBytes reports whether c contains full source ("full bytes") for group. It returns true for groups explicitly added to the context and for groups whose
// full bytes are included. It returns false for groups that are not in the context or are present only as partial snippets (docs/signatures).
func (c *Context) HasFullBytes(group *IdentifierGroup) bool {
	return slices.Contains(c.originalGroups, group) || c.groupsWithFullBytes[group]
}

// Code returns code that we can send to an LLM for the identifiers in groups. The code is grouped by source file and preserves the order in which it appears in
// the source file.
//
// Groups are the primary identifiers we need context for. The context includes:
//   - Full bodies of the groups' identifiers
//   - Full bodies of 'used by' dependencies
//   - Snippets for direct dependencies
//
// Example code (the only IdentifierGroup is a var, which has a direct dependency on a function):
//
//	// code.go:
//
//	// myFunc does...
//	func myFunc() int
//
//	// other.go:
//
//	// myVar is...
//	var myVar = myFunc()
func (c *Context) Code() string {
	var b strings.Builder

	// Gather internal snippets (non-external groups) that are part of the context.
	type snippetInfo struct {
		file      string
		position  int
		snippet   gocode.Snippet
		fullBytes bool
	}
	var snippets []*snippetInfo
	addedSnippet := make(map[gocode.Snippet]bool)

	for g, fullBytes := range c.groupsWithFullBytes {
		if g.IsExternal {
			continue // external snippets are handled separately below
		}
		for _, snip := range g.Snippets {
			if addedSnippet[snip] {
				continue
			}
			addedSnippet[snip] = true

			pos := snip.Position()
			snippets = append(snippets, &snippetInfo{
				file:      pos.Filename,
				position:  pos.Offset,
				snippet:   snip,
				fullBytes: fullBytes,
			})
		}
	}

	// Sort snippets by (file, position)
	sort.Slice(snippets, func(i, j int) bool {
		if snippets[i].file != snippets[j].file {
			return snippets[i].file < snippets[j].file
		}
		return snippets[i].position < snippets[j].position
	})

	// Write snippets grouped by file
	var wroteFile string
	for _, si := range snippets {
		if si.file != wroteFile {
			wroteFile = si.file
			b.WriteString("// ")
			b.WriteString(si.file)
			b.WriteString(":\n\n")
		}

		if si.fullBytes {
			b.Write(si.snippet.FullBytes())
		} else {
			b.Write(si.snippet.Bytes())
		}
		b.WriteString("\n\n")
	}

	// Collect external dependency snippets referenced by the original groups.
	type extSnipInfo struct {
		importPath string
		snippet    gocode.Snippet
	}

	var extSnippets []*extSnipInfo
	addedExt := make(map[gocode.Snippet]bool)

	for _, g := range c.originalGroups {
		for _, dep := range g.DirectDeps {
			if !dep.IsExternal {
				continue
			}
			for _, snip := range dep.Snippets {
				if addedExt[snip] {
					continue
				}
				addedExt[snip] = true

				extSnippets = append(extSnippets, &extSnipInfo{
					importPath: dep.ExternalImportPath,
					snippet:    snip,
				})
			}
		}
	}

	if len(extSnippets) > 0 {
		// Sort by (importPath, first identifier of snippet)
		sort.Slice(extSnippets, func(i, j int) bool {
			if extSnippets[i].importPath != extSnippets[j].importPath {
				return extSnippets[i].importPath < extSnippets[j].importPath
			}
			idsI := extSnippets[i].snippet.IDs()
			idsJ := extSnippets[j].snippet.IDs()
			var idI, idJ string
			if len(idsI) > 0 {
				idI = idsI[0]
			}
			if len(idsJ) > 0 {
				idJ = idsJ[0]
			}
			return idI < idJ
		})

		b.WriteString("//\n")
		b.WriteString("// Select documentation from dependency packages:\n")
		b.WriteString("//\n\n")

		var wroteImport string
		for _, si := range extSnippets {
			if si.importPath != wroteImport {
				wroteImport = si.importPath
				b.WriteString("// ")
				b.WriteString(si.importPath)
				b.WriteString(":\n\n")
			}

			publicBytes, err := si.snippet.PublicSnippet()
			if err != nil {
				panic(fmt.Errorf("Code: could not get public snippet for %s in %s: %w", si.snippet.IDs()[0], si.importPath, err))
			}
			b.Write(publicBytes)
			b.WriteString("\n\n")
		}
	}

	return b.String()
}

// Prune will mutate c and c's groups so that c.Cost() <= tokenBudget. It returns true if successful, or false if the prune failed. If the prune failed, c might
// still be mutated.
//
// Prune will make a best-effort attempt to preserve context's utility using a variety of techniques. For example:
//   - Remove "used by" deps, up to a limit.
//   - If c is the package documentation, we will cut private symbols.
func (c *Context) Prune(tokenBudget int) bool {
	// Already fits:
	if c.Cost() <= tokenBudget {
		return true
	}

	// If c is package-level documentation, we aggressively trim its dependencies
	// to those that expose at least one exported identifier. This reduces the
	// context to public-facing API only.
	if len(c.originalGroups) == 1 && strings.Join(c.originalGroups[0].IDs, ",") == gocode.PackageIdentifier {
		pkgGroup := c.originalGroups[0]

		// Keep only deps that have at least one exported identifier.
		var kept []*IdentifierGroup
		for _, dep := range pkgGroup.DirectDeps {
			if groupHasExportedIdentifier(dep) {
				kept = append(kept, dep)
			}
		}
		pkgGroup.DirectDeps = kept

		// Recompute inclusion map and check budget again.
		c.groupsWithFullBytes = calculateGroupsWithFullBytes(c.originalGroups)
		return c.Cost() <= tokenBudget
	}

	// General case: Cut UsedBy deps that exit c.originalGroups in a breadth-first
	// manner, one at a time, until we meet the budget or no further safe cuts are
	// possible. We do not reduce below a per-group minimum to keep some usage
	// context for each group.
	const minPrunedUsedByDeps = 2

	// Quick lookup for membership in the explicitly added groups.
	originalSet := make(map[*IdentifierGroup]bool, len(c.originalGroups))
	for _, g := range c.originalGroups {
		originalSet[g] = true
	}

	// Helper to select a removable UsedBy dep for a group. Prefer the heaviest
	// group (by BodyTokens) to maximize savings.
	pickRemovable := func(g *IdentifierGroup) (int, *IdentifierGroup) {
		// Build a list of indices of candidates that are not in original groups.
		type cand struct {
			idx  int
			grp  *IdentifierGroup
			cost int
		}
		var candidates []cand
		for i, ub := range g.UsedByDeps {
			if originalSet[ub] {
				continue
			}
			// External groups should never appear in UsedByDeps per invariants,
			// but guard and treat their cost as snippet tokens if present.
			estCost := ub.BodyTokens
			if ub.IsExternal {
				estCost = ub.SnippetTokens
			}
			candidates = append(candidates, cand{idx: i, grp: ub, cost: estCost})
		}

		// Enforce the minimum retained UsedBy deps.
		if len(candidates) <= 0 || len(g.UsedByDeps)-1 < minPrunedUsedByDeps {
			return -1, nil
		}

		// Prefer to drop the heaviest usage first.
		sort.Slice(candidates, func(i, j int) bool { return candidates[i].cost > candidates[j].cost })
		return candidates[0].idx, candidates[0].grp
	}

	// Breadth-first style: in each round, remove at most one UsedBy dep per
	// original group, then re-evaluate cost.
	for {
		if c.Cost() <= tokenBudget {
			return true
		}

		madeProgress := false

		for _, g := range c.originalGroups {
			// Find a candidate to remove for this group.
			rmIdx, _ := pickRemovable(g)
			if rmIdx < 0 {
				continue
			}

			// Remove the candidate from g.UsedByDeps.
			g.UsedByDeps = append(g.UsedByDeps[:rmIdx], g.UsedByDeps[rmIdx+1:]...)

			// Recompute inclusion decisions after the mutation.
			c.groupsWithFullBytes = calculateGroupsWithFullBytes(c.originalGroups)
			madeProgress = true

			if c.Cost() <= tokenBudget {
				return true
			}
		}

		if !madeProgress {
			break
		}
	}

	// Failed to fit within budget.
	return false
}

// groupHasExportedIdentifier reports whether the group contains at least one exported identifier. For compound identifiers like "X.B", both the receiver and the
// member must be exported to count as exported API.
func groupHasExportedIdentifier(g *IdentifierGroup) bool {
	for _, id := range g.IDs {
		if isExportedID(id) {
			return true
		}
	}
	return false
}

// isExportedID returns true if the identifier is exported. For method/field style identifiers containing a dot, both sides must start with an uppercase letter to
// be treated as exported.
func isExportedID(id string) bool {
	if dot := strings.IndexByte(id, '.'); dot >= 0 {
		recv := id[:dot]
		member := id[dot+1:]
		rRecv, _ := utf8.DecodeRuneInString(recv)
		rMember, _ := utf8.DecodeRuneInString(member)
		return unicode.IsUpper(rRecv) && unicode.IsUpper(rMember)
	}
	r, _ := utf8.DecodeRuneInString(id)
	return unicode.IsUpper(r)
}

// identifiersInGroups returns all identifiers in the provided groups.
func identifiersInGroups(groups []*IdentifierGroup) []string {
	var ids []string

	for _, g := range groups {
		ids = append(ids, g.IDs...)
	}

	return ids
}

// calculateGroupsWithFullBytes returns a map indicating which IdentifierGroups should be included in the context and for each, whether full bytes (true) or only
// snippet bytes (false) are required.
//
// The rules follow those in NewContext:
//   - Every group provided to the context, along with all its UsedByDeps, must appear with full bytes.
//   - Each DirectDep must also be included. If the dependency is external or already documented, only a snippet is needed; otherwise, full bytes are required.
func calculateGroupsWithFullBytes(groups []*IdentifierGroup) map[*IdentifierGroup]bool {
	groupsWithFullBytes := make(map[*IdentifierGroup]bool)

	// First pass: ensure all groups themselves and their UsedByDeps have full bytes.
	for _, g := range groups {
		groupsWithFullBytes[g] = true
		for _, dep := range g.UsedByDeps {
			groupsWithFullBytes[dep] = true
		}
	}

	// Second pass: evaluate DirectDeps.
	for _, g := range groups {
		groupsWithFullBytes[g] = true
		for _, dep := range g.DirectDeps {
			if dep.IsExternal || dep.IsDocumented {
				// Dependency only needs snippet bytes unless it was already marked as requiring full bytes.
				if _, ok := groupsWithFullBytes[dep]; !ok {
					groupsWithFullBytes[dep] = false
				}
			} else {
				// Undocumented internal dependency needs full bytes.
				groupsWithFullBytes[dep] = true
			}
		}
	}

	return groupsWithFullBytes
}
