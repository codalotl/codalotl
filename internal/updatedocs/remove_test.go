package updatedocs

import (
	"fmt"
	"github.com/codalotl/codalotl/internal/gocode"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoveDocumentationInFile(t *testing.T) {
	// Input source with various documentation scenarios
	inputSource := `// Package mypkg provides utilities for testing.
// This is a multi-line package comment.
package mypkg

import "fmt"

// Constants block documentation
const (
	// FirstConst is the first constant
	FirstConst = 1 // end of line comment for FirstConst
	
	// SecondConst is the second constant
	SecondConst = 2
)

// SingleConst is a single constant declaration
const SingleConst = 3 // end of line comment

// Variables block documentation
var (
	// FirstVar is the first variable
	FirstVar string // end of line comment for FirstVar
	
	// SecondVar is the second variable
	SecondVar int
)

// SingleVar is a single variable declaration
var SingleVar bool // end of line comment

// MyStruct is a struct with documentation
// This is a multi-line struct comment.
type MyStruct struct {
	// Field1 is the first field
	Field1 string // end of line field comment
	
	// Field2 is the second field
	Field2 int
	
	// This is a floating comment inside the struct, not attached to a field
	
	// Field3 is the third field
	Field3 bool
	// Field4 is the forth field
	Field4 struct {
		Field5 int // Nested
	}
	Field6 interface {
		// bar
		Bar()
	}
}

// MyMethod is a method with documentation
func (m *MyStruct) MyMethod() {
	// Internal method comment - should NOT be removed
	fmt.Println("method")
}

// MyInterface is an interface with documentation
type MyInterface interface {
	// Method1 is the first method
	Method1() string
	
	// Method2 is the second method
	Method2(x int) bool
}

// MyFunction is a function with documentation
// This is a multi-line function comment.
func MyFunction(s string) string {
	// This comment is inside the function and should NOT be removed
	result := fmt.Sprintf("Hello %s", s)
	// Another internal comment
	return result
}

// This is a floating comment not attached to any declaration

// init function with documentation
func init() {
	// Internal init comment - should NOT be removed
}
`

	// Expected output after removing documentation
	expectedOutput := `package mypkg

import "fmt"

const (
	FirstConst = 1

	SecondConst = 2
)

const SingleConst = 3

var (
	FirstVar string

	SecondVar int
)

var SingleVar bool

type MyStruct struct {
	Field1 string

	Field2 int

	// This is a floating comment inside the struct, not attached to a field

	Field3 bool

	Field4 struct {
		Field5 int
	}
	Field6 interface {
		Bar()
	}
}

func (m *MyStruct) MyMethod() {
	// Internal method comment - should NOT be removed
	fmt.Println("method")
}

type MyInterface interface {
	Method1() string

	Method2(x int) bool
}

func MyFunction(s string) string {
	// This comment is inside the function and should NOT be removed
	result := fmt.Sprintf("Hello %s", s)
	// Another internal comment
	return result
}

// This is a floating comment not attached to any declaration

func init() {
	// Internal init comment - should NOT be removed
}
`

	// Create a File
	file := &gocode.File{
		FileName:     "test.go",
		AbsolutePath: "/tmp/test.go",
		Contents:     []byte(inputSource),
		PackageName:  "mypkg",
	}

	// Parse the file
	fset := token.NewFileSet()
	_, err := file.Parse(fset)
	require.NoError(t, err)

	// Call RemoveDocumentationInFile
	modified, err := RemoveDocumentationInFile(file, nil)

	// The function should return true and modify the contents
	assert.True(t, modified)
	assert.NoError(t, err)

	// Debug: print actual vs expected
	if string(file.Contents) != expectedOutput {
		t.Logf("ACTUAL length: %d", len(file.Contents))
		t.Logf("EXPECTED length: %d", len(expectedOutput))
	}

	assert.Equal(t, expectedOutput, string(file.Contents))
}

func TestRemoveDocumentation(t *testing.T) {
	// Create a temporary directory for test files
	tempDir := t.TempDir()

	// Create test files with documentation
	file1Content := `// Package testpkg provides test utilities.
package testpkg

// Foo is a test function
func Foo() string {
	// Internal comment
	return "foo"
}
`

	file2Content := `package testpkg

// Bar is another test function
func Bar() int {
	return 42
}

// TestStruct is a test struct
type TestStruct struct {
	// Field1 is documented
	Field1 string
}
`

	// Write test files
	err := os.WriteFile(filepath.Join(tempDir, "file1.go"), []byte(file1Content), 0644)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(tempDir, "file2.go"), []byte(file2Content), 0644)
	require.NoError(t, err)

	// Create a mock module
	module := &gocode.Module{
		Name:         "test.com/testmod",
		AbsolutePath: tempDir,
		Packages:     make(map[string]*gocode.Package),
	}

	// Create a package
	pkg, err := gocode.NewPackage("", tempDir, []string{"file1.go", "file2.go"}, module)
	require.NoError(t, err)

	// Remove documentation
	newPkg, err := RemoveDocumentation(pkg, nil)
	require.NoError(t, err)
	require.NotNil(t, newPkg)

	// Check that files were modified
	expectedFile1 := `package testpkg

func Foo() string {
	// Internal comment
	return "foo"
}
`

	expectedFile2 := `package testpkg

func Bar() int {
	return 42
}

type TestStruct struct {
	Field1 string
}
`

	// Read the actual files from disk
	actualFile1, err := os.ReadFile(filepath.Join(tempDir, "file1.go"))
	require.NoError(t, err)
	assert.Equal(t, expectedFile1, string(actualFile1))

	actualFile2, err := os.ReadFile(filepath.Join(tempDir, "file2.go"))
	require.NoError(t, err)
	assert.Equal(t, expectedFile2, string(actualFile2))

	// Verify the new package has the updated contents
	assert.Equal(t, expectedFile1, string(newPkg.Files["file1.go"].Contents))
	assert.Equal(t, expectedFile2, string(newPkg.Files["file2.go"].Contents))
}

func TestRemoveDocumentationInFile_PreserveSpecialComments(t *testing.T) {
	input := `//go:build linux

// Code generated; DO NOT EDIT.

// Package comment
package mypkg

// Bar docs
// Another line
//
//nolint:revive
func Bar() {}

const Foo = 1 //lint:ignore
`

	expected := `//go:build linux

// Code generated; DO NOT EDIT.

package mypkg

//nolint:revive
func Bar() {}

const Foo = 1 //lint:ignore
`

	file := &gocode.File{
		FileName:     "test.go",
		AbsolutePath: "/tmp/test.go",
		Contents:     []byte(input),
		PackageName:  "mypkg",
	}

	fset := token.NewFileSet()
	_, err := file.Parse(fset)
	require.NoError(t, err)

	modified, err := RemoveDocumentationInFile(file, nil)
	assert.True(t, modified)
	assert.NoError(t, err)
	assert.Equal(t, expected, string(file.Contents))
}

func TestRemoveDocumentationInFile_Identifiers_Basic(t *testing.T) {
	input := dedent(`
		package mypkg

		// Foo is the first constant
		const Foo = 1

		// Bar is the second constant
		const Bar = 2

		// Baz is a function
		func Baz() {}
	`)

	expected := dedent(`
		package mypkg

		const Foo = 1

		// Bar is the second constant
		const Bar = 2

		func Baz() {}
	`)

	file := &gocode.File{
		FileName:     "test.go",
		AbsolutePath: "/tmp/test.go",
		Contents:     []byte(input),
		PackageName:  "mypkg",
	}

	fset := token.NewFileSet()
	_, err := file.Parse(fset)
	require.NoError(t, err)

	modified, err := RemoveDocumentationInFile(file, []string{"Foo", "Baz"})
	assert.True(t, modified)
	assert.NoError(t, err)
	assert.Equal(t, expected, string(file.Contents))
}

func TestRemoveDocumentationInFile_Identifiers_Method(t *testing.T) {
	input := dedent(`
		package mypkg

		// Barbar
		func (c *Foo) Bar() {}
	`)

	expected := dedent(`
		package mypkg

		func (c *Foo) Bar() {}
	`)

	file := &gocode.File{
		FileName:     "test.go",
		AbsolutePath: "/tmp/test.go",
		Contents:     []byte(input),
		PackageName:  "mypkg",
	}

	fset := token.NewFileSet()
	_, err := file.Parse(fset)
	require.NoError(t, err)

	modified, err := RemoveDocumentationInFile(file, []string{"*Foo.Bar"})
	assert.True(t, modified)
	assert.NoError(t, err)
	assert.Equal(t, expected, string(file.Contents))
}

func TestRemoveDocumentationInFile_Identifiers_Advanced(t *testing.T) {
	input := dedent(`
		// Package docs
		package mypkg

		// foo and bar ...
		var foo, bar string

		// person
		type person struct {
			Name string // Name
			Age int     // Age

			Address struct {
				Street string // street
				// City
				City string
			}
		}

		// These consts...
		const (
			C1 int = iota // C1
			C2            // C2
		)
	`)

	expectedMap := map[string]string{
		"package": dedent(`
			package mypkg

			// foo and bar ...
			var foo, bar string

			// person
			type person struct {
				Name string // Name
				Age  int    // Age

				Address struct {
					Street string // street
					// City
					City string
				}
			}

			// These consts...
			const (
				C1 int = iota // C1
				C2            // C2
			)
		`),
		"bar": dedent(`
			// Package docs
			package mypkg

			var foo, bar string

			// person
			type person struct {
				Name string // Name
				Age  int    // Age

				Address struct {
					Street string // street
					// City
					City string
				}
			}

			// These consts...
			const (
				C1 int = iota // C1
				C2            // C2
			)
		`),
		"person": dedent(`
			// Package docs
			package mypkg

			// foo and bar ...
			var foo, bar string

			type person struct {
				Name string
				Age  int

				Address struct {
					Street string

					City string
				}
			}

			// These consts...
			const (
				C1 int = iota // C1
				C2            // C2
			)
		`),
		"C1": dedent(`
			// Package docs
			package mypkg

			// foo and bar ...
			var foo, bar string

			// person
			type person struct {
				Name string // Name
				Age  int    // Age

				Address struct {
					Street string // street
					// City
					City string
				}
			}

			// These consts...
			const (
				C1 int = iota
				C2     // C2
			)
		`),
	}

	for ident, expected := range expectedMap {
		file := &gocode.File{
			FileName:     "test.go",
			AbsolutePath: "/tmp/test.go",
			Contents:     []byte(input),
			PackageName:  "mypkg",
		}

		fset := token.NewFileSet()
		_, err := file.Parse(fset)
		require.NoError(t, err)

		modified, err := RemoveDocumentationInFile(file, []string{ident})
		assert.True(t, modified)
		assert.NoError(t, err)
		if !assert.Equal(t, expected, string(file.Contents)) {
			fmt.Println("Actual for ", ident)
			fmt.Println(string(file.Contents))
		}
	}

}
