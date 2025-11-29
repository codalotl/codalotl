package gocodecontext

// NewContextsForIdentifiers returns a map that groups identifiers by context:
//   - Identifiers are grouped together when the same context describes all of them.
//   - All value slices are disjoint (each identifier appears in only one context).
//   - All identifiers are included in a value slice, unless an identifier is not in groups (in which case, it is ignored).
//
// Since finding an optimal solution is NP-hard (see Set-Cover/Exact-Cover), this function creates a minimal context for the first identifier, adds any other identifiers that are free,
// then creates a new context for the next unhandled identifier, and so on.
func NewContextsForIdentifiers(groups []*IdentifierGroup, identifiers []string) map[*Context][]string {
	// map id -> group
	idToGroup := make(map[string]*IdentifierGroup)
	for _, g := range groups {
		for _, id := range g.IDs {
			idToGroup[id] = g
		}
	}

	// Track identifiers that are still not assigned a context.
	remaining := make(map[string]struct{}, len(identifiers))
	for _, id := range identifiers {
		remaining[id] = struct{}{}
	}

	// Return value:
	contexts := make(map[*Context][]string)

	// Iterate through the identifiers in the order they were provided.
	for _, id := range identifiers {

		// Already handled by a previous context:
		if _, ok := remaining[id]; !ok {
			continue
		}

		grp := idToGroup[id]

		// Ignore ids not in group:
		if grp == nil {
			delete(remaining, id)
			continue
		}

		// Start a new context with grp.
		ctx := NewContext([]*IdentifierGroup{grp})

		// Determine which of the remaining identifiers are covered by ctx.
		coveredSet := make(map[string]struct{})
		for _, cid := range ctx.AllIdentifiers() {
			coveredSet[cid] = struct{}{}
		}

		covered := []string{id}
		delete(remaining, id)

		for _, cand := range identifiers {
			if _, stillRemaining := remaining[cand]; !stillRemaining {
				continue
			}
			if _, ok := coveredSet[cand]; ok {
				covered = append(covered, cand)
				delete(remaining, cand)
			}
		}

		contexts[ctx] = covered
	}

	return contexts
}
