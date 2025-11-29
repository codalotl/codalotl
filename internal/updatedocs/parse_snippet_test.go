package updatedocs

import (
	"go/parser"
	"go/token"
	"testing"
)

// TestParseValidateSnippet exercises the validation matrix described in the parseValidateSnippet contract. Each sub-test feeds a code snippet to the helper and asserts whether it should
// succeed or fail.
func TestParseValidateSnippet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		snippet  string
		wantErr  bool
		wantKind snippetKind
	}{
		//
		// Functions
		//
		{
			name: "single function",
			snippet: dedent(`
				func Foo() {}
			`),
			wantErr:  false,
			wantKind: snippetKindFunc,
		},
		{
			name: "two functions (disallowed)",
			snippet: dedent(`
				func Foo() {}
				func Bar() {}
			`),
			wantErr:  true,
			wantKind: snippetKindUnknown,
		},
		{
			name: "func with comments",
			snippet: dedent(`
				// Foo does something
				func Foo()
			`),
			wantErr:  false,
			wantKind: snippetKindFunc,
		},
		{
			name: "func with brace mismatch with comments",
			snippet: dedent(`
				// Foo does something
				func Foo() {
			`),
			wantErr:  false,
			wantKind: snippetKindFunc,
		},

		//
		// Types
		//
		{
			name: "single type",
			snippet: dedent(`
				type Foo struct{}
			`),
			wantErr:  false,
			wantKind: snippetKindType,
		},
		{
			name: "type block with single types (allowed)",
			snippet: dedent(`
				type (
					Foo struct{}
				)
			`),
			wantErr:  false,
			wantKind: snippetKindType,
		},
		{
			name: "type block with multiple types (allowed)",
			snippet: dedent(`
				type (
					Foo struct{}
					Bar int
				)
			`),
			wantErr:  false,
			wantKind: snippetKindType,
		},
		{
			name: "type block with no types (disallowed)",
			snippet: dedent(`
				type (
				)
			`),
			wantErr:  true,
			wantKind: snippetKindUnknown,
		},
		{
			name: "type followed by var (disallowed)",
			snippet: dedent(`
				type Foo struct{}
				var x int
			`),
			wantErr:  true,
			wantKind: snippetKindUnknown,
		},
		{
			name: "type with comments",
			snippet: dedent(`
				// Foo does something
				type Foo struct {
					// Field
					Field int
				}
			`),
			wantErr:  false,
			wantKind: snippetKindType,
		},
		{
			name: "type with comments - invalid",
			snippet: dedent(`
				// Foo does something
				type Foo struct {
					// Field
					Field int // field
				}
			`),
			wantErr: true,
		},

		//
		// Vars/Consts
		//
		{
			name: "vars only - individual and block (allowed)",
			snippet: dedent(`
				var a int
				var b string
				var (
					c float64
					d bool
				)
			`),
			wantErr:  false,
			wantKind: snippetKindVar,
		},
		{
			name: "vars mixed with const (disallowed)",
			snippet: dedent(`
				var a int
				const b = 2
			`),
			wantErr:  true,
			wantKind: snippetKindUnknown,
		},
		{
			name: "consts only - individual and block (allowed)",
			snippet: dedent(`
				const Pi = 3.14
				const (
					E   = 2.7182
					Phi = 1.6180
				)
			`),
			wantErr:  false,
			wantKind: snippetKindConst,
		},
		{
			name: "empty const block",
			snippet: dedent(`
				const ()
			`),
			wantErr:  true,
			wantKind: snippetKindConst,
		},

		//
		// Package docs
		//
		{
			name: "package comments are ok",
			snippet: dedent(`
				// Package level comment
				package mypkg
			`),
			wantErr:  false,
			wantKind: snippetKindPackageDoc,
		},
		{
			name: "package with no comment is not ok",
			snippet: dedent(`
				package mypkg
			`),
			wantErr:  true,
			wantKind: snippetKindUnknown,
		},
		{
			name: "package comments with other stuff is not ok",
			snippet: dedent(`
				// Package level comment
				package mypkg

				// Foo is...
				func Foo()
			`),
			wantErr:  true,
			wantKind: snippetKindUnknown,
		},
		{
			name: "package without comment is ok with something else",
			snippet: dedent(`
				package mypkg

				// Foo is...
				func Foo()
			`),
			wantErr:  false,
			wantKind: snippetKindFunc,
		},
		{
			name: "name mismatch: pkg comment",
			snippet: dedent(`
				// Package level comment
				package yourpkg
			`),
			wantErr:  true,
			wantKind: snippetKindUnknown,
		},
		{
			name: "name mismatch: pkg with other stuff",
			snippet: dedent(`
				package yourpkg

				// Foo is...
				func Foo()
			`),
			wantErr:  true,
			wantKind: snippetKindUnknown,
		},

		//
		// Misc errors / corner cases
		//
		{
			name: "syntax error (parse failure)",
			snippet: dedent(`
				func Foo( {}
			`),
			wantErr:  true,
			wantKind: snippetKindUnknown,
		},

		{
			name:     "blank",
			snippet:  ``,
			wantErr:  true,
			wantKind: snippetKindUnknown,
		},
	}

	for _, tc := range tests {
		tc := tc // capture
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			file, fset, kind, err := parseValidateSnippet("mypkg", tc.snippet, Options{})
			if (err != nil) != tc.wantErr {
				t.Fatalf("parseValidateSnippet() error = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && file == nil {
				t.Fatalf("expected non-nil *ast.File for valid snippet")
			}
			if !tc.wantErr && fset == nil {
				t.Fatalf("expected non-nil *token.FileSet for valid snippet")
			}
			if !tc.wantErr && kind != tc.wantKind {
				t.Errorf("parseValidateSnippet() kind = %v, wantKind %v", kind, tc.wantKind)
			}
		})
	}
}

func TestStripBackticks(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "no wrap",
			input: `fmt.Println("hi")`,
			want:  `fmt.Println("hi")`,
		},
		{
			name:  "wrapped no language",
			input: "```\nfmt.Println(\"hi\")\n```",
			want:  "fmt.Println(\"hi\")\n",
		},
		{
			name:  "wrapped with newline",
			input: "```\nfmt.Println(\"hi\")\n```\n",
			want:  "fmt.Println(\"hi\")\n",
		},
		{
			name:  "wrapped go language",
			input: "```go\nfmt.Println(\"hi\")\n```",
			want:  "fmt.Println(\"hi\")\n",
		},
		{
			name:    "unsupported language",
			input:   "```python\nprint('hi')\n```",
			wantErr: true,
		},
		{
			name:    "wrong opener length",
			input:   "``\nfoo\n```",
			wantErr: true,
		},
		{
			name:    "wrong closer length",
			input:   "```\nfoo\n````",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		got, err := stripBackticks(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("%s: error expectation mismatch (got err=%v)", tt.name, err)
			continue
		}
		if !tt.wantErr && got != tt.want {
			t.Errorf("%s: expected %q, got %q", tt.name, tt.want, got)
		}
	}
}

func TestValidSnippetComments(t *testing.T) {
	tests := []struct {
		name        string
		snippet     string
		expectError bool
	}{
		{
			name: "valid single line type - eol",
			snippet: dedent(`
				type Foo int // comment
			`),
			expectError: false,
		},
		{
			name: "valid single line type - doc",
			snippet: dedent(`
				// comment
				type Foo int
			`),
			expectError: false,
		},
		{
			name: "valid single line type - neither",
			snippet: dedent(`
				type Foo int
			`),
			expectError: false,
		},
		{
			name: "invalid single line type - both",
			snippet: dedent(`
				// comment
				type Foo int // comment
			`),
			expectError: true,
		},
		{
			name: "valid type with doc comment",
			snippet: dedent(`
				// Foo is a type
				type Foo struct {
					Bar int
				}
			`),
			expectError: false,
		},
		// {
		// 	name: "invalid type with EOL comment on multi-line declaration",
		// 	snippet: dedent(`
		// 		type Foo struct {
		// 			Bar int
		// 		} // this is invalid
		// 	`),
		// 	expectError: true,
		// },
		{
			name: "valid type with doc comment",
			snippet: dedent(`
				// Foo is a type
				type Foo struct {
					Bar int
				}
			`),
			expectError: false,
		},
		{
			name: "valid type of struct -- good field doc",
			snippet: dedent(`
				type Foo struct {
					// bar
					Bar int
				}
			`),
			expectError: false,
		},
		{
			name: "valid type of struct -- good field eol",
			snippet: dedent(`
				type Foo struct {
					Bar int // bar
				}
			`),
			expectError: false,
		},
		{
			name: "valid type of struct -- good field nothing",
			snippet: dedent(`
				type Foo struct {
					Bar int
				}
			`),
			expectError: false,
		},
		{
			name: "valid type of struct -- bad field both",
			snippet: dedent(`
				type Foo struct {
					// bar
					Bar int // bar
				}
			`),
			expectError: true,
		},
		{
			name: "valid type of struct with nested structs",
			snippet: dedent(`
				// Foo is a type
				type Foo struct {
					Bar int // eol

					// qux ...
					Qux struct {
						A int // ok

						// ok
						B int
					}
				}
			`),
			expectError: false,
		},
		{
			name: "invalid type of struct with nested structs",
			snippet: dedent(`
				// Foo is a type
				type Foo struct {
					Bar int // eol

					// qux ...
					Qux struct {
						// a-ok
						A int // not ok

						// ok
						B int
					}
				}
			`),
			expectError: true,
		},
		{
			name: "valid block type",
			snippet: dedent(`
				// block comment
				type (
					// ok
					Foo int

					Bar int // ok
				)
			`),
			expectError: false,
		},
		{
			name: "invalid block type",
			snippet: dedent(`
				// block comment
				type (
					// ok
					Foo int

					// not ok
					Bar int // not ok
				)
			`),
			expectError: true,
		},
		{
			name: "valid var with doc comment",
			snippet: dedent(`
				// Foo is a variable
				var Foo = 42
			`),
			expectError: false,
		},
		{
			name: "valid var with EOL comment",
			snippet: dedent(`
				var Foo = 42 // is a variable
			`),
			expectError: false,
		},
		{
			name: "invalid var with both doc and EOL comments",
			snippet: dedent(`
				// Foo is a variable
				var Foo = 42 // is a number
			`),
			expectError: true,
		},
		{
			name: "valid const with doc comment",
			snippet: dedent(`
				// Foo is a constant
				const Foo = 42
			`),
			expectError: false,
		},
		{
			name: "valid const with EOL comment",
			snippet: dedent(`
				const Foo = 42 // is a constant
			`),
			expectError: false,
		},
		{
			name: "invalid const with both doc and EOL comments",
			snippet: dedent(`
				// Foo is a constant
				const Foo = 42 // is a number
			`),
			expectError: true,
		},
		{
			name: "valid var with block - doc",
			snippet: dedent(`
				// decl doc
				var (
					// spec doc
					Foo = 42
				)
			`),
			expectError: false,
		},
		{
			name: "valid var with block - eol",
			snippet: dedent(`
				// decl doc
				var (
					Foo = 42 // spec eol
				)
			`),
			expectError: false,
		},
		{
			name: "invalid var with block - doc and eol",
			snippet: dedent(`
				// decl doc
				var (
					// spec doc
					Foo = 42 // spec eol
				)
			`),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "test.go", "package code\n\n"+tt.snippet, parser.ParseComments)
			if err != nil {
				t.Fatalf("failed to parse test snippet: %v", err)
			}

			err = validSnippetComments(file)
			if tt.expectError && err == nil {
				t.Errorf("validSnippetComments() expected error but got nil")
			} else if !tt.expectError && err != nil {
				t.Errorf("validSnippetComments() expected no error but got: %v", err)
			}
		})
	}
}
