package updatedocs

import "testing"

func TestUpdateDocumentationTypesTableDriven(t *testing.T) {
	tests := []tableDrivenDocUpdateTest{
		{
			name: "type of basic int - new comment",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo int
				`),
			},
			snippets: []string{
				dedent(`
					// Foo is ...
					type Foo int
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// Foo is ...
					type Foo int
				`),
			},
		},
		{
			name: "type of basic int - update comment",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// Existing comment
					type Foo int
				`),
			},
			snippets: []string{
				dedent(`
					// Foo is ...
					type Foo int
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// Foo is ...
					type Foo int
				`),
			},
		},
		{
			name: "type of basic int - new eol comment",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo int
				`),
			},
			snippets: []string{
				dedent(`
					type Foo int // Foo is ...
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo int // Foo is ...
				`),
			},
		},
		{
			name: "type of basic int - replace eol comment",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo int // Foo was ...
				`),
			},
			snippets: []string{
				dedent(`
					type Foo int // Foo is ...
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo int // Foo is ...
				`),
			},
		},
		{
			name: "type of basic int - new eol comment unsets doc comment",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// foo
					type Foo int
				`),
			},
			snippets: []string{
				dedent(`
					type Foo int // Foo is ...
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo int // Foo is ...
				`),
			},
		},
		{
			name: "type of basic int - new doc comment unsets eol comment",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo int // foo
				`),
			},
			snippets: []string{
				dedent(`
					// Foo is ...
					type Foo int
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// Foo is ...
					type Foo int
				`),
			},
		},
		{
			name: "type of struct - set comment on EOL of one field",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo struct {
						Bar string
						Baz int
					}
				`),
			},
			snippets: []string{
				dedent(`
					type Foo struct {
						Bar string // bar does things
					}
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo struct {
						Bar string // bar does things
						Baz int
					}
				`),
			},
		},
		{
			name: "type of struct - replace comment on EOL of one field",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo struct {
						Bar string // does bar do things?
						Baz int
					}
				`),
			},
			snippets: []string{
				dedent(`
					type Foo struct {
						Bar string // bar does things
					}
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo struct {
						Bar string // bar does things
						Baz int
					}
				`),
			},
		},
		{
			name: "type of struct - set comment on EOL of one field also deletes doc",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo struct {
						// does bar do things?
						Bar string
						Baz int
					}
				`),
			},
			snippets: []string{
				dedent(`
					type Foo struct {
						Bar string // bar does things
					}
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo struct {
						Bar string // bar does things
						Baz int
					}
				`),
			},
		},
		{
			name: "type of struct - set doc comment for one field",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo struct {
						Bar string
						Baz int
					}
				`),
			},
			snippets: []string{
				dedent(`
					type Foo struct {
						// bar does things
						Bar string
					}
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo struct {
						// bar does things
						Bar string
						Baz int
					}
				`),
			},
		},
		{
			name: "type of struct - replace doc comment for one field",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo struct {
						// old bar comment
						Bar string
						Baz int
					}
				`),
			},
			snippets: []string{
				dedent(`
					type Foo struct {
						// bar does things
						Bar string
					}
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo struct {
						// bar does things
						Bar string
						Baz int
					}
				`),
			},
		},
		{
			name: "type of struct - set doc comment for one field deletes line comment",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo struct {
						Bar string // old bar comment
						Baz int
					}
				`),
			},
			snippets: []string{
				dedent(`
					type Foo struct {
						// bar does things
						Bar string
					}
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo struct {
						// bar does things
						Bar string
						Baz int
					}
				`),
			},
		},
		{
			name: "type of struct - documents multiple things",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo struct {
						Bar string
						Baz int
						Qux bool
					}
				`),
			},
			snippets: []string{
				dedent(`
					// Foo is an
					// important struct
					type Foo struct {
						Qux bool // qux above them	
						Bar string // bar
						Baz int // baz
					}
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// Foo is an
					// important struct
					type Foo struct {
						Bar string // bar
						Baz int    // baz
						Qux bool   // qux above them
					}
				`),
			},
		},
		{
			name: "type of struct - re-documents multiple things",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// Foo is an
					// important struct
					type Foo struct {
						Bar string // bar
						Baz int    // baz
						Qux bool   // qux above them
					}
				`),
			},
			snippets: []string{
				dedent(`
					// Foo is great.
					type Foo struct {
						// Bar is rather neat
						// it supports multiline redocuments.
						Bar string
						Baz int // baz

						// Qux is ok
						Qux bool
					}
				`),
			},
			newSource: map[string]string{ // NOTE: this test might need to be changed if we properly implement newlines
				"code.go": dedent(`
					package mypkg

					// Foo is great.
					type Foo struct {
						// Bar is rather neat
						// it supports multiline redocuments.
						Bar string
						Baz int // baz

						// Qux is ok
						Qux bool
					}
				`),
			},
		},
		{
			name: "type of struct - can document two fields on one line",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo struct {
						Bar, Qux string
						Baz int
					}
				`),
			},
			snippets: []string{
				dedent(`
					type Foo struct {
						// These are something
						Bar, Qux string
					}
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo struct {
						// These are something
						Bar, Qux string
						Baz      int
					}
				`),
			},
		},
		{
			name: "type of struct - can document an embedded struct",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Qux struct{}
					type Foo struct {
						Qux
						Baz int
					}
				`),
			},
			snippets: []string{
				dedent(`
					type Foo struct {
						Qux // Qux allows...
					}
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Qux struct{}
					type Foo struct {
						Qux // Qux allows...
						Baz int
					}
				`),
			},
		},
		{
			name: "type of struct - can document an embedded struct ptr",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Qux struct{}
					type Foo struct {
						*Qux
						Baz int
					}
				`),
			},
			snippets: []string{
				dedent(`
					type Foo struct {
						// Qux allows...
						*Qux
					}
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Qux struct{}
					type Foo struct {
						// Qux allows...
						*Qux
						Baz int
					}
				`),
			},
		},
		{
			name: "type of struct - replace block comments with normal ones",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					/* Foo */
					type Foo struct {
						/* bar */
						Bar string
						Baz int /* baz
						           so baz */
					}
				`),
			},
			snippets: []string{
				dedent(`
					// Foo
					type Foo struct {
						// bar
						Bar string
						Baz int // baz
					}
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// Foo
					type Foo struct {
						// bar
						Bar string
						Baz int // baz
					}
				`),
			},
		},
		{
			name: "type blocks",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type (
						foo int
						bar string
					)
				`),
			},
			snippets: []string{
				dedent(`
					// my favourite types
					type (
						// foo
						foo int

						bar string // bar
					)
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// my favourite types
					type (
						// foo
						foo int
						bar string // bar
					)
				`),
			},
		},
		{
			name: "type of struct - missing embedded struct",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo struct {
						Baz int
					}
				`),
			},
			snippets: []string{
				dedent(`
					type Foo struct {
						Qux // Qux allows...
					}
				`),
			},
			expectSnippetErrCount: 1,
			expectSnippetErrLike:  "Source type does not match type in snippet",
		},
		{
			name: "type of struct - two fields on one line, can't document just one of them",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo struct {
						Bar, Qux string
						Baz int
					}
				`),
			},
			snippets: []string{
				dedent(`
					type Foo struct {
						// These are something
						Bar string
					}
				`),
			},
			expectSnippetErrCount: 1,
		},
		{
			name: "type of struct - type mismatch",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo struct {
						Bar int
					}
				`),
			},
			snippets: []string{
				dedent(`
					type Foo struct {
						Bar string
					}
				`),
			},
			expectSnippetErrCount: 1,
			expectSnippetErrLike:  "Source type does not match type in snippet",
		},
		{
			name: "type of struct - no comment",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo struct {
						Bar int
					}
				`),
			},
			snippets: []string{
				dedent(`
					type Foo struct {
						Bar int
					}
				`),
			},
			expectSnippetErrCount: 1,
			expectSnippetErrLike:  "No comments to apply",
		},
		{
			name: "type of struct - nested struct",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo struct {
						Bar struct {
							Baz int
						}
					}
				`),
			},
			snippets: []string{
				dedent(`
					type Foo struct {
						// Bar is cool
						Bar struct {
							Baz int // Baz is nice
						}
					}
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo struct {
						// Bar is cool
						Bar struct {
							Baz int // Baz is nice
						}
					}
				`),
			},
		},
		{
			name: "type of interface - basics",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo interface {
						Bar()
						Baz() int
					}
				`),
			},
			snippets: []string{
				dedent(`
					// Foo...
					type Foo interface {
						Bar() // Bar...

						// Baz...
						Baz() int
					}
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// Foo...
					type Foo interface {
						Bar() // Bar...

						// Baz...
						Baz() int
					}
				`),
			},
		},
		{
			name: "type of interface - embedded interfaces",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo interface {
						Embedded
						otherPkg.AnotherEmbedded
					}
				`),
			},
			snippets: []string{
				dedent(`
					// Foo...
					type Foo interface {
						Embedded // Embedded...

						// AnotherEmbedded...
						otherPkg.AnotherEmbedded
					}
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// Foo...
					type Foo interface {
						Embedded // Embedded...

						// AnotherEmbedded...
						otherPkg.AnotherEmbedded
					}
				`),
			},
		},
		{
			name: "type of interface - invalid method",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo interface {
						Bar()
						Baz() int
					}
				`),
			},
			snippets: []string{
				dedent(`
					type Foo interface {
						
						// Baz...
						Qux() int
					}
				`),
			},
			expectSnippetErrCount: 1,
		},
		{
			name: "private types",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type foo int
				`),
			},
			snippets: []string{
				dedent(`
					// foo is ...
					type foo int
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// foo is ...
					type foo int
				`),
			},
		},
		{
			name: "rejecting types - can still set things",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo int
				`),
			},
			snippets: []string{
				dedent(`
					// Foo is ...
					type Foo int
				`),
			},
			options: Options{RejectUpdates: true},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// Foo is ...
					type Foo int
				`),
			},
		},
		{
			name: "rejecting types - set doc, existing doc comment",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// Existing
					type Foo int
				`),
			},
			snippets: []string{
				dedent(`
					// New
					type Foo int
				`),
			},
			options:               Options{RejectUpdates: true},
			expectSnippetErrCount: 1,
			expectPartial:         true,
		},
		{
			name: "rejecting types - set doc, existing eol comment",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo int // Existing
				`),
			},
			snippets: []string{
				dedent(`
					// New
					type Foo int
				`),
			},
			options:               Options{RejectUpdates: true},
			expectSnippetErrCount: 1,
			expectPartial:         true,
		},
		{
			name: "rejecting types - set eol, existing doc comment",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// Existing
					type Foo int
				`),
			},
			snippets: []string{
				dedent(`
					type Foo int // New
				`),
			},
			options:               Options{RejectUpdates: true},
			expectSnippetErrCount: 1,
			expectPartial:         true,
		},
		{
			name: "rejecting types - set eol, existing eol comment",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg
					
					type Foo int // Existing
				`),
			},
			snippets: []string{
				dedent(`
					type Foo int // New
				`),
			},
			options:               Options{RejectUpdates: true},
			expectSnippetErrCount: 1,
			expectPartial:         true,
		},
		{
			name: "rejecting types - all struct fields rejected",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg
					
					type Foo struct {
						// A
						A int
						B int // B
						C int
					}
				`),
			},
			snippets: []string{
				dedent(`
					type Foo struct {
						// A prime
						A int
						B int // B prime
						C int
					}
				`),
			},
			options:               Options{RejectUpdates: true},
			expectSnippetErrCount: 1,
			expectPartial:         true,
		},
		{
			name: "rejecting types - some struct fields rejected, one applied",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg
					
					type Foo struct {
						// A
						A int
						B int // B
						C int
					}
				`),
			},
			snippets: []string{
				dedent(`
					type Foo struct {
						A int // A prime
						// B prime
						B int
						C int // C prime
					}
				`),
			},
			options: Options{RejectUpdates: true},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo struct {
						// A
						A int
						B int // B
						C int // C prime
					}
				`),
			},
			expectSnippetErrCount: 1,
			expectPartial:         true,
		},
		{
			name: "rejecting types - nested struct fields",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg
					
					type Foo struct {
						// A
						A int
						B int
						C struct {
							D int // D
							E int
							// F
							F int
						}
					}
				`),
			},
			snippets: []string{
				dedent(`
					// Foo
					type Foo struct {
						// A prime
						A int
						B int // B prime
						C struct {
							D int // D
							E int // E prime
							// F prime
							F int
						}
					}
				`),
			},
			options: Options{RejectUpdates: true},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// Foo
					type Foo struct {
						// A
						A int
						B int // B prime
						C struct {
							D int // D
							E int // E prime
							// F
							F int
						}
					}
				`),
			},
			expectSnippetErrCount: 1,
			expectPartial:         true,
		},
		{
			name: "rejecting interfaces - some applied, some rejected",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type Foo interface {
						// Bar1
						Bar1()
						Baz1() // Baz1
						// Bar2
						Bar2()
						Baz2() // Baz2
						Qux()
					}
				`),
			},
			snippets: []string{
				dedent(`
					// Foo
					type Foo interface {
						// Bar1 prime
						Bar1()
						Baz1() // Baz1 prime
						Bar2() // Bar2 prime
						// Baz2 prime
						Baz2()
						Qux() // Qux
					}
				`),
			},
			options: Options{RejectUpdates: true},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// Foo
					type Foo interface {
						// Bar1
						Bar1()
						Baz1() // Baz1
						// Bar2
						Bar2()
						Baz2() // Baz2
						Qux()  // Qux
					}
				`),
			},
			expectSnippetErrCount: 1,
			expectPartial:         true,
		},
		{
			name: "empty type block doesn't crash things",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg
					type ()
				`),
			},
			snippets: []string{
				dedent(`
					// empty
					type ()
				`),
			},
			expectSnippetErrCount: 1,
			expectSnippetErrLike:  "type block with no specs",
		},
		{
			name: "can document single-line struct with field - eol",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type X struct { err error }
				`),
			},
			snippets: []string{
				dedent(`
					// X
					type X struct {
						err error // err
					}
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// X
					type X struct {
						err error // err
					}
				`),
			},
		},
		{
			name: "can document single-line struct with field - doc",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type X struct { err error }
				`),
			},
			snippets: []string{
				dedent(`
					// X
					type X struct {
						// err
						err error
					}
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// X
					type X struct {
						// err
						err error
					}
				`),
			},
		},
		{
			name: "can document double single-line struct with field",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type X struct { Y struct { err error } }
				`),
			},
			snippets: []string{
				dedent(`
					// X
					type X struct {
						// Y
						Y struct {
							err error // err
						}
					}
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// X
					type X struct {
						// Y
						Y struct {
							err error // err
						}
					}
				`),
			},
		},
		{
			name: "can document single-line interface - Doc",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type X interface { Bar() }
				`),
			},
			snippets: []string{
				dedent(`
					// X
					type X interface {
						// Bar
						Bar()
					}
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// X
					type X interface {
						// Bar
						Bar()
					}
				`),
			},
		},
		{
			name: "can document single-line interface - eol",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					type X interface { Bar() }
				`),
			},
			snippets: []string{
				dedent(`
					// X
					type X interface {
						Bar() // Bar
					}
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// X
					type X interface {
						Bar() // Bar
					}
				`),
			},
		},
	}

	for _, testCase := range tests {
		runTableDrivenDocUpdateTest(t, testCase)
	}
}
