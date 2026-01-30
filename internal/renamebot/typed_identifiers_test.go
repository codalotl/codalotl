package renamebot

import (
	"fmt"
	"go/ast"
	"path/filepath"
	"testing"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"github.com/codalotl/codalotl/internal/gotypes"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var dedent = gocodetesting.Dedent

// newBasicTI returns a new typedIdentifier for testing where:
//   - RootType == CompleteType
//   - non-named type, no ptr/map/slice, etc.
func newBasicTI(kind IdentifierKind, id string, typ string, snippetID string) typedIdentifier {
	return typedIdentifier{
		Kind:              kind,
		Identifier:        id,
		RootType:          typ,
		CompleteType:      typ,
		IsNamedType:       false,
		IsSlice:           false,
		IsMap:             false,
		IsPtr:             false,
		SnippetIdentifier: snippetID,
	}
}

func TestTypedIdentifiersReflexive(t *testing.T) {
	mod, err := gocode.NewModule(gocode.MustCwd())
	require.NoError(t, err)

	pkg, err := mod.LoadPackageByRelativeDir("internal/renamebot")
	require.NoError(t, err)

	typeInfo, err := gotypes.LoadTypeInfoInto(pkg, false)
	require.NoError(t, err)

	file := pkg.Files["typed_identifiers.go"]
	require.NotNil(t, file)
	require.NotNil(t, file.AST)

	typedIdentifiers, err := typedIdentifiersInFile(pkg, file, typeInfo)
	require.NoError(t, err)

	var infoVar *typedIdentifier
	for _, ti := range typedIdentifiers {
		assert.EqualValues(t, "typed_identifiers.go", ti.FileName)

		if ti.Identifier == "info" && ti.Kind == IdentifierKindFuncVar && ti.SnippetIdentifier == "typedIdentifiersInFile" {
			assert.Nil(t, infoVar)
			infoVar = ti
		}
	}

	require.NotNil(t, infoVar)
	require.NotNil(t, infoVar.Expr)
	assert.Equal(t, "go/types.Info", infoVar.RootType)
	assert.Equal(t, "*go/types.Info", infoVar.CompleteType)
	assert.Equal(t, true, infoVar.IsNamedType)

	ident, ok := infoVar.Expr.(*ast.Ident)
	require.True(t, ok)
	assert.Equal(t, "info", ident.Name)

	pos := file.FileSet.Position(ident.Pos())
	assert.Equal(t, "typed_identifiers.go", filepath.Base(pos.Filename))
	assert.True(t, pos.Line > 0)
}

func TestTypedIdentifiersInPackage_SplitsTests(t *testing.T) {
	gocodetesting.WithMultiCode(t, map[string]string{
		"code.go": dedent(`
            type T int
            var V int
            func F(x int) {}
        `),
		"code_test.go": dedent(`
            import "testing"
            func TestX(t *testing.T) {
                y := 3
                _ = y
            }
        `),
	}, func(pkg *gocode.Package) {
		// Non-test pass
		ids, updatedPkg, err := typedIdentifiersInPackage(pkg, false)
		require.NoError(t, err)
		require.NotNil(t, updatedPkg)

		// Expect non-test identifiers only (T, V, F param x)
		var names []string
		for _, id := range ids {
			names = append(names, fmt.Sprintf("%s:%s", id.Kind, id.Identifier))
		}

		// Ensure there are some and none from tests
		assert.Contains(t, names, fmt.Sprintf("%s:%s", IdentifierKindType, "T"))
		assert.Contains(t, names, fmt.Sprintf("%s:%s", IdentifierKindPkgVar, "V"))
		assert.Contains(t, names, fmt.Sprintf("%s:%s", IdentifierKindFuncParam, "x"))
		for _, n := range names {
			assert.NotContains(t, n, ":y") // from Test file
		}

		// Test-only pass
		idsTest, _, err := typedIdentifiersInPackage(pkg, true)
		require.NoError(t, err)

		var testNames []string
		for _, id := range idsTest {
			testNames = append(testNames, fmt.Sprintf("%s:%s", id.Kind, id.Identifier))
		}
		// Should contain the y func-var from the test and not contain T/V/x
		assert.Contains(t, testNames, fmt.Sprintf("%s:%s", IdentifierKindFuncVar, "y"))
		for _, n := range testNames {
			assert.NotContains(t, n, ":T")
			assert.NotContains(t, n, ":V")
			assert.NotContains(t, n, ":x")
		}
	})
}

func TestTypedIdentifiersTableDriven(t *testing.T) {
	var zero = 0
	var two = 2
	tests := []struct {
		name          string
		src           string
		expected      []typedIdentifier
		expectedCount *int
	}{
		//
		// IdentifierKindFuncVar
		//
		{
			name: "variable in function",
			src: dedent(`
				func foo() {
					var a int
					_ = a
				}
			`),
			expected: []typedIdentifier{newBasicTI(IdentifierKindFuncVar, "a", "int", "foo")},
		},
		{
			name: "short var in if init",
			src: dedent(`
                func foo() {
                    if x := 3; x > 0 {
                        _ = x
                    }
                }
            `),
			expected: []typedIdentifier{newBasicTI(IdentifierKindFuncVar, "x", "int", "foo")},
		},
		{
			name: "short var in switch init",
			src: dedent(`
                func foo(x int) {
                    switch y := x + 1; y {
                    case 1:
                    }
                }
            `),
			expected: []typedIdentifier{newBasicTI(IdentifierKindFuncVar, "y", "int", "foo")},
		},
		{
			name: "short var in for init",
			src: dedent(`
                func foo() {
                    for i := 0; i < 3; i++ {
                        _ = i
                    }
                }
            `),
			expected: []typedIdentifier{newBasicTI(IdentifierKindFuncVar, "i", "int", "foo")},
		},
		{
			name: "short var in select case",
			src: dedent(`
                func foo(ch <-chan int) {
                    select {
                    case v := <-ch:
                        _ = v
                    }
                }
            `),
			expected: []typedIdentifier{newBasicTI(IdentifierKindFuncVar, "v", "int", "foo")},
		},
		{
			name: "short var in select case with ok",
			src: dedent(`
                func foo(ch <-chan int) {
                    select {
                    case v, ok := <-ch:
                        _ = v
                        _ = ok
                    }
                }
            `),
			expected: []typedIdentifier{newBasicTI(IdentifierKindFuncVar, "v", "int", "foo"), newBasicTI(IdentifierKindFuncVar, "ok", "bool", "foo")},
		},
		{
			name: "partial redeclaration only counts new idents",
			src: dedent(`
                func foo() {
                    a := 1
                    a, b := 2, 3
                    _ = a
                    _ = b
                }
            `),
			expected:      []typedIdentifier{newBasicTI(IdentifierKindFuncVar, "a", "int", "foo"), newBasicTI(IdentifierKindFuncVar, "b", "int", "foo")},
			expectedCount: &two,
		},
		{
			name: "range single index",
			src: dedent(`
                func foo() {
                    for i := range []string{"a"} {
                        _ = i
                    }
                }
            `),
			expected: []typedIdentifier{newBasicTI(IdentifierKindFuncVar, "i", "int", "foo")},
		},
		{
			name: "range blank index only value defined",
			src: dedent(`
                func foo() {
                    for _, v := range []string{"a"} {
                        _ = v
                    }
                }
            `),
			expected: []typedIdentifier{newBasicTI(IdentifierKindFuncVar, "v", "string", "foo")},
		},
		{
			name: "range blank value only index defined",
			src: dedent(`
                func foo() {
                    for i, _ := range []string{"a"} {
                        _ = i
                    }
                }
            `),
			expected: []typedIdentifier{newBasicTI(IdentifierKindFuncVar, "i", "int", "foo")},
		},
		{
			name: "range channel value only",
			src: dedent(`
                func foo(ch <-chan int) {
                    for v := range ch {
                        _ = v
                    }
                }
            `),
			expected: []typedIdentifier{newBasicTI(IdentifierKindFuncVar, "v", "int", "foo")},
		},
		{
			name: "range map key only",
			src: dedent(`
                func foo() {
                    for k := range map[string]int{"a":1} {
                        _ = k
                    }
                }
            `),
			expected: []typedIdentifier{newBasicTI(IdentifierKindFuncVar, "k", "string", "foo")},
		},
		{
			name: "range map key and value",
			src: dedent(`
                func foo() {
                    for k, v := range map[string]int{"a":1} {
                        _ = k
                        _ = v
                    }
                }
            `),
			expected: []typedIdentifier{newBasicTI(IdentifierKindFuncVar, "k", "string", "foo"), newBasicTI(IdentifierKindFuncVar, "v", "int", "foo")},
		},
		{
			name: "range with '=' assignment defines no new vars",
			src: dedent(`
                func foo() {
                    var i int
                    var v string
                    for i, v = range []string{"a"} {
                        _ = i
                        _ = v
                    }
                }
            `),
			expected:      []typedIdentifier{newBasicTI(IdentifierKindFuncVar, "i", "int", "foo"), newBasicTI(IdentifierKindFuncVar, "v", "string", "foo")},
			expectedCount: &two,
		},
		{
			name: "short var with blank identifier (second defined)",
			src: dedent(`
                func f() (int, bool) { return 1, true }
                func foo() {
                    _, b := f()
                    _ = b
                }
            `),
			expected: []typedIdentifier{newBasicTI(IdentifierKindFuncVar, "b", "bool", "foo")},
		},
		{
			name: "short var with blank identifier (first defined)",
			src: dedent(`
                func f() (int, bool) { return 1, true }
                func foo() {
                    a, _ := f()
                    _ = a
                }
            `),
			expected: []typedIdentifier{newBasicTI(IdentifierKindFuncVar, "a", "int", "foo")},
		},
		{
			name: "implicit var in func",
			src: dedent(`
				func bar() bool {return true}
				func foo() {
					a := bar()
					_ = a
				}
			`),
			expected: []typedIdentifier{newBasicTI(IdentifierKindFuncVar, "a", "bool", "foo")},
		},
		{
			name: "multi-var in func",
			src: dedent(`
				func foo() {
					var a, b int
					a = b
					_ = a
				}
			`),
			expected: []typedIdentifier{newBasicTI(IdentifierKindFuncVar, "a", "int", "foo"), newBasicTI(IdentifierKindFuncVar, "b", "int", "foo")},
		},
		{
			name: "multi-assign in func",
			src: dedent(`
				func bar() (bool, error) {return true, nil}
				func foo() {
					a, b := bar()
					_ = a
					_ = b
				}
			`),
			expected: []typedIdentifier{newBasicTI(IdentifierKindFuncVar, "a", "bool", "foo"), newBasicTI(IdentifierKindFuncVar, "b", "error", "foo")},
		},
		{
			name: "variable nested func",
			src: dedent(`
				func foo() int {
					f := func() int {
						var a int
						return a
					}
					return f()
				}
			`),
			expected: []typedIdentifier{
				newBasicTI(IdentifierKindFuncVar, "f", "func()", "foo"),
				newBasicTI(IdentifierKindFuncVar, "a", "int", "foo"),
			},
		},
		{
			name: "for var",
			src: dedent(`
				func foo() {
					for i, v := range []string{"a","b"} {
						_ = i
						_ = v
					}
				}
			`),
			expected: []typedIdentifier{newBasicTI(IdentifierKindFuncVar, "i", "int", "foo"), newBasicTI(IdentifierKindFuncVar, "v", "string", "foo")},
		},

		//
		// IdentifierKindFuncConst
		//
		{
			name: "const in function",
			src: dedent(`
				func foo() {
					const c = 3
				}
			`),
			expected: []typedIdentifier{newBasicTI(IdentifierKindFuncConst, "c", "untyped int", "foo")},
		},

		//
		// IdentifierKindFuncParam
		//
		{
			name: "function parameter",
			src: dedent(`
				func foo(x string) {}
			`),
			expected: []typedIdentifier{newBasicTI(IdentifierKindFuncParam, "x", "string", "foo")},
		},
		{
			name: "multiple function parameters",
			src: dedent(`
				func foo(x string, y int) {}
			`),
			expected: []typedIdentifier{newBasicTI(IdentifierKindFuncParam, "x", "string", "foo"), newBasicTI(IdentifierKindFuncParam, "y", "int", "foo")},
		},
		{
			name: "multiple function parameters, implicit type",
			src: dedent(`
				func foo(x, y string) {}
			`),
			expected: []typedIdentifier{newBasicTI(IdentifierKindFuncParam, "x", "string", "foo"), newBasicTI(IdentifierKindFuncParam, "y", "string", "foo")},
		},
		{
			name: "variadic param",
			src: dedent(`
				func foo(x ...string) {}
			`),
			expected: []typedIdentifier{
				{
					Kind:              IdentifierKindFuncParam,
					Identifier:        "x",
					RootType:          "string",
					CompleteType:      "[]string",
					IsNamedType:       false,
					IsSlice:           true,
					IsMap:             false,
					IsPtr:             false,
					SnippetIdentifier: "foo",
				},
			},
		},
		{
			name: "return function params",
			src: dedent(`
				func foo() (x int, err error) {return 0, nil}
			`),
			expected: []typedIdentifier{newBasicTI(IdentifierKindFuncParam, "x", "int", "foo"), newBasicTI(IdentifierKindFuncParam, "err", "error", "foo")},
		},

		//
		// IdentifierKindFuncReceiver
		//
		{
			name: "function receiver",
			src: dedent(`
				type R struct{}
				func (r *R) M() {}
			`),
			expected: []typedIdentifier{{
				Kind:              IdentifierKindFuncReceiver,
				Identifier:        "r",
				RootType:          "R",
				CompleteType:      "*R",
				IsNamedType:       true,
				IsSlice:           false,
				IsMap:             false,
				IsPtr:             true,
				SnippetIdentifier: "*R.M",
			}},
		},

		//
		// IdentifierKindField
		//
		{
			name: "struct field (type-level)",
			src: dedent(`
                type S struct{ F int }
            `),
			expected: []typedIdentifier{newBasicTI(IdentifierKindType, "S", "struct{...}", "S"), newBasicTI(IdentifierKindField, "F", "int", "S")},
		},
		{
			name: "struct in func",
			src: dedent(`
                func foo() {
                    type s struct{F int}
                }
            `),
			expected: []typedIdentifier{newBasicTI(IdentifierKindType, "s", "struct{...}", "foo"), newBasicTI(IdentifierKindField, "F", "int", "foo")},
		},
		{
			name: "nested struct in func",
			src: dedent(`
				func foo() {
					type s struct{
						F1 int
						F2 struct {
							F3 struct {
								F4 string
							}
						}
					}
				}
			`),
			expected: []typedIdentifier{
				newBasicTI(IdentifierKindField, "F1", "int", "foo"),
				newBasicTI(IdentifierKindField, "F2", "struct{...}", "foo"),
				newBasicTI(IdentifierKindField, "F3", "struct{...}", "foo"),
				newBasicTI(IdentifierKindField, "F4", "string", "foo"),
			},
		},

		//
		// IdentifierKindPkgVar
		//
		{
			name: "package-level var",
			src: dedent(`
				var v int
			`),
			expected: []typedIdentifier{newBasicTI(IdentifierKindPkgVar, "v", "int", "v")},
		},
		{
			name: "package-level var in block",
			src: dedent(`
				var (
					v int
					w string
				)
			`),
			expected: []typedIdentifier{newBasicTI(IdentifierKindPkgVar, "v", "int", "v"), newBasicTI(IdentifierKindPkgVar, "w", "string", "w")},
		},

		//
		// IdentifierKindPkgConst
		//
		{
			name: "package-level const",
			src: dedent(`
				const C = 3
			`),
			expected: []typedIdentifier{newBasicTI(IdentifierKindPkgConst, "C", "untyped int", "C")},
		},
		{
			name: "package-level const block with iota",
			src: dedent(`
				const (
					C int = iota
					D
				)
			`),
			expected: []typedIdentifier{newBasicTI(IdentifierKindPkgConst, "C", "int", "C"), newBasicTI(IdentifierKindPkgConst, "D", "int", "D")},
		},

		//
		// IdentifierKindType
		//
		{
			name: "type declaration",
			src: dedent(`
				type T int
			`),
			expected: []typedIdentifier{newBasicTI(IdentifierKindType, "T", "int", "T")},
		},
		{
			name: "type alias",
			src: dedent(`
				type T = int
			`),
			expected: []typedIdentifier{newBasicTI(IdentifierKindType, "T", "int", "T")},
		},

		//
		// Non-kind-specific
		//
		{
			name: "is slice",
			src: dedent(`
				func foo() {
					var x []*int
					_ = x
				}
			`),
			expected: []typedIdentifier{{
				Kind:              IdentifierKindFuncVar,
				Identifier:        "x",
				RootType:          "int",
				CompleteType:      "[]*int",
				IsNamedType:       false,
				IsSlice:           true,
				IsMap:             false,
				IsPtr:             false,
				SnippetIdentifier: "foo",
			}},
		},
		{
			name: "is ptr",
			src: dedent(`
				func foo() {
					var x *[]int
					_ = x
				}
			`),
			expected: []typedIdentifier{{
				Kind:              IdentifierKindFuncVar,
				Identifier:        "x",
				RootType:          "int",
				CompleteType:      "*[]int",
				IsNamedType:       false,
				IsSlice:           false,
				IsMap:             false,
				IsPtr:             true,
				SnippetIdentifier: "foo",
			}},
		},
		{
			name: "is map",
			src: dedent(`
				import "bytes"
				func foo() {
					var x map[int]*bytes.Buffer
					_ = x
				}
			`),
			expected: []typedIdentifier{{
				Kind:              IdentifierKindFuncVar,
				Identifier:        "x",
				RootType:          "map[int]*bytes.Buffer",
				CompleteType:      "map[int]*bytes.Buffer",
				IsNamedType:       false, // not named, even tho it involves a named type
				IsSlice:           false,
				IsMap:             true,
				IsPtr:             false,
				SnippetIdentifier: "foo",
			}},
		},
		{
			name: "init funcs",
			src: dedent(`
				func init() {
					var x int
					_ = x
				}
				func init() {
					var x int
					_ = x
				}
			`),
			expected: []typedIdentifier{newBasicTI(IdentifierKindFuncVar, "x", "int", "init:code.go:3:6"), newBasicTI(IdentifierKindFuncVar, "x", "int", "init:code.go:7:6")},
		},
		{
			name: "_ var - doesnt show up",
			src: dedent(`
				var _ int = 3
			`),
			expectedCount: &zero,
		},
		{
			name: "anonymous funcs",
			src: dedent(`
				func _() {
					var x = 3
					_ = x
				}
			`),
			expected: []typedIdentifier{newBasicTI(IdentifierKindFuncVar, "x", "int", "_:code.go:3:6")},
		},
		{
			name: "generics",
			src: dedent(`
				type A struct {}
				type B struct {}
				type streamable interface { A | B }
				type streamReader[T streamable] struct {
					foo int
				}
				func (s *streamReader[T]) Recv() (T, error) {
					var x T
					return x, nil
				}
			`),
			expected: []typedIdentifier{
				newBasicTI(IdentifierKindType, "streamable", "interface{...}", "streamable"),
				newBasicTI(IdentifierKindType, "streamReader", "struct{...}", "streamReader"),
				newBasicTI(IdentifierKindField, "foo", "int", "streamReader"),
				{
					Kind:              IdentifierKindFuncVar,
					Identifier:        "x",
					RootType:          "T",
					CompleteType:      "T",
					IsNamedType:       false,
					IsTypeParam:       true,
					SnippetIdentifier: "*streamReader.Recv",
				},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			gocodetesting.WithCode(t, tt.src, func(pkg *gocode.Package) {
				// Load type information for the temporary package.
				typeInfo, err := gotypes.LoadTypeInfoInto(pkg, false)
				require.NoError(t, err)

				file := pkg.Files["code.go"]
				require.NotNil(t, file)

				got, err := typedIdentifiersInFile(pkg, file, typeInfo)
				require.NoError(t, err)

				if tt.expectedCount != nil {
					assert.EqualValues(t, *tt.expectedCount, len(got))
					// fmt.Println("got:")
					// for _, g := range got {
					// 	fmt.Println(g)
					// }
				}

				// Zero out Expr and FileName before comparing.
				var gotNorm []typedIdentifier
				for _, id := range got {
					if id == nil {
						continue
					}
					v := *id
					v.Expr = nil
					v.FileName = ""
					gotNorm = append(gotNorm, v)
				}

				for _, want := range tt.expected {
					if !assert.Contains(t, gotNorm, want) {
						fmt.Println("gotNorm:")
						for _, g := range gotNorm {
							fmt.Println(g)
						}
					}
				}
			})
		})
	}
}
