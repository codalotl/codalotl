package updatedocs

import "testing"

func TestUpdateDocumentationValuesTableDriven(t *testing.T) {
	tests := []tableDrivenDocUpdateTest{
		{
			name: "value of basic int - new doc comment",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var Foo int
				`),
			},
			snippets: []string{
				dedent(`
					// Foo is ...
					var Foo int
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// Foo is ...
					var Foo int
				`),
			},
		},
		{
			name: "value of basic int - update doc comment",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// existing
					var Foo int
				`),
			},
			snippets: []string{
				dedent(`
					// Foo is ...
					var Foo int
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// Foo is ...
					var Foo int
				`),
			},
		},
		{
			name: "value of basic int - set doc comment and unset EOL",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var Foo int // existing
				`),
			},
			snippets: []string{
				dedent(`
					// Foo is ...
					// something great
					var Foo int
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// Foo is ...
					// something great
					var Foo int
				`),
			},
		},
		{
			name: "value of basic int - sets EOL comment",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var Foo int
				`),
			},
			snippets: []string{
				dedent(`
					var Foo int // Foo is ...
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var Foo int // Foo is ...
				`),
			},
		},
		{
			name: "value of basic int - updates EOL comment",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var Foo int // old comment
				`),
			},
			snippets: []string{
				dedent(`
					var Foo int // Foo is ...
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var Foo int // Foo is ...
				`),
			},
		},
		{
			name: "value of basic int - set EOL comment unsets doc comment",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// old comment
					var Foo int
				`),
			},
			snippets: []string{
				dedent(`
					var Foo int // Foo is ...
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var Foo int // Foo is ...
				`),
			},
		},
		{
			name: "const - value of basic int - set EOL comment unsets doc comment",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// old comment
					const Foo = 7
				`),
			},
			snippets: []string{
				dedent(`
					const Foo = 7 // Foo is ...
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					const Foo = 7 // Foo is ...
				`),
			},
		},
		{
			name: "error - no comment",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					const Foo = 7
				`),
			},
			snippets: []string{
				dedent(`
					const Foo = 7
				`),
			},
			expectSnippetErrCount: 1,
			expectSnippetErrLike:  "No comments to apply",
		},
		{
			name: "var - can do multiple vars at once",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var Foo int
					
					// Existing doc
					var Bar int
					var Quxx int // existing EOL
				`),
			},
			// NOTE: Quxx is present and "there for reference", but we don't update it's comment.
			snippets: []string{
				dedent(`
					// foo
					var Foo int
					var Bar int // bar
					var Quxx int
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// foo
					var Foo int

					var Bar int  // bar
					var Quxx int // existing EOL
				`),
			},
		},
		{
			name: "var - multi identifier supported",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var Foo, Bar int
				`),
			},
			snippets: []string{
				dedent(`
					// Foo and Bar store...
					var Foo, Bar int
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// Foo and Bar store...
					var Foo, Bar int
				`),
			},
		},
		{
			name: "var - multi identifier error if not exact",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var Foo, Bar int
				`),
			},
			snippets: []string{
				dedent(`
					// Foo is...
					var Foo int
				`),
			},
			expectSnippetErrCount: 1,
			expectSnippetErrLike:  "No comments to apply", // TODO: this isn't a great error message
		},
		{
			name: "var - multi identifier error if not exact",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var Foo int
					const Bar = 3
				`),
			},
			snippets: []string{
				dedent(`
					var Foo int   // foo...
					const Bar = 3 // bar...
				`),
			},
			expectSnippetErrCount: 1,
			expectSnippetErrLike:  "snippet contains 1 var, 1 const",
		},
		{
			name: "value of basic int - private vars allowed",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var foo int
				`),
			},
			snippets: []string{
				dedent(`
					// foo is ...
					var foo int
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// foo is ...
					var foo int
				`),
			},
		},
		{
			name: "const blocks",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					const (
						kindA int = iota
						kindB
						kindC
					)
				`),
			},
			snippets: []string{
				dedent(`
					// kind enums
					const (
						kindA int = iota // kind of A
						kindB            // kind of B
						kindC            // kind of C
					)
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// kind enums
					const (
						kindA int = iota // kind of A
						kindB            // kind of B
						kindC            // kind of C
					)
				`),
			},
		},
		{
			name: "const blocks - unset doc, set eol",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					const (
						// thing
						thing = 1
					)
				`),
			},
			snippets: []string{
				dedent(`
					const (
						thing = 1 // thing
					)
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					const (
						thing = 1 // thing
					)
				`),
			},
		},
		{
			name: "const blocks - unset eol, set doc",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					const (
						thing = 1 // thing
					)
				`),
			},
			snippets: []string{
				dedent(`
					const (
						// thing
						thing = 1
					)
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					const (
						// thing
						thing = 1
					)
				`),
			},
		},
		{
			name: "const blocks - partial and out of order",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					const (
						kindA int = iota
						kindB
						kindC
						kindD
					)
				`),
			},
			snippets: []string{
				dedent(`
					const (
						// kind of A
						kindA int = iota
						kindD // d
						kindB // b
					)
				`),
			},
			// NOTE: I am not sure why the b comment is floating at exactly that spot.
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					const (
						// kind of A
						kindA int = iota
						kindB     // b
						kindC
						kindD // d
					)
				`),
			},
		},
		{
			name: "rejecting values - basic",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var Foo int // Foo
				`),
			},
			snippets: []string{
				dedent(`
					// Foo is ...
					var Foo int
				`),
			},
			options:               Options{RejectUpdates: true},
			expectSnippetErrCount: 1,
			expectSnippetErrLike:  "not applied due to options restrictions",
			expectPartial:         true,
		},
		{
			name: "rejecting values - multi",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var Foo int // Foo
					// Bar
					var Bar int
					var Baz int
				`),
			},
			snippets: []string{
				dedent(`
					
					var Foo int // Foo is ...
					var Bar int // Bar
					var Baz int // Baz
				`),
			},
			options: Options{RejectUpdates: true},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var Foo int // Foo
					// Bar
					var Bar int
					var Baz int // Baz
				`),
			},
			expectSnippetErrCount: 1,
			expectPartial:         true,
		},
		{
			name: "rejecting values - block",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// block
					const (
						A int = iota // A
						B
						C
					)
				`),
			},
			snippets: []string{
				dedent(`
					// block prime
					const (
						A int = iota // A prime
						B
						C // C prime
					)
				`),
			},
			options: Options{RejectUpdates: true},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					// block
					const (
						A int = iota // A
						B
						C // C prime
					)
				`),
			},
			expectSnippetErrCount: 1,
			expectPartial:         true,
		},
		{
			name: "source block, snippet non-block - doc comment",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var (
						Foo int
						Bar string
						Baz bool
					)
				`),
			},
			snippets: []string{
				dedent(`
					// Bar is the name of something
					var Bar string
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var (
						Foo int

						// Bar is the name of something
						Bar string
						Baz bool
					)
				`),
			},
		},
		{
			name: "source block, snippet non-block - EOL comment",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var (
						Foo int
						Bar string
						Baz bool
					)
				`),
			},
			snippets: []string{
				dedent(`
					var Foo int // Foo stores the count
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var (
						Foo int // Foo stores the count
						Bar string
						Baz bool
					)
				`),
			},
		},
		{
			name: "source block, snippet non-block - replace existing doc comment",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					const (
						// OldDoc
						KindA int = iota
						KindB
						KindC
					)
				`),
			},
			snippets: []string{
				dedent(`
					// KindA represents the first kind
					const KindA int = iota
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					const (
						// KindA represents the first kind
						KindA int = iota
						KindB
						KindC
					)
				`),
			},
		},
		{
			name: "source block, snippet non-block - replace existing EOL comment",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var (
						Foo int // old comment
						Bar string
						Baz bool
					)
				`),
			},
			snippets: []string{
				dedent(`
					var Foo int // Foo is the new improved comment
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var (
						Foo int // Foo is the new improved comment
						Bar string
						Baz bool
					)
				`),
			},
		},
		{
			name: "source block, snippet non-block - doc comment replaces EOL comment",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var (
						Alpha int // eol comment
						Beta string
					)
				`),
			},
			snippets: []string{
				dedent(`
					// Alpha is documented now
					var Alpha int
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var (
						// Alpha is documented now
						Alpha int
						Beta  string
					)
				`),
			},
		},
		{
			name: "source block, snippet non-block - EOL comment replaces doc comment",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var (
						// old doc
						Alpha int
						Beta string
					)
				`),
			},
			snippets: []string{
				dedent(`
					var Alpha int // Alpha with EOL
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var (
						Alpha int // Alpha with EOL
						Beta  string
					)
				`),
			},
		},
		{
			name: "source block, snippet non-block - multi-name vars",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var (
						A, B int
						C string
					)
				`),
			},
			snippets: []string{
				dedent(`
					// A and B are related
					var A, B int
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var (
						// A and B are related
						A, B int
						C    string
					)
				`),
			},
		},
		{
			name: "source block, snippet non-block - reject updates with existing doc",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var (
						// existing doc
						Foo int
						Bar string
					)
				`),
			},
			snippets: []string{
				dedent(`
					// new doc
					var Foo int
				`),
			},
			options:               Options{RejectUpdates: true},
			expectSnippetErrCount: 1,
			expectSnippetErrLike:  "not applied due to options restrictions",
			expectPartial:         true,
		},
		{
			name: "source block, snippet non-block - reject updates with existing EOL",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					const (
						Foo int = iota // existing
						Bar
					)
				`),
			},
			snippets: []string{
				dedent(`
					const Foo int = iota // new comment
				`),
			},
			options:               Options{RejectUpdates: true},
			expectSnippetErrCount: 1,
			expectSnippetErrLike:  "not applied due to options restrictions",
			expectPartial:         true,
		},
		{
			name: "source block, snippet non-block - no matching identifier error",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var (
						Foo int
						Bar string
					)
				`),
			},
			snippets: []string{
				dedent(`
					// Baz is something
					var Baz bool
				`),
			},
			expectSnippetErrCount: 1,
			expectSnippetErrLike:  "Could not find identifier definition for",
		},
		{
			name: "source block, snippet non-block - apply to middle of block",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var (
						First int
						Second string
						Third bool
						Fourth float64
					)
				`),
			},
			snippets: []string{
				dedent(`
					var Third bool // Third is in the middle
				`),
			},
			newSource: map[string]string{
				"code.go": dedent(`
					package mypkg

					var (
						First  int
						Second string
						Third  bool // Third is in the middle
						Fourth float64
					)
				`),
			},
		},
		{
			name: "empty var block doesn't crash things",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg
					var ()
				`),
			},
			snippets: []string{
				dedent(`
					// empty
					var ()
				`),
			},
			expectSnippetErrCount: 1,
			expectSnippetErrLike:  "var with no specs",
		},
		{
			name: "empty const block doesn't crash things",
			existingSource: map[string]string{
				"code.go": dedent(`
					package mypkg
					const ()
				`),
			},
			snippets: []string{
				dedent(`
					// empty
					const ()
				`),
			},
			expectSnippetErrCount: 1,
			expectSnippetErrLike:  "const with no specs",
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
	}

	for _, testCase := range tests {
		runTableDrivenDocUpdateTest(t, testCase)
	}
}
