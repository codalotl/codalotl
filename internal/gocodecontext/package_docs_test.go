package gocodecontext

import (
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublicPackageDocumentation_Reflexive(t *testing.T) {
	mod, err := gocode.NewModule(gocode.MustCwd())
	require.NoError(t, err)

	pkg, err := mod.LoadPackageByRelativeDir("internal/gocodecontext")
	require.NoError(t, err)

	got, err := PublicPackageDocumentation(pkg)
	require.NoError(t, err)

	// fmt.Println(got)

	//
	// Basic sanity checks. Don't add too many; we don't want test to become brittle.
	//

	assert.Contains(t, got, "// package_docs.go:")                                                                         // File marker
	assert.Contains(t, got, "// PublicPackageDocumentation returns a godoc-like documentation string for the package:")    // comment on the below sig
	assert.Contains(t, got, "func PublicPackageDocumentation(pkg *gocode.Package, identifiers ...string) (string, error)") // signature
	assert.NotContains(t, got, "\toriginalGroups []*IdentifierGroup")                                                      // private field in struct
	assert.NotContains(t, got, "import (")                                                                                 // no imports
	assert.NotContains(t, got, "groupHasExportedIdentifier")                                                               // private method
}

func TestPublicPackageDocumentation_Basic(t *testing.T) {
	code := map[string]string{
		"doc.go": gocodetesting.Dedent(`
			// Package mypkg provides helpers.
			package mypkg
		`),
		"types.go": gocodetesting.Dedent(`
			package mypkg

			//
			// Types (shouldn't go in docs):
			//

			// Exported is a demo struct.
			type Exported struct {
				ExportedField int  // ExportedField doc.
				unexported    int
			}

			type unexported struct{}
		`),
		"funcs.go": gocodetesting.Dedent(`
			package mypkg

			// ExportedFunc does something.
			func ExportedFunc() {}

			func exportedFunc() {}

			func (e Exported) Method() {}
			func (e *Exported) helper() {}

			func _() {} // anon func
		`),
		"values.go": gocodetesting.Dedent(`
			package mypkg

			const (
				// ExportedConst doc.
				ExportedConst = 1
				unexportedConst = 2
			)

			var (
				// ExportedVar doc.
				ExportedVar = 3
				unexportedVar = 4
			)
		`),
		"helpers_test.go": gocodetesting.Dedent(`
			package mypkg

			import "testing"

			func TestThing(t *testing.T) {}
		`),
	}

	gocodetesting.WithMultiCode(t, code, func(pkg *gocode.Package) {
		got, err := PublicPackageDocumentation(pkg)
		require.NoError(t, err)

		want := gocodetesting.Dedent(`
			// doc.go:
			
			// Package mypkg provides helpers.
			package mypkg

			// funcs.go:

			// ExportedFunc does something.
			func ExportedFunc()

			func (e Exported) Method()

			// types.go:

			// Exported is a demo struct.
			type Exported struct {
				ExportedField int // ExportedField doc.
				// contains filtered or unexported fields
			}

			// values.go:

			const (
				// ExportedConst doc.
				ExportedConst = 1
			)

			var (
				// ExportedVar doc.
				ExportedVar = 3
			)
		`)

		assert.Equal(t, strings.TrimSpace(want), strings.TrimSpace(got))
		assert.NotContains(t, got, "TestThing")
		assert.NotContains(t, got, "exportedFunc")
		assert.NotContains(t, got, "unexportedVar")
	})
}

func TestPublicPackageDocumentation_SkipsEmpty(t *testing.T) {
	code := map[string]string{
		"code.go": gocodetesting.Dedent(`
			package mypkg

			var hidden = 1
		`),
	}

	gocodetesting.WithMultiCode(t, code, func(pkg *gocode.Package) {
		got, err := PublicPackageDocumentation(pkg)
		require.NoError(t, err)
		assert.Equal(t, "", strings.TrimSpace(got))
	})
}

func TestPublicPackageDocumentation_ErrorsForTestPackage(t *testing.T) {
	code := map[string]string{
		"code.go": gocodetesting.Dedent(`
			package mypkg

			func Visible() {}
		`),
		"code_test.go": gocodetesting.Dedent(`
			package mypkg_test

			import "testing"

			func TestVisible(t *testing.T) {}
		`),
	}

	gocodetesting.WithMultiCode(t, code, func(pkg *gocode.Package) {
		require.NotNil(t, pkg.TestPackage)

		_, err := PublicPackageDocumentation(pkg.TestPackage)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "test package")
	})
}

func TestPublicPackageDocumentation_LimitedToIdentifiers_TypeIncludesMethods(t *testing.T) {
	code := map[string]string{
		"doc.go": gocodetesting.Dedent(`
			// Package mypkg provides helpers.
			package mypkg
		`),
		"types.go": gocodetesting.Dedent(`
			package mypkg

			// Exported is a demo struct.
			type Exported struct {
				ExportedField int
			}
		`),
		"funcs.go": gocodetesting.Dedent(`
			package mypkg

			func ExportedFunc() {}
			func (e Exported) Method() {}
			func (e *Exported) PMethod() {}
		`),
		"values.go": gocodetesting.Dedent(`
			package mypkg

			const ExportedConst = 1
			var ExportedVar = 2
		`),
	}

	gocodetesting.WithMultiCode(t, code, func(pkg *gocode.Package) {
		got, err := PublicPackageDocumentation(pkg, "Exported") // limit to the type 'Exported'
		require.NoError(t, err)

		// Should include the type and its exported methods, but nothing else.
		assert.Contains(t, got, "type Exported struct")
		assert.Contains(t, got, "func (e Exported) Method()")
		assert.Contains(t, got, "func (e *Exported) PMethod()")

		assert.NotContains(t, got, "ExportedFunc(")
		assert.NotContains(t, got, "ExportedConst")
		assert.NotContains(t, got, "ExportedVar")
		assert.NotContains(t, got, "// doc.go:") // package docs not requested
	})
}

func TestPublicPackageDocumentation_LimitedToIdentifiers_SpecificFunction(t *testing.T) {
	code := map[string]string{
		"funcs.go": gocodetesting.Dedent(`
			package mypkg

			func ExportedFunc() {}
			func AnotherExported() {}
		`),
	}

	gocodetesting.WithMultiCode(t, code, func(pkg *gocode.Package) {
		got, err := PublicPackageDocumentation(pkg, "ExportedFunc")
		require.NoError(t, err)

		assert.Contains(t, got, "func ExportedFunc()")
		assert.NotContains(t, got, "AnotherExported")
	})
}

func TestInternalPackageSignatures_Reflexive(t *testing.T) {
	mod, err := gocode.NewModule(gocode.MustCwd())
	require.NoError(t, err)

	pkg, err := mod.LoadPackageByRelativeDir("internal/gocodecontext")
	require.NoError(t, err)

	got, err := InternalPackageSignatures(pkg, false, false)
	require.NoError(t, err)

	// fmt.Println(got)

	//
	// Basic sanity checks. Don't add too many; we don't want test to become brittle.
	//

	assert.Contains(t, got, "// package_docs.go:")                                                                         // File marker
	assert.NotContains(t, got, "// PublicPackageDocumentation returns a godoc-like documentation string for the package:") // comment on the below sig
	assert.Contains(t, got, "func PublicPackageDocumentation(pkg *gocode.Package, identifiers ...string) (string, error)") // signature
	assert.Contains(t, got, "\toriginalGroups []*IdentifierGroup")                                                         // private field in struct
	assert.Contains(t, got, "import (")                                                                                    // no imports
	assert.Contains(t, got, "groupHasExportedIdentifier")                                                                  // private method
}

func TestInternalPackageSignatures_Basic(t *testing.T) {
	gocodetesting.WithMultiCode(t, internalSignatureSampleCode(), func(pkg *gocode.Package) {
		got, err := InternalPackageSignatures(pkg, false, true)
		require.NoError(t, err)

		want := gocodetesting.Dedent(`
			// doc.go:

			// Package mypkg gives helpers.
			package mypkg

			// main.go:

			package mypkg

			import "fmt"

			// Foo docs.
			func Foo(x int) string

			// floating comment between declarations

			var helper = Foo(1)

			type widget struct {
				// Widget doc field.
				name string
			}
		`)

		assert.Equal(t, strings.TrimSpace(want), strings.TrimSpace(got))
		assert.NotContains(t, got, "fmt.Sprintf(")
		assert.NotContains(t, got, "return")
		assert.NotContains(t, got, "TestFoo(")
	})
}

func TestInternalPackageSignatures_NoDocs(t *testing.T) {
	gocodetesting.WithMultiCode(t, internalSignatureSampleCode(), func(pkg *gocode.Package) {
		got, err := InternalPackageSignatures(pkg, false, false)
		require.NoError(t, err)

		want := gocodetesting.Dedent(`
			// doc.go:
			package mypkg
			// main.go:
			package mypkg
			import "fmt"
			func Foo(x int) string
			var helper = Foo(1)
			type widget struct {
				name string
			}
		`)

		assert.Equal(t, strings.TrimSpace(want), strings.TrimSpace(got))
		assert.NotContains(t, got, "// Package mypkg gives helpers.")
		assert.NotContains(t, got, "// Foo docs.")
		assert.NotContains(t, got, "Widget doc field.")
		assert.NotContains(t, got, "floating comment between declarations")
	})
}

func TestInternalPackageSignatures_TestsOnly(t *testing.T) {
	gocodetesting.WithMultiCode(t, internalSignatureSampleCode(), func(pkg *gocode.Package) {
		got, err := InternalPackageSignatures(pkg, true, true)
		require.NoError(t, err)

		want := gocodetesting.Dedent(`
			// tests_test.go:

			package mypkg

			import "testing"

			// TestFoo docs.
			func TestFoo(t *testing.T)

			// floating test comment
		`)

		assert.Equal(t, strings.TrimSpace(want), strings.TrimSpace(got))
		assert.NotContains(t, got, "// doc.go:")
		assert.NotContains(t, got, "package mypkg gives helpers.")

		require.NotNil(t, pkg.TestPackage)

		gotExternal, err := InternalPackageSignatures(pkg.TestPackage, true, false)
		require.NoError(t, err)

		assert.Contains(t, gotExternal, "// external_test.go:")
		assert.NotContains(t, gotExternal, "// TestFoo docs.")
		assert.NotContains(t, gotExternal, "floating test comment")
	})
}

func TestInternalPackageSignatures_TestPackageRequiresTestsFlag(t *testing.T) {
	gocodetesting.WithMultiCode(t, internalSignatureSampleCode(), func(pkg *gocode.Package) {
		require.NotNil(t, pkg.TestPackage)
		_, err := InternalPackageSignatures(pkg.TestPackage, false, true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tests must be true")
	})
}

func internalSignatureSampleCode() map[string]string {
	return map[string]string{
		"doc.go": gocodetesting.Dedent(`
			// Package mypkg gives helpers.
			package mypkg
		`),
		"main.go": gocodetesting.Dedent(`
			package mypkg

			import "fmt"

			// Foo docs.
			func Foo(x int) string {
				return fmt.Sprintf("%d", x)
			}

			// floating comment between declarations

			var helper = Foo(1)

			type widget struct {
				// Widget doc field.
				name string
			}
		`),
		"tests_test.go": gocodetesting.Dedent(`
			package mypkg

			import "testing"

			// TestFoo docs.
			func TestFoo(t *testing.T) {
				t.Helper()
				Foo(2)
			}

			// floating test comment
		`),
		"external_test.go": gocodetesting.Dedent(`
			package mypkg_test

			import "testing"

			func TestExternal(t *testing.T) {
				t.Helper()
			}
		`),
	}
}
