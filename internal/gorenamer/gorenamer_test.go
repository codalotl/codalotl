package gorenamer

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"unicode"

	"github.com/codalotl/codalotl/internal/goclitools"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"
)

// test alias per instructions
var dedent = gocodetesting.Dedent

func TestRename_Errors(t *testing.T) {
	// Save and restore renameFunc
	savedRename := renameFunc
	defer func() { renameFunc = savedRename }()

	type testCase struct {
		name    string
		src     string
		in      IdentifierRename
		stubErr error // error returned by rename tool stub
		wantErr string
	}

	cases := []testCase{
		{
			name: "invalid from identifier",
			src: dedent(`
                package mypkg

                func Foo() {}
            `),
			in:      IdentifierRename{From: "", To: "NewName", DeclID: "Foo", Context: "func Foo() {}", FileName: "code.go"},
			wantErr: "invalid identifier in From",
		},
		{
			name: "invalid to identifier",
			src: dedent(`
                func Foo() {}
            `),
			in:      IdentifierRename{From: "Foo", To: "", DeclID: "Foo", Context: "func Foo() {}", FileName: "code.go"},
			wantErr: "invalid identifier in To",
		},
		{
			name: "decl id not found",
			src: dedent(`
                func Foo() {}
            `),
			in:      IdentifierRename{From: "Foo", To: "Bar", DeclID: "Nope", Context: "func Foo() {}", FileName: "code.go"},
			wantErr: "could not find DeclID",
		},
		{
			name: "file name not found",
			src: dedent(`
                func Foo() {}
            `),
			in:      IdentifierRename{From: "Foo", To: "Bar", DeclID: "Foo", Context: "func Foo() {}", FileName: "missing.go"},
			wantErr: "could not find FileName",
		},
		{
			name: "context empty",
			src: dedent(`
                func Foo() {}
            `),
			in:      IdentifierRename{From: "Foo", To: "Bar", DeclID: "Foo", Context: "", FileName: "code.go"},
			wantErr: "context invalid",
		},
		{
			name: "context missing from",
			src: dedent(`
                func Foo() {
					var x = 0
					_ = x
				}
            `),
			in:      IdentifierRename{From: "x", To: "y", DeclID: "Foo", Context: "func Foo() {", FileName: "code.go"},
			wantErr: "context does not contain from",
		},
		{
			name: "context not found",
			src: dedent(`
                func Foo() {}
            `),
			in:      IdentifierRename{From: "Foo", To: "Bar", DeclID: "Foo", Context: "func Foo() {} ", FileName: "code.go"},
			wantErr: "could not find context",
		},
		{
			name: "context ambiguous",
			src: dedent(`
				func foo(p int) {
					if p > 0 {
						var x = 1
						_ = x
					} else {
						var x = 1
						_ = x
					}
				}
			`),
			in:      IdentifierRename{From: "x", To: "y", DeclID: "foo", Context: "		var x = 1", FileName: "code.go"},
			wantErr: "context is ambiguous",
		},
		{
			name: "cannot find defining AST on line",
			src: dedent(`
                func Foo() {
					var x = 0
					_ = x
				}
            `),
			in:      IdentifierRename{From: "x", To: "y", DeclID: "Foo", Context: "	_ = x", FileName: "code.go"},
			wantErr: "could not find defining AST",
		},
		{
			name: "external rename tool error",
			src: dedent(`
                func Foo() {}
            `),
			in:      IdentifierRename{From: "Foo", To: "Bar", DeclID: "Foo", Context: "func Foo() {}", FileName: "code.go"},
			stubErr: errors.New("tool failed"),
			wantErr: "tool failed",
		},
	}

	for _, tc := range cases {
		// capture range var
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// stub external tool
			renameFunc = func(string, int, int, string) error { return tc.stubErr }

			gocodetesting.WithCode(t, tc.src, func(pkg *gocode.Package) {
				_, failed, err := Rename(pkg, []IdentifierRename{tc.in})
				if err != nil {
					t.Fatalf("unexpected fatal err: %v", err)
				}
				if len(failed) == 0 {
					t.Fatalf("expected a failure containing %q, got none", tc.wantErr)
				}
				if failed[0].Err == nil || !strings.Contains(failed[0].Err.Error(), tc.wantErr) {
					t.Fatalf("want error containing %q, got %v", tc.wantErr, failed[0].Err)
				}
			})
		})
	}
}

func TestRename_Success(t *testing.T) {
	// Save and restore renameFunc
	savedRename := renameFunc
	defer func() { renameFunc = savedRename }()

	type testCase struct {
		name     string
		src      string
		in       IdentifierRename
		wantLine int
		wantCol  int
	}

	cases := []testCase{
		{
			name: "function rename",
			src: dedent(`
				func Foo() {}
			`),
			in:       IdentifierRename{From: "Foo", To: "Bar", DeclID: "Foo", Context: "func Foo() {}", FileName: "code.go"},
			wantLine: 3,
			wantCol:  6,
		},
		{
			name: "type rename",
			src: dedent(`
				type T struct {}
			`),
			in:       IdentifierRename{From: "T", To: "U", DeclID: "T", Context: "type T struct {}", FileName: "code.go"},
			wantLine: 3,
			wantCol:  6,
		},
		{
			name: "var rename",
			src: dedent(`
				var myVar = 1
			`),
			in:       IdentifierRename{From: "myVar", To: "newVar", DeclID: "myVar", Context: "var myVar = 1", FileName: "code.go"},
			wantLine: 3,
			wantCol:  5,
		},
		{
			name: "var in func rename",
			src: dedent(`
				func foo() {
					var x = 1
					_ = x
				}
			`),
			in:       IdentifierRename{From: "x", To: "y", DeclID: "foo", Context: "	var x = 1", FileName: "code.go"},
			wantLine: 4,
			wantCol:  6,
		},
		{
			name: "2 line context",
			src: dedent(`
				func foo() {
					var x = 1
					_ = x
				}
			`),
			in:       IdentifierRename{From: "x", To: "y", DeclID: "foo", Context: "func foo() {\n	var x = 1", FileName: "code.go"},
			wantLine: 4,
			wantCol:  6,
		},
		{
			name: "if assign test",
			src: dedent(`
				func bar() int {return 0}
				func foo() {
					if b := bar(); b > 0 {
					}
				}
			`),
			in:       IdentifierRename{From: "b", To: "y", DeclID: "foo", Context: "	if b := bar(); b > 0 {", FileName: "code.go"},
			wantLine: 5,
			wantCol:  5,
		},
		{
			name: "ambiguous context resolved by decl id",
			src: dedent(`
				func bar() {
					var x = 1
					_ = x
				}
				func foo() {
					var x = 1
					_ = x
				}
			`),
			in:       IdentifierRename{From: "x", To: "y", DeclID: "foo", Context: "	var x = 1", FileName: "code.go"},
			wantLine: 8,
			wantCol:  6,
		},
		{
			name: "ambiguous resolved by context",
			src: dedent(`
				func foo(p int) {
					if p > 0 {
						var x = 1
						_ = x
					} else {
						var x = 1
						_ = x
					}
				}
			`),
			in:       IdentifierRename{From: "x", To: "y", DeclID: "foo", Context: "	if p > 0 {\n		var x = 1", FileName: "code.go"},
			wantLine: 5,
			wantCol:  7,
		},
		{
			name: "ambiguous resolved by context 2",
			src: dedent(`
				func foo(p int) {
					if p > 0 {
						var x = 1
						_ = x
					} else {
						var x = 1
						_ = x
					}
				}
			`),
			in:       IdentifierRename{From: "x", To: "y", DeclID: "foo", Context: "	} else {\n		var x = 1", FileName: "code.go"},
			wantLine: 8,
			wantCol:  7,
		},
		{
			name: "short var in func",
			src: dedent(`
				func foo() {
					x := 1
					_ = x
				}
			`),
			in:       IdentifierRename{From: "x", To: "y", DeclID: "foo", Context: "\tx := 1", FileName: "code.go"},
			wantLine: 4,
			wantCol:  2,
		},
		{
			name: "short var multi-assign in func (rename second)",
			src: dedent(`
				func foo() {
					x, y := 1, 2
					_, _ = x, y
				}
			`),
			in:       IdentifierRename{From: "y", To: "z", DeclID: "foo", Context: "\tx, y := 1, 2", FileName: "code.go"},
			wantLine: 4,
			wantCol:  5,
		},
		{
			name: "var multi on one line (rename second)",
			src: dedent(`
				func foo() {
					var a, b int
					_, _ = a, b
				}
			`),
			in:       IdentifierRename{From: "b", To: "bb", DeclID: "foo", Context: "\tvar a, b int", FileName: "code.go"},
			wantLine: 4,
			wantCol:  9,
		},
		{
			name: "var block (rename y)",
			src: dedent(`
				func foo() {
					var (
						x int
						y = 2
					)
					_, _ = x, y
				}
			`),
			in:       IdentifierRename{From: "y", To: "yy", DeclID: "foo", Context: "\t\ty = 2", FileName: "code.go"},
			wantLine: 6,
			wantCol:  3,
		},
		{
			name: "function param (single)",
			src: dedent(`
				func foo(a int) {}
			`),
			in:       IdentifierRename{From: "a", To: "arg", DeclID: "foo", Context: "func foo(a int) {}", FileName: "code.go"},
			wantLine: 3,
			wantCol:  10,
		},
		{
			name: "function params (multiple, rename second)",
			src: dedent(`
				func foo(a, b int) {}
			`),
			in:       IdentifierRename{From: "b", To: "beta", DeclID: "foo", Context: "func foo(a, b int) {}", FileName: "code.go"},
			wantLine: 3,
			wantCol:  13,
		},
		{
			name: "named result param (single)",
			src: dedent(`
				func foo() (n int) {}
			`),
			in:       IdentifierRename{From: "n", To: "res", DeclID: "foo", Context: "func foo() (n int) {}", FileName: "code.go"},
			wantLine: 3,
			wantCol:  13,
		},
		{
			name: "named result params (multiple, rename second)",
			src: dedent(`
				func foo() (n, m int) {}
			`),
			in:       IdentifierRename{From: "m", To: "res2", DeclID: "foo", Context: "func foo() (n, m int) {}", FileName: "code.go"},
			wantLine: 3,
			wantCol:  16,
		},
		{
			name: "method receiver (pointer)",
			src: dedent(`
				type T struct{}
				func (r *T) M() {}
			`),
			in:       IdentifierRename{From: "r", To: "recv", DeclID: "*T.M", Context: "func (r *T) M() {}", FileName: "code.go"},
			wantLine: 4,
			wantCol:  7,
		},
		{
			name: "switch init short var",
			src: dedent(`
				func bar() int {return 0}
				func foo() {
					switch s := bar(); s {
					default:
					}
				}
			`),
			in:       IdentifierRename{From: "s", To: "sel", DeclID: "foo", Context: "\tswitch s := bar(); s {", FileName: "code.go"},
			wantLine: 5,
			wantCol:  9,
		},
		{
			name: "for init short var",
			src: dedent(`
				func foo() {
					for i := 0; i < 10; i++ {
					}
				}
			`),
			in:       IdentifierRename{From: "i", To: "idx", DeclID: "foo", Context: "\tfor i := 0; i < 10; i++ {", FileName: "code.go"},
			wantLine: 4,
			wantCol:  6,
		},
		{
			name: "range",
			src: dedent(`
				func foo() {
					for _, x := range []string{"a"} {
						_ = a
					}
				}
			`),
			in:       IdentifierRename{From: "x", To: "y", DeclID: "foo", Context: "\tfor _, x := range []string{\"a\"} {", FileName: "code.go"},
			wantLine: 4,
			wantCol:  9,
		},
		{
			name: "func literal param",
			src: dedent(`
				func foo() {
					_ = func(p int) {}
				}
			`),
			in:       IdentifierRename{From: "p", To: "param", DeclID: "foo", Context: "\t_ = func(p int) {}", FileName: "code.go"},
			wantLine: 4,
			wantCol:  11,
		},
		{
			name: "fallback: missing leading whitespace on context line",
			src: dedent(`
				func foo() {
					var x = 1
					_ = x
				}
			`),
			in:       IdentifierRename{From: "x", To: "y", DeclID: "foo", Context: "var x = 1", FileName: "code.go"},
			wantLine: 4,
			wantCol:  6,
		},
		// note: multi-line first-line-missing-indent is not covered by fallback; only exact or spaces/tabs fallback
		{
			name: "fallback: spaces instead of tabs in leading indent",
			src: dedent(`
				func foo() {
					var x = 1
					_ = x
				}
			`),
			in:       IdentifierRename{From: "x", To: "y", DeclID: "foo", Context: "    var x = 1", FileName: "code.go"},
			wantLine: 4,
			wantCol:  6,
		},
	}

	for _, tc := range cases {
		// capture range var
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// capture calls to the external tool
			type call struct {
				path      string
				line, col int
				to        string
			}
			var calls []call
			renameFunc = func(path string, line, col int, to string) error {
				calls = append(calls, call{path: path, line: line, col: col, to: to})
				return nil
			}

			gocodetesting.WithCode(t, tc.src, func(pkg *gocode.Package) {
				succeeded, failed, err := Rename(pkg, []IdentifierRename{tc.in})
				if err != nil {
					t.Fatalf("unexpected fatal err: %v", err)
				}
				if len(failed) != 0 {
					t.Fatalf("expected no failures, got %d: %+v", len(failed), failed)
				}
				if len(succeeded) != 1 {
					t.Fatalf("expected 1 success, got %d", len(succeeded))
				}
				if len(calls) != 1 {
					t.Fatalf("expected 1 tool call, got %d", len(calls))
				}

				got := calls[0]
				wantPath := pkg.Files["code.go"].AbsolutePath
				if got.path != wantPath {
					t.Fatalf("tool path mismatch: want %q got %q", wantPath, got.path)
				}
				if got.line != tc.wantLine || got.col != tc.wantCol {
					t.Fatalf("want line=%d col=%d, got line=%d col=%d", tc.wantLine, tc.wantCol, got.line, got.col)
				}
				if got.to != tc.in.To {
					t.Fatalf("tool new-name mismatch: want %q got %q", tc.in.To, got.to)
				}
			})
		})
	}
}

func TestRename_NoStub_VarRename(t *testing.T) {
	// Skip if gopls is not available; this test exercises the real CLI rename.
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available; skipping integration rename test")
	}

	// Ensure we use the real external tool implementation.
	saved := renameFunc
	renameFunc = goclitools.Rename
	defer func() { renameFunc = saved }()

	src := dedent(`
		func foo() {
			x := 1
			_ = x
		}
	`)

	gocodetesting.WithCode(t, src, func(pkg *gocode.Package) {
		in := IdentifierRename{From: "x", To: "y", DeclID: "foo", Context: "\tx := 1", FileName: "code.go"}
		succeeded, failed, err := Rename(pkg, []IdentifierRename{in})
		if err != nil {
			t.Fatalf("unexpected fatal err: %v", err)
		}
		if len(failed) != 0 {
			t.Fatalf("expected no failures, got %d: %+v", len(failed), failed)
		}
		if len(succeeded) != 1 {
			t.Fatalf("expected 1 success, got %d", len(succeeded))
		}

		path := pkg.Files["code.go"].AbsolutePath
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed reading modified file: %v", err)
		}
		got := string(b)
		if !strings.Contains(got, "\ty := 1") || !strings.Contains(got, "\t_ = y") {
			t.Fatalf("rename did not update file as expected. Contents:\n%s", got)
		}
		if strings.Contains(got, "\tx := 1") || strings.Contains(got, "\t_ = x") {
			t.Fatalf("old identifier still present in file. Contents:\n%s", got)
		}
	})
}

func TestRename_SequentialReload_ConflictingColumns(t *testing.T) {
	// Save and restore renameFunc
	saved := renameFunc
	defer func() { renameFunc = saved }()

	// Fake fast in-memory renamer that edits the file at the given line/col,
	// replacing the identifier that starts at col with `to`.
	renameFunc = func(path string, line, col int, to string) error {
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lines := strings.Split(string(b), "\n")
		if line-1 < 0 || line-1 >= len(lines) {
			return errors.New("line out of range")
		}
		ln := lines[line-1]
		// Convert to rune slice for correct column handling with unicode
		rs := []rune(ln)
		start := col - 1
		if start < 0 || start >= len(rs) {
			return errors.New("col out of range")
		}
		// Scan the identifier starting at start
		end := start
		for end < len(rs) {
			r := rs[end]
			if r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
				end++
				continue
			}
			break
		}
		// Replace identifier
		newLine := string(rs[:start]) + to + string(rs[end:])
		lines[line-1] = newLine
		return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
	}

	src := dedent(`
		func foo() {
			var a, bb int
			_, _ = a, bb
		}
	`)

	gocodetesting.WithCode(t, src, func(pkg *gocode.Package) {
		// Two renames on the same line: first increases the length of the first identifier,
		// which would shift the column of the second identifier if computed early.
		in := []IdentifierRename{
			{From: "a", To: "alpha", DeclID: "foo", Context: "\tvar a, bb int", FileName: "code.go"},
			{From: "bb", To: "b2", DeclID: "foo", Context: "\tvar a, bb int", FileName: "code.go"},
		}

		succeeded, failed, err := Rename(pkg, in)
		if err != nil {
			t.Fatalf("unexpected fatal err: %v", err)
		}
		if len(failed) != 0 {
			t.Fatalf("expected no failures, got %d: %+v", len(failed), failed)
		}
		if len(succeeded) != 2 {
			t.Fatalf("expected 2 successes, got %d", len(succeeded))
		}

		// Verify file contents moved to the new state after both renames.
		path := pkg.Files["code.go"].AbsolutePath
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed reading modified file: %v", err)
		}
		got := string(b)
		if !strings.Contains(got, "\tvar alpha, b2 int") {
			t.Fatalf("expected updated declarations; got contents:\n%s", got)
		}
		// Note: our fake tool does not update references; this test only asserts sequential column handling & reload.
	})
}
