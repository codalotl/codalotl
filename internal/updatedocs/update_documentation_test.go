package updatedocs

import (
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

type tableDrivenDocUpdateTest struct {
	name                  string
	existingSource        map[string]string
	snippets              []string
	options               Options
	newSource             map[string]string // only entries for files that should change
	expectSnippetErrCount int
	expectSnippetErrLike  string // if present AND expectSnippetErrCount==1, expected substring in snippet user error
	expectOverallErrLike  string // expected substring in error message, empty if no error expected
	expectPartial         bool
}

func TestUpdateDocumentationErrors(t *testing.T) {
	existingSource := map[string]string{
		"mypkg.go": dedent(`
			package mypkg
		`),
	}
	t.Run("invalid wrapping", func(t *testing.T) {
		gocodetesting.WithMultiCode(t, existingSource, func(pkg *gocode.Package) {
			snippet := "````\npackage mypkg\n````" // quad backticks: invalid
			updatedPkg, updatedFiles, snippetErrors, err := UpdateDocumentation(pkg, []string{snippet})
			assert.Nil(t, updatedPkg)
			assert.Nil(t, updatedFiles)
			assert.NoError(t, err)

			if assert.Len(t, snippetErrors, 1) {
				se := snippetErrors[0]
				assert.Equal(t, snippet, se.Snippet)
				assert.Contains(t, se.UserErrorMessage, "could not be unwrapped")
				assert.Contains(t, se.Err.Error(), "unsupported language") // code thinks a backtick is the language, which is fine. Feel free to make a better error in future and update this test to match.
			}

			// Make sure source didn't change:
			assertFileSourceEquals(t, pkg.Files["mypkg.go"], dedent(`
				package mypkg
			`))
		})
	})

	t.Run("bad snippet", func(t *testing.T) {
		gocodetesting.WithMultiCode(t, existingSource, func(pkg *gocode.Package) {
			snippet := "```go\n// Package comment\npackage mypkg\n\n// Variable comment\nvar Foo int\n```"
			updatedPkg, updatedFiles, snippetErrors, err := UpdateDocumentation(pkg, []string{snippet})
			assert.Nil(t, updatedPkg)
			assert.Nil(t, updatedFiles)
			assert.NoError(t, err)

			if assert.Len(t, snippetErrors, 1) {
				se := snippetErrors[0]
				assert.Equal(t, snippet, se.Snippet)
				assert.Contains(t, se.UserErrorMessage, "package doc comment snippet may not contain other declarations")
				assert.Contains(t, se.Err.Error(), "package doc comment snippet may not contain other declarations")
			}

			// Make sure source didn't change:
			assertFileSourceEquals(t, pkg.Files["mypkg.go"], dedent(`
				package mypkg
			`))
		})
	})

	t.Run("partial failure", func(t *testing.T) {
		gocodetesting.WithMultiCode(t, map[string]string{"mypkg.go": "// doc\npackage mypkg"}, func(pkg *gocode.Package) {
			snippets := []string{
				dedent(`
					// Package comment
					// Package comment2
					package mypkg
				`),
				dedent(`
					syntax error
				`),
			}
			updatedPkg, updatedFiles, snippetErrors, err := UpdateDocumentation(pkg, snippets)
			assert.NotNil(t, updatedPkg)
			assert.True(t, pkg != updatedPkg)
			if assert.Len(t, updatedFiles, 1) {
				assert.Equal(t, "mypkg.go", updatedFiles[0])
			}
			assert.NoError(t, err)

			if assert.Len(t, snippetErrors, 1) {
				se := snippetErrors[0]
				assert.Equal(t, snippets[1], se.Snippet)
				assert.Contains(t, se.UserErrorMessage, "expected declaration")
				assert.Contains(t, se.Err.Error(), "expected declaration")
			}
		})
	})
}

func TestUpdateDocumentationTableDriven(t *testing.T) {
	tests := []tableDrivenDocUpdateTest{
		//
		// Reflowing comments
		//
		{
			name: "reflow - function",
			existingSource: map[string]string{
				"funcs.go": dedent(`
					package mypkg

					func exampleFunc() {}
				`),
			},
			snippets: []string{
				dedent(`
					// exampleFunc demonstrates the example functionality.
					// It has two short lines.
					func exampleFunc()
				`),
			},
			options: Options{Reflow: true},
			newSource: map[string]string{
				"funcs.go": dedent(`
					package mypkg

					// exampleFunc demonstrates the example functionality. It has two short lines.
					func exampleFunc() {}
				`),
			},
		},
		{
			name: "reflow - types",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo struct {
						A int
						B struct {
							C int
						}
					}
				`),
			},
			snippets: []string{
				dedent(`
					// Type foo is a type.
					// Second line.
					//
					//	code1;
					//	code2;
					type Foo struct {
						A int // a is an end of line comment that is a pretty long, certainly longer than 40 width

						// But B is too long and will wrap because it's an end of line comment
						B struct {
							// C is a nested field.
							// line2
							C int
						}
					}
				`),
			},
			options: Options{Reflow: true, ReflowMaxWidth: 40},
			newSource: map[string]string{ // NOTE: missing newline after A int line, not sure why.
				"code.go": dedent(`
					package mypkg

					// Type foo is a type. Second line.
					//
					//	code1;
					//	code2;
					type Foo struct {
						// a is an end of line comment that is a
						// pretty long, certainly longer than 40
						// width
						A int

						// But B is too long and will wrap because
						// it's an end of line comment
						B struct {
							// C is a nested field. line2
							C int
						}
					}
				`),
			},
		},
		{
			name: "convert eol to doc and visa versa",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					const (
						v0 int = iota
						v1
						v2
					)
				`),
			},
			snippets: []string{
				dedent(`
					const (
						// v0
						v0 int = iota
						// v1
						v1
						v2 // v2
					)
				`),
			},
			options: Options{Reflow: true},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					const (
						v0 int = iota // v0
						v1            // v1
						v2            // v2
					)
				`),
			},
		},

		//
		// Errors and others
		//
		{
			name: "error when invalid snippet",
			existingSource: map[string]string{
				"mypkg.go": dedent(`
					package mypkg
				`),
			},
			snippets: []string{
				dedent(`
					invalid golang code @#%
				`),
			},
			expectSnippetErrCount: 1,
		},
		{
			name: "error when snippet has invalid comments",
			existingSource: map[string]string{
				"mypkg.go": dedent(`
					package mypkg
				`),
			},
			snippets: []string{
				dedent(`
					// Foo is a type
					type Foo int // also has end-of-line comment
				`),
			},
			expectSnippetErrCount: 1,
			expectSnippetErrLike:  "type Foo has both doc comment and end-of-line comment",
		},
	}

	for _, testCase := range tests {
		runTableDrivenDocUpdateTest(t, testCase)
	}
}

func runTableDrivenDocUpdateTest(t *testing.T, testCase tableDrivenDocUpdateTest) {
	t.Run(testCase.name, func(t *testing.T) {
		gocodetesting.WithMultiCode(t, testCase.existingSource, func(pkg *gocode.Package) {
			if testCase.expectSnippetErrLike != "" && testCase.expectSnippetErrCount != 1 {
				panic("expectedSnippetErrLike is not empty implies expectSnippetErrCount==1 (but this wasn't the case)")
			}

			updatedPkg, updatedFileNames, snippetErrs, err := UpdateDocumentation(pkg, testCase.snippets, testCase.options)
			assert.True(t, pkg != updatedPkg) // different pointers! updatedPkg can also be nil, in which case this assert is also correct
			assert.Len(t, snippetErrs, testCase.expectSnippetErrCount, "wrong number of snippet errors")

			if testCase.expectSnippetErrLike != "" && len(snippetErrs) == 1 {
				assert.Contains(t, snippetErrs[0].UserErrorMessage, testCase.expectSnippetErrLike)
			}
			if len(snippetErrs) == 1 {
				assert.Equal(t, testCase.expectPartial, snippetErrs[0].PartiallyRejected)
			}

			if testCase.expectOverallErrLike != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), testCase.expectOverallErrLike)
				assert.Nil(t, updatedPkg)
				return
			}

			assert.NoError(t, err)
			assert.EqualValues(t, len(testCase.newSource), len(updatedFileNames))

			// TODO: assert that the set updatedFileNames == the set of keys from tt.newSource

			// Check that the updated file matches expectations
			if len(testCase.newSource) > 0 {
				if updatedPkg == nil {
					assert.Fail(t, "expected new source, but updatedPkg is nil")
					return
				}

				for filename, expectedContent := range testCase.newSource {
					f := updatedPkg.Files[filename]
					if assert.NotNil(t, f) {
						assert.Equal(t, expectedContent, string(f.Contents))                   // asserts in-memory version of content
						assertFileSourceEquals(t, updatedPkg.Files[filename], expectedContent) // asserts disk bytes
					}
				}
			}

			// Check that all other files remain unchanged
			for filename, originalContent := range testCase.existingSource {
				// Skip the file that is exacted to change
				if _, ok := testCase.newSource[filename]; ok {
					continue
				}

				contents, err := os.ReadFile(pkg.Files[filename].AbsolutePath)
				assert.NoError(t, err)
				assert.Equal(t, originalContent, string(contents), "File %s was unexpectedly changed", filename)
			}
		})
	})
}

// assertFileSourceEquals reads the file from disk and asserts that it equals the expected file.
func assertFileSourceEquals(t *testing.T, file *gocode.File, expected string) {
	t.Helper()

	// Read it from disk
	contents, err := os.ReadFile(file.AbsolutePath)
	assert.NoError(t, err)

	// Compare the contents
	assert.Equal(t, expected, string(contents))
}

var dedent = gocodetesting.Dedent
