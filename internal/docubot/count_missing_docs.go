package docubot

import "github.com/codalotl/codalotl/internal/gocode"

// CountMissingDocs returns how many AddDocs targets selected by options lack docs.
//
// CountMissingDocs does not edit files or make LLM requests.
func CountMissingDocs(pkg *gocode.Package, options AddDocsOptions) (int, error) {
	if options.OnlyDocumentExportedIdentifiers && options.OnlyDocumentImportantIdentifiers {
		return 0, options.LogNewErr("OnlyDocumentImportantIdentifiers and OnlyDocumentExportedIdentifiers are mutually exclusive")
	}

	if options.OnlyDocumentImportantIdentifiers {
		return countMissingImportantDocs(pkg, options)
	}

	mainCount := countMissingDocsForPackage(pkg, options, options.DocumentTestFiles)
	if options.OnlyDocumentExportedIdentifiers {
		mainCount = countMissingPublicDocsForPackage(pkg, options, options.DocumentTestFiles)
	}

	if options.DocumentTestFiles && !pkg.IsTestPackage() && pkg.HasTestPackage() {
		if options.OnlyDocumentExportedIdentifiers {
			mainCount += countMissingPublicDocsForPackage(pkg.TestPackage, options, true)
		} else {
			mainCount += countMissingDocsForPackage(pkg.TestPackage, options, true)
		}
	}

	return mainCount, nil
}

// NeedsDocs reports whether any AddDocs target selected by options lacks docs.
//
// NeedsDocs does not edit files or make LLM requests.
func NeedsDocs(pkg *gocode.Package, options AddDocsOptions) (bool, error) {
	if options.OnlyDocumentExportedIdentifiers && options.OnlyDocumentImportantIdentifiers {
		return false, options.LogNewErr("OnlyDocumentImportantIdentifiers and OnlyDocumentExportedIdentifiers are mutually exclusive")
	}

	if options.OnlyDocumentImportantIdentifiers {
		return needsImportantDocs(pkg, options)
	}

	if needsDocsForPackage(pkg, options, options.DocumentTestFiles) {
		return true, nil
	}

	if options.DocumentTestFiles && !pkg.IsTestPackage() && pkg.HasTestPackage() {
		return needsDocsForPackage(pkg.TestPackage, options, true), nil
	}

	return false, nil
}

func needsDocsForPackage(pkg *gocode.Package, options AddDocsOptions, includeTest bool) bool {
	if options.OnlyDocumentExportedIdentifiers {
		return countMissingPublicDocsForPackage(pkg, options, includeTest) > 0
	}
	return countMissingDocsForPackage(pkg, options, includeTest) > 0
}

func countMissingDocsForPackage(pkg *gocode.Package, options AddDocsOptions, includeTest bool) int {
	idents := NewIdentifiersFromPackage(pkg)
	for _, identifier := range appendExclusionForGeneratedFiles(options.ExcludeIdentifiers, pkg) {
		idents.MarkDocumented(identifier)
	}
	return idents.TotalUndocumented(includeTest)
}

func countMissingPublicDocsForPackage(pkg *gocode.Package, options AddDocsOptions, includeTest bool) int {
	idents := NewIdentifiersFromPackage(pkg)
	for _, identifier := range appendExclusionForGeneratedFiles(options.ExcludeIdentifiers, pkg) {
		idents.MarkDocumented(identifier)
	}
	return idents.TotalPublicUndocumented(includeTest)
}

func countMissingImportantDocs(pkg *gocode.Package, options AddDocsOptions) (int, error) {
	_, count, err := importantIdentifiersNeedingDocs(pkg, options.DocumentTestFiles, options, nil)
	if err != nil {
		return 0, options.LogWrappedErr("count_missing_docs.important_identifiers", err)
	}

	if options.DocumentTestFiles && !pkg.IsTestPackage() && pkg.HasTestPackage() {
		_, testCount, err := importantIdentifiersNeedingDocs(pkg.TestPackage, true, options, nil)
		if err != nil {
			return 0, options.LogWrappedErr("count_missing_docs.test_important_identifiers", err)
		}
		count += testCount
	}

	return count, nil
}
