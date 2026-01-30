package docubot

import (
	"github.com/codalotl/codalotl/internal/gocode"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewIdentifiersFromPackage(t *testing.T) {
	withCodeFixture(t, func(pkg *gocode.Package) {
		id := NewIdentifiersFromPackage(pkg)

		// Test function tracking - 3 functions + 3 methods + 1 testing func = 7 total
		assert.Equal(t, 7, len(id.allFuncs))

		// Verify all expected functions and methods
		expectedFuncs := map[string]bool{
			"Average":                   true,
			"sumTemp":                   true,
			"NewReading":                true,
			"Temperature.Celsius":       true,
			"Temperature.AboveFreezing": true,
			"Temperature.above":         true,
			"tempsFreezingBoiling":      true,
		}
		for _, fn := range id.allFuncs {
			assert.True(t, expectedFuncs[fn])
		}

		// Test documentation status
		assert.Contains(t, id.withDocs, "Temperature.Celsius")
		assert.NotContains(t, id.withDocs, "Average")
		assert.NotContains(t, id.withDocs, "sumTemp")
		assert.NotContains(t, id.withDocs, "NewReading")
		assert.NotContains(t, id.withDocs, "Temperature.AboveFreezing")
		assert.NotContains(t, id.withDocs, "Temperature.above")

		// Test exported status - public identifiers should be marked as exported
		assert.Contains(t, id.isExported, "Average")
		assert.Contains(t, id.isExported, "NewReading")
		assert.Contains(t, id.isExported, "Temperature.Celsius")
		assert.Contains(t, id.isExported, "Temperature.AboveFreezing")
		// sumTemp and Temperature.above are private so should not be exported
		assert.NotContains(t, id.isExported, "sumTemp")
		assert.NotContains(t, id.isExported, "Temperature.above")

		// Test type tracking
		assert.Equal(t, 2, len(id.allTypes))
		expectedTypes := map[string]bool{
			"Reading":     true,
			"Temperature": true,
		}
		for _, typ := range id.allTypes {
			assert.True(t, expectedTypes[typ])
		}

		// Test type documentation status
		assert.NotContains(t, id.withDocs, "Reading")
		assert.NotContains(t, id.withDocs, "Temperature")

		// Test type export status - public types should be marked as exported
		assert.Contains(t, id.isExported, "Reading")
		assert.Contains(t, id.isExported, "Temperature")

		// Test value tracking (constants and variables)
		assert.Equal(t, 3, len(id.allValues))
		expectedValues := map[string]bool{
			"Freezing":        true,
			"Boiling":         true,
			"DefaultLocation": true,
		}
		for _, val := range id.allValues {
			assert.True(t, expectedValues[val])
		}

		// Test value documentation status
		assert.NotContains(t, id.withDocs, "Freezing")
		assert.NotContains(t, id.withDocs, "Boiling")
		assert.NotContains(t, id.withDocs, "DefaultLocation")

		// Test value export status - public values should be marked as exported
		assert.Contains(t, id.isExported, "Freezing")
		assert.Contains(t, id.isExported, "Boiling")
		assert.Contains(t, id.isExported, "DefaultLocation")

		// Test package documentation
		assert.NotContains(t, id.withDocs, gocode.PackageIdentifier)

		// Test method identification
		methodsFound := map[string]bool{}
		for _, fn := range id.allFuncs {
			if strings.Contains(fn, ".") {
				methodsFound[fn] = true
			}
		}
		assert.Equal(t, 3, len(methodsFound))
		assert.True(t, methodsFound["Temperature.Celsius"])
		assert.True(t, methodsFound["Temperature.AboveFreezing"])
		assert.True(t, methodsFound["Temperature.above"])

		assert.Equal(t, 1, len(id.isTest))

		// Test FuncIDs method
		// All functions without docs (5 total: Average, sumTemp, NewReading, Temperature.AboveFreezing, Temperature.above)
		undocumented := id.FuncIDs(false, false)
		assert.Equal(t, 5, len(undocumented))
		assert.Contains(t, undocumented, "Average")
		assert.Contains(t, undocumented, "sumTemp")
		assert.Contains(t, undocumented, "NewReading")
		assert.Contains(t, undocumented, "Temperature.AboveFreezing")
		assert.Contains(t, undocumented, "Temperature.above")

		// Only documented functions (1 total: Temperature.Celsius)
		documented := id.FuncIDs(true, false)
		assert.Equal(t, 1, len(documented))
		assert.Contains(t, documented, "Temperature.Celsius")

		undocTestFuncs := id.FuncIDs(false, true)
		assert.Equal(t, len(undocumented)+1, len(undocTestFuncs))
		assert.Contains(t, undocTestFuncs, "tempsFreezingBoiling")

		// Test TypeIDs method
		// All types without docs (2 total: Reading, Temperature)
		undocumentedTypes := id.TypeIDs(false, false)
		assert.Equal(t, 2, len(undocumentedTypes))
		assert.Contains(t, undocumentedTypes, "Reading")
		assert.Contains(t, undocumentedTypes, "Temperature")

		// No documented types
		documentedTypes := id.TypeIDs(true, false)
		assert.Equal(t, 0, len(documentedTypes))

		// Test ValueIDs method
		// All values without docs (3 total: Freezing, Boiling, DefaultLocation)
		undocumentedValues := id.ValueIDs(false, false)
		assert.Equal(t, 3, len(undocumentedValues))
		assert.Contains(t, undocumentedValues, "Freezing")
		assert.Contains(t, undocumentedValues, "Boiling")
		assert.Contains(t, undocumentedValues, "DefaultLocation")

		// No documented values
		documentedValues := id.ValueIDs(true, false)
		assert.Equal(t, 0, len(documentedValues))

		// Test FieldIDs method
		// Get all fields for types without documentation
		fieldsUndoc := id.FieldIDs(false, false)
		assert.Equal(t, 1, len(fieldsUndoc)) // Only Reading type has fields
		assert.Contains(t, fieldsUndoc, "Reading")
		assert.Equal(t, 3, len(fieldsUndoc["Reading"])) // Value, Timestamp, location
		assert.Contains(t, fieldsUndoc["Reading"], "Value")
		assert.Contains(t, fieldsUndoc["Reading"], "Timestamp")
		assert.Contains(t, fieldsUndoc["Reading"], "location")

		// No fields have documentation
		fieldsDoc := id.FieldIDs(true, false)
		assert.Equal(t, 0, len(fieldsDoc))

		// Test TotalUndocumented method
		// Count all undocumented identifiers (all non-test files):
		// - Functions: Average, sumTemp, NewReading, Temperature.AboveFreezing, Temperature.above = 5
		// - Types: Reading (has undocumented fields), Temperature (no docs) = 2
		// - Values: Freezing, Boiling, DefaultLocation = 3
		// - Package: no docs = 1
		// Total: 5 + 2 + 3 + 1 = 11
		totalUndoc := id.TotalUndocumented(false)
		assert.Equal(t, 11, totalUndoc)

		// Test String method
		summary := id.String()
		assert.Contains(t, summary, "Package doc: ✗")
		assert.Contains(t, summary, "Functions: 1/7 documented")
		assert.Contains(t, summary, "Types: 0/2 documented")
		assert.Contains(t, summary, "Values: 0/3 documented")

		// Test FilteredString method
		// All non-test files
		filteredAll := id.FilteredString(false)
		assert.Contains(t, filteredAll, "Filter: all")
		assert.Contains(t, filteredAll, "Functions: 1/6 documented")
		assert.Contains(t, filteredAll, "Types: 0/2 documented")
		assert.Contains(t, filteredAll, "Values: 0/3 documented")

		// Test NeedsDocsIDs method
		// All identifiers from non-test files
		idsAll, fieldsAll := id.IDsNeedingDocs(false)
		assert.ElementsMatch(t, []string{
			"Average", "NewReading", "Temperature.AboveFreezing", "sumTemp", "Temperature.above", // funcs
			"Reading", "Temperature", // types
			"Freezing", "Boiling", "DefaultLocation", // values
			gocode.PackageIdentifier, // package doc
		}, idsAll)
		expectedFields := map[string][]string{
			"Reading": {"Value", "Timestamp", "location"},
		}
		assert.Equal(t, len(expectedFields), len(fieldsAll))
		readingFieldsAll, ok := fieldsAll["Reading"]
		assert.True(t, ok)
		assert.ElementsMatch(t, expectedFields["Reading"], readingFieldsAll)

		//
		// Test the test package:
		//

		// Notable: the PackageIdentifier does not need docs!

		testID := NewIdentifiersFromPackage(pkg.TestPackage)
		testIDsAll, testFieldsAll := testID.IDsNeedingDocs(true)
		assert.ElementsMatch(t, []string{"assertAboutNow"}, testIDsAll)
		assert.Len(t, testFieldsAll, 0)

		assert.EqualValues(t, 1, testID.TotalUndocumented(true))
		assert.EqualValues(t, 0, testID.TotalUndocumented(false))
	})
}
