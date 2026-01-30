package updatedocs

import (
	"testing"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"
)

func TestApplyLineEditsTableDriven(t *testing.T) {
	type testCase struct {
		name         string
		source       string
		edits        []LineEdit
		want         string
		expectErr    bool
		wantFileName string
	}

	testCases := []testCase{
		//
		// EditOpInsertBlankLineAbove:
		//
		{
			name: "insert blank line",
			source: dedent(`
				package testpkg

				func Foo() {
					// a comment
				}
			`),
			edits: []LineEdit{
				{EditOp: EditOpInsertBlankLineAbove, Line: 4},
			},
			want: dedent(`
				package testpkg

				func Foo() {

					// a comment
				}
			`),
		},
		{
			name: "insert multiple blank lines",
			source: dedent(`
				package testpkg

				var a int
				var b int
				var c int
			`),
			edits: []LineEdit{
				{EditOp: EditOpInsertBlankLineAbove, Line: 4}, {EditOp: EditOpInsertBlankLineAbove, Line: 5},
			},
			want: dedent(`
				package testpkg

				var a int

				var b int

				var c int
			`),
		},

		//
		// EditOpRemoveBlankLine:
		//
		{
			name: "remove blank line",
			source: dedent(`
				package testpkg

				func Bar() {

					// another comment
				}
			`),
			edits: []LineEdit{
				{EditOp: EditOpRemoveBlankLine, Line: 4},
			},
			want: dedent(`
				package testpkg

				func Bar() {
					// another comment
				}
			`),
		},
		{
			name: "error on remove non-blank line",
			source: dedent(`
				package testpkg
				func Foo() {}
			`),
			edits: []LineEdit{
				{EditOp: EditOpRemoveBlankLine, Line: 2},
			},
			expectErr: true,
		},

		//
		// EditOpSetEOLComment:
		//
		{
			name: "insert eol comment",
			source: dedent(`
				package testpkg

				func Foo() {
					// a comment
				}
			`),
			edits: []LineEdit{
				{EditOp: EditOpSetEOLComment, Line: 3, Comment: "// EOL"},
			},
			want: dedent(`
				package testpkg

				func Foo() { // EOL
					// a comment
				}
			`),
		},
		{
			name: "insert eol comment with newline terminated comment",
			source: dedent(`
				package testpkg

				func Foo() {
					// a comment
				}
			`),
			edits: []LineEdit{
				{EditOp: EditOpSetEOLComment, Line: 3, Comment: "// EOL\n"},
			},
			want: dedent(`
				package testpkg

				func Foo() { // EOL
					// a comment
				}
			`),
		},
		{
			name: "replace eol comment",
			source: dedent(`
				package testpkg

				var a int // orig
			`),
			edits: []LineEdit{
				{EditOp: EditOpSetEOLComment, Line: 3, Comment: "// new"},
			},
			want: dedent(`
				package testpkg

				var a int // new
			`),
		},
		{
			name: "set comment on blank line",
			source: dedent(`
				package testpkg

				type s struct {
					a int

					b int
				}
			`),
			edits: []LineEdit{
				{EditOp: EditOpSetEOLComment, Line: 5, Comment: "// b is a variable"},
			},
			want: dedent(`
				package testpkg

				type s struct {
					a int
					// b is a variable
					b int
				}
			`),
		},
		{
			name: "replace comment on comment line",
			source: dedent(`
				package testpkg

				// comment
				var a int
			`),
			edits: []LineEdit{
				{EditOp: EditOpSetEOLComment, Line: 3, Comment: "// new"},
			},
			want: dedent(`
				package testpkg

				// new
				var a int
			`),
		},
		{
			name: "EditOpSetEOLComment - error when multi line comment",
			source: dedent(`
				package testpkg

				var a int // comment
			`),
			edits: []LineEdit{
				{EditOp: EditOpSetEOLComment, Line: 3, Comment: "// new\n// second line"},
			},
			expectErr: true,
		},
		{
			name:   "comment // inside quoted string",
			source: "package testpkg\n\nvar x = \"// fake comment\" // old\n",
			edits: []LineEdit{
				{EditOp: EditOpSetEOLComment, Line: 3, Comment: "// new"},
			},
			want: "package testpkg\n\nvar x = \"// fake comment\" // new\n",
		},
		{
			name:   "comment // inside backtick string",
			source: "package testpkg\n\nvar x = `// fake comment` // old\n",
			edits: []LineEdit{
				{EditOp: EditOpSetEOLComment, Line: 3, Comment: "// new"},
			},
			want: "package testpkg\n\nvar x = `// fake comment` // new\n",
		},

		//
		// EditOpRemoveEOLComment:
		//
		{
			name: "remove eol comment",
			source: dedent(`
				package testpkg

				func Baz() { // existing EOL
				}
			`),
			edits: []LineEdit{
				{EditOp: EditOpRemoveEOLComment, Line: 3},
			},
			want: dedent(`
				package testpkg

				func Baz() {
				}
			`),
		},
		{
			name: "remove eol comment that is the whole line",
			source: dedent(`
				package testpkg

				func Qux() {
					// this whole line is a comment
				}
			`),
			edits: []LineEdit{
				{EditOp: EditOpRemoveEOLComment, Line: 4},
			},
			want: dedent(`
				package testpkg

				func Qux() {
				}
			`),
		},
		{
			name: "remove eol comment on line with no comment is a no op, no error",
			source: dedent(`
				package testpkg

				func Qux() int {
					return 0
				}
			`),
			edits: []LineEdit{
				{EditOp: EditOpRemoveEOLComment, Line: 4},
			},
			want: dedent(`
				package testpkg

				func Qux() int {
					return 0
				}
			`),
		},

		//
		// Other:
		//
		{
			name: "error on duplicate line",
			source: dedent(`
				package testpkg
				func Foo() {}
			`),
			edits:     []LineEdit{{EditOp: EditOpRemoveBlankLine, Line: 2}, {EditOp: EditOpRemoveBlankLine, Line: 2}},
			expectErr: true,
		},
		{
			name: "gofmt is run", // gofmt inserts a blank line above the new comment
			source: dedent(`
				package testpkg

				var a int
				// b
				var b int
			`),
			want: dedent(`
				package testpkg

				var a int

				// b is...
				var b int
			`),
			edits: []LineEdit{{EditOp: EditOpSetEOLComment, Line: 4, Comment: "// b is..."}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fileName := tc.wantFileName
			if fileName == "" {
				fileName = "file.go"
			}
			gocodetesting.WithMultiCode(t, map[string]string{fileName: tc.source}, func(pkg *gocode.Package) {
				file := pkg.Files[fileName]
				got, err := ApplyLineEdits(file, tc.edits)
				if (err != nil) != tc.expectErr {
					t.Fatalf("ApplyLineEdits() error = %v, expectErr %v", err, tc.expectErr)
				}
				if err != nil {
					return
				}

				assertFileSourceEquals(t, got, tc.want)
			})
		})
	}
}
