package gocode

import (
	"go/ast"
	"go/token"
)

// filterExportedTypes takes a type declaration and returns a cloned decl without unexported types or unexported fields or methods in structs or interfaces. If preserveMixed
// is true, mixed named fields are kept intact when at least one name is exported. If a struct has elided members, it will contain `// contains filtered or unexported fields`;
// if an interface has elided members, it will contain `// contains filtered or unexported methods`.
func filterExportedTypes(genDecl *ast.GenDecl, preserveMixed bool) *ast.GenDecl {
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
					Type:       filterType(typeSpec.Type, preserveMixed),
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
func filterType(typeExpr ast.Expr, preserveMixed bool) ast.Expr {
	return filterTypeWithIncomplete(typeExpr, preserveMixed, true)
}

func filterTypeWithIncomplete(typeExpr ast.Expr, preserveMixed bool, markIncomplete bool) ast.Expr {
	switch t := typeExpr.(type) {
	case *ast.StructType:
		if markIncomplete {
			return filterStruct(t, preserveMixed)
		}
		return filterStructWithIncomplete(t, preserveMixed, markIncomplete)
	case *ast.InterfaceType:
		if markIncomplete {
			return filterInterface(t, preserveMixed)
		}
		return filterInterfaceWithIncomplete(t, preserveMixed, markIncomplete)
	case *ast.StarExpr:
		dup := *t
		dup.X = filterTypeWithIncomplete(t.X, preserveMixed, markIncomplete)
		return &dup
	case *ast.ParenExpr:
		dup := *t
		dup.X = filterTypeWithIncomplete(t.X, preserveMixed, markIncomplete)
		return &dup
	case *ast.ArrayType:
		dup := *t
		dup.Elt = filterTypeWithIncomplete(t.Elt, preserveMixed, markIncomplete)
		return &dup
	case *ast.Ellipsis:
		dup := *t
		dup.Elt = filterTypeWithIncomplete(t.Elt, preserveMixed, markIncomplete)
		return &dup
	case *ast.MapType:
		dup := *t
		dup.Key = filterTypeWithIncomplete(t.Key, preserveMixed, markIncomplete)
		dup.Value = filterTypeWithIncomplete(t.Value, preserveMixed, markIncomplete)
		return &dup
	case *ast.ChanType:
		dup := *t
		dup.Value = filterTypeWithIncomplete(t.Value, preserveMixed, markIncomplete)
		return &dup
	case *ast.FuncType:
		dup := *t
		dup.Params = filterFieldListTypes(t.Params, preserveMixed, markIncomplete)
		dup.Results = filterFieldListTypes(t.Results, preserveMixed, markIncomplete)
		return &dup
	case *ast.IndexExpr:
		dup := *t
		dup.Index = filterTypeWithIncomplete(t.Index, preserveMixed, markIncomplete)
		return &dup
	case *ast.IndexListExpr:
		dup := *t
		dup.Indices = make([]ast.Expr, len(t.Indices))
		for i, index := range t.Indices {
			dup.Indices[i] = filterTypeWithIncomplete(index, preserveMixed, markIncomplete)
		}
		return &dup
	default:
		// For other types (aliases, primitives, etc.), return as-is
		return typeExpr
	}
}

func filterFieldListTypes(fields *ast.FieldList, preserveMixed bool, markIncomplete bool) *ast.FieldList {
	if fields == nil {
		return nil
	}

	dup := &ast.FieldList{
		Opening: fields.Opening,
		Closing: fields.Closing,
	}
	if len(fields.List) == 0 {
		return dup
	}

	dup.List = make([]*ast.Field, len(fields.List))
	for i, field := range fields.List {
		fieldDup := *field
		fieldDup.Type = filterTypeWithIncomplete(field.Type, preserveMixed, markIncomplete)
		dup.List[i] = &fieldDup
	}
	return dup
}

// filterStruct returns a copy of orig that retains only exported fields and embedded types. Unexported fields (and unexported embedded types) are removed. For fields
// that declare multiple names, preserveMixed controls whether mixed exported/unexported fields are kept intact or duplicated with only exported names. The result
// preserves field order and positions, and sets Incomplete to true if anything was removed. orig is not mutated.
func filterStruct(orig *ast.StructType, preserveMixed bool) *ast.StructType {
	return filterStructWithIncomplete(orig, preserveMixed, true)
}

func filterStructWithIncomplete(orig *ast.StructType, preserveMixed bool, markIncomplete bool) *ast.StructType {
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
				keep = append(keep, cloneFieldWithFilteredType(fld, preserveMixed))
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
		if preserveMixed {
			keep = append(keep, cloneFieldWithFilteredType(fld, preserveMixed))
			continue
		}
		if len(names) != len(fld.Names) {
			dup := *fld
			dup.Names = names
			dup.Type = filterType(fld.Type, preserveMixed)
			keep = append(keep, &dup)
			skipped = true
		} else {
			keep = append(keep, cloneFieldWithFilteredType(fld, preserveMixed))
		}
	}

	out.Fields.List = keep
	if orig.Fields != nil && skipped && markIncomplete {
		out.Incomplete = true
	}

	return out
}

func cloneFieldWithFilteredType(fld *ast.Field, preserveMixed bool) *ast.Field {
	dup := *fld
	dup.Type = filterType(fld.Type, preserveMixed)
	return &dup
}

// filterInterface returns a copy of orig that retains only exported interface API. Exported methods and exported embedded interfaces are kept; unexported ones are
// removed. Union, tilde, and parenthesized type-set terms used by constraints (ex: ~int | ~float) are preserved; bare unexported type terms are removed. The result
// preserves order and positions, and sets Incomplete to true if anything was removed. orig is not mutated.
func filterInterface(orig *ast.InterfaceType, preserveMixed bool) *ast.InterfaceType {
	return filterInterfaceWithIncomplete(orig, preserveMixed, true)
}

func filterInterfaceWithIncomplete(orig *ast.InterfaceType, preserveMixed bool, markIncomplete bool) *ast.InterfaceType {
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
				keep = append(keep, cloneFieldWithFilteredType(m, preserveMixed))
				continue
			}

			if isExportedEmbedded(m.Type) {
				keep = append(keep, cloneFieldWithFilteredType(m, preserveMixed))
			} else {
				skipped = true
			}
			continue
		}

		var names []*ast.Ident
		for _, n := range m.Names {
			if ast.IsExported(n.Name) {
				names = append(names, n)
			}
		}
		if len(names) == 0 {
			skipped = true
			continue
		}
		if preserveMixed || len(names) == len(m.Names) {
			keep = append(keep, cloneFieldWithFilteredType(m, preserveMixed))
		} else {
			dup := *m
			dup.Names = names
			dup.Type = filterType(m.Type, preserveMixed)
			keep = append(keep, &dup)
			skipped = true
		}
	}

	out.Methods.List = keep
	if orig.Methods != nil && skipped && markIncomplete {
		out.Incomplete = true
	}

	return out
}

// isExportedEmbedded reports whether an embedded field or interface element is exported. The argument is an ast.Expr that names the type; peel off syntactic layers
// until reaching the identifier.
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
