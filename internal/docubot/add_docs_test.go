package docubot

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddDocs(t *testing.T) {
	// Define the documentation snippets that the mock LLM will return.
	snippets := []string{
		dedentWithBackticks(`
			// Temperature represents a temperature value in degrees Celsius.
			type Temperature int
		`),

		dedentWithBackticks(`
			const (
				// Freezing represents the freezing point of water (0°C).
				Freezing Temperature = 0
				// Boiling represents the boiling point of water (100°C).
				Boiling  Temperature = 100
			)
		`),

		dedentWithBackticks(`
			// AboveFreezing returns true if the temperature is above freezing point.
			func (t Temperature) AboveFreezing() bool
		`),

		dedentWithBackticks(`
			// above returns true if the temperature is above the given threshold.
			func (t Temperature) above(threshold Temperature) bool
		`),

		dedentWithBackticks(`
			// Reading represents a temperature measurement at a specific time and location.
			type Reading struct {
				// Value is the temperature value.
				Value     Temperature
				// Timestamp is the time of the reading.
				Timestamp time.Time
				// location is the location of the reading.
				location  string
			}
		`),

		dedentWithBackticks(`
			// DefaultLocation is the default location used when none is specified.
			var DefaultLocation = "Unknown"
		`),

		dedentWithBackticks(`
			// NewReading creates a new temperature reading with the given value and location.
			// If location is empty, DefaultLocation is used.
			func NewReading(t Temperature, location string) Reading
		`),

		dedentWithBackticks(`
			// Average calculates the average temperature from a slice of temperature values.
			// Returns 0 if the slice is empty.
			func Average(values []Temperature) Temperature
		`),

		dedentWithBackticks(`
			// sumTemp calculates the sum of all temperature values in the slice.
			func sumTemp(values []Temperature) Temperature
		`),

		dedentWithBackticks(`
			// Package mypkg is a sample package.
			package mypkg
		`),
	}

	// Create a mock conversationalist that will return the snippets.
	conv := &responsesCompleter{responses: []string{
		"Here are the documentation snippets:\n\n" + strings.Join(snippets, "\n\n"),
	}}

	// Run the test within the fixture.
	withCodeFixture(t, func(pkg *gocode.Package) {
		// Ensure documentation is added.
		changes, err := AddDocs(pkg, AddDocsOptions{
			BaseOptions: BaseOptions{Completer: conv},
		})
		assert.NoError(t, err)
		updatedFiles := filenamesFromChanges(changes)
		assert.Len(t, updatedFiles, 4) // All three files should be updated, plus a doc.go for package docs.

		// Verify the documentation was added correctly.
		packageDocFound := false
		for _, file := range pkg.Files {
			content := string(file.Contents)
			if strings.Contains(content, "// Package mypkg is a sample package.") {
				packageDocFound = true
			}
			switch file.FileName {
			case "temperature.go":
				assert.Contains(t, content, "// Temperature represents a temperature value in degrees Celsius.")
				assert.Contains(t, content, "// Freezing represents the freezing point of water (0°C).")
				assert.Contains(t, content, "// Boiling represents the boiling point of water (100°C).")
				assert.Contains(t, content, "// Celsius returns the temperature in °C as an int.") // Existing comment
				assert.Contains(t, content, "// AboveFreezing returns true if the temperature is above freezing point.")
				assert.Contains(t, content, "// above returns true if the temperature is above the given threshold.")
			case "reading.go":
				assert.Contains(t, content, "// Reading represents a temperature measurement at a specific time and location.")
				assert.Contains(t, content, "// DefaultLocation is the default location used when none is specified.")
				assert.Contains(t, content, "// NewReading creates a new temperature reading with the given value and location.")
			case "average.go":
				assert.Contains(t, content, "// Average calculates the average temperature from a slice of temperature values.")
				assert.Contains(t, content, "// sumTemp calculates the sum of all temperature values in the slice.")
			}
		}
		assert.True(t, packageDocFound, "package documentation was not found in any file")
	})
}

func TestAddDocs_OnlyDocumentExportedIdentifier_DocumentsFixturePublicOnly(t *testing.T) {
	snippets := []string{
		dedentWithBackticks(`
			// Temperature represents a temperature value in degrees Celsius.
			type Temperature int
		`),

		dedentWithBackticks(`
			const (
				// Freezing represents the freezing point of water (0°C).
				Freezing Temperature = 0
				// Boiling represents the boiling point of water (100°C).
				Boiling  Temperature = 100
			)
		`),

		dedentWithBackticks(`
			// AboveFreezing reports whether the temperature is above freezing.
			func (t Temperature) AboveFreezing() bool
		`),

		dedentWithBackticks(`
			// above reports whether the temperature is above the threshold.
			func (t Temperature) above(threshold Temperature) bool
		`),

		dedentWithBackticks(`
			// Reading represents a temperature measurement.
			type Reading struct {
				// Value is the measured temperature.
				Value     Temperature
				// Timestamp is when the measurement was taken.
				Timestamp time.Time
				// location is where the measurement was taken.
				location  string
			}
		`),

		dedentWithBackticks(`
			// DefaultLocation is the fallback location for new readings.
			var DefaultLocation = "Unknown"
		`),

		dedentWithBackticks(`
			// NewReading creates a reading with the provided temperature and location.
			func NewReading(t Temperature, location string) Reading
		`),

		dedentWithBackticks(`
			// Average returns the mean of the supplied temperatures.
			func Average(values []Temperature) Temperature
		`),

		dedentWithBackticks(`
			// sumTemp returns the sum of the supplied temperatures.
			func sumTemp(values []Temperature) Temperature
		`),

		dedentWithBackticks(`
			// Package mypkg provides temperature helpers.
			package mypkg
		`),
	}

	conv := &responsesCompleter{responses: []string{
		"Here are the documentation snippets:\n\n" + strings.Join(snippets, "\n\n"),
	}}

	withCodeFixture(t, func(pkg *gocode.Package) {
		changes, err := AddDocs(pkg, AddDocsOptions{
			OnlyDocumentExportedIdentifiers: true,
			BaseOptions:                     BaseOptions{Completer: conv},
		})
		require.NoError(t, err)

		updatedFiles := filenamesFromChanges(changes)
		assert.Contains(t, updatedFiles, "temperature.go")
		assert.Contains(t, updatedFiles, "reading.go")
		assert.Contains(t, updatedFiles, "average.go")

		pkg, err = pkg.Reload()
		require.NoError(t, err)

		tempContent := string(pkg.Files["temperature.go"].Contents)
		assert.Contains(t, tempContent, "// Temperature represents a temperature value in degrees Celsius.")
		assert.Contains(t, tempContent, "// Freezing represents the freezing point of water (0°C).")
		assert.Contains(t, tempContent, "// Boiling represents the boiling point of water (100°C).")
		assert.Contains(t, tempContent, "// AboveFreezing reports whether the temperature is above freezing.")
		assert.NotContains(t, tempContent, "// above reports whether the temperature is above the threshold.")

		readingContent := string(pkg.Files["reading.go"].Contents)
		assert.Contains(t, readingContent, "// Reading represents a temperature measurement.")
		assert.Contains(t, readingContent, "// Value is the measured temperature.")
		assert.Contains(t, readingContent, "// Timestamp is when the measurement was taken.")
		assert.Contains(t, readingContent, "// DefaultLocation is the fallback location for new readings.")
		assert.Contains(t, readingContent, "// NewReading creates a reading with the provided temperature and location.")
		assert.NotContains(t, readingContent, "// location is where the measurement was taken.")

		averageContent := string(pkg.Files["average.go"].Contents)
		assert.Contains(t, averageContent, "// Average returns the mean of the supplied temperatures.")
		assert.NotContains(t, averageContent, "// sumTemp returns the sum of the supplied temperatures.")

		packageDocFound := false
		for _, file := range pkg.Files {
			if strings.Contains(string(file.Contents), "// Package mypkg provides temperature helpers.") {
				packageDocFound = true
				break
			}
		}
		assert.True(t, packageDocFound)

		userText := conv.allUserText()
		assert.Contains(t, userText, "- Temperature.above")
		assert.Contains(t, userText, "- sumTemp")
	})
}

func TestAddDocs_OnlyDocumentExportedIdentifier_SkipsWorkWhenOnlyPrivateDocsAreMissing(t *testing.T) {
	code := dedent(`
		// Package mypkg exercises public-only no-op behavior.
		package mypkg

		// Foo does something.
		func Foo() {}

		func bar() {}
	`)
	conv := &responsesCompleter{responses: []string{"unexpected"}}

	gocodetesting.WithCode(t, code, func(pkg *gocode.Package) {
		changes, err := AddDocs(pkg, AddDocsOptions{
			OnlyDocumentExportedIdentifiers: true,
			BaseOptions:                     BaseOptions{Completer: conv},
		})
		require.NoError(t, err)
		assert.Empty(t, changes)
		assert.Empty(t, conv.convs)

		pkg, err = pkg.Reload()
		require.NoError(t, err)

		content := string(pkg.Files["code.go"].Contents)
		assert.Contains(t, content, "// Foo does something.")
		assert.NotContains(t, content, "// bar")
	})
}

func TestAddDocs_OnlyDocumentExportedIdentifier_ConvertsMixedValueBlockDoc(t *testing.T) {
	code := dedent(`
		const (
			Public = 1
			private = 2
		)
	`)
	snippet := dedentWithBackticks(`
		// Values define package constants.
		const (
			Public = 1
			private = 2
		)
	`)
	conv := &responsesCompleter{responses: []string{
		"Here are the documentation snippets:\n\n" + snippet,
	}}

	gocodetesting.WithCode(t, code, func(pkg *gocode.Package) {
		changes, err := AddDocs(pkg, AddDocsOptions{
			OnlyDocumentExportedIdentifiers: true,
			ExcludeIdentifiers:              []string{gocode.PackageIdentifier},
			BaseOptions:                     BaseOptions{Completer: conv},
		})
		require.NoError(t, err)
		assert.Contains(t, filenamesFromChanges(changes), "code.go")

		pkg, err = pkg.Reload()
		require.NoError(t, err)

		content := string(pkg.Files["code.go"].Contents)
		assert.Contains(t, content, "// Values define package constants.\nconst (\n\tPublic  = 1\n\tprivate = 2\n)")
		assert.NotContains(t, content, "private = 2 // Values define package constants.")
		assert.Contains(t, conv.allUserText(), "- private")
	})
}

func TestAddDocs_OnlyDocumentExportedIdentifier_DocumentsMixedValueSpecWithoutSplitting(t *testing.T) {
	code := dedent(`
		var Public, private = 1, 2

		func Other() {}
	`)
	snippets := []string{
		dedentWithBackticks(`
			// Values hold package state.
			var Public, private = 1, 2
		`),
		dedentWithBackticks(`
			// Other performs another public operation.
			func Other()
		`),
	}
	conv := &responsesCompleter{responses: []string{
		"Here are the documentation snippets:\n\n" + strings.Join(snippets, "\n\n"),
	}}

	gocodetesting.WithCode(t, code, func(pkg *gocode.Package) {
		changes, err := AddDocs(pkg, AddDocsOptions{
			OnlyDocumentExportedIdentifiers: true,
			ExcludeIdentifiers:              []string{gocode.PackageIdentifier},
			BaseOptions:                     BaseOptions{Completer: conv},
		})
		require.NoError(t, err)
		assert.Contains(t, filenamesFromChanges(changes), "code.go")

		pkg, err = pkg.Reload()
		require.NoError(t, err)

		content := string(pkg.Files["code.go"].Contents)
		assert.Contains(t, content, "// Values hold package state.\nvar Public, private = 1, 2")
		assert.Contains(t, content, "// Other performs another public operation.\nfunc Other() {}")
	})
}

func TestAddDocs_OnlyDocumentExportedIdentifier_DocumentsMixedStructFieldListWithoutSplitting(t *testing.T) {
	code := dedent(`
		type Foo struct {
			Public, private int
		}
	`)
	snippet := dedentWithBackticks(`
		// Foo stores visible and private fields.
		type Foo struct {
			// Public is visible.
			Public, private int
		}
	`)
	conv := &responsesCompleter{responses: []string{
		"Here are the documentation snippets:\n\n" + snippet,
	}}

	gocodetesting.WithCode(t, code, func(pkg *gocode.Package) {
		changes, err := AddDocs(pkg, AddDocsOptions{
			OnlyDocumentExportedIdentifiers: true,
			ExcludeIdentifiers:              []string{gocode.PackageIdentifier},
			BaseOptions:                     BaseOptions{Completer: conv},
		})
		require.NoError(t, err)
		assert.Contains(t, filenamesFromChanges(changes), "code.go")

		pkg, err = pkg.Reload()
		require.NoError(t, err)

		content := string(pkg.Files["code.go"].Contents)
		assert.Contains(t, content, "// Foo stores visible and private fields.\ntype Foo struct")
		assert.Contains(t, content, "Public, private int")
		assert.Contains(t, content, "// Public is visible.")
	})
}

func TestAddDocs_OnlyDocumentExportedIdentifier_FiltersNestedPrivateStructFieldDocs(t *testing.T) {
	code := dedent(`
		type Foo struct {
			Inner struct {
				Public string
				private string
			}
		}
	`)
	snippet := dedentWithBackticks(`
		// Foo stores nested state.
		type Foo struct {
			// Inner stores child state.
			Inner struct {
				// Public is visible nested state.
				Public string
				// private is hidden nested state.
				private string
			}
		}
	`)
	conv := &responsesCompleter{responses: []string{
		"Here are the documentation snippets:\n\n" + snippet,
	}}

	gocodetesting.WithCode(t, code, func(pkg *gocode.Package) {
		changes, err := AddDocs(pkg, AddDocsOptions{
			OnlyDocumentExportedIdentifiers: true,
			ExcludeIdentifiers:              []string{gocode.PackageIdentifier},
			BaseOptions:                     BaseOptions{Completer: conv},
		})
		require.NoError(t, err)
		assert.Contains(t, filenamesFromChanges(changes), "code.go")

		pkg, err = pkg.Reload()
		require.NoError(t, err)

		content := string(pkg.Files["code.go"].Contents)
		assert.Contains(t, content, "// Foo stores nested state.")
		assert.Contains(t, content, "// Inner stores child state.")
		assert.Contains(t, content, "// Public is visible nested state.")
		assert.NotContains(t, content, "// private is hidden nested state.")
	})
}

func TestAddDocs_OnlyDocumentExportedIdentifier_PreservesMixedValueBlockDocWhenSpecHasComment(t *testing.T) {
	code := dedent(`
		const (
			A = 1
			// B already has docs.
			B = 2
			private = 3
		)
	`)
	snippet := dedentWithBackticks(`
		// Values define package constants.
		const (
			A = 1
			// B already has docs.
			B = 2
			private = 3
		)
	`)
	conv := &responsesCompleter{responses: []string{
		"Here are the documentation snippets:\n\n" + snippet,
	}}

	gocodetesting.WithCode(t, code, func(pkg *gocode.Package) {
		changes, err := AddDocs(pkg, AddDocsOptions{
			OnlyDocumentExportedIdentifiers: true,
			ExcludeIdentifiers:              []string{gocode.PackageIdentifier},
			BaseOptions:                     BaseOptions{Completer: conv},
		})
		require.NoError(t, err)
		assert.Contains(t, filenamesFromChanges(changes), "code.go")

		pkg, err = pkg.Reload()
		require.NoError(t, err)

		content := string(pkg.Files["code.go"].Contents)
		assert.Contains(t, content, "// Values define package constants.\nconst (")
		assert.Contains(t, content, "// B already has docs.\n\tB = 2")
		assert.NotContains(t, content, "// Values define package constants.\n\tprivate = 3")
	})
}

func TestAddDocs_OnlyDocumentExportedIdentifier_UsesOriginalModuleForScratchContext(t *testing.T) {
	code := dedent(`
		import "mymodule/otherpkg"

		type Public struct {
			Dep otherpkg.DepType
			hidden string
		}

		func UseDep(d otherpkg.DepType) Public {
			return Public{Dep: d}
		}

		func private(d otherpkg.DepType) {}
	`)
	response := "Here are the documentation snippets:\n\n" + strings.Join([]string{
		dedentWithBackticks(`
			// Public holds dependency state.
			type Public struct {
				// Dep is the dependency value.
				Dep otherpkg.DepType
				// hidden is internal state.
				hidden string
			}
		`),
		dedentWithBackticks(`
			// UseDep wraps a dependency value.
			func UseDep(d otherpkg.DepType) Public
		`),
		dedentWithBackticks(`
			// private observes a dependency value.
			func private(d otherpkg.DepType)
		`),
		dedentWithBackticks(`
			// Package mypkg exercises dependency context.
			package mypkg
		`),
	}, "\n\n")
	conv := &responsesCompleter{responses: []string{response}}

	gocodetesting.WithCode(t, code, func(pkg *gocode.Package) {
		err := gocodetesting.AddPackage(t, pkg.Module, "otherpkg", map[string]string{
			"other.go": dedent(`
				// DepType is a dependency type.
				type DepType struct{}
			`),
		})
		require.NoError(t, err)
		_, err = pkg.Module.LoadPackageByRelativeDir("otherpkg")
		require.NoError(t, err)

		changes, err := AddDocs(pkg, AddDocsOptions{
			OnlyDocumentExportedIdentifiers: true,
			BaseOptions:                     BaseOptions{Completer: conv},
		})
		require.NoError(t, err)
		assert.Contains(t, filenamesFromChanges(changes), "code.go")

		pkg, err = pkg.Reload()
		require.NoError(t, err)
		content := string(pkg.Files["code.go"].Contents)
		assert.Contains(t, content, "// Public holds dependency state.")
		assert.Contains(t, content, "// Dep is the dependency value.")
		assert.Contains(t, content, "// UseDep wraps a dependency value.")
		assert.NotContains(t, content, "// hidden is internal state.")
		assert.NotContains(t, content, "// private observes a dependency value.")
		assert.Contains(t, conv.allUserText(), "// mymodule/otherpkg:")
	})
}

func TestAddDocs_DocumentTestFiles(t *testing.T) {
	// ---------------------------------------------------------------------
	// Prepare documentation snippets for the three phases that AddDocs will
	// run through when DocumentTestFiles is true:
	//   1. Non-test identifiers in the main package.
	//   2. Identifiers in the black-box test package (mypkg_test).
	//   3. Test helpers in the main package’s *_test.go files.
	// Each phase gets its own canned response from the mock LLM so that we
	// don’t send irrelevant snippets that would fail to apply.
	// ---------------------------------------------------------------------

	// Phase 1 – non-test identifiers (same set reused in other tests):
	nonTestSnippets := []string{
		dedentWithBackticks(`
			// Temperature represents a temperature value in degrees Celsius.
			type Temperature int
		`),

		dedentWithBackticks(`
			const (
				// Freezing represents the freezing point of water (0°C).
				Freezing Temperature = 0
				// Boiling represents the boiling point of water (100°C).
				Boiling  Temperature = 100
			)
		`),

		dedentWithBackticks(`
			// AboveFreezing returns true if the temperature is above freezing point.
			func (t Temperature) AboveFreezing() bool
		`),

		dedentWithBackticks(`
			// above returns true if the temperature is above the given threshold.
			func (t Temperature) above(threshold Temperature) bool
		`),

		dedentWithBackticks(`
			// Reading represents a temperature measurement at a specific time and location.
			type Reading struct {
				// Value is the temperature value.
				Value     Temperature
				// Timestamp is the time of the reading.
				Timestamp time.Time
				// location is the location of the reading.
				location  string
			}
		`),

		dedentWithBackticks(`
			// DefaultLocation is the default location used when none is specified.
			var DefaultLocation = "Unknown"
		`),

		dedentWithBackticks(`
			// NewReading creates a new temperature reading with the given value and location.
			// If location is empty, DefaultLocation is used.
			func NewReading(t Temperature, location string) Reading
		`),

		dedentWithBackticks(`
			// Average calculates the average temperature from a slice of temperature values.
			// Returns 0 if the slice is empty.
			func Average(values []Temperature) Temperature
		`),

		dedentWithBackticks(`
			// sumTemp calculates the sum of all temperature values in the slice.
			func sumTemp(values []Temperature) Temperature
		`),

		dedentWithBackticks(`
			// Package mypkg is a sample package.
			package mypkg
		`),
	}

	// Phase 2 – identifiers in the black-box test package (mypkg_test):
	testPkgSnippets := []string{
		dedentWithBackticks(`
			// assertAboutNow verifies the Reading timestamp is recent (within 100ms).
			func assertAboutNow(t *testing.T, r mypkg.Reading)
		`),
	}

	// Phase 3 – helpers in the main package’s *_test files:
	mainPkgTestSnippets := []string{
		dedentWithBackticks(`
			// tempsFreezingBoiling returns a slice of Temperature containing freezing and boiling temperatures.
			func tempsFreezingBoiling() []Temperature
		`),
	}

	responses := []string{
		"Here are the documentation snippets:\n\n" + strings.Join(nonTestSnippets, "\n\n"),
		"Here are the documentation snippets:\n\n" + strings.Join(testPkgSnippets, "\n\n"),
		"Here are the documentation snippets:\n\n" + strings.Join(mainPkgTestSnippets, "\n\n"),
	}

	conv := &responsesCompleter{responses: responses}

	// ---------------------------------------------------------------------
	// Run the fixture including the test files and invoke AddDocs with
	// DocumentTestFiles set to true.
	// ---------------------------------------------------------------------
	withCodeFixture(t, func(pkg *gocode.Package) {
		changes, err := AddDocs(pkg, AddDocsOptions{
			BaseOptions:       BaseOptions{Completer: conv},
			DocumentTestFiles: true,
			// Ctx:               health.NewCtx(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))),
		})
		assert.NoError(t, err)

		updatedFiles := filenamesFromChanges(changes)
		// Both test files should have been updated alongside the regular files.
		assert.Contains(t, updatedFiles, "average_test.go")
		assert.Contains(t, updatedFiles, "reading_test.go")

		pkg, err = pkg.Reload()
		assert.NoError(t, err)

		// Verify helper docs were added and TestXxx docs were NOT added.
		// Average test helper lives in main package test file.
		avgContent := string(pkg.Files["average_test.go"].Contents)
		assert.Contains(t, avgContent, "// tempsFreezingBoiling returns")
		assert.NotContains(t, avgContent, "// TestAverage")

		// Helper in the black-box test package.
		if pkg.TestPackage != nil {
			readTestContent := string(pkg.TestPackage.Files["reading_test.go"].Contents)
			assert.Contains(t, readTestContent, "// assertAboutNow verifies")
			assert.NotContains(t, readTestContent, "// TestReading")
		} else {
			t.Fatalf("expected TestPackage to be non-nil")
		}

		// Ensure the helpers were requested for documentation and TestXxx funcs were not.
		var allUserMsgs []string
		for _, c := range conv.convs {
			allUserMsgs = append(allUserMsgs, c.userMessagesText...)
		}
		combinedMsgs := strings.Join(allUserMsgs, "\n")

		assert.Contains(t, combinedMsgs, "tempsFreezingBoiling")
		assert.Contains(t, combinedMsgs, "assertAboutNow")
		assert.NotContains(t, combinedMsgs, "- TestAverage") // TestAverage appears (as context), but "- TestAverage" would only appear if we list it as an identifier to document
		assert.NotContains(t, combinedMsgs, "- TestReading")
	})
}

func TestAddDocs_OnlyDocumentExportedIdentifier_DocumentTestFiles(t *testing.T) {
	files := map[string]string{
		"code.go": dedent(`
			// Package mypkg exercises public-only test documentation.
			package mypkg
		`),
		"whitebox_test.go": dedent(`
			package mypkg

			import "testing"

			func TestWhitebox(t *testing.T) {
				ExportedSamePackageHelper()
				unexportedSamePackageHelper()
			}

			func ExportedSamePackageHelper() {}

			func unexportedSamePackageHelper() {}
		`),
		"blackbox_test.go": dedent(`
			package mypkg_test

			import "testing"

			func TestBlackbox(t *testing.T) {
				ExportedBlackBoxHelper()
				unexportedBlackBoxHelper()
			}

			func ExportedBlackBoxHelper() {}

			func unexportedBlackBoxHelper() {}
		`),
	}

	blackBoxSnippets := []string{
		dedentWithBackticks(`
			// ExportedBlackBoxHelper supports external package tests.
			func ExportedBlackBoxHelper()
		`),
		dedentWithBackticks(`
			// unexportedBlackBoxHelper supports external package tests privately.
			func unexportedBlackBoxHelper()
		`),
	}
	samePackageSnippets := []string{
		dedentWithBackticks(`
			// ExportedSamePackageHelper supports same-package tests.
			func ExportedSamePackageHelper()
		`),
		dedentWithBackticks(`
			// unexportedSamePackageHelper supports same-package tests privately.
			func unexportedSamePackageHelper()
		`),
	}
	conv := &responsesCompleter{responses: []string{
		"Here are the documentation snippets:\n\n" + strings.Join(blackBoxSnippets, "\n\n"),
		"Here are the documentation snippets:\n\n" + strings.Join(samePackageSnippets, "\n\n"),
	}}

	gocodetesting.WithMultiCode(t, files, func(pkg *gocode.Package) {
		changes, err := AddDocs(pkg, AddDocsOptions{
			DocumentTestFiles:               true,
			OnlyDocumentExportedIdentifiers: true,
			BaseOptions:                     BaseOptions{Completer: conv},
		})
		require.NoError(t, err)

		updatedFiles := filenamesFromChanges(changes)
		assert.Contains(t, updatedFiles, "whitebox_test.go")
		assert.Contains(t, updatedFiles, "blackbox_test.go")

		pkg, err = pkg.Reload()
		require.NoError(t, err)
		require.NotNil(t, pkg.TestPackage)

		whiteboxContent := string(pkg.Files["whitebox_test.go"].Contents)
		assert.Contains(t, whiteboxContent, "// ExportedSamePackageHelper supports same-package tests.")
		assert.NotContains(t, whiteboxContent, "// unexportedSamePackageHelper supports same-package tests privately.")
		assert.NotContains(t, whiteboxContent, "// TestWhitebox")

		blackboxContent := string(pkg.TestPackage.Files["blackbox_test.go"].Contents)
		assert.Contains(t, blackboxContent, "// ExportedBlackBoxHelper supports external package tests.")
		assert.NotContains(t, blackboxContent, "// unexportedBlackBoxHelper supports external package tests privately.")
		assert.NotContains(t, blackboxContent, "// TestBlackbox")
	})
}

func TestAddDocs_Fix(t *testing.T) {
	// ---------------------------------------------------------------------
	// In this test, the first LLM response includes an invalid snippet that
	// fails to apply (doc + EOL comment). AddDocs should then invoke the
	// fix-up flow, resulting in a second LLM request whose response contains
	// the corrected snippet. We verify that the final source contains the
	// corrected documentation and no lingering EOL comment.
	// ---------------------------------------------------------------------

	// Invalid snippet – has both a doc comment and an end-of-line comment.
	badTemperatureSnippet := dedentWithBackticks(`
        // Temperature represents a temperature value in degrees Celsius.
        type Temperature int // Temperature represents a temperature value in degrees Celsius.
    `)

	// Corrected snippet – only the doc comment.
	goodTemperatureSnippet := dedentWithBackticks(`
        // Temperature represents a temperature value in degrees Celsius.
        type Temperature int
    `)

	// All the other (valid) snippets that the first response will contain.
	otherSnippets := []string{
		dedentWithBackticks(`
            const (
                // Freezing represents the freezing point of water (0°C).
                Freezing Temperature = 0
                // Boiling represents the boiling point of water (100°C).
                Boiling  Temperature = 100
            )
        `),
		dedentWithBackticks(`
            // AboveFreezing returns true if the temperature is above freezing point.
            func (t Temperature) AboveFreezing() bool
        `),
		dedentWithBackticks(`
            // above returns true if the temperature is above the given threshold.
            func (t Temperature) above(threshold Temperature) bool
        `),
		dedentWithBackticks(`
            // Reading represents a temperature measurement at a specific time and location.
            type Reading struct {
                // Value is the temperature value.
                Value     Temperature
                // Timestamp is the time of the reading.
                Timestamp time.Time
                // location is the location of the reading.
                location  string
            }
        `),
		dedentWithBackticks(`
            // DefaultLocation is the default location used when none is specified.
            var DefaultLocation = "Unknown"
        `),
		dedentWithBackticks(`
            // NewReading creates a new temperature reading with the given value and location.
            // If location is empty, DefaultLocation is used.
            func NewReading(t Temperature, location string) Reading
        `),
		dedentWithBackticks(`
            // Average calculates the average temperature from a slice of temperature values.
            // Returns 0 if the slice is empty.
            func Average(values []Temperature) Temperature
        `),
		dedentWithBackticks(`
            // sumTemp calculates the sum of all temperature values in the slice.
            func sumTemp(values []Temperature) Temperature
        `),
		dedentWithBackticks(`
            // Package mypkg is a sample package.
            package mypkg
        `),
	}

	// First response: all snippets, but Temperature snippet is invalid.
	firstResponseSnippets := append([]string{badTemperatureSnippet}, otherSnippets...)

	firstResponse := "Here are the documentation snippets:\n\n" + strings.Join(firstResponseSnippets, "\n\n")

	// Second response (fix attempt): only the corrected Temperature snippet.
	secondResponse := "Here are the documentation snippets:\n\n" + goodTemperatureSnippet

	conv := &responsesCompleter{responses: []string{firstResponse, secondResponse}}

	withCodeFixture(t, func(pkg *gocode.Package) {
		changes, err := AddDocs(pkg, AddDocsOptions{
			BaseOptions: BaseOptions{Completer: conv},
		})
		assert.NoError(t, err)
		// We expect at least temperature.go to have been updated.
		assert.Contains(t, filenamesFromChanges(changes), "temperature.go")

		// Reload the package to read updated contents.
		pkg, err = pkg.Reload()
		assert.NoError(t, err)

		tempContent := string(pkg.Files["temperature.go"].Contents)
		// The corrected doc comment should be present.
		assert.Contains(t, tempContent, "// Temperature represents a temperature value in degrees Celsius.")
		// The erroneous EOL comment should NOT be present.
		assert.NotContains(t, tempContent, "int // Temperature represents")
	})
}

func TestAddDocs_SendsContext(t *testing.T) {
	// ---------------------------------------------------------------------
	// This test verifies that AddDocs sends complete context to the LLM,
	// including both the current package source files and any dependency
	// package snippets required for documentation.
	// ---------------------------------------------------------------------

	// Documentation snippets that the mock LLM will return. These cover all
	// identifiers in the fixture plus the new UseDep helper that imports
	// a dependency package.
	snippets := []string{
		dedentWithBackticks(`
            // Temperature represents a temperature value in degrees Celsius.
            type Temperature int
        `),
		dedentWithBackticks(`
            const (
                // Freezing represents the freezing point of water (0°C).
                Freezing Temperature = 0
                // Boiling represents the boiling point of water (100°C).
                Boiling  Temperature = 100
            )
        `),
		dedentWithBackticks(`
            // AboveFreezing returns true if the temperature is above freezing point.
            func (t Temperature) AboveFreezing() bool
        `),
		dedentWithBackticks(`
            // above returns true if the temperature is above the given threshold.
            func (t Temperature) above(threshold Temperature) bool
        `),
		dedentWithBackticks(`
            // Reading represents a temperature measurement at a specific time and location.
            type Reading struct {
                // Value is the temperature value.
                Value     Temperature
                // Timestamp is the time of the reading.
                Timestamp time.Time
                // location is the location of the reading.
                location  string
            }
        `),
		dedentWithBackticks(`
            // DefaultLocation is the default location used when none is specified.
            var DefaultLocation = "Unknown"
        `),
		dedentWithBackticks(`
            // NewReading creates a new temperature reading with the given value and location.
            // If location is empty, DefaultLocation is used.
            func NewReading(t Temperature, location string) Reading
        `),
		dedentWithBackticks(`
            // Average calculates the average temperature from a slice of temperature values.
            // Returns 0 if the slice is empty.
            func Average(values []Temperature) Temperature
        `),
		dedentWithBackticks(`
            // sumTemp calculates the sum of all temperature values in the slice.
            func sumTemp(values []Temperature) Temperature
        `),
		dedentWithBackticks(`
            // UseDep returns the dependency type unchanged.
            func UseDep(d otherpkg.DepType) otherpkg.DepType
        `),
		dedentWithBackticks(`
            // Package mypkg is a sample package.
            package mypkg
        `),
	}

	responseText := "Here are the documentation snippets:\n\n" + strings.Join(snippets, "\n\n")

	// Provide the same canned response for multiple conversations to avoid
	// running out of responses if AddDocs needs several iterations.
	conv := &responsesCompleter{responses: []string{responseText, responseText, responseText}}

	withCodeFixture(t, func(pkg *gocode.Package) {
		// ------------------------------------------------------------------
		// Create a dependency package inside the temporary module.
		// ------------------------------------------------------------------
		mod := pkg.Module
		depDir := filepath.Join(mod.AbsolutePath, "otherpkg")
		err := os.Mkdir(depDir, 0755)
		assert.NoError(t, err)

		depCode := `package otherpkg

// DepType is an example dependency type used by mypkg.
// It intentionally has documentation so that PublicSnippet() returns bytes.
type DepType struct {
    Field int
}`
		err = os.WriteFile(filepath.Join(depDir, "other.go"), []byte(depCode), 0644)
		assert.NoError(t, err)

		// Load the dependency package so gocode can resolve it.
		_, err = mod.ReadPackage("otherpkg", nil)
		assert.NoError(t, err)

		// ------------------------------------------------------------------
		// Add a new file in mypkg that references the dependency package.
		// ------------------------------------------------------------------
		useDepCode := `package mypkg

import "` + mod.Name + `/otherpkg"

func UseDep(d otherpkg.DepType) otherpkg.DepType {
    return d
}`
		err = os.WriteFile(filepath.Join(pkg.AbsolutePath(), "dep.go"), []byte(useDepCode), 0644)
		assert.NoError(t, err)

		// Reload mypkg so it includes the new dep.go file.
		pkg, err = mod.ReadPackage(pkg.RelativeDir, nil)
		assert.NoError(t, err)

		// ------------------------------------------------------------------
		// Invoke AddDocs and capture the context sent to the LLM.
		// ------------------------------------------------------------------
		_, err = AddDocs(pkg, AddDocsOptions{
			BaseOptions: BaseOptions{Completer: conv},
		})
		assert.NoError(t, err)

		// Concatenate all user messages that were sent to the mock LLM.
		var allUserMsgs []string
		for _, c := range conv.convs {
			allUserMsgs = append(allUserMsgs, c.userMessagesText...)
		}
		combinedMsgs := strings.Join(allUserMsgs, "\n")

		// ------------------------------------------------------------------
		// Assertions:
		//   1. Context from the package itself (dep.go and existing files).
		//   2. Context from the dependency package (mymodule/otherpkg).
		// ------------------------------------------------------------------
		assert.Contains(t, combinedMsgs, "// dep.go:")
		assert.Contains(t, combinedMsgs, "// average.go:")
		assert.Contains(t, combinedMsgs, "// temperature.go:")

		// Dependency package header and snippet should be present.
		assert.Contains(t, combinedMsgs, "Select documentation from dependency packages")
		assert.Contains(t, combinedMsgs, "// "+mod.Name+"/otherpkg:")
		assert.Contains(t, combinedMsgs, "type DepType")
	})
}

func TestContextForAddDocsPartial_Order(t *testing.T) {
	withCodeFixture(t, func(pkg *gocode.Package) {
		idents := NewIdentifiersFromPackage(pkg)

		codeCtx, _, err := contextForAddDocsPartial(pkg, idents, defaultTokenBudget, true, BaseOptions{})
		assert.NoError(t, err)
		codeContext := codeCtx.Code()

		// We expect the files to be in alphabetical order.
		avgIdx := strings.Index(codeContext, "average.go")
		readIdx := strings.Index(codeContext, "reading.go")
		tempIdx := strings.Index(codeContext, "temperature.go")

		assert.True(t, avgIdx != -1, "average.go not found")
		assert.True(t, readIdx != -1, "reading.go not found")
		assert.True(t, tempIdx != -1, "temperature.go not found")

		assert.True(t, avgIdx < readIdx, "average.go should come before reading.go")
		assert.True(t, readIdx < tempIdx, "reading.go should come before temperature.go")

		// Inside temperature.go, we expect `Temperature` to be defined before `AboveFreezing`
		tempFileContent := codeContext[tempIdx:]
		typeIdx := strings.Index(tempFileContent, "type Temperature int")
		funcIdx := strings.Index(tempFileContent, "func (t Temperature) AboveFreezing() bool")

		assert.True(t, typeIdx != -1, "type Temperature not found")
		assert.True(t, funcIdx != -1, "AboveFreezing func not found")

		assert.True(t, typeIdx < funcIdx, "type Temperature should come before AboveFreezing func")
	})
}

func TestAddDocs_ExcludeIdentifiers(t *testing.T) {
	code := dedent(`
               func Foo() {}
               func Bar() {}
       `)
	snippet := dedentWithBackticks(`
               // Foo does something.
               func Foo()
       `)
	conv := &responsesCompleter{responses: []string{"Here are the documentation snippets:\n\n" + snippet}}

	gocodetesting.WithCode(t, code, func(pkg *gocode.Package) {
		changes, err := AddDocs(pkg, AddDocsOptions{
			BaseOptions:        BaseOptions{Completer: conv},
			ExcludeIdentifiers: []string{"Bar", gocode.PackageIdentifier},
		})
		assert.NoError(t, err)
		assert.Contains(t, filenamesFromChanges(changes), "code.go")

		pkg, err = pkg.Reload()
		assert.NoError(t, err)
		content := string(pkg.Files["code.go"].Contents)
		assert.Contains(t, content, "// Foo does something.")
		assert.NotContains(t, content, "// Bar")
	})
}

func TestAddDocs_SkipGeneratedFiles(t *testing.T) {
	snippetFoo := dedentWithBackticks(`
               // Foo does something.
               func Foo()
       `)
	conv := &responsesCompleter{responses: []string{"Here are the documentation snippets:\n\n" + snippetFoo}}

	tmpDir, err := os.MkdirTemp("", "doc-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	pkgDir := filepath.Join(tmpDir, "mypkg")
	err = os.Mkdir(pkgDir, 0755)
	assert.NoError(t, err)

	err = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module mymodule"), 0644)
	assert.NoError(t, err)

	err = os.WriteFile(filepath.Join(pkgDir, "code.go"), []byte("package mypkg\n\nfunc Foo() {}\n"), 0644)
	assert.NoError(t, err)

	genContent := "// Code generated by x; DO NOT EDIT.\npackage mypkg\n\nfunc Bar() {}\n"
	err = os.WriteFile(filepath.Join(pkgDir, "generated.go"), []byte(genContent), 0644)
	assert.NoError(t, err)

	module, err := gocode.NewModule(tmpDir)
	assert.NoError(t, err)

	pkg, err := module.LoadPackageByRelativeDir("mypkg")
	assert.NoError(t, err)

	changes, err := AddDocs(pkg, AddDocsOptions{BaseOptions: BaseOptions{Completer: conv}})
	assert.NoError(t, err)
	files := filenamesFromChanges(changes)
	assert.Contains(t, files, "code.go")
	assert.NotContains(t, files, "generated.go")

	pkg, err = pkg.Reload()
	assert.NoError(t, err)

	content := string(pkg.Files["code.go"].Contents)
	assert.Contains(t, content, "// Foo does something.")

	gen := string(pkg.Files["generated.go"].Contents)
	assert.NotContains(t, gen, "// Bar does something.")
}

func TestAddDocs_OnlyDocumentExportedIdentifier_SkipGeneratedFiles(t *testing.T) {
	snippets := []string{
		dedentWithBackticks(`
			// Foo does something.
			func Foo()
		`),
		dedentWithBackticks(`
			// Bar does something.
			func Bar()
		`),
	}
	conv := &responsesCompleter{responses: []string{
		"Here are the documentation snippets:\n\n" + strings.Join(snippets, "\n\n"),
	}}

	files := map[string]string{
		"code.go": dedent(`
			func Foo() {}
		`),
		"generated.go": dedent(`
			// Code generated by x; DO NOT EDIT.
			package mypkg

			func Bar() {}
		`),
	}
	gocodetesting.WithMultiCode(t, files, func(pkg *gocode.Package) {
		changes, err := AddDocs(pkg, AddDocsOptions{
			OnlyDocumentExportedIdentifiers: true,
			ExcludeIdentifiers:              []string{gocode.PackageIdentifier},
			BaseOptions:                     BaseOptions{Completer: conv},
		})
		require.NoError(t, err)

		updatedFiles := filenamesFromChanges(changes)
		assert.Contains(t, updatedFiles, "code.go")
		assert.NotContains(t, updatedFiles, "generated.go")
		assert.NotContains(t, conv.allUserText(), "- Bar")

		pkg, err = pkg.Reload()
		require.NoError(t, err)

		content := string(pkg.Files["code.go"].Contents)
		assert.Contains(t, content, "// Foo does something.")

		generatedContent := string(pkg.Files["generated.go"].Contents)
		assert.NotContains(t, generatedContent, "// Bar does something.")
	})
}

func TestContextForAddDocsPartial(t *testing.T) {
	t.Run("table", func(t *testing.T) {
		tests := []struct {
			name              string
			files             map[string]string // filename -> code
			budget            int
			documentTestFiles bool
			wantIds           []string
			wantErrIsBudget   bool
		}{
			{
				name: "single func fits",
				files: map[string]string{"code.go": dedent(`
                    func A() {}
                `)},
				budget:            1000,
				documentTestFiles: false,
				wantIds:           []string{"A"}, // NOTE: package isn't here because package is only a used-by dep if it has docs; otherwise it has a dep to everything.
			},
			{
				name: "test helper included, TestXxx excluded",
				files: map[string]string{
					"code_test.go": dedent(`
                        package mypkg
                        import "testing"

                        func helper() {}

                        func TestA(t *testing.T) { helper() }
                    `),
				},
				budget:            1000,
				documentTestFiles: true,
				wantIds:           []string{"helper", "package"}, // NOTE: package is here because all of package's direct non-test deps are documented (there are none).
			},
			{
				name: "budget exceeded cannot prune",
				files: map[string]string{"code.go": dedent(`
                    func A() {}
                `)},
				budget:          1,    // Intentionally too small to fit any group
				wantErrIsBudget: true, // Expect token budget exceeded error
			},
			{
				name: "only tests without helpers -> package only when not documenting tests",
				files: map[string]string{
					"code_test.go": dedent(`
                        package mypkg
                        import "testing"

                        func TestA(t *testing.T) {}
                    `),
				},
				budget:            1000,
				documentTestFiles: false,
				wantIds:           []string{"package"}, // with no non-test ids, package is eligible
			},
			{
				name: "const block returns all identifiers",
				files: map[string]string{"code.go": dedent(`
                        const (
                            A = 1
                            B = 2
                        )
                    `)},
				budget:            1000,
				documentTestFiles: false,
				wantIds:           []string{"A", "B"},
			},
			{
				name: "mixed code + test helper, test helpers excluded when flag is false",
				files: map[string]string{
					"code.go": dedent(`
                            func A() {}
                        `),
					"code_test.go": dedent(`
                            package mypkg
                            func helper() {}
                        `),
				},
				budget:            1000,
				documentTestFiles: false,
				wantIds:           []string{"A"},
			},
			{
				name: "external dependency usage does not alter ids",
				files: map[string]string{"code.go": dedent(`
                        import "fmt"
                        func A() { fmt.Println("hi") }
                    `)},
				budget:            1000,
				documentTestFiles: false,
				wantIds:           []string{"A"},
			},
			{
				name: "type and method cause both to be selected (method added for free)",
				files: map[string]string{"code.go": dedent(`
                        type T struct{}
                        func (t T) M() {}
                    `)},
				budget:            1000,
				documentTestFiles: false,
				wantIds:           []string{"T", "T.M"},
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				gocodetesting.WithMultiCode(t, tc.files, func(pkg *gocode.Package) {
					idents := NewIdentifiersFromPackage(pkg)

					ctx, ids, err := contextForAddDocsPartial(pkg, idents, tc.budget, tc.documentTestFiles, BaseOptions{})

					if tc.wantErrIsBudget {
						require.Error(t, err)
						var te *tokenBudgetExceededError
						assert.True(t, errors.As(err, &te), "expected tokenBudgetExceededError, got %v", err)
						assert.Nil(t, ctx)
						assert.Nil(t, ids)
						return
					}

					require.NoError(t, err)
					require.NotNil(t, ctx)
					assert.ElementsMatch(t, tc.wantIds, ids)
				})
			})
		}
	})
}
