package updatedocs

import (
	"fmt"
	"os"
	"testing"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"

	"github.com/stretchr/testify/assert"
)

func TestReflowDocumentationBasic(t *testing.T) {
	var (
		initialSource = dedent(`
			package mypkg

			// MyFunction is a function with a very long comment that should be wrapped because it exceeds the maximum line width that we are going to set in the options for the test.
			func MyFunction() {}
		`)
		expectedSource = dedent(`
			package mypkg

			// MyFunction is a function with a very long comment that should be wrapped because
			// it exceeds the maximum line width that we are going to set in the options for
			// the test.
			func MyFunction() {}
		`)
	)

	gocodetesting.WithMultiCode(t, map[string]string{"code.go": initialSource}, func(pkg *gocode.Package) {
		newPkg, failed, err := ReflowDocumentation(pkg, []string{"MyFunction"}, Options{
			ReflowMaxWidth: 80,
		})

		assert.NoError(t, err)
		assert.Empty(t, failed)
		if assert.NotNil(t, newPkg) && assert.Contains(t, newPkg.Files, "code.go") {
			file := newPkg.Files["code.go"]
			assert.Equal(t, expectedSource, string(file.Contents))
		}
	})
}

func TestReflowDocumentationTableDriven(t *testing.T) {
	testCases := []struct {
		name                      string
		initialSource             string
		expectedSource            string
		identifiers               []string
		expectedFailedIdentifiers []string
		options                   Options
	}{
		{
			name: "Simple function comment reflow",
			initialSource: dedent(`
				package mypkg

				// MyFunction is a function with a very long comment that should be wrapped because it exceeds the maximum line width that we are going to set in the options for the test.
				func MyFunction() {}
			`),
			expectedSource: dedent(`
				package mypkg

				// MyFunction is a function with a very long comment that should be wrapped because
				// it exceeds the maximum line width that we are going to set in the options for
				// the test.
				func MyFunction() {}
			`),
			identifiers: []string{"MyFunction"},
		},
		{
			name: "Identifier not found",
			initialSource: dedent(`
				package mypkg

				// MyFunction does something.
				func MyFunction() {}
			`),
			identifiers:               []string{"NonExistentFunction"},
			expectedFailedIdentifiers: []string{"NonExistentFunction"},
		},
		{
			name: "Struct with long comment",
			initialSource: dedent(`
				package mypkg

				// MyStruct is a struct with a very long comment that should be wrapped because it exceeds the maximum line width that we are going to set in the options for the test.
				type MyStruct struct {
					// FieldA is a field with a very long comment that should also be wrapped because it is also very long.
					FieldA string
				}
			`),
			expectedSource: dedent(`
				package mypkg

				// MyStruct is a struct with a very long comment that should be wrapped because it
				// exceeds the maximum line width that we are going to set in the options for the
				// test.
				type MyStruct struct {
					// FieldA is a field with a very long comment that should also be wrapped because
					// it is also very long.
					FieldA string
				}
			`),
			identifiers: []string{"MyStruct"},
			options:     Options{ReflowMaxWidth: 80},
		},
		{
			name: "multiple identifiers in the same snippet",
			initialSource: dedent(`
				package mypkg

				// enums
				// multiline gets single line
				const (
					enum1 int = iota // enum1
					// enum2
					enum2
					enum3 // enum3
				)
			`),
			expectedSource: dedent(`
				package mypkg

				// enums multiline gets single line
				const (
					enum1 int = iota // enum1
					enum2            // enum2
					enum3            // enum3
				)
			`),
			identifiers: []string{"enum1", "enum3"},
		},
		{
			name: "no change needed",
			initialSource: dedent(`
				package mypkg

				// enums multiline gets single line
				const (
					enum1 int = iota // enum1
					enum2            // enum2
					enum3            // enum3
				)
			`),
			identifiers: []string{"enum1"},
		},
		{
			name: "not found plus success",
			initialSource: dedent(`
				package mypkg

				type (
					foo int // very long comment 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789
				)
			`),
			expectedSource: dedent(`
				package mypkg

				type (
					// very long comment 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789
					// 0123456789 0123456789
					foo int
				)
			`),
			identifiers:               []string{"foo", "bar"},
			expectedFailedIdentifiers: []string{"bar"},
		},
		{
			// NOTE: mystruct sticks with doc comments because there's a forced one, and then only 2; otherstruct has a sequence of 3 EOL-izable ones.
			name: "eol-ize comments, 2 snippets",
			initialSource: dedent(`
				package mypkg

				type mystruct struct {
					// foo
					// struct
					foo struct {

					}

					// bar
					bar int
					// baz
					baz int
				}

				type otherstruct struct {
					// very long comment 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789
					foo int
					// eol1
					eol1 int
					// eol2
					eol2 int
					// eol3
					eol3 int

					// very long comment 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789
					bar int
				}
			`),
			expectedSource: dedent(`
				package mypkg

				type mystruct struct {
					// foo struct
					foo struct {
					}

					// bar
					bar int

					// baz
					baz int
				}

				type otherstruct struct {
					// very long comment 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789
					// 0123456789 0123456789
					foo int

					eol1 int // eol1
					eol2 int // eol2
					eol3 int // eol3

					// very long comment 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789
					// 0123456789 0123456789
					bar int
				}
			`),
			identifiers: []string{"mystruct", "otherstruct"},
		},
		{
			name: "keeps floating comments",
			initialSource: dedent(`
				package mypkg

				type mystruct struct {
					// foo
					// struct
					foo struct {
						a int
					}

					// Floating divider

					// bar
					bar int
					// baz
					baz int
				}
			`),
			expectedSource: dedent(`
				package mypkg

				type mystruct struct {
					// foo struct
					foo struct {
						a int
					}

					// Floating divider

					bar int // bar
					baz int // baz
				}
			`),
			identifiers: []string{"mystruct"},
		},
		{
			name: "doesn't modify unrelated snippets",
			initialSource: dedent(`
				package mypkg

				// my
				// function
				func myFunction() {}

				// your
				// function
				func yourFunction() {}
			`),
			expectedSource: dedent(`
				package mypkg

				// my function
				func myFunction() {}

				// your
				// function
				func yourFunction() {}
			`),
			identifiers: []string{"myFunction"},
		},
		{
			name: "can reflow package docs",
			initialSource: dedent(`
				// some docs
				// that are too short
				package mypkg
			`),
			expectedSource: dedent(`
				// some docs that are too short
				package mypkg
			`),
			identifiers: []string{gocode.PackageIdentifier},
		},
		{
			name: "cant reflow a snippet when it's an in invalid state",
			initialSource: dedent(`
				package mypkg

				type x struct {
					// doc
					a int // eol
				}
			`),
			identifiers:               []string{"x"},
			expectedFailedIdentifiers: []string{"x"},
		},
		{
			name: "generics identifiers don't include type params",
			initialSource: dedent(`
				package mypkg

				type Vector[T any] struct {
					// X and Y
					X, Y T
				}
			`),
			expectedSource: dedent(`
				package mypkg

				type Vector[T any] struct {
					X, Y T // X and Y
				}
			`),
			identifiers: []string{"Vector"}, // this test just asserts "Vector" is correct, not "Vector[T]"
		},
		{
			name: "conversion to Doc puts a space above it",
			initialSource: dedent(`
				package mypkg

				type f struct {
					a int // a
					b int // b
					c int // c
					d int // very long comment 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789
				}
			`),
			expectedSource: dedent(`
				package mypkg

				type f struct {
					a int // a
					b int // b
					c int // c

					// very long comment 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789
					// 0123456789 0123456789
					d int
				}
			`),
			identifiers: []string{"f"},
		},
		{
			name: "removes blank lines between specs",
			initialSource: dedent(`
				package mypkg

				const (

					A = 1 // a

					B = 1 // b

					C = 1 // c
				)
			`),
			expectedSource: dedent(`
				package mypkg

				const (
					A = 1 // a
					B = 1 // b
					C = 1 // c
				)
			`),
			identifiers: []string{"A"},
		},
		{
			name: "removes blank lines between fields",
			initialSource: dedent(`
				package mypkg

				type foo struct {

					a int // a

					b int // b

					// c
					c int

				}
			`),
			expectedSource: dedent(`
				package mypkg

				type foo struct {
					a int // a
					b int // b
					c int // c
				}
			`),
			identifiers: []string{"foo"},
		},
		{
			name: "formats nested structs",
			initialSource: dedent(`
				package mypkg

				type foo struct {

					// a
					a int

					b int // b

					c struct {
						d int

						e int

						// very long comment 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789
						f int

					}

				}
			`),
			expectedSource: dedent(`
				package mypkg

				type foo struct {
					// a
					a int

					// b
					b int

					c struct {
						d int
						e int

						// very long comment 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789
						// 0123456789 0123456789
						f int
					}
				}
			`),
			identifiers: []string{"foo"},
		},
		{
			name: "single-line structs, interfaces",
			initialSource: dedent(`
				package mypkg

				// S
				type S struct{ err error }

				// I
				type I struct{ err error }
			`),
			expectedSource: dedent(`
				package mypkg

				// S
				type S struct{ err error }

				// I
				type I struct{ err error }
			`),
			identifiers: []string{"S", "I"},
		},
		{
			name: "doesn't squish leading/trailing floater comments",
			initialSource: dedent(`
				package mypkg

				type S struct {

					//
					// Divider
					//

					A int // A
					B int // B
					C int // C

					//
					// End Divider
					//
				}
			`),
			expectedSource: dedent(`
				package mypkg

				type S struct {
					//
					// Divider
					//

					A int // A
					B int // B
					C int // C

					//
					// End Divider
					//
				}
			`),
			identifiers: []string{"S"},
		},
		{
			name: "keeps floaters between EOL comments",
			initialSource: dedent(`
				package mypkg

				type S struct {
					A int // A
					B int // B
					C int // C

					//
					// Divider
					//

					D int // D
					E int // E
					F int // F
				}
			`),
			expectedSource: dedent(`
				package mypkg

				type S struct {
					A int // A
					B int // B
					C int // C

					//
					// Divider
					//

					D int // D
					E int // E
					F int // F
				}
			`),
			identifiers: []string{"S"},
		},
		{
			name: "reflows interfaces to always be doc comments",
			initialSource: dedent(`
				package mypkg

				type S interface {

					// A
					A()

					// B
					B()
					C() // C

					// D
					D()
				}
			`),
			expectedSource: dedent(`
				package mypkg

				type S interface {
					// A
					A()

					// B
					B()

					// C
					C()

					// D
					D()
				}
			`),
			identifiers: []string{"S"},
		},
		{
			name: "reflows interfaces - embedded interfaces",
			initialSource: dedent(`
				package mypkg

				type S interface {
					OtherInterface // Embedded

					// A
					A()
				}
			`),
			expectedSource: dedent(`
				package mypkg

				type S interface {
					// Embedded
					OtherInterface

					// A
					A()
				}
			`),
			identifiers: []string{"S"},
		},
		{
			name: "reflows interfaces - generics",
			initialSource: dedent(`
				package mypkg

				// S
				type S interface {
					~int64 | ~float64 // numeric
					String() string // stringer
				}
			`),
			expectedSource: dedent(`
				package mypkg

				// S
				type S interface {
					~int64 | ~float64 // numeric

					// stringer
					String() string
				}
			`),
			identifiers: []string{"S"},
		},
		{
			name: "reflows interfaces - generics2",
			initialSource: dedent(`
				package mypkg

				// Signed
				type Signed interface {
					~int | ~int8 | ~int16 | ~int32 | ~int64
				}
			`),
			expectedSource: dedent(`
				package mypkg

				// Signed
				type Signed interface {
					~int | ~int8 | ~int16 | ~int32 | ~int64
				}
			`),
			identifiers: []string{"Signed"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gocodetesting.WithMultiCode(t, map[string]string{"code.go": tc.initialSource}, func(pkg *gocode.Package) {
				newPkg, failed, err := ReflowDocumentation(pkg, tc.identifiers, tc.options)

				assert.NoError(t, err)

				if tc.expectedFailedIdentifiers == nil {
					assert.Empty(t, failed)
				} else {
					assert.Equal(t, tc.expectedFailedIdentifiers, failed)
				}

				// If tc.expectedSource is missing, it's implied it's the initial source (unchanged).
				expectedSource := tc.expectedSource
				if expectedSource == "" {
					expectedSource = tc.initialSource
				}

				// If newPkg is nil (no changes made to source), we can't have expected something different:
				if newPkg == nil {
					assert.Equal(t, tc.initialSource, expectedSource)
				} else {
					if assert.Contains(t, newPkg.Files, "code.go") {
						file := newPkg.Files["code.go"]

						if !assert.Equal(t, expectedSource, string(file.Contents)) {
							// Print out for easier visual inspection and copy/paste:
							fmt.Println("Expected Source:")
							fmt.Println(expectedSource)
							fmt.Println("Actual Source:")
							fmt.Println(string(file.Contents))
						}
					}
				}
			})
		})
	}
}

func TestReflowAllDocumentation_ReflowsTestPackage(t *testing.T) {
	mainSource := dedent(`
                package mypkg

                // MyFunc does something.
                func MyFunc() {}
        `)

	testSource := dedent(`
                package mypkg_test

                // MyHelper is a helper function used in tests and it has a really long comment that should be wrapped when reflowed to ensure the lines do not exceed the maximum width allowed for this test.
                func MyHelper(t *testing.T) {}
        `)

	expectedTest := dedent(`
                package mypkg_test

                // MyHelper is a helper function used in tests and it has a really long comment that
                // should be wrapped when reflowed to ensure the lines do not exceed the maximum
                // width allowed for this test.
                func MyHelper(t *testing.T) {}
        `)

	gocodetesting.WithMultiCode(t, map[string]string{
		"code.go":      mainSource,
		"code_test.go": testSource,
	}, func(pkg *gocode.Package) {
		newPkg, failed, err := ReflowAllDocumentation(pkg, Options{ReflowMaxWidth: 80})
		assert.NoError(t, err)
		assert.Empty(t, failed)

		if assert.NotNil(t, newPkg) && newPkg.TestPackage != nil {
			if assert.Contains(t, newPkg.TestPackage.Files, "code_test.go") {
				file := newPkg.TestPackage.Files["code_test.go"]
				assertFileSourceEquals(t, file, expectedTest)
			}
		} else {
			t.Fatalf("expected test package to be present")
		}
	})
}

func TestReflowAllDocumentation_DoesntReflowGeneratedFiles(t *testing.T) {
	genSource := dedent(`
		// Code generated by something. DO NOT EDIT.
		package mypkg

		// MyGeneratedFunc has a very long comment that should be wrapped when reflowed to ensure the lines do not exceed the maximum width allowed for this test.
		func MyGeneratedFunc() {}
	`)

	nonGenSource := dedent(`
		package mypkg

		// MyFunc has a very long comment that should be wrapped when reflowed to ensure the lines do not exceed the maximum width allowed for this test.
		func MyFunc() {}
    `)

	expectedNonGen := dedent(`
		package mypkg

		// MyFunc has a very long comment that should be wrapped when reflowed to ensure
		// the lines do not exceed the maximum width allowed for this test.
		func MyFunc() {}
    `)

	gocodetesting.WithMultiCode(t, map[string]string{
		"gen.go":  genSource,
		"code.go": nonGenSource,
	}, func(pkg *gocode.Package) {
		newPkg, failed, err := ReflowAllDocumentation(pkg, Options{ReflowMaxWidth: 80})
		assert.NoError(t, err)
		assert.Empty(t, failed)

		if assert.NotNil(t, newPkg) {
			if assert.Contains(t, newPkg.Files, "gen.go") {
				file := newPkg.Files["gen.go"]
				assertFileSourceEquals(t, file, genSource)
			}
			if assert.Contains(t, newPkg.Files, "code.go") {
				file := newPkg.Files["code.go"]
				assertFileSourceEquals(t, file, expectedNonGen)
			}
		}
	})
}

func TestReflowDocumentationPaths_Sanity_File(t *testing.T) {
	initialA := dedent(`
		package mypkg

		// MyFunction is a function with a very long comment that should be wrapped because it exceeds the maximum line width that we are going to set in the options for the test.
		func MyFunction() {}
	`)
	expectedA := dedent(`
		package mypkg

		// MyFunction is a function with a very long comment that should be wrapped because
		// it exceeds the maximum line width that we are going to set in the options for
		// the test.
		func MyFunction() {}
	`)

	initialB := dedent(`
		package mypkg

		// Other is fine as-is.
		func Other() {}
	`)

	gocodetesting.WithMultiCode(t, map[string]string{
		"a.go": initialA,
		"b.go": initialB,
	}, func(pkg *gocode.Package) {
		aPath := pkg.Files["a.go"].AbsolutePath
		bPath := pkg.Files["b.go"].AbsolutePath

		modified, failed, err := ReflowDocumentationPaths([]string{aPath}, Options{
			ReflowMaxWidth: 80,
		})
		assert.NoError(t, err)
		assert.Empty(t, failed)
		assert.Equal(t, []string{aPath}, modified)

		aBytes, err := os.ReadFile(aPath)
		assert.NoError(t, err)
		assert.Equal(t, expectedA, string(aBytes))

		bBytes, err := os.ReadFile(bPath)
		assert.NoError(t, err)
		assert.Equal(t, initialB, string(bBytes))
	})
}
