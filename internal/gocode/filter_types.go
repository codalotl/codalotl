package gocode

import (
	"go/ast"
	"go/token"
)

// filterExportedTypes takes a type declaration and returns a cloned decl without unexported types or unexported fields or methods in structs or interfaces. If a struct or interface
// has elided members, it will contain `// contains filtered or unexported fields`.
func filterExportedTypes(genDecl *ast.GenDecl) *ast.GenDecl {
	if genDecl == nil {
		return genDecl
	}
	if genDecl.Tok != token.TYPE {
		panic("unexpected token type")
	}

	// Create a clone of the GenDecl
	filtered := &ast.GenDecl{
		Doc:    genDecl.Doc,
		TokPos: genDecl.TokPos,
		Tok:    genDecl.Tok,
		Lparen: genDecl.Lparen,
		Rparen: genDecl.Rparen,
	}

	// Process each type specification
	var filteredSpecs []ast.Spec
	for _, spec := range genDecl.Specs {
		if typeSpec, ok := spec.(*ast.TypeSpec); ok {
			// Only include exported types
			if ast.IsExported(typeSpec.Name.Name) {
				// Clone the type spec and filter its type
				filteredTypeSpec := &ast.TypeSpec{
					Doc:        typeSpec.Doc,
					Name:       typeSpec.Name,
					TypeParams: typeSpec.TypeParams,
					Assign:     typeSpec.Assign,
					Type:       filterType(typeSpec.Type),
					Comment:    typeSpec.Comment,
				}
				filteredSpecs = append(filteredSpecs, filteredTypeSpec)
			}
		}
	}

	filtered.Specs = filteredSpecs
	return filtered
}

// filterType filters a type expression, removing unexported struct fields and, for interfaces, unexported methods and embedded interfaces.
func filterType(typeExpr ast.Expr) ast.Expr {
	switch t := typeExpr.(type) {
	case *ast.StructType:
		return filterStruct(t)
	case *ast.InterfaceType:
		return filterInterface(t)
	default:
		// For other types (aliases, primitives, etc.), return as-is
		return typeExpr
	}
}

// filterStruct returns a copy of orig that retains only exported fields and embedded types. Unexported fields (and unexported embedded types) are removed. For fields that declare multiple
// names, only exported names are kept; their ast.Field is duplicated with the remaining names while preserving type, tag, and comments. The result preserves field order and positions,
// and sets Incomplete to true if anything was removed. orig is not mutated.
func filterStruct(orig *ast.StructType) *ast.StructType {
	if orig.Fields == nil { // nothing to filter
		return orig
	}
	out := &ast.StructType{
		Struct:     orig.Struct,
		Fields:     &ast.FieldList{Opening: orig.Fields.Opening, Closing: orig.Fields.Closing},
		Incomplete: orig.Incomplete,
	}

	var keep []*ast.Field
	skipped := false

	for _, fld := range orig.Fields.List {
		// Embedded field (fld.Names == nil)
		if len(fld.Names) == 0 {
			if isExportedEmbedded(fld.Type) {
				keep = append(keep, fld)
			} else {
				skipped = true
			}
			continue
		}

		// Named field list
		var names []*ast.Ident
		for _, n := range fld.Names {
			if ast.IsExported(n.Name) {
				names = append(names, n)
			}
		}
		if len(names) == 0 {
			skipped = true
			continue
		}
		if len(names) != len(fld.Names) {
			dup := *fld
			dup.Names = names
			keep = append(keep, &dup)
			skipped = true
		} else {
			keep = append(keep, fld)
		}
	}

	out.Fields.List = keep
	if orig.Fields != nil && skipped {
		out.Incomplete = true
	}

	return out
}

// filterInterface returns a copy of orig that retains only exported interface API. Exported methods and exported embedded interfaces are kept; unexported ones are removed. Type-set
// terms used by constraints (e.g., union/tilde expressions) are always preserved. The result preserves order and positions, and sets Incomplete to true if anything was removed. orig
// is not mutated.
func filterInterface(orig *ast.InterfaceType) *ast.InterfaceType {
	if orig.Methods == nil { // nothing to filter
		return orig
	}

	out := &ast.InterfaceType{
		Interface:  orig.Interface,
		Methods:    &ast.FieldList{Opening: orig.Methods.Opening, Closing: orig.Methods.Closing},
		Incomplete: orig.Incomplete,
	}

	var keep []*ast.Field
	skipped := false

	for _, m := range orig.Methods.List {
		// Embedded or explicit method?
		if len(m.Names) == 0 { // embedded interface or type constraint/term
			// Keep type constraint terms like "~int | ~float" (they appear as *ast.UnaryExpr or *ast.BinaryExpr
			// in the AST) regardless of whether the underlying identifier is exported. These are not
			// methods or embedded interfaces, but type terms used by parameterized (generic) code.
			switch m.Type.(type) {
			case *ast.BinaryExpr, *ast.UnaryExpr, *ast.ParenExpr:
				// Always keep – they represent type sets/union constraints.
				keep = append(keep, m)
				continue
			}

			if isExportedEmbedded(m.Type) {
				keep = append(keep, m)
			} else {
				skipped = true
			}
			continue
		}

		if ast.IsExported(m.Names[0].Name) {
			keep = append(keep, m)
		} else {
			skipped = true
		}
	}

	out.Methods.List = keep
	if orig.Methods != nil && skipped {
		out.Incomplete = true
	}

	return out
}

// isExportedEmbedded reports whether an embedded field or interface element is exported. The argument is an ast.Expr that names the type; peel off syntactic layers until reaching the
// identifier.
func isExportedEmbedded(expr ast.Expr) bool {
	for {
		switch e := expr.(type) {
		case *ast.StarExpr:
			expr = e.X
		case *ast.SelectorExpr:
			return ast.IsExported(e.Sel.Name)
		case *ast.Ident:
			return ast.IsExported(e.Name)
		default:
			// Other syntactic forms are treated as unexported.
			return false
		}
	}
}
