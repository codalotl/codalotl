package docubot

import (
	"bytes"
	"path/filepath"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodecontext"
	"github.com/codalotl/codalotl/internal/gopackagediff"
	"github.com/codalotl/codalotl/internal/updatedocs"
)

type importantIdentifierPolicy struct {
	BigFunctionSourceLines int
	GroupFanInThreshold    int
	GroupFanOutThreshold   int
}

var defaultImportantIdentifierPolicy = importantIdentifierPolicy{
	BigFunctionSourceLines: 20,
	GroupFanInThreshold:    10,
	GroupFanOutThreshold:   12,
}

func addDocsOnlyDocumentImportantIdentifiers(pkg *gocode.Package, options AddDocsOptions, contextModule *gocode.Module, allowTokenBudgetExpansion bool) ([]*gopackagediff.Change, error) {
	importantIDs, importantUndocumented, err := importantIdentifiersNeedingDocs(pkg, options.DocumentTestFiles, options, contextModule)
	if err != nil {
		return nil, options.LogWrappedErr("ensure_docs.important_only.important_identifiers", err)
	}

	var testImportantIDs map[string]struct{}
	if options.DocumentTestFiles && !pkg.IsTestPackage() && pkg.HasTestPackage() {
		var testImportantUndocumented int
		testImportantIDs, testImportantUndocumented, err = importantIdentifiersNeedingDocs(pkg.TestPackage, true, options, contextModule)
		if err != nil {
			return nil, options.LogWrappedErr("ensure_docs.important_only.test_important_identifiers", err)
		}
		importantUndocumented += testImportantUndocumented
	}

	if importantUndocumented == 0 {
		options.userMessagef("Everything important is already documented")
		return nil, nil
	}

	clonedPkg, err := pkg.Clone()
	if err != nil {
		return nil, options.LogWrappedErr("ensure_docs.clone", err)
	}
	defer clonedPkg.Module.DeleteClone()

	scratchPkg, err := pkg.Clone()
	if err != nil {
		return nil, options.LogWrappedErr("ensure_docs.important_only.scratch_clone", err)
	}
	defer scratchPkg.Module.DeleteClone()

	scratchOptions := options
	scratchOptions.OnlyDocumentImportantIdentifiers = false
	scratchOptions.ExcludeIdentifiers = importantScratchExclusions(options.ExcludeIdentifiers, scratchPkg, importantIDs)
	if options.DocumentTestFiles && scratchPkg.HasTestPackage() {
		scratchImportantIDs := make(map[string]struct{}, len(importantIDs)+len(testImportantIDs))
		for identifier := range importantIDs {
			scratchImportantIDs[identifier] = struct{}{}
		}
		for identifier := range testImportantIDs {
			scratchImportantIDs[identifier] = struct{}{}
		}
		scratchOptions.ExcludeIdentifiers = importantScratchExclusions(scratchOptions.ExcludeIdentifiers, scratchPkg.TestPackage, scratchImportantIDs)
	}
	if contextModule == nil {
		contextModule = pkg.Module
	}

	_, err = addDocs(scratchPkg, scratchOptions, contextModule, allowTokenBudgetExpansion)
	if err != nil {
		return nil, options.LogWrappedErr("ensure_docs.important_only.scratch_add_docs", err)
	}

	scratchPkg, err = reloadPackageDiscoveringNewFiles(scratchPkg)
	if err != nil {
		return nil, options.LogWrappedErr("ensure_docs.important_only.scratch_reload", err)
	}

	pkg, err = applyImportantDocsFromScratchPackage(pkg, scratchPkg, importantIDs, options, options.DocumentTestFiles, "important-only application of scratch snippets")
	if err != nil {
		return nil, err
	}

	if options.DocumentTestFiles && pkg.HasTestPackage() && scratchPkg.HasTestPackage() {
		testPkg, err := applyImportantDocsFromScratchPackage(pkg.TestPackage, scratchPkg.TestPackage, testImportantIDs, options, true, "important-only application of scratch _test snippets")
		if err != nil {
			return nil, err
		}
		pkg.TestPackage = testPkg
	}

	pkgChanges, err := gopackagediff.Diff(clonedPkg, pkg, nil, nil, true)
	if err != nil {
		return nil, options.LogWrappedErr("ensure_docs.important_only.diff", err)
	}

	if !options.DocumentTestFiles || !clonedPkg.HasTestPackage() || !pkg.HasTestPackage() {
		return pkgChanges, nil
	}

	testPkgChanges, err := gopackagediff.Diff(clonedPkg.TestPackage, pkg.TestPackage, nil, nil, true)
	if err != nil {
		return nil, options.LogWrappedErr("ensure_docs.important_only.test_diff", err)
	}
	if len(testPkgChanges) == 0 {
		return pkgChanges, nil
	}
	merged := make([]*gopackagediff.Change, 0, len(testPkgChanges)+len(pkgChanges))
	merged = append(merged, testPkgChanges...)
	merged = append(merged, pkgChanges...)
	return merged, nil
}

func importantIdentifiersNeedingDocs(pkg *gocode.Package, includeTest bool, options AddDocsOptions, contextModule *gocode.Module) (map[string]struct{}, int, error) {
	importantIDs, err := importantIdentifiersForPackage(pkg, includeTest, contextModule, options.BaseOptions)
	if err != nil {
		return nil, 0, err
	}
	removeExcludedImportantIdentifiers(importantIDs, options.ExcludeIdentifiers, pkg)

	idents := NewIdentifiersFromPackage(pkg)
	for _, identifier := range appendExclusionForGeneratedFiles(options.ExcludeIdentifiers, pkg) {
		idents.MarkDocumented(identifier)
	}
	return importantIDs, totalImportantUndocumented(idents, importantIDs, includeTest), nil
}

func needsImportantDocs(pkg *gocode.Package, options AddDocsOptions) (bool, error) {
	needsDocs, err := needsImportantDocsForPackage(pkg, options.DocumentTestFiles, options, nil)
	if err != nil {
		return false, options.LogWrappedErr("needs_docs.important_identifiers", err)
	}
	if needsDocs {
		return true, nil
	}

	if options.DocumentTestFiles && !pkg.IsTestPackage() && pkg.HasTestPackage() {
		needsDocs, err := needsImportantDocsForPackage(pkg.TestPackage, true, options, nil)
		if err != nil {
			return false, options.LogWrappedErr("needs_docs.test_important_identifiers", err)
		}
		return needsDocs, nil
	}

	return false, nil
}

func needsImportantDocsForPackage(pkg *gocode.Package, includeTest bool, options AddDocsOptions, contextModule *gocode.Module) (bool, error) {
	ids := NewIdentifiersFromPackage(pkg)
	markImportantStatusExclusionsDocumented(ids, pkg, options.ExcludeIdentifiers)

	staticImportant, _ := defaultImportantIdentifierPolicy.staticIdentifiersFromIDs(pkg, ids, includeTest)
	removeExcludedImportantIdentifiers(staticImportant, options.ExcludeIdentifiers, pkg)
	if totalImportantUndocumented(ids, staticImportant, includeTest) > 0 {
		return true, nil
	}

	graphCandidates := graphImportantCandidatesNeedingDocs(ids, staticImportant, includeTest)
	if len(graphCandidates) == 0 {
		return false, nil
	}

	if contextModule == nil {
		contextModule = pkg.Module
	}
	groups, err := gocodecontext.Groups(contextModule, pkg, gocodecontext.GroupOptions{
		IncludePackageDocs:             true,
		IncludeTestFiles:               includeTest,
		IncludeExternalDeps:            true,
		CountTokens:                    countTokens,
		ConsiderAmbiguousDocumented:    true,
		ConsiderTestFuncsDocumented:    true,
		ConsiderConstBlocksDocumenting: true,
	})
	if err != nil {
		return false, options.LogWrappedErr("needs_docs.important_identifiers.groups", err)
	}
	for _, group := range groups {
		if group.IsExternal || !defaultImportantIdentifierPolicy.importantGroup(group) {
			continue
		}
		for _, identifier := range group.IDs {
			if _, needsDocs := graphCandidates[identifier]; needsDocs {
				return true, nil
			}
		}
	}

	return false, nil
}

func markImportantStatusExclusionsDocumented(ids *Identifiers, pkg *gocode.Package, excluded []string) {
	for _, identifier := range appendExclusionForGeneratedFiles(excluded, pkg) {
		ids.MarkDocumented(identifier)
	}

	excludedSet := sliceToSet(excluded)
	for _, fn := range pkg.FuncSnippets {
		if _, excluded := excludedSet[leadingIdentifier(fn.IndirectedReceiverType())]; excluded {
			ids.MarkDocumented(fn.Identifier)
		}
	}
}

func graphImportantCandidatesNeedingDocs(ids *Identifiers, staticImportant map[string]struct{}, includeTest bool) map[string]struct{} {
	candidates := make(map[string]struct{})
	addIfNeedsDocs := func(identifier string) {
		if _, alreadyImportant := staticImportant[identifier]; alreadyImportant {
			return
		}
		if !ids.includeIdentifier(identifier, includeTest, false) {
			return
		}
		if _, hasDocs := ids.withDocs[identifier]; !hasDocs {
			candidates[identifier] = struct{}{}
		}
	}

	for _, identifier := range ids.allFuncs {
		addIfNeedsDocs(identifier)
	}
	for _, identifier := range ids.allTypes {
		addIfNeedsDocs(identifier)
	}
	for _, identifier := range ids.allValues {
		addIfNeedsDocs(identifier)
	}

	return candidates
}

func importantIdentifiersForPackage(pkg *gocode.Package, includeTest bool, contextModule *gocode.Module, options BaseOptions) (map[string]struct{}, error) {
	return defaultImportantIdentifierPolicy.identifiers(pkg, includeTest, contextModule, options)
}

func (p importantIdentifierPolicy) identifiers(pkg *gocode.Package, includeTest bool, contextModule *gocode.Module, options BaseOptions) (map[string]struct{}, error) {
	if contextModule == nil {
		contextModule = pkg.Module
	}

	ids := NewIdentifiersFromPackage(pkg)
	important, generatedIDs := p.staticIdentifiersFromIDs(pkg, ids, includeTest)
	add := func(identifier string) {
		if _, generated := generatedIDs[identifier]; generated {
			return
		}
		important[identifier] = struct{}{}
	}
	addIfIncluded := func(identifier string) {
		if ids.includeIdentifier(identifier, includeTest, false) {
			add(identifier)
		}
	}

	if !pkg.IsTestPackage() {
		add(gocode.PackageIdentifier)
	}

	groups, err := gocodecontext.Groups(contextModule, pkg, gocodecontext.GroupOptions{
		IncludePackageDocs:             true,
		IncludeTestFiles:               includeTest,
		IncludeExternalDeps:            true,
		CountTokens:                    countTokens,
		ConsiderAmbiguousDocumented:    true,
		ConsiderTestFuncsDocumented:    true,
		ConsiderConstBlocksDocumenting: true,
	})
	if err != nil {
		return nil, options.LogWrappedErr("important_identifiers.groups", err)
	}
	for _, group := range groups {
		if group.IsExternal || !p.importantGroup(group) {
			continue
		}
		for _, identifier := range group.IDs {
			if identifier == gocode.PackageIdentifier {
				add(identifier)
				continue
			}
			addIfIncluded(identifier)
		}
	}

	return important, nil
}

func (p importantIdentifierPolicy) staticIdentifiersFromIDs(pkg *gocode.Package, ids *Identifiers, includeTest bool) (map[string]struct{}, map[string]struct{}) {
	generatedIDs := sliceToSet(appendExclusionForGeneratedFiles(nil, pkg))
	important := make(map[string]struct{})

	add := func(identifier string) {
		if _, generated := generatedIDs[identifier]; generated {
			return
		}
		important[identifier] = struct{}{}
	}
	addIfIncluded := func(identifier string) {
		if ids.includeIdentifier(identifier, includeTest, false) {
			add(identifier)
		}
	}

	if !pkg.IsTestPackage() {
		add(gocode.PackageIdentifier)
	}

	for _, identifier := range ids.allFuncs {
		if ids.includeIdentifier(identifier, includeTest, true) {
			add(identifier)
		}
	}
	for _, identifier := range ids.allTypes {
		if ids.includeIdentifier(identifier, includeTest, true) {
			add(identifier)
		}
	}
	for _, identifier := range ids.allValues {
		if ids.includeIdentifier(identifier, includeTest, true) {
			add(identifier)
		}
	}

	typeIDs := make(map[string]struct{}, len(ids.allTypes))
	for _, identifier := range ids.allTypes {
		typeIDs[identifier] = struct{}{}
		addIfIncluded(identifier)
	}
	for _, fn := range pkg.FuncSnippets {
		if !includeSnippetForImportant(pkg, fn, includeTest) {
			continue
		}
		if _, ok := typeIDs[leadingIdentifier(fn.IndirectedReceiverType())]; ok {
			add(fn.Identifier)
		}
		if sourceLineCount(fn.FullBytes()) >= p.BigFunctionSourceLines {
			add(fn.Identifier)
		}
	}

	return important, generatedIDs
}

func importantScratchExclusions(exclude []string, pkg *gocode.Package, importantIDs map[string]struct{}) []string {
	excludeSet := sliceToSet(exclude)
	ids := NewIdentifiersFromPackage(pkg)

	addIfUnimportant := func(identifier string) {
		if _, important := importantIDs[identifier]; important {
			return
		}
		excludeSet[identifier] = struct{}{}
	}

	if !pkg.IsTestPackage() {
		addIfUnimportant(gocode.PackageIdentifier)
	}
	for _, identifier := range ids.allFuncs {
		addIfUnimportant(identifier)
	}
	for _, identifier := range ids.allTypes {
		addIfUnimportant(identifier)
	}
	for _, identifier := range ids.allValues {
		addIfUnimportant(identifier)
	}

	return setToSlice(excludeSet)
}

func removeExcludedImportantIdentifiers(importantIDs map[string]struct{}, excluded []string, pkg *gocode.Package) {
	excludedSet := sliceToSet(excluded)
	for identifier := range excludedSet {
		delete(importantIDs, identifier)
	}
	for _, fn := range pkg.FuncSnippets {
		if _, excluded := excludedSet[leadingIdentifier(fn.IndirectedReceiverType())]; excluded {
			delete(importantIDs, fn.Identifier)
		}
	}
}

func (p importantIdentifierPolicy) importantGroup(group *gocodecontext.IdentifierGroup) bool {
	return len(group.UsedByDeps) >= p.GroupFanInThreshold ||
		len(group.DirectDeps) >= p.GroupFanOutThreshold
}

func totalImportantUndocumented(ids *Identifiers, important map[string]struct{}, includeTest bool) int {
	count := 0
	count += ids.countImportantUndocumentedTopLevel(ids.allFuncs, important, includeTest)
	count += ids.countImportantUndocumentedTopLevel(ids.allValues, important, includeTest)
	count += ids.countImportantUndocumentedTypes(important, includeTest)

	if !ids.isTestPkg {
		if _, important := important[gocode.PackageIdentifier]; important {
			if _, ok := ids.withDocs[gocode.PackageIdentifier]; !ok {
				count++
			}
		}
	}

	return count
}

func (ids *Identifiers) countImportantUndocumentedTopLevel(identifiers []string, important map[string]struct{}, includeTest bool) int {
	count := 0
	for _, identifier := range identifiers {
		if _, ok := important[identifier]; !ok {
			continue
		}
		if !ids.includeIdentifier(identifier, includeTest, false) {
			continue
		}
		if _, hasDoc := ids.withDocs[identifier]; !hasDoc {
			count++
		}
	}
	return count
}

func (ids *Identifiers) countImportantUndocumentedTypes(important map[string]struct{}, includeTest bool) int {
	count := 0
	for _, typ := range ids.allTypes {
		if _, ok := important[typ]; !ok {
			continue
		}
		if !ids.includeIdentifier(typ, includeTest, false) {
			continue
		}

		if _, hasDoc := ids.typeDocs[typ]; !hasDoc {
			count++
			continue
		}

		for _, fieldID := range ids.typeToFields[typ] {
			if _, hasDoc := ids.withDocs[fieldID]; !hasDoc {
				count++
				break
			}
		}
	}
	return count
}

func applyImportantDocsFromScratchPackage(pkg *gocode.Package, scratchPkg *gocode.Package, importantIDs map[string]struct{}, options AddDocsOptions, includeTestSnippets bool, logContext string) (*gocode.Package, error) {
	importantSnippets := importantDocumentationSnippets(scratchPkg, importantIDs, includeTestSnippets)
	if len(importantSnippets) == 0 {
		return pkg, nil
	}

	options.userMessagef("Applying important docs from scratch copy: %d snippets", len(importantSnippets))
	updatedPkg, _, snippetErrors, err := updatedocs.UpdateDocumentation(pkg, importantSnippets, options.updatedocsOptions(true))
	if err != nil {
		return nil, options.LogWrappedErr("ensure_docs.important_only.update_documentation", err)
	}
	logSnippetErrors(options.Logger, logContext, snippetErrors)
	hardSnippetErrors := removePartiallyRejectedSnippetErrors(snippetErrors)
	if len(hardSnippetErrors) > 0 {
		return nil, options.LogWrappedErr("ensure_docs.important_only.update_documentation.snippet_errors", errSomeSnippetsFailed)
	}
	if updatedPkg != nil {
		pkg = updatedPkg
	}
	return pkg, nil
}

func importantDocumentationSnippets(pkg *gocode.Package, importantIDs map[string]struct{}, includeTestSnippets bool) []string {
	var snippets []string
	for _, snippet := range pkg.Snippets() {
		if !includeSnippetForImportant(pkg, snippet, includeTestSnippets) {
			continue
		}

		if _, ok := snippet.(*gocode.PackageDocSnippet); ok {
			if _, important := importantIDs[gocode.PackageIdentifier]; !important {
				continue
			}
		} else if !snippetHasImportantID(snippet, importantIDs) {
			continue
		}

		text := string(snippet.Bytes())
		if hasDocumentationComment(text) {
			snippets = append(snippets, text)
		}
	}
	return snippets
}

func snippetHasImportantID(snippet gocode.Snippet, importantIDs map[string]struct{}) bool {
	for _, identifier := range snippet.IDs() {
		if _, important := importantIDs[identifier]; important {
			return true
		}
	}
	return false
}

func includeSnippetForImportant(pkg *gocode.Package, snippet gocode.Snippet, includeTest bool) bool {
	if snippetInGeneratedFile(pkg, snippet) {
		return false
	}
	if snippet.Test() && !includeTest {
		return false
	}
	if funcSnippet, ok := snippet.(*gocode.FuncSnippet); ok && funcSnippet.IsTestFunc() {
		return false
	}
	return true
}

func snippetInGeneratedFile(pkg *gocode.Package, snippet gocode.Snippet) bool {
	if fileName := filepath.Base(snippet.Position().Filename); fileName != "." {
		if file := pkg.Files[fileName]; file != nil && file.IsCodeGenerated() {
			return true
		}
	}
	return false
}

func sourceLineCount(source []byte) int {
	if len(source) == 0 {
		return 0
	}
	lines := bytes.Count(source, []byte("\n"))
	if source[len(source)-1] != '\n' {
		lines++
	}
	return lines
}
