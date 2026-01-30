package gocodecontext

import (
	"fmt"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gograph"
	"sort"
)

// FilterGroupsForIdentifiers returns the groups containing at least one of the ids. The returned slice maintains the original input groups' order. Each group appears at most once in
// the result.
func FilterGroupsForIdentifiers(groups []*IdentifierGroup, ids []string) []*IdentifierGroup {
	if len(ids) == 0 || len(groups) == 0 {
		return nil
	}

	// Build a set for quick membership checks.
	idSet := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		idSet[id] = struct{}{}
	}

	var matched []*IdentifierGroup
	for _, g := range groups {
		for _, gid := range g.IDs {
			if _, ok := idSet[gid]; ok {
				matched = append(matched, g)
				break // avoid adding the same group multiple times
			}
		}
	}
	return matched
}

// IdentifierGroup represents a set of identifiers treated as a single unit. Identifiers are grouped for two reasons:
//  1. They appear together in the same var/const/type block (ex: `var ( ... )`).
//  2. The identifiers form a strongly connected component (SCC), meaning they are cyclically dependent.
//
// These identifier groups form a graph that can be analyzed for purposes such as constructing contexts to send to an LLM.
type IdentifierGroup struct {
	IDs           []string                  // all identifiers this group describes.
	Snippets      map[string]gocode.Snippet // map of id->snippet. All IDs in this struct must be in Snippets.
	BodyTokens    int                       // number of tokens in the docs, signatures, and function bodies of the identifiers
	SnippetTokens int                       // number of tokens in the docs and signatures only (excludes function bodies)
	IsDocumented  bool                      // true if all identifiers have full documentation; external groups are always considered documented
	IsTestFile    bool                      // true if these identifiers come from test files

	// Groups directly referenced by this group (ex: a function calling another function).
	//
	// Includes both internal and external direct dependencies.
	DirectDeps []*IdentifierGroup

	// Groups that reference this group. Ex:
	//   - If a type X has methods, the methods use X. X's methods will be here.
	//   - A global variable like `var a int` has no direct dependencies, but to document it, we need to see how it's used. These uses are listed here.
	//
	// IsExternal groups will never include UsedByDeps, nor will an external group be included in UsedByDeps.
	UsedByDeps []*IdentifierGroup

	// True if this identifier group belongs to another package.
	IsExternal bool

	// If IsExternal is true, this specifies the import path of the identifier group.
	ExternalImportPath string
}

// GetSnippet returns the snippet for id. It panics if there is no such snippet.
func (g *IdentifierGroup) GetSnippet(id string) gocode.Snippet {
	snippet, ok := g.Snippets[id]
	if !ok {
		panic(fmt.Errorf("in IdentifierGroup.GetSnippet, could not find snippet for id %q", id))
	}
	return snippet
}

// AllDirectDepsDocumented reports whether all of g's direct dependencies are documented. It checks only g.DirectDeps (not transitive dependencies) and treats external groups as documented
// via their IsDocumented status.
func (g *IdentifierGroup) AllDirectDepsDocumented() bool {
	for _, dd := range g.DirectDeps {
		if !dd.IsDocumented {
			return false
		}
	}
	return true
}

// GroupOptions configures how identifier groups are constructed for a package.
type GroupOptions struct {
	IncludePackageDocs          bool // if true, include package-level documentation groups
	IncludeTestFiles            bool // if true, include identifiers and groups from test files
	IncludeExternalDeps         bool // if true, include dependencies from external packages as snippet-only groups
	ConsiderAmbiguousDocumented bool // if true, treat ambiguous identifiers (such as compiler-generated names) as documented to avoid requesting their documentation
	ConsiderTestFuncsDocumented bool // if true, treat test functions as documented, excluding them from identifiers needing documentation

	// If true, a const block with a comment above the block makes all consts documented, even if specs don't have documentation (ex: "// These consts...\nconst ( ... )").
	ConsiderConstBlocksDocumenting bool

	// CountTokens returns the token count for the relevant code bytes; if nil, a default implementation is used
	CountTokens CountTokensFunc
}

// CountTokensFunc is a function that computes the token count of the provided code bytes. It is used to estimate context cost; callers should supply a model-appropriate implementation.
type CountTokensFunc func(code []byte) int

// Groups returns identifier groups for pkg. Mod is used to look up external packages. See GroupOptions for additional input parameters.
func Groups(mod *gocode.Module, pkg *gocode.Package, options GroupOptions) ([]*IdentifierGroup, error) {
	if options.CountTokens == nil {
		options.CountTokens = defaultCountTokens
	}

	// Get a graph, so we can analyze dependencies between IDs. Slice off tests unless we want to include them.
	graph, err := gograph.NewGoGraph(pkg)
	if err != nil {
		return nil, err
	}
	if !options.IncludeTestFiles {
		graph = graph.WithoutTestIdentifiers()
	}

	// Return value:
	var groups []*IdentifierGroup

	// Step 1: Find all SCCs (strongly connected components) to group cyclic dependencies
	sccs := graph.StronglyConnectedComponents()

	// Step 2: Build a map from identifier to its containing snippet to find block-grouped identifiers
	idToSnippet := make(map[string]gocode.Snippet)
	for _, id := range graph.AllIdentifiers() {
		if snippet := pkg.GetSnippet(id); snippet != nil {
			idToSnippet[id] = snippet
		}
	}

	// Step 3: Build initial groups from SCCs
	idToGroup := make(map[string]*IdentifierGroup)
	for _, scc := range sccs {
		if len(scc) > 1 { // Only create SCC groups for actual cycles
			// Convert map[string]struct{} to []string
			var sccIds []string
			for id := range scc {
				sccIds = append(sccIds, id)
			}

			group := &IdentifierGroup{
				IDs: sccIds,
			}
			groups = append(groups, group)
			for id := range scc {
				idToGroup[id] = group
			}
		}
	}

	// Step 4: Group identifiers that are in the same var/const/type block
	processedSnippets := make(map[gocode.Snippet]bool)
	for _, snippet := range idToSnippet {
		if processedSnippets[snippet] {
			continue // Already processed this snippet
		}

		// Check if this snippet defines multiple identifiers (block declaration)
		var blockIds []string
		switch s := snippet.(type) {
		case *gocode.TypeSnippet:
			blockIds = s.Identifiers
		case *gocode.ValueSnippet:
			blockIds = s.Identifiers
		}

		if len(blockIds) > 1 {
			// Find the group that these identifiers should belong to.
			// It might be an existing SCC group, or we might need to create a new one.
			var targetGroup *IdentifierGroup
			for _, blockId := range blockIds {
				if group, ok := idToGroup[blockId]; ok {
					targetGroup = group
					break
				}
			}

			// If no existing group, create a new one.
			if targetGroup == nil {
				targetGroup = &IdentifierGroup{}
				groups = append(groups, targetGroup)
			}

			// Add all identifiers from the block to the target group.
			existingIds := make(map[string]bool)
			for _, id := range targetGroup.IDs {
				existingIds[id] = true
			}

			for _, blockId := range blockIds {
				if !existingIds[blockId] {
					targetGroup.IDs = append(targetGroup.IDs, blockId)
				}
				idToGroup[blockId] = targetGroup
			}

			processedSnippets[snippet] = true
		}
	}

	// Step 5: Create groups for remaining identifiers (each identififer in its own group)
	for _, id := range graph.AllIdentifiers() {
		if _, hasGroup := idToGroup[id]; !hasGroup {
			group := &IdentifierGroup{
				IDs: []string{id},
			}
			groups = append(groups, group)
			idToGroup[id] = group
		}
	}

	// Step 6: Sort groups' IDs for deterministic behavior
	for _, group := range groups {
		if len(group.IDs) > 1 {
			sort.Strings(group.IDs)
		}
	}

	// Step 7: Calculate token counts and documentation status for each group
	for _, group := range groups {
		// Initialize the Snippets map for this group. We do this here (rather than when the
		// group is first created) because the final list of IDs may be augmented in prior
		// steps that merge groups together. Placing the initialization here guarantees we
		// have the final, complete set of IDs for the group before populating.
		if group.Snippets == nil {
			group.Snippets = make(map[string]gocode.Snippet, len(group.IDs))
		}
		bodyTokens := 0
		snippetTokens := 0
		allDocumented := true

		// Track snippets we've already counted to avoid double-counting
		countedSnippets := make(map[gocode.Snippet]bool)

		for _, id := range group.IDs {
			snippet := idToSnippet[id]
			if snippet == nil {
				return nil, fmt.Errorf("Groups: missing snippet for identifier %q", id)
			}
			// Record the snippet in the group's Snippets map.
			group.Snippets[id] = snippet

			// Only count tokens if we haven't counted this snippet yet
			if !countedSnippets[snippet] {
				// Count snippet tokens (docs + signature)
				snippetBytes := snippet.Bytes()
				snippetTokens += options.CountTokens(snippetBytes)

				// Count full body tokens
				fullBytes := snippet.FullBytes()
				bodyTokens += options.CountTokens(fullBytes)

				countedSnippets[snippet] = true
			}

			// Compute blockDocsAllSpecs, which we pass to IDIsDocumeneted:
			// (for the ConsiderConstBlocksDocumenting option)
			blockDocsAllSpecs := false
			isConst := false
			if valueSnippet, ok := snippet.(*gocode.ValueSnippet); ok {
				isConst = !valueSnippet.IsVar
			}
			if isConst && options.ConsiderConstBlocksDocumenting {
				blockDocsAllSpecs = true
			}

			// Check if documented (do this for each identifier)
			_, fullDocs := gocode.IDIsDocumented(snippet, id, blockDocsAllSpecs)
			if !fullDocs {
				isArtificiallyDocumeneted := false

				// IMPORTANT: consider anonymous/ambiguous identifiers as documented.
				// Several things feed into this:
				//  1. We lack the technology to document them (LLM doesn't have a way (as currently prompted) to provide a snippet with file/line/col numbers, and updatedocs also lacks such a way).
				//  2. So we don't want to ask LLM to document them, and considering them documeneted is one such way.
				//  3. But also, there's an dep from PackageIdentifier -> all snippets, including these. We can only decide to document packages if all its deps are documented.
				//  4. And finally, to document a package, it IS relevant (in theory) to see init code. And these anon things ARE relevant context in "used by" reverse deps. So we want to keep them.
				//  5. Finally, ideally, an init's "snippet" bytes will be the whole function, unless the init has a comment.
				// In order to delete this, I believe one would need to let them be documented as per normal snippets, and then this wrinkle can just go away.
				if options.ConsiderAmbiguousDocumented {
					isArtificiallyDocumeneted = gocode.IsAmbiguousIdentifier(id)
				}

				// Also consider test functions (TestXxx) to be documeneted.
				// We don't want to document them, and considering them documeneted is one way.
				// I'm unsure if this is the best way. These identifiers were making their way into the "identifiers needing docs".
				if options.ConsiderTestFuncsDocumented {
					if fs, ok := snippet.(*gocode.FuncSnippet); ok && fs.IsTestFunc() {
						isArtificiallyDocumeneted = true
					}
				}

				if !isArtificiallyDocumeneted {
					allDocumented = false
				}
			}

			// See if snippet is in a test file:
			// NOTE: this occurs once per ID, which may set IsTestFile mulitple times. IsTestFile also assumes that groups cannot span test/non-test.
			if snippet.Test() {
				group.IsTestFile = true
			}
		}

		group.BodyTokens = bodyTokens
		group.SnippetTokens = snippetTokens
		group.IsDocumented = allDocumented
	}

	// Step 8: Build dependency relationships between groups
	for _, group := range groups {
		directDeps := make(map[*IdentifierGroup]bool)
		usedBy := make(map[*IdentifierGroup]bool)

		for _, id := range group.IDs {
			// Find direct dependencies (identifiers this one uses)
			if deps := graph.IdentifiersFrom(id); deps != nil {
				for _, depId := range deps {
					if depGroup := idToGroup[depId]; depGroup != nil && depGroup != group {
						directDeps[depGroup] = true
					}
				}
			}

			// Find reverse dependencies (identifiers that use this one)
			if users := graph.IdentifiersTo(id); users != nil {
				for _, userId := range users {
					if userGroup := idToGroup[userId]; userGroup != nil && userGroup != group {
						usedBy[userGroup] = true
					}
				}
			}
		}

		// Convert maps to slices. Sort for determinism:
		for dep := range directDeps {
			group.DirectDeps = append(group.DirectDeps, dep)
		}
		sort.Slice(group.DirectDeps, func(i, j int) bool {
			return group.DirectDeps[i].IDs[0] < group.DirectDeps[j].IDs[0]
		})
		for user := range usedBy {
			group.UsedByDeps = append(group.UsedByDeps, user)
		}
		sort.Slice(group.UsedByDeps, func(i, j int) bool {
			return group.UsedByDeps[i].IDs[0] < group.UsedByDeps[j].IDs[0]
		})
	}

	// Step 9: Add a synthetic group for package-level documentation (for non-test packages).
	// If we have documentation, that can be context for everything (used by for everything).
	// If we don't have documentation:
	//   - don't set used by, since there's no useful bytes to supply as context.
	//   - make sure the package-level doc group has direct deps on everything, since we want to doc everything before the package.
	// NOTE: when we add groups, we don't sort them, because it doesn't matter if they're sorted, we just want them to be deterministic (and since this is added last, they are).
	if options.IncludePackageDocs && !pkg.IsTestPackage() {
		packageGroup := &IdentifierGroup{
			IDs:      []string{gocode.PackageIdentifier},
			Snippets: make(map[string]gocode.Snippet, 1),
		}

		pkgSnippet := pkg.GetSnippet(gocode.PackageIdentifier)
		packageGroup.IsDocumented = pkgSnippet != nil
		if pkgSnippet != nil {
			packageGroup.Snippets[gocode.PackageIdentifier] = pkgSnippet
			packageGroup.BodyTokens = options.CountTokens(pkgSnippet.FullBytes())
			packageGroup.SnippetTokens = options.CountTokens(pkgSnippet.Bytes())

			// Add a usedBy dep to all other groups (even test groups if they're present):
			//
			for _, g := range groups {
				g.UsedByDeps = append(g.UsedByDeps, packageGroup)
			}
		} else {
			packageGroup.Snippets[gocode.PackageIdentifier] = &syntheticPackageSnippet{pkgName: pkg.Name}

			// Add a directDep from packageGroup -> all non-test groups g.
			for _, g := range groups {
				if !g.IsTestFile {
					packageGroup.DirectDeps = append(packageGroup.DirectDeps, g)
				}
			}
		}

		groups = append(groups, packageGroup)
	}

	// Step 10: add deps from (g in groups) -> external package identifiers, so that we can send these docs to the LLM.
	if options.IncludeExternalDeps {
		if err := addExternalGroups(mod, graph, groups, options); err != nil {
			return nil, err
		}
	}

	return groups, nil
}

// addExternalGroups adds edges to each group in internalGroups' group.DirectDeps that represent external package documentation. The mod parameter must reference a non-cloned module
// with full source code and is used to look up external packages. External groups are added only as dependencies to internalGroups - they are not appended to the groups slice, since
// we do not need to document them.
func addExternalGroups(mod *gocode.Module, graph *gograph.Graph, internalGroups []*IdentifierGroup, options GroupOptions) error {
	// Cache for created external groups to avoid redundant work.
	externalGroupsCache := make(map[gograph.ExternalID]*IdentifierGroup)

	for _, group := range internalGroups {
		// Use a map to collect unique direct snippet dependencies for the current group,
		// including pre-existing internal ones.
		depsForGroup := make(map[*IdentifierGroup]bool)
		for _, dep := range group.DirectDeps {
			depsForGroup[dep] = true
		}

		for _, id := range group.IDs {
			externalIDs := graph.ExternalIdentifiersFrom(id, false, false)

			for _, extID := range externalIDs {
				extGroup, inCache := externalGroupsCache[extID]

				if !inCache {
					// The external group is not in our cache, so we need to create it.
					otherPkg, err := mod.LoadPackageByImportPath(extID.ImportPath)
					if err == gocode.ErrImportNotInModule {
						// Mark as nil in cache to avoid reprocessing.
						externalGroupsCache[extID] = nil
						continue
					} else if err != nil {
						return fmt.Errorf("failed to load package for external ID %v: %w", extID, err)
					}

					snippet := otherPkg.GetSnippet(extID.ID)
					if snippet == nil {
						// Mark as nil in cache to avoid reprocessing.
						externalGroupsCache[extID] = nil
						continue
					}

					publicBytes, err := snippet.PublicSnippet()
					if err != nil {
						return fmt.Errorf("failed to get public snippet for %v: %w", extID, err)
					}

					extGroup = &IdentifierGroup{
						IDs:                []string{extID.ID},
						Snippets:           make(map[string]gocode.Snippet, 1),
						SnippetTokens:      options.CountTokens(publicBytes),
						IsDocumented:       true, // Assume external dependencies are documented.
						IsExternal:         true,
						ExternalImportPath: extID.ImportPath,
					}
					extGroup.Snippets[extID.ID] = snippet
					externalGroupsCache[extID] = extGroup
				}

				// Add the dependency if it was successfully created/retrieved.
				if extGroup != nil {
					depsForGroup[extGroup] = true
				}
			}
		}

		// Rebuild the directSnippetDeps slice from the map to ensure uniqueness.
		group.DirectDeps = make([]*IdentifierGroup, 0, len(depsForGroup))
		for dep := range depsForGroup {
			group.DirectDeps = append(group.DirectDeps, dep)
		}

		// Sort for deterministic behavior.
		sort.Slice(group.DirectDeps, func(i, j int) bool {
			// Sort by the first ID in the group.
			if len(group.DirectDeps[i].IDs) > 0 && len(group.DirectDeps[j].IDs) > 0 {
				return group.DirectDeps[i].IDs[0] < group.DirectDeps[j].IDs[0]
			}
			// Fallback for empty ID lists, though this shouldn't typically happen for valid groups.
			return i < j
		})
	}

	return nil
}

// defaultCountTokens estimates the token count of code; callers who know which model they are using can supply their own countTokens function for greater accuracy.
func defaultCountTokens(code []byte) int {
	return len(code) / 4
}
