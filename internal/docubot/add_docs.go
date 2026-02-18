package docubot

import (
	"errors"
	"fmt"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodecontext"
	"github.com/codalotl/codalotl/internal/gopackagediff"
	"github.com/codalotl/codalotl/internal/updatedocs"
	"strings"
)

// defaultTokenBudget is used when AddDocsOptions.TokenBudget is zero.
var defaultTokenBudget = 10000

// tokenBudgetExceededError contains detailed information about why the token budget was exceeded.
type tokenBudgetExceededError struct {
	RequiredTokens int // Minimum tokens needed to process the smallest viable target.
	BudgetTokens   int // Available token budget when the request was planned.
}

// Error returns a concise message describing the required and available token counts.
func (e *tokenBudgetExceededError) Error() string {
	return fmt.Sprintf("token budget exceeded: smallest target requires %d tokens, but budget is %d", e.RequiredTokens, e.BudgetTokens)
}

// Is reports whether target is ErrTokenBudgetExceeded, enabling errors.Is(err, ErrTokenBudgetExceeded) to match a tokenBudgetExceededError that carries additional
// details.
func (e *tokenBudgetExceededError) Is(target error) bool {
	return target == ErrTokenBudgetExceeded
}

// ErrTokenBudgetExceeded is a sentinel error reported when an operation would exceed the token budget. Use errors.Is(err, ErrTokenBudgetExceeded) to detect this
// condition. Some errors may carry additional details (ex: required and budgeted tokens) while still matching this sentinel.
var ErrTokenBudgetExceeded = fmt.Errorf("token budget exceeded")

// ErrTriesExceeded is returned by AddDocs when repeated attempts fail to reduce the number of undocumented identifiers.
var ErrTriesExceeded = fmt.Errorf("attempts to doc with no progress have been exceeded")

// AddDocsOptions controls how AddDocs documents identifiers in a package.
type AddDocsOptions struct {
	DocumentTestFiles  bool     // Document helpers, types, and variables in test code. TestXxx/BenchXxx/etc. functions are not documented.
	TokenBudget        int      // Maximum token budget for one LLM request (prompt + code context + instructions). Zero uses defaultTokenBudget.
	ExcludeIdentifiers []string // ExcludeIdentifiers marks identifiers as already documented, skipping them during documentation.
	BaseOptions                 // Shared configuration and dependencies (ex: model, conversationalist, logging) for LLM-backed operations.
}

// AddDocs adds documentation to undocumented identifiers in the package and returns the set of documentation changes (if DocumentTestFiles, it includes _test package
// changes if pkg has one). If an error occurs, no changes are returned, except for non-fatal errors like errNoSnippets or errSomeSnippetsFailed, where processing
// continues and changes may be returned.
func AddDocs(pkg *gocode.Package, options AddDocsOptions) ([]*gopackagediff.Change, error) {
	if options.TokenBudget == 0 {
		options.TokenBudget = defaultTokenBudget
	}

	options.Log("Entering AddDocs", "DocumentTestFiles", options.DocumentTestFiles, "TokenBudget", options.TokenBudget)

	options.ExcludeIdentifiers = appendExclusionForGeneratedFiles(options.ExcludeIdentifiers, pkg)

	// Clone current package to compute a diff for this package at the end.
	clonedPkg, err := pkg.Clone()
	if err != nil {
		return nil, options.LogWrappedErr("ensure_docs.clone", err)
	}

	// Changes from documenting a separate test package (if any).
	var testPkgChanges []*gopackagediff.Change

	// If we're documenting test files:
	//   - first document non-test files without the added weight of the test files.
	//   - then document the _test package if there is one
	//   - finally, main package test identifiers (the ordering of the _test pkg then the main package test identifiers is just one of convenience of how the code works out).
	// NOTE: For a sufficiently smart LLM with a big, usable context window, this is probably the wrong choice.
	// But with 2025-06-15 era LLMs, they can get overwhelmed with too much text, and test files can be long and verbose.
	if options.DocumentTestFiles && !pkg.IsTestPackage() {
		optionsPrime := options
		optionsPrime.DocumentTestFiles = false

		options.Log("Documenting non-test identifiers first...")
		_, err := AddDocs(pkg, optionsPrime)
		if err != nil {
			return nil, options.LogWrappedErr("error documenting non-test identifiers", err)
		}

		// reload package so it has all the latest comments:
		pkg, err = pkg.Reload()
		if err != nil {
			return nil, options.LogWrappedErr("error reloading package", err)
		}

		// Now document the _test package, if there is one:
		if pkg.HasTestPackage() {
			options.Log("Now documenting _test-package identifiers...")
			var err error
			testPkgChanges, err = AddDocs(pkg.TestPackage, options)
			if err != nil {
				return nil, options.LogWrappedErr("error documenting _test package identifiers", err)
			}

			// I don't think it's necessary to reload package here, since package shouldnt have been changed by documenting the separate test package
		}

		// Finally, document the main package test identifiers
		options.Log("Now documenting remaining test identifiers...")
	}

	idents := NewIdentifiersFromPackage(pkg)
	for _, ex := range options.ExcludeIdentifiers {
		idents.MarkDocumented(ex)
	}
	originalIdents := idents

	totalUndocumented := idents.TotalUndocumented(options.DocumentTestFiles)
	if totalUndocumented == 0 {
		// NOTE: this if statement is to prevent "Everything is already documented" from being printed 2 times, based on how EnsureDocs calls itself without test flag.
		// This is admittedly a bit of a hack.
		if !options.DocumentTestFiles {
			options.userMessagef("Everything is already documented")
		}

		return testPkgChanges, nil
	}
	options.userMessagef("Need docs for %d identifiers", totalUndocumented)

	logIdentifiersDebug(idents, options.DocumentTestFiles, options.BaseOptions)

	prevTotalUndocumented := totalUndocumented
	noProgressCount := 0
	const maxNoProgressAttempts = 2

	for {
		// addDocsPartial does one round of doc applications to pkg based on what's needed in idents and options.
		// It may decide to do big batches, or it may decide to do small targeted leaves first.
		updatedPkg, _, err := addDocsPartial(pkg, idents, options)

		// If errNoSnippets or errSomeSnippetsFailed, the LLM or the Fix probably returned something dumb, so keep on going.
		// If we keep getting errors like this, the noProgressCount failsafe will bail us out.
		if err != nil && err != errNoSnippets && err != errSomeSnippetsFailed {

			// If err is a budget error, many identifiers were possibly documeneted. If so, reflow them.
			var tokenErr *tokenBudgetExceededError
			if errors.As(err, &tokenErr) {
				if reflowErr := reflowDocumentedIdents(pkg, originalIdents, idents, options.BaseOptions); reflowErr != nil {
					return nil, options.LogWrappedErr("ensure_docs.budget.reflow", errors.Join(reflowErr, err))
				}
			}

			return nil, options.LogWrappedErr("ensure_docs.partially_document", err)
		}

		if updatedPkg != nil {
			pkg = updatedPkg
		}

		// Update idents.
		idents = NewIdentifiersFromPackage(pkg)
		for _, ex := range options.ExcludeIdentifiers {
			idents.MarkDocumented(ex)
		}
		logIdentifiersDebug(idents, options.DocumentTestFiles, options.BaseOptions)
		totalUndocumented := idents.TotalUndocumented(options.DocumentTestFiles)
		if totalUndocumented == 0 {
			options.userMessagef("Nothing left to document!")
			break
		}

		if totalUndocumented == prevTotalUndocumented {
			noProgressCount++
			if noProgressCount >= maxNoProgressAttempts {
				options.userMessagef("ERROR: Failed to make progress reducing total undocumeneted identifiers. tries: %d", noProgressCount)
				return nil, options.LogWrappedErr("ensure_docs.failed to make progress", ErrTriesExceeded, "tries", maxNoProgressAttempts, "undocumeneted_identifiers", totalUndocumented)
			}
			options.userMessagef("WARNING: No progress was made in reducing total undocumented identifiers. tries: %d", noProgressCount)
			continue // Try again with the same totalUndocumented
		}

		// Reset no-progress counter and update previous count
		noProgressCount = 0
		prevTotalUndocumented = totalUndocumented
	}

	// Finally, reflow again all modified identifiers, because there's cases where complete reflowing didn't happen yet, specifically when partial snippets are sent by LLM (ex: if there's a const block
	// but they send a single const).
	if err := reflowDocumentedIdents(pkg, originalIdents, idents, options.BaseOptions); err != nil {
		return nil, err
	}

	// Compute diff for this package and merge with test package changes (if any).
	pkgChanges, err := gopackagediff.Diff(clonedPkg, pkg, nil, nil, true)
	if err != nil {
		return nil, options.LogWrappedErr("ensure_docs.diff", err)
	}
	if len(testPkgChanges) == 0 {
		return pkgChanges, nil
	}
	merged := make([]*gopackagediff.Change, 0, len(testPkgChanges)+len(pkgChanges))
	merged = append(merged, testPkgChanges...)
	merged = append(merged, pkgChanges...)
	return merged, nil
}

// addDocsPartial performs a single documentation pass sized to the current token budget. It estimates overhead (prompt and instructions), builds an LLM context
// and target identifier list, requests snippets, applies successful updates, and returns the possibly updated package, the set of files changed, and any error.
//
// On error, the returned package is nil; updatedFiles still reflects any files modified before the error occurred. Existing docs are never overwritten (redocument=false).
func addDocsPartial(pkg *gocode.Package, idents *Identifiers, options AddDocsOptions) (*gocode.Package, map[string]struct{}, error) {

	// Reduce the token budget by the size of the prompt and instructions:
	// NOTE: instructions size isn't known until we already have identifiers to document, but newContextForDocumentation (and gocodecontext.Context's .Cost()) doesn't have knowledge of
	// instructions at all (they break an abstraction barrier). So we're going to reduce the token budget by 20 identifiers, which emperically is more than the average (but identifiers can
	// be higher - maybe 60-100 - in some cases, like documenting a bunch of constants). So this is a bit of a hack and can fail in degenerate cases.
	tokenBudget := options.TokenBudget - promptTokenLen
	const numFakeIdentifiers = 20
	fakeIdentifiers := make([]string, 0, numFakeIdentifiers)
	for range numFakeIdentifiers {
		fakeIdentifiers = append(fakeIdentifiers, "someFakeIdentifier")
	}
	fakeInstructions := llmInstructionsForIdentifiers(pkg, fakeIdentifiers, nil)
	tokenBudget -= countTokens([]byte(fakeInstructions))

	codeCtx, identifiers, err := contextForAddDocsPartial(pkg, idents, tokenBudget, options.DocumentTestFiles, options.BaseOptions)
	if err != nil {
		return nil, nil, options.LogWrappedErr("add_docs_partial.new_context_for_documentation", err)
	}

	updatedPkg, updatedFiles, err := generateAndApplyDocs(pkg, codeCtx, identifiers, false, options.BaseOptions)
	if err != nil {
		return nil, sliceToSet(updatedFiles), options.LogWrappedErr("add_docs_partial.generate_and_apply_docs", err)
	}

	return updatedPkg, sliceToSet(updatedFiles), nil
}

// contextForAddDocsPartial returns a context and identifiers to document for use in generateAndApplyDocs. If no groups initially fit the budget, it selects the
// smallest-cost group and attempts to prune. On success, it returns a non-nil context. On failure, it returns an error (ex: tokenBudgetExceededError).
func contextForAddDocsPartial(pkg *gocode.Package, idents *Identifiers, tokenBudget int, documentTestFiles bool, options BaseOptions) (*gocodecontext.Context, []string, error) {

	// Build contexts for the package:
	groupOptions := gocodecontext.GroupOptions{
		IncludePackageDocs:             true,
		IncludeTestFiles:               documentTestFiles,
		IncludeExternalDeps:            true,
		CountTokens:                    countTokens,
		ConsiderAmbiguousDocumented:    true,
		ConsiderTestFuncsDocumented:    true,
		ConsiderConstBlocksDocumenting: true,
	}
	groups, err := gocodecontext.Groups(pkg.Module, pkg, groupOptions)
	if err != nil {
		return nil, nil, options.LogWrappedErr("new_context_for_documentation.groups", err)
	}

	// Filter and sort the groups to determine the best order for documentation.
	groupsNeedingDocs := prioritizeGroupsForDocumentation(groups)

	groupsNeedingDocsSet := make(map[*gocodecontext.IdentifierGroup]bool)
	for _, g := range groupsNeedingDocs {
		groupsNeedingDocsSet[g] = true
	}

	codeCtx := gocodecontext.NewContext(nil)
	added := make(map[*gocodecontext.IdentifierGroup]bool)
	var smallestGroup *groupWithCost // Track the smallest cost group encountered
	for {
		groupAdded := false
		for _, g := range groupsNeedingDocs {
			if added[g] {
				continue
			}

			if g.AllDirectDepsDocumented() || codeCtx.HasFullBytes(g) {
				additionalCost := codeCtx.AdditionalCostForGroup(g)
				if codeCtx.Cost()+additionalCost <= tokenBudget {
					codeCtx.AddGroup(g)
					added[g] = true
					groupAdded = true

					for {
						addedGroupForFree := false
						for _, groupForFree := range codeCtx.GroupsForFree() {
							if groupsNeedingDocsSet[groupForFree] {
								codeCtx.AddGroup(groupForFree)
								added[groupForFree] = true
								addedGroupForFree = true
							}
						}
						if !addedGroupForFree {
							break
						}
					}
				} else {
					// Track the smallest cost group that we couldn't fit
					if smallestGroup == nil || additionalCost < smallestGroup.cost {
						smallestGroup = &groupWithCost{group: g, cost: additionalCost}
					}
				}
			}
		}

		if !groupAdded {
			break
		}
	}

	if len(codeCtx.AddedGroups()) == 0 {
		if smallestGroup == nil {
			return nil, nil, options.LogNewErr("new_context_for_documentation: no smallest group")
		}

		options.Log("new_context_for_documentation: smallest group too big, starting to prune", "group", strings.Join(smallestGroup.group.IDs, ","), "cost", smallestGroup.cost, "usedByDepsCount", len(smallestGroup.group.UsedByDeps))

		codeCtx.AddGroup(smallestGroup.group)
		pruneSuccessful := codeCtx.Prune(tokenBudget)
		if !pruneSuccessful {
			options.Log("new_context_for_documentation: prune failed", "cost", codeCtx.Cost())
			return nil, nil, &tokenBudgetExceededError{
				RequiredTokens: smallestGroup.cost,
				BudgetTokens:   tokenBudget,
			}
		}
		options.Log("new_context_for_documentation: prune successful", "cost", codeCtx.Cost())
	}

	// Calculate idsToDocument:
	var idsToDocument []string
	for _, g := range codeCtx.AddedGroups() {
		for _, id := range g.IDs {
			// only document an id if it doesn't have docs (groups can have a mix of doc'ed and undoc'ed ids)
			if _, ok := idents.withDocs[id]; !ok {
				// Don't document TestXxx functions, but let them be part of the context.
				snippet := g.GetSnippet(id)
				if snippet != nil {
					if fs, ok := snippet.(*gocode.FuncSnippet); ok && fs.IsTestFunc() {
						continue
					}
				}
				idsToDocument = append(idsToDocument, id)
			}
		}
	}

	return codeCtx, idsToDocument, nil
}

// reflowDocumentedIdents reflows any identifiers that gained documentation since originalIdents. It wraps text, normalizes EOL versus doc comments, and adjusts
// whitespace using options.ReflowMaxWidth. If no identifiers were newly documented, it is a no-op.
//
// On partial failures, identifiers that failed to reflow are logged, and the function still returns nil. A non-nil error is returned only when the reflow operation
// itself fails.
func reflowDocumentedIdents(pkg *gocode.Package, originalIdents *Identifiers, currentIdents *Identifiers, options BaseOptions) error {
	reflowIdents := currentIdents.DocumentedSince(originalIdents)
	if len(reflowIdents) > 0 {
		options.userMessagef("Reflowing identifiers: %s", strings.Join(reflowIdents, ", "))
		_, failedIdents, err := updatedocs.ReflowDocumentation(pkg, reflowIdents, updatedocs.Options{ReflowMaxWidth: options.effectiveReflowMaxWidth()})
		if err != nil {
			return options.LogWrappedErr("ensure_docs.reflow", err)
		}
		if len(failedIdents) > 0 {
			options.Log("failed reflow identifiers", "identifiers", strings.Join(failedIdents, ","))
		}
	}
	return nil
}

// appendExclusionForGeneratedFiles returns exclude plus all identifiers that originate from code-generated files in pkg. The result is de-duplicated and lexicographically
// sorted to keep behavior deterministic.
func appendExclusionForGeneratedFiles(exclude []string, pkg *gocode.Package) []string {
	excludeSet := sliceToSet(exclude)
	for _, file := range pkg.Files {
		if !file.IsCodeGenerated() {
			continue
		}

		for _, fs := range pkg.FuncSnippets {
			if fs.FileName == file.FileName {
				excludeSet[fs.Identifier] = struct{}{}
			}
		}

		for _, vs := range pkg.ValueSnippets {
			if vs.FileName == file.FileName {
				for _, id := range vs.Identifiers {
					excludeSet[id] = struct{}{}
				}
			}
		}

		for _, ts := range pkg.TypeSnippets {
			if ts.FileName == file.FileName {
				for _, id := range ts.Identifiers {
					excludeSet[id] = struct{}{}
				}
			}
		}

		for _, ps := range pkg.PackageDocSnippets {
			if ps.FileName == file.FileName {
				excludeSet[ps.Identifier] = struct{}{}
			}
		}
	}

	return setToSlice(excludeSet)
}
