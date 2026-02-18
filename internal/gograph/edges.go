package gograph

import "sort"

// IdentifiersFrom returns a set of intra-package identifiers where for each returned id, there's an edge fromID -> id.
func (g *Graph) IdentifiersFrom(fromID string) []string {
	uses, exists := g.intraUses[fromID]
	if !exists {
		return nil
	}

	result := make([]string, 0, len(uses))
	for id := range uses {
		result = append(result, id)
	}
	return result
}

// IdentifiersTo returns a set of intra-package identifiers where for each returned id, there's an edge id -> toID.
func (g *Graph) IdentifiersTo(toID string) []string {
	var result []string
	for fromID, uses := range g.intraUses {
		if _, exists := uses[toID]; exists {
			result = append(result, fromID)
		}
	}
	return result
}

// ExternalIdentifiersFrom returns the set of ExternalIDs (identifiers outside the current package) that originated at fromID. If includeVendor and includeStdlib
// are false, then only edges inside the current module are returned. If includeVendor, edges to vendored code are also included. If includeStdlib, edges to the
// stdlib are also included.
func (g *Graph) ExternalIdentifiersFrom(fromID string, includeVendor bool, includeStdlib bool) []ExternalID {
	refs, exists := g.crossPackageUses[fromID]
	if !exists {
		return nil
	}

	// Build quick lookup sets for each import category.
	modSet := make(map[string]struct{})
	for _, p := range g.pkg.ImportPathsModule() {
		modSet[p] = struct{}{}
	}
	stdSet := make(map[string]struct{})
	for _, p := range g.pkg.ImportPathsStdlib() {
		stdSet[p] = struct{}{}
	}
	venSet := make(map[string]struct{})
	for _, p := range g.pkg.ImportPathsVendor() {
		venSet[p] = struct{}{}
	}

	var result []ExternalID
	for ref := range refs {
		if _, ok := modSet[ref.ImportPath]; ok {
			// Always include references within the current module.
			result = append(result, ref)
			continue
		}
		if _, ok := venSet[ref.ImportPath]; ok {
			if includeVendor {
				result = append(result, ref)
			}
			continue
		}
		if _, ok := stdSet[ref.ImportPath]; ok {
			if includeStdlib {
				result = append(result, ref)
			}
			continue
		}
		// Fallback: if the import path is not recognised, treat it as vendor.
		if includeVendor {
			result = append(result, ref)
		}
	}

	// Deterministic order: sort by import path then identifier.
	sort.Slice(result, func(i, j int) bool {
		if result[i].ImportPath == result[j].ImportPath {
			return result[i].ID < result[j].ID
		}
		return result[i].ImportPath < result[j].ImportPath
	})

	return result
}
