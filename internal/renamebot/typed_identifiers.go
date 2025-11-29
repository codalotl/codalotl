package renamebot

import (
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gotypes"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
)

type IdentifierKind string

const (
	IdentifierKindFuncVar      IdentifierKind = "func_var"      // variable in a func. ex: `var foo int` or `bar := f()`
	IdentifierKindFuncConst    IdentifierKind = "func_const"    // const in a func. ex: `const foo = 3`
	IdentifierKindFuncParam    IdentifierKind = "func_param"    // function parameter (input OR output). does not include receiver.
	IdentifierKindFuncReceiver IdentifierKind = "func_receiver" // function receiver. ex: `func (r *R) F() {}` defines receiver "r".
	IdentifierKindField        IdentifierKind = "field"         // all fields (inside or outside of func). ex: `struct { F int }` defines "F".
	IdentifierKindPkgVar       IdentifierKind = "pkg_var"       // package-level var
	IdentifierKindPkgConst     IdentifierKind = "pkg_const"     // package-level func
	IdentifierKindType         IdentifierKind = "type"          // type (inside or outside of func)
	// NOTE: could add function/method names; interface methods
	// other identifiers that we probably won't add: goto labels; import aliases; package names; type parameters
)

type typedIdentifier struct {
	Kind       IdentifierKind
	Identifier string // name. ex: "foo" for "var foo int".
	// root type string, stripping off all indirections (ex: "myType" for "[]*myType").
	// Unnamed structs or interfaces are just "struct{...}" or "interface{...}" (literally, with actual triple dots).
	// Function types ar just "func()" regardless of the shape of the function.
	// Root type for maps is just the map (ex: "map[int]myType").
	// If the type is defined in the same package, it must not have a package selector; if it's via import, it must (ex: "myType" vs "otherpkg.TheirType").
	// TODO: what if type is imported with "."?
	RootType     string
	CompleteType string // raw type string, including all indirections. ex: "[]*myType"

	IsNamedType bool // true if the root type contains a non-standard type. `int` and `error` are built-in, whereas `myType` is named. Note that std-lib types like bytes.Buffer are still named.
	IsSlice     bool // true if the type is a "slice of X" (ex: `[]int`). Note that something like `*[]int` is NOT a slice.
	IsMap       bool // true if the type is a map
	IsPtr       bool // true if the type is pointer to something.
	IsTypeParam bool // true if the type is a type parameter (ex: `var x T`, where T is parametric with it's container)

	Expr     ast.Expr // expression for the named identifier.
	FileName string   // file name (no directory) where the identifier was found.

	// IDEA: IsMasked bool // true if the identifier is masked, or masks any other identifier, of the same name at it's scope

	// The gocode snipet identifier where this named identifier occured.
	// If the identifer was in a func (including param/receiever), this is the function id (ex: "myFunc" or "*myType.MyFunc").
	// If the identifier was in an unnamed func at the package level, this is the snippet ID of where it occurred (ex: `var f = func() {... }` would be "f").
	SnippetIdentifier string
}

// if !tests, no testing identifiers. If tests, we ONLY include testing identifiers.
// note that pkg might be a _test package, in which case tests must be true.
// In addition to the identifiers, an updated package is also returned, since it needs to be reparsed with type info.
func typedIdentifiersInPackage(pkg *gocode.Package, tests bool) ([]*typedIdentifier, *gocode.Package, error) {
	// Validate: if pkg is an external test package, we must be collecting test identifiers
	if pkg.IsTestPackage() && !tests {
		return nil, nil, fmt.Errorf("typedIdentifiersInPackage: pkg is a _test package; tests must be true")
	}

	// Load type info; for test collection we need test variants included.
	includeTests := tests || pkg.IsTestPackage()
	typeInfo, err := gotypes.LoadTypeInfoInto(pkg, includeTests)
	if err != nil {
		return nil, nil, err
	}

	// Collect identifiers file-by-file based on whether the file is a test file.
	var out []*typedIdentifier
	for _, f := range pkg.Files {
		if f.IsTest != tests {
			continue
		}
		ids, err := typedIdentifiersInFile(pkg, f, typeInfo)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, ids...)
	}

	return out, pkg, nil
}

func typedIdentifiersInFile(pkg *gocode.Package, file *gocode.File, typeInfo *gotypes.TypeInfo) ([]*typedIdentifier, error) {
	info := &typeInfo.Info

	// Qualifier that omits same‑package prefixes and uses full import path for others.
	qual := func(p *types.Package) string {
		if p == nil {
			return ""
		}
		if p.Path() == pkg.ImportPath {
			return ""
		}
		return p.Path()
	}

	// Helpers to compute type strings and flags.
	typeStrings := func(t types.Type) (complete, root string, isNamed, isSlice, isMap, isPtr bool, isTypeParam bool) {
		if t == nil {
			return "", "", false, false, false, false, false
		}

		// Build complete type string, collapsing unnamed struct/interface to literal ellipsis forms.
		var collapse func(types.Type) string
		collapse = func(u types.Type) string {
			switch v := u.(type) {
			case *types.Struct:
				return "struct{...}"
			case *types.Interface:
				return "interface{...}"
			case *types.Signature:
				// Normalize all function types to generic func() regardless of parameters/returns.
				return "func()"
			case *types.Pointer:
				return "*" + collapse(v.Elem())
			case *types.Slice:
				return "[]" + collapse(v.Elem())
			default:
				return types.TypeString(u, qual)
			}
		}
		complete = collapse(t)

		// Top-level kind flags only: do not propagate pointer/slice flags from nested element types.
		switch t.(type) {
		case *types.Pointer:
			isPtr = true
		case *types.Slice:
			isSlice = true
		case *types.Map:
			isMap = true
		}

		// Derive root type by peeling pointers and slices; keep maps intact in root string.
		rt := t
		for {
			switch u := rt.(type) {
			case *types.Pointer:
				rt = u.Elem()
				continue
			case *types.Slice:
				rt = u.Elem()
				continue
			case *types.Map:
				// For maps, root is the map itself (even under pointer/slice) using collapsed key/elem.
				root = "map[" + collapse(u.Key()) + "]" + collapse(u.Elem())
			}
			break
		}
		if root == "" {
			root = types.TypeString(rt, qual)
		}

		// Collapse unnamed struct/interface root types to literal ellipsis form.
		switch rt.(type) {
		case *types.Struct:
			root = "struct{...}"
		case *types.Interface:
			root = "interface{...}"
		case *types.Signature:
			// Normalize all function root types to func()
			root = "func()"
		case *types.TypeParam:
			isTypeParam = true
		}

		// Named if the root is a named type that is not predeclared (built‑in).
		if n, ok := rt.(*types.Named); ok {
			// Predeclared types (like "error", "byte", "rune") have no package; treat as not named.
			if obj := n.Obj(); obj != nil && obj.Pkg() != nil {
				isNamed = true
			} else {
				isNamed = false
			}
		} else {
			isNamed = false
		}

		return
	}

	// Build snippet identifiers for functions/methods using gocode helpers.
	funcSnippet := func(fd *ast.FuncDecl) string {
		return gocode.FuncIdentifierFromDecl(fd, file.FileSet)
	}

	// Walk top‑level declarations explicitly to distinguish package vs function scope.
	var out []*typedIdentifier

	for _, decl := range file.AST.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			switch d.Tok {
			case token.TYPE:
				for _, s := range d.Specs {
					ts, ok := s.(*ast.TypeSpec)
					if !ok || ts.Name == nil {
						continue
					}
					// Type declaration itself
					var t types.Type
					if tv, ok := info.Types[ts.Type]; ok {
						t = tv.Type
					}
					complete, root, isNamed, isSlice, isMap, isPtr, isTP := typeStrings(t)
					out = append(out, &typedIdentifier{
						Kind:              IdentifierKindType,
						Identifier:        ts.Name.Name,
						RootType:          root,
						CompleteType:      complete,
						IsNamedType:       isNamed,
						IsSlice:           isSlice,
						IsMap:             isMap,
						IsPtr:             isPtr,
						IsTypeParam:       isTP,
						Expr:              ts.Name, // name expression
						FileName:          file.FileName,
						SnippetIdentifier: ts.Name.Name,
					})

					// Fields inside struct type declarations
					if st, ok := ts.Type.(*ast.StructType); ok && st.Fields != nil {
						for _, f := range st.Fields.List {
							for _, nm := range f.Names {
								if nm == nil || nm.Name == "_" {
									continue
								}
								// Field object/type
								var ft types.Type
								if obj := info.Defs[nm]; obj != nil {
									ft = obj.Type()
								} else if tv, ok := info.Types[f.Type]; ok {
									ft = tv.Type
								}
								c, r, n, sl, mp, pt, tp := typeStrings(ft)
								out = append(out, &typedIdentifier{
									Kind:              IdentifierKindField,
									Identifier:        nm.Name,
									RootType:          r,
									CompleteType:      c,
									IsNamedType:       n,
									IsSlice:           sl,
									IsMap:             mp,
									IsPtr:             pt,
									IsTypeParam:       tp,
									Expr:              nm,
									FileName:          file.FileName,
									SnippetIdentifier: ts.Name.Name,
								})
							}
						}
					}
				}
			case token.VAR, token.CONST:
				kind := IdentifierKindPkgVar
				if d.Tok == token.CONST {
					kind = IdentifierKindPkgConst
				}
				for _, s := range d.Specs {
					vs, ok := s.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, nm := range vs.Names {
						if nm == nil || nm.Name == "_" {
							continue
						}
						var vt types.Type
						if obj := info.Defs[nm]; obj != nil {
							vt = obj.Type()
						} else if vs.Type != nil {
							if tv, ok := info.Types[vs.Type]; ok {
								vt = tv.Type
							}
						}
						c, r, n, sl, mp, pt, tp := typeStrings(vt)
						out = append(out, &typedIdentifier{
							Kind:              kind,
							Identifier:        nm.Name,
							RootType:          r,
							CompleteType:      c,
							IsNamedType:       n,
							IsSlice:           sl,
							IsMap:             mp,
							IsPtr:             pt,
							IsTypeParam:       tp,
							Expr:              nm,
							FileName:          file.FileName,
							SnippetIdentifier: nm.Name,
						})
					}
				}
			}
		case *ast.FuncDecl:
			snippet := funcSnippet(d)

			// Receiver(s)
			if d.Recv != nil {
				for _, fld := range d.Recv.List {
					for _, nm := range fld.Names {
						if nm == nil || nm.Name == "_" {
							continue
						}
						var rt types.Type
						if obj := info.Defs[nm]; obj != nil {
							rt = obj.Type()
						} else if tv, ok := info.Types[fld.Type]; ok {
							rt = tv.Type
						}
						c, r, n, sl, mp, pt, tp := typeStrings(rt)
						out = append(out, &typedIdentifier{
							Kind:              IdentifierKindFuncReceiver,
							Identifier:        nm.Name,
							RootType:          r,
							CompleteType:      c,
							IsNamedType:       n,
							IsSlice:           sl,
							IsMap:             mp,
							IsPtr:             pt,
							IsTypeParam:       tp,
							Expr:              nm,
							FileName:          file.FileName,
							SnippetIdentifier: snippet,
						})
					}
				}
			}

			// Params
			if d.Type.Params != nil {
				for _, fld := range d.Type.Params.List {
					for _, nm := range fld.Names {
						if nm == nil || nm.Name == "_" {
							continue
						}
						var pt types.Type
						if obj := info.Defs[nm]; obj != nil {
							pt = obj.Type()
						} else if tv, ok := info.Types[fld.Type]; ok {
							pt = tv.Type
						}
						c, r, n, sl, mp, pptr, tp := typeStrings(pt)
						out = append(out, &typedIdentifier{
							Kind:              IdentifierKindFuncParam,
							Identifier:        nm.Name,
							RootType:          r,
							CompleteType:      c,
							IsNamedType:       n,
							IsSlice:           sl,
							IsMap:             mp,
							IsPtr:             pptr,
							IsTypeParam:       tp,
							Expr:              nm,
							FileName:          file.FileName,
							SnippetIdentifier: snippet,
						})
					}
				}
			}

			// Named result parameters
			if d.Type.Results != nil {
				for _, fld := range d.Type.Results.List {
					for _, nm := range fld.Names { // only named results have Names
						if nm == nil || nm.Name == "_" {
							continue
						}
						var rt types.Type
						if obj := info.Defs[nm]; obj != nil {
							rt = obj.Type()
						} else if tv, ok := info.Types[fld.Type]; ok {
							rt = tv.Type
						}
						c, r, n, sl, mp, pptr, tp := typeStrings(rt)
						out = append(out, &typedIdentifier{
							Kind:              IdentifierKindFuncParam,
							Identifier:        nm.Name,
							RootType:          r,
							CompleteType:      c,
							IsNamedType:       n,
							IsSlice:           sl,
							IsMap:             mp,
							IsPtr:             pptr,
							IsTypeParam:       tp,
							Expr:              nm,
							FileName:          file.FileName,
							SnippetIdentifier: snippet,
						})
					}
				}
			}

			// Local const/var declarations in function body
			if d.Body != nil {
				ast.Inspect(d.Body, func(n ast.Node) bool {
					ds, ok := n.(*ast.DeclStmt)
					if !ok {
						// Handle range statements with short declarations: for i, v := range ...
						if rs, ok := n.(*ast.RangeStmt); ok && rs.Tok == token.DEFINE {
							processIdent := func(ident *ast.Ident) {
								if ident == nil || ident.Name == "_" {
									return
								}
								// Only include identifiers that are newly declared in this short var decl.
								obj := info.Defs[ident]
								if obj == nil {
									return
								}
								vt := obj.Type()
								c, r, n, sl, mp, pt, tp := typeStrings(vt)
								out = append(out, &typedIdentifier{
									Kind:              IdentifierKindFuncVar,
									Identifier:        ident.Name,
									RootType:          r,
									CompleteType:      c,
									IsNamedType:       n,
									IsSlice:           sl,
									IsMap:             mp,
									IsPtr:             pt,
									IsTypeParam:       tp,
									Expr:              ident,
									FileName:          file.FileName,
									SnippetIdentifier: snippet,
								})
							}
							if id, _ := rs.Key.(*ast.Ident); id != nil {
								processIdent(id)
							}
							if id, _ := rs.Value.(*ast.Ident); id != nil {
								processIdent(id)
							}
							return true
						}
						// Handle short variable declarations: a := expr
						if as, ok := n.(*ast.AssignStmt); ok && as.Tok == token.DEFINE {
							for _, lhs := range as.Lhs {
								ident, ok := lhs.(*ast.Ident)
								if !ok || ident == nil || ident.Name == "_" {
									continue
								}
								// Only include identifiers that are newly declared in this short var decl.
								obj := info.Defs[ident]
								if obj == nil {
									continue
								}
								vt := obj.Type()
								c, r, n, sl, mp, pt, tp := typeStrings(vt)
								out = append(out, &typedIdentifier{
									Kind:              IdentifierKindFuncVar,
									Identifier:        ident.Name,
									RootType:          r,
									CompleteType:      c,
									IsNamedType:       n,
									IsSlice:           sl,
									IsMap:             mp,
									IsPtr:             pt,
									IsTypeParam:       tp,
									Expr:              ident,
									FileName:          file.FileName,
									SnippetIdentifier: snippet,
								})
							}
							return true
						}
						return true
					}
					gen, ok := ds.Decl.(*ast.GenDecl)
					if !ok {
						return true
					}
					switch gen.Tok {
					case token.VAR, token.CONST:
						var kind IdentifierKind
						if gen.Tok == token.VAR {
							kind = IdentifierKindFuncVar
						} else {
							kind = IdentifierKindFuncConst
						}
						for _, s := range gen.Specs {
							vs, ok := s.(*ast.ValueSpec)
							if !ok {
								continue
							}
							for _, nm := range vs.Names {
								if nm == nil || nm.Name == "_" {
									continue
								}
								var vt types.Type
								if obj := info.Defs[nm]; obj != nil {
									vt = obj.Type()
								} else if vs.Type != nil {
									if tv, ok := info.Types[vs.Type]; ok {
										vt = tv.Type
									}
								}
								c, r, n, sl, mp, pt, tp := typeStrings(vt)
								out = append(out, &typedIdentifier{
									Kind:              kind,
									Identifier:        nm.Name,
									RootType:          r,
									CompleteType:      c,
									IsNamedType:       n,
									IsSlice:           sl,
									IsMap:             mp,
									IsPtr:             pt,
									IsTypeParam:       tp,
									Expr:              nm,
									FileName:          file.FileName,
									SnippetIdentifier: snippet,
								})
							}
						}
					case token.TYPE:
						for _, s := range gen.Specs {
							ts, ok := s.(*ast.TypeSpec)
							if !ok || ts.Name == nil {
								continue
							}
							var t types.Type
							if tv, ok := info.Types[ts.Type]; ok {
								t = tv.Type
							}
							c, r, n, sl, mp, pt, tp := typeStrings(t)
							out = append(out, &typedIdentifier{
								Kind:              IdentifierKindType,
								Identifier:        ts.Name.Name,
								RootType:          r,
								CompleteType:      c,
								IsNamedType:       n,
								IsSlice:           sl,
								IsMap:             mp,
								IsPtr:             pt,
								IsTypeParam:       tp,
								Expr:              ts.Name,
								FileName:          file.FileName,
								SnippetIdentifier: snippet,
							})
							// capture fields inside local struct type declarations, recursively
							if st, ok := ts.Type.(*ast.StructType); ok && st.Fields != nil {
								var walk func(node *ast.StructType)
								walk = func(node *ast.StructType) {
									for _, f := range node.Fields.List {
										for _, nm := range f.Names {
											if nm == nil || nm.Name == "_" {
												continue
											}
											var ft types.Type
											if obj := info.Defs[nm]; obj != nil {
												ft = obj.Type()
											} else if tv, ok := info.Types[f.Type]; ok {
												ft = tv.Type
											}
											fc, fr, fn, fsl, fmp, fpt, ftp := typeStrings(ft)
											out = append(out, &typedIdentifier{
												Kind:              IdentifierKindField,
												Identifier:        nm.Name,
												RootType:          fr,
												CompleteType:      fc,
												IsNamedType:       fn,
												IsSlice:           fsl,
												IsMap:             fmp,
												IsPtr:             fpt,
												IsTypeParam:       ftp,
												Expr:              nm,
												FileName:          file.FileName,
												SnippetIdentifier: snippet,
											})
											if nested, ok := f.Type.(*ast.StructType); ok {
												walk(nested)
											}
										}
									}
								}
								walk(st)
							}
						}
					default:
						return true
					}
					return true
				})
			}
		}
	}

	return out, nil
}
