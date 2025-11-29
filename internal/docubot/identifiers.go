package docubot

import (
	"fmt"
	"go/ast"
	"sort"
	"strings"

	"github.com/codalotl/codalotl/internal/gocode"
)

// Idea: i kinda want to move this into gocode, or somehow merge with package (ex: pkg.IsDocumented("foo")).

// Identifiers tracks all top-level identifiers in a package and their documentation status.
type Identifiers struct {
	allFuncs  []string // All func identifiers, including methods (ex: "*foo.Bar" for func (f *foo) Bar(x int) bool).
	allTypes  []string // All type identifiers.
	allValues []string // All value identifiers.

	// typeToFields maps type identifier to its field identifiers. All fields present, regardless of doc status. Field identifiers include type in its dot notation.
	typeToFields map[string][]string

	// Identifiers that have documentation. For types, keys include both the overall type and each field (dot notation, including the type). An overall type identifier is considered documented
	// if the type and all fields are documented.
	withDocs map[string]struct{}

	isExported map[string]struct{} // Identifier is exported (capitalized). Only top-level idents (no fields).
	isTest     map[string]struct{} // Identifier occurs in a test file. Only top-level idents (no fields).
	isTestPkg  bool                // True if pkg is a _test package (and so wouldn't need package docs).
}

// NewIdentifiersFromPackage creates a new Identifiers struct from a Package.
func NewIdentifiersFromPackage(pkg *gocode.Package) *Identifiers {
	id := &Identifiers{
		allFuncs:     make([]string, 0),
		allTypes:     make([]string, 0),
		allValues:    make([]string, 0),
		withDocs:     make(map[string]struct{}),
		isExported:   make(map[string]struct{}),
		isTest:       make(map[string]struct{}),
		typeToFields: make(map[string][]string),
	}

	// Process function snippets
	for _, fn := range pkg.FuncSnippets {
		// Function TestXxx (or a benchmark, etc).
		// In this context of documenting code, we don't want to document them, so we don't add them here.
		// If we ever use this for something like function call graphs, test funcs are indeed valuable.
		if fn.IsTestFunc() {
			continue
		}

		identifier := fn.Identifier
		if gocode.IsAmbiguousIdentifier(identifier) {
			continue
		}
		id.allFuncs = append(id.allFuncs, identifier)

		// Check if has documentation
		if fn.Doc != "" {
			id.withDocs[identifier] = struct{}{}
		}

		// Check if exported (function name and receiver must be exported)
		if fn.HasExported() && !fn.Test() {
			id.isExported[identifier] = struct{}{}
		}

		// Check if in test file
		if fn.Test() {
			id.isTest[identifier] = struct{}{}
		}
	}

	// Process type snippets
	for _, typ := range pkg.TypeSnippets {
		for _, identifier := range typ.Identifiers {
			if gocode.IsAnonymousIdentifier(identifier) {
				continue
			}
			id.allTypes = append(id.allTypes, identifier)

			// Check if exported (type name starts with capital letter)
			if ast.IsExported(identifier) && !typ.Test() {
				id.isExported[identifier] = struct{}{}
			}

			// Check if in test file
			if typ.Test() {
				id.isTest[identifier] = struct{}{}
			}

			// Process fields for this type, tracking their documentation status individually.
			allFieldsDocumented := true
			if typ.FieldDocs != nil {
				var fields []string
				for fieldID, fieldDoc := range typ.FieldDocs {
					fields = append(fields, fieldID)

					// Track field documentation.
					if fieldDoc != "" {
						id.withDocs[fieldID] = struct{}{}
					} else {
						allFieldsDocumented = false
					}
				}
				id.typeToFields[identifier] = fields
			}

			// A type is considered documented only if the type identifier itself AND all of its fields have documentation.
			if doc, ok := typ.IdentifierDocs[identifier]; ok && doc != "" {
				if allFieldsDocumented {
					id.withDocs[identifier] = struct{}{}
				}
			}
		}
	}

	// Process value snippets (variables and constants)
	for _, val := range pkg.ValueSnippets {
		for _, identifier := range val.Identifiers {
			if gocode.IsAnonymousIdentifier(identifier) {
				continue
			}

			id.allValues = append(id.allValues, identifier)

			// Check if this specific value has documentation.
			// Consts are also considered documented if they're in block form and the block has a doc.
			if doc, ok := val.IdentifierDocs[identifier]; ok && doc != "" {
				id.withDocs[identifier] = struct{}{}
			} else if !val.IsVar {
				if val.BlockDoc != "" {
					id.withDocs[identifier] = struct{}{}
				}
			}

			// Check if exported (value name starts with capital letter)
			if ast.IsExported(identifier) && !val.Test() {
				id.isExported[identifier] = struct{}{}
			}

			// Check if in test file
			if val.Test() {
				id.isTest[identifier] = struct{}{}
			}
		}
	}

	// Check for package documentation
	if len(pkg.PackageDocSnippets) > 0 {
		id.withDocs[gocode.PackageIdentifier] = struct{}{}
	}

	id.isTestPkg = pkg.IsTestPackage()

	return id
}

// MarkDocumented marks an identifier as documented. If the identifier represents a type, all of its fields are also marked as documented.
func (ids *Identifiers) MarkDocumented(identifier string) {
	ids.withDocs[identifier] = struct{}{}
	if fields, ok := ids.typeToFields[identifier]; ok {
		for _, f := range fields {
			ids.withDocs[f] = struct{}{}
		}
	}
}

// FuncIDs returns function identifiers that match the specified criteria.
//
// The filtering works as follows:
//   - isDocumented: if true, only returns functions with documentation; if false, only returns functions without documentation.
//   - includeTest: includes functions from test files that are tracked by Identifiers (both exported and private). Note: Go test/benchmark entry points (ex: TestXxx, BenchmarkXxx) are
//     not tracked and will never be returned, even when includeTest is true.
//
// When includeTest is false, all non-test file functions are included regardless of their export status.
func (ids *Identifiers) FuncIDs(isDocumented bool, includeTest bool) []string {
	var result []string

	for _, fn := range ids.allFuncs {
		// Check documentation status
		_, hasDoc := ids.withDocs[fn]
		if hasDoc != isDocumented {
			continue
		}

		// Check if in test file
		_, inTest := ids.isTest[fn]

		if inTest {
			// If in test file, only include if includeTest is true
			if includeTest {
				result = append(result, fn)
			}
		} else {
			// Not in test file, always include
			result = append(result, fn)
		}
	}

	return result
}

// TypeIDs returns type identifiers that match the specified criteria.
//
// The filtering works as follows:
//   - isDocumented: if true, only returns types with documentation; if false, only returns types without documentation.
//   - includeTest: includes all types from test files (both exported and private).
//
// When includeTest is false, all non-test file types are included regardless of their export status.
func (ids *Identifiers) TypeIDs(isDocumented bool, includeTest bool) []string {
	var result []string

	for _, typ := range ids.allTypes {
		// Check documentation status
		_, hasDoc := ids.withDocs[typ]
		if hasDoc != isDocumented {
			continue
		}

		// Check if in test file
		_, inTest := ids.isTest[typ]

		if inTest {
			// If in test file, only include if includeTest is true
			if includeTest {
				result = append(result, typ)
			}
		} else {
			// Not in test file, always include
			result = append(result, typ)
		}
	}

	return result
}

// ValueIDs returns value identifiers that match the specified criteria.
//
// The filtering works as follows:
//   - isDocumented: if true, only returns values with documentation; if false, only returns values without documentation.
//   - includeTest: includes all values from test files (both exported and private).
//
// When includeTest is false, all non-test file values are included regardless of their export status.
func (ids *Identifiers) ValueIDs(isDocumented bool, includeTest bool) []string {
	var result []string

	for _, val := range ids.allValues {
		// Check documentation status
		_, hasDoc := ids.withDocs[val]
		if hasDoc != isDocumented {
			continue
		}

		// Check if in test file
		_, inTest := ids.isTest[val]

		if inTest {
			// If in test file, only include if includeTest is true
			if includeTest {
				result = append(result, val)
			}
		} else {
			// Not in test file, always include
			result = append(result, val)
		}
	}

	return result
}

// FieldIDs returns a map of type identifiers to their field identifiers that match the specified criteria. Field identifiers do not have a type name prefix, but do use dot notation
// for nested fields (see gocode.TypeSnippet).
//
// The type filtering works as follows:
//   - includeTest: includes all types from test files (both exported and private).
//
// When includeTest is false, all non-test file types are included regardless of their export status. The isDocumented parameter filters individual fields based on their documentation
// status (and not the overall type symbol doc status).
func (ids *Identifiers) FieldIDs(isDocumented bool, includeTest bool) map[string][]string {
	result := make(map[string][]string)

	// First, get the types that match our criteria
	// We use TypeIDs with both true and false for isDocumented to get all matching types regardless of their documentation status
	matchingTypesWithDocs := ids.TypeIDs(true, includeTest)
	matchingTypesWithoutDocs := ids.TypeIDs(false, includeTest)

	// Combine both lists into a set
	matchingTypes := make(map[string]struct{})
	for _, typ := range matchingTypesWithDocs {
		matchingTypes[typ] = struct{}{}
	}
	for _, typ := range matchingTypesWithoutDocs {
		matchingTypes[typ] = struct{}{}
	}

	// Now process fields for matching types
	for typ := range matchingTypes {
		fields, hasFields := ids.typeToFields[typ]
		if !hasFields {
			continue
		}

		var matchingFields []string
		for _, fieldID := range fields {
			// Check documentation status
			_, hasDoc := ids.withDocs[fieldID]
			if hasDoc == isDocumented {
				// Remove the type prefix from field identifier
				// e.g., "Reading.Value" -> "Value", "Reading.Inner.Field" -> "Inner.Field"
				fieldName := fieldID
				if strings.HasPrefix(fieldID, typ+".") {
					fieldName = strings.TrimPrefix(fieldID, typ+".")
				}
				matchingFields = append(matchingFields, fieldName)
			}
		}

		if len(matchingFields) > 0 {
			result[typ] = matchingFields
		}
	}

	return result
}

// IDsNeedingDocs returns the identifiers and fields that need documentation. Fields are in the format of type name to field name. Field names use dot notation for nested fields, but
// does not include the type name itself. The identifier gocode.PackageIdentifier represents package documentation.
func (ids *Identifiers) IDsNeedingDocs(includeTest bool) ([]string, map[string][]string) {
	var targetIdentifiers []string
	targetFields := make(map[string][]string)

	// Get all undocumented identifiers based on options
	// Get undocumented functions
	undocFuncs := ids.FuncIDs(false, includeTest)
	targetIdentifiers = append(targetIdentifiers, undocFuncs...)

	// Get undocumented types
	undocTypes := ids.TypeIDs(false, includeTest)
	targetIdentifiers = append(targetIdentifiers, undocTypes...)

	// Get undocumented values
	undocValues := ids.ValueIDs(false, includeTest)
	targetIdentifiers = append(targetIdentifiers, undocValues...)

	// Check if package documentation is missing
	if !ids.isTestPkg {
		if _, ok := ids.withDocs[gocode.PackageIdentifier]; !ok {
			targetIdentifiers = append(targetIdentifiers, gocode.PackageIdentifier)
		}
	}

	// Get undocumented fields
	undocFields := ids.FieldIDs(false, includeTest)
	for typeName, fields := range undocFields {
		if len(fields) > 0 {
			targetFields[typeName] = fields
			// Ensure the type is in the identifiers list if it has undocumented fields
			typeFound := false
			for _, id := range targetIdentifiers {
				if id == typeName {
					typeFound = true
					break
				}
			}
			if !typeFound {
				targetIdentifiers = append(targetIdentifiers, typeName)
			}
		}
	}

	return targetIdentifiers, targetFields
}

// TotalUndocumented returns the total number of undocumented top-level identifiers, plus missing package-level documentation for non-_test packages. A type is counted as undocumented
// if it has undocumented fields or if the type itself lacks documentation.
func (ids *Identifiers) TotalUndocumented(includeTest bool) int {
	count := 0

	// Count undocumented functions
	undocFuncs := ids.FuncIDs(false, includeTest)
	count += len(undocFuncs)

	// Count undocumented values
	undocValues := ids.ValueIDs(false, includeTest)
	count += len(undocValues)

	// Count types - a type is undocumented if either:
	// 1. The type itself lacks documentation, OR
	// 2. The type has any undocumented fields

	// Get all types that match our criteria
	matchingTypesWithDocs := ids.TypeIDs(true, includeTest)
	matchingTypesWithoutDocs := ids.TypeIDs(false, includeTest)

	// Types without documentation are automatically counted
	count += len(matchingTypesWithoutDocs)

	// For types with documentation, check if they have undocumented fields
	for _, typ := range matchingTypesWithDocs {
		fields, hasFields := ids.typeToFields[typ]
		if !hasFields {
			continue
		}

		// Check if any field lacks documentation
		hasUndocumentedField := false
		for _, fieldID := range fields {
			if _, hasDoc := ids.withDocs[fieldID]; !hasDoc {
				hasUndocumentedField = true
				break
			}
		}

		if hasUndocumentedField {
			count++
		}
	}

	// Overall package docs:
	if !ids.isTestPkg {
		if _, ok := ids.withDocs[gocode.PackageIdentifier]; !ok {
			count++
		}
	}

	return count
}

// DocumentedSince returns the set of top-level identifiers that have gained documentation since older was measured. For types with fields, the type is included if any field gained
// documentation, even if the overall documentation status of the type is still undocumented. Identifiers that lost documentation are not included in this set.
func (ids *Identifiers) DocumentedSince(older *Identifiers) []string {
	// If no older snapshot, nothing to compare against.
	if older == nil {
		return nil
	}

	// Track improvements using a set to avoid duplicates.
	improved := make(map[string]struct{})

	// Helper to mark an identifier as improved.
	markImproved := func(id string) {
		improved[id] = struct{}{}
	}

	// 1. Any *new* fully-documented identifiers (functions, values, fully-documented types, package docs).
	for id := range ids.withDocs {
		if _, already := older.withDocs[id]; already {
			continue // already documented before
		}

		// Check if this id is a recorded field of some type. If so, give credit to the parent type.
		parent := ""
		for typ, fields := range ids.typeToFields {
			for _, f := range fields {
				if id == f {
					parent = typ
					break
				}
			}
			if parent != "" {
				break
			}
		}

		if parent != "" {
			markImproved(parent)
		} else {
			markImproved(id)
		}
	}

	// 2. Check for types that gained *some* documentation on their fields even if still not fully documented.
	// Iterate through all known types in the current snapshot.
	for typ, fields := range ids.typeToFields {
		// Skip if we already counted this type above.
		if _, ok := improved[typ]; ok {
			continue
		}

		// For each field of the type, if the field is documented now but was not documented before, mark the type improved.
		for _, f := range fields {
			if _, nowDoc := ids.withDocs[f]; !nowDoc {
				continue
			}
			if _, beforeDoc := older.withDocs[f]; !beforeDoc {
				markImproved(typ)
				break
			}
		}
	}

	// Convert set to sorted slice for deterministic output.
	var result []string
	for id := range improved {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

// String returns a one-line summary of all package-level identifiers.
func (ids *Identifiers) String() string {
	// Count documented vs undocumented for each category
	funcWithDocs := 0
	for _, fn := range ids.allFuncs {
		if _, hasDoc := ids.withDocs[fn]; hasDoc {
			funcWithDocs++
		}
	}

	typeWithDocs := 0
	for _, typ := range ids.allTypes {
		if _, hasDoc := ids.withDocs[typ]; hasDoc {
			typeWithDocs++
		}
	}

	valueWithDocs := 0
	for _, val := range ids.allValues {
		if _, hasDoc := ids.withDocs[val]; hasDoc {
			valueWithDocs++
		}
	}

	pkgDoc := ""
	if ids.isTestPkg {
		pkgDoc = "-"
	} else {
		if _, ok := ids.withDocs[gocode.PackageIdentifier]; ok {
			pkgDoc = "✓"
		} else {
			pkgDoc = "✗"
		}
	}

	return fmt.Sprintf("Package doc: %s | Functions: %d/%d documented | Types: %d/%d documented | Values: %d/%d documented",
		pkgDoc,
		funcWithDocs, len(ids.allFuncs),
		typeWithDocs, len(ids.allTypes),
		valueWithDocs, len(ids.allValues))
}

// FilteredString returns a one-line summary of identifiers matching the given filters.
func (ids *Identifiers) FilteredString(includeTest bool) string {
	// Get filtered identifiers
	funcs := ids.FuncIDs(true, includeTest)
	funcs = append(funcs, ids.FuncIDs(false, includeTest)...)

	types := ids.TypeIDs(true, includeTest)
	types = append(types, ids.TypeIDs(false, includeTest)...)

	values := ids.ValueIDs(true, includeTest)
	values = append(values, ids.ValueIDs(false, includeTest)...)

	// Count documented vs total for filtered items
	funcWithDocs := len(ids.FuncIDs(true, includeTest))
	typeWithDocs := len(ids.TypeIDs(true, includeTest))
	valueWithDocs := len(ids.ValueIDs(true, includeTest))

	// Build filter description
	filterDesc := "all"
	if includeTest {
		filterDesc = "all+test"
	}

	pkgDoc := ""
	if ids.isTestPkg {
		pkgDoc = "-"
	} else {
		if _, ok := ids.withDocs[gocode.PackageIdentifier]; ok {
			pkgDoc = "✓"
		} else {
			pkgDoc = "✗"
		}
	}

	return fmt.Sprintf("Filter: %s | Package doc: %s | Functions: %d/%d documented | Types: %d/%d documented | Values: %d/%d documented",
		filterDesc,
		pkgDoc,
		funcWithDocs, len(funcs),
		typeWithDocs, len(types),
		valueWithDocs, len(values))
}
