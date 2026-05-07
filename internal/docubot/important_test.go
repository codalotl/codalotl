package docubot

import (
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImportantIdentifiersForPackage_SelectsPolicyCategories(t *testing.T) {
	code := dedent(`
		var PublicValue = 1
		var privateValue = 2

		func Public() {}

		type privateType struct {
			field string
		}

		func (privateType) privateMethod() {}

		func small() {}

		func highFanIn() {}
		func fanInCaller1() { highFanIn() }
		func fanInCaller2() { highFanIn() }
		func fanInCaller3() { highFanIn() }

		func highFanOut() {
			fanOutDep1()
			fanOutDep2()
			fanOutDep3()
			fanOutDep4()
		}
		func fanOutDep1() {}
		func fanOutDep2() {}
		func fanOutDep3() {}
		func fanOutDep4() {}

		func cycleA() { cycleB() }
		func cycleB() { cycleC() }
		func cycleC() { cycleA() }
	`) + "\n" + bigFunctionSource("big")

	gocodetesting.WithCode(t, code, func(pkg *gocode.Package) {
		important, err := importantIdentifiersForPackage(pkg, false, nil, BaseOptions{})
		require.NoError(t, err)

		assert.Contains(t, important, gocode.PackageIdentifier)
		assert.Contains(t, important, "Public")
		assert.Contains(t, important, "PublicValue")
		assert.Contains(t, important, "privateType")
		assert.Contains(t, important, "privateType.privateMethod")
		assert.Contains(t, important, "big")
		assert.Contains(t, important, "highFanIn")
		assert.Contains(t, important, "highFanOut")
		assert.Contains(t, important, "cycleA")
		assert.Contains(t, important, "cycleB")
		assert.Contains(t, important, "cycleC")

		assert.NotContains(t, important, "privateValue")
		assert.NotContains(t, important, "small")
		assert.NotContains(t, important, "fanInCaller1")
		assert.NotContains(t, important, "fanOutDep1")
	})
}

func TestImportantIdentifiersForPackage_DocumentTestFiles(t *testing.T) {
	files := map[string]string{
		"code.go": dedent(`
			package mypkg

			func helper() {}
		`),
		"code_test.go": dedent(`
			package mypkg

			import "testing"

			func TestA(t *testing.T) {}

			func ExportedTestHelper() {}

			type testType struct{}

			func (testType) helper() {}
		`),
	}

	gocodetesting.WithMultiCode(t, files, func(pkg *gocode.Package) {
		withoutTests, err := importantIdentifiersForPackage(pkg, false, nil, BaseOptions{})
		require.NoError(t, err)
		assert.NotContains(t, withoutTests, "ExportedTestHelper")
		assert.NotContains(t, withoutTests, "testType")
		assert.NotContains(t, withoutTests, "testType.helper")
		assert.NotContains(t, withoutTests, "TestA")

		withTests, err := importantIdentifiersForPackage(pkg, true, nil, BaseOptions{})
		require.NoError(t, err)
		assert.Contains(t, withTests, "ExportedTestHelper")
		assert.Contains(t, withTests, "testType")
		assert.Contains(t, withTests, "testType.helper")
		assert.NotContains(t, withTests, "TestA")
	})
}

func TestAddDocs_OnlyDocumentImportantIdentifiers_DocumentsImportantOnly(t *testing.T) {
	code := dedent(`
		func Public() {}

		type privateType struct {
			field string
		}

		func (privateType) helper() {}

		func small() {}
	`) + "\n" + bigFunctionSource("big")

	snippets := []string{
		dedentWithBackticks(`
			// Public performs a public operation.
			func Public()
		`),
		dedentWithBackticks(`
			// privateType stores private state.
			type privateType struct {
				// field stores private data.
				field string
			}
		`),
		dedentWithBackticks(`
			// helper supports privateType.
			func (privateType) helper()
		`),
		dedentWithBackticks(`
			// big performs enough work to be important.
			func big()
		`),
		dedentWithBackticks(`
			// small is intentionally not important.
			func small()
		`),
		dedentWithBackticks(`
			// Package mypkg exercises important documentation.
			package mypkg
		`),
	}
	conv := &responsesCompleter{responses: []string{
		"Here are the documentation snippets:\n\n" + strings.Join(snippets, "\n\n"),
	}}

	gocodetesting.WithCode(t, code, func(pkg *gocode.Package) {
		changes, err := AddDocs(pkg, AddDocsOptions{
			OnlyDocumentImportantIdentifiers: true,
			BaseOptions:                      BaseOptions{Completer: conv},
		})
		require.NoError(t, err)
		assert.Contains(t, filenamesFromChanges(changes), "code.go")

		pkg, err = pkg.Reload()
		require.NoError(t, err)
		content := string(pkg.Files["code.go"].Contents)
		assert.Contains(t, content, "// Public performs a public operation.")
		assert.Contains(t, content, "// privateType stores private state.")
		assert.Contains(t, content, "// field stores private data.")
		assert.Contains(t, content, "// helper supports privateType.")
		assert.Contains(t, content, "// big performs enough work to be important.")
		assert.NotContains(t, content, "// small is intentionally not important.")

		secondConv := &responsesCompleter{responses: []string{"unexpected"}}
		changes, err = AddDocs(pkg, AddDocsOptions{
			OnlyDocumentImportantIdentifiers: true,
			BaseOptions:                      BaseOptions{Completer: secondConv},
		})
		require.NoError(t, err)
		assert.Empty(t, changes)
		assert.Empty(t, secondConv.convs)
	})
}

func TestAddDocs_OnlyDocumentImportantIdentifiers_UnimportantIdentifiersDoNotBlockScratchPass(t *testing.T) {
	code := dedent(`
		func Public() {}

		func small() {}
	`)
	conv := &identifierSnippetsCompleter{
		snippetsByIdentifier: map[string]string{
			"Public": dedentWithBackticks(`
				// Public performs an important operation.
				func Public()
			`),
		},
	}

	gocodetesting.WithCode(t, code, func(pkg *gocode.Package) {
		changes, err := AddDocs(pkg, AddDocsOptions{
			OnlyDocumentImportantIdentifiers: true,
			ExcludeIdentifiers:               []string{gocode.PackageIdentifier},
			BaseOptions:                      BaseOptions{Completer: conv},
		})
		require.NoError(t, err)
		assert.Contains(t, filenamesFromChanges(changes), "code.go")
		assert.Len(t, conv.convs, 1)

		userText := conv.allUserText()
		assert.Contains(t, userText, "- Public")
		assert.NotContains(t, userText, "- small")

		pkg, err = pkg.Reload()
		require.NoError(t, err)
		content := string(pkg.Files["code.go"].Contents)
		assert.Contains(t, content, "// Public performs an important operation.")
		assert.NotContains(t, content, "// small")
	})
}

func TestImportantScratchExclusions_PreservesImportantAndExistingExclusions(t *testing.T) {
	code := dedent(`
		func Public() {}

		func small() {}
	`)

	gocodetesting.WithCode(t, code, func(pkg *gocode.Package) {
		exclusions := importantScratchExclusions([]string{"Existing"}, pkg, map[string]struct{}{
			"Public":                 {},
			gocode.PackageIdentifier: {},
		})

		assert.Contains(t, exclusions, "Existing")
		assert.Contains(t, exclusions, "small")
		assert.NotContains(t, exclusions, "Public")
		assert.NotContains(t, exclusions, gocode.PackageIdentifier)
	})
}

func TestAddDocs_ImportantAndExportedOnlyAreMutuallyExclusive(t *testing.T) {
	conv := &responsesCompleter{responses: []string{"unexpected"}}
	gocodetesting.WithCode(t, "func Foo() {}", func(pkg *gocode.Package) {
		changes, err := AddDocs(pkg, AddDocsOptions{
			OnlyDocumentExportedIdentifiers:  true,
			OnlyDocumentImportantIdentifiers: true,
			BaseOptions:                      BaseOptions{Completer: conv},
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "mutually exclusive")
		assert.Nil(t, changes)
		assert.Empty(t, conv.convs)
	})
}

func bigFunctionSource(name string) string {
	return "func " + name + "() {\n" + strings.Repeat("\tprintln(1)\n", 18) + "}\n"
}
