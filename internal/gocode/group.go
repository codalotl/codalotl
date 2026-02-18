package gocode

import (
	"go/ast"
	"strings"
)

// groupFunctionsByType associates functions with their corresponding types. It categorizes functions based on their receiver and return types. Functions with no
// receiver may be associated with a type T if:
//  1. the function returns T or *T.
//  2. the function does not return any other type from the same package.
func groupFunctionsByType(exportedTypes []*TypeSnippet, funcs []*FuncSnippet) map[string][]*FuncSnippet {
	typeToMethods := map[string][]*FuncSnippet{}

	knownTypes := make(map[string]struct{})
	for _, t := range exportedTypes {
		for _, id := range t.Identifiers {
			knownTypes[id] = struct{}{}
		}
	}

	const noReceiver = "none"

	for _, f := range funcs {
		receiverType := f.IndirectedReceiverType()
		if f.ReceiverType == "" {
			// No receiver - may be associated with return type
			if returnType := getAssociableReturnType(f, knownTypes); returnType != "" {
				typeToMethods[returnType] = append(typeToMethods[returnType], f)
			} else {
				typeToMethods[noReceiver] = append(typeToMethods[noReceiver], f)
			}
		} else {
			// Function has exported receiver type
			if _, exists := knownTypes[receiverType]; exists {
				typeToMethods[receiverType] = append(typeToMethods[receiverType], f)
			} else {
				typeToMethods[noReceiver] = append(typeToMethods[noReceiver], f)
			}
		}
	}

	return typeToMethods
}

// getAssociableReturnType examines a function's return types and determines whether it can be associated with a specific type T. A function can be associated with
// type T if:
//  1. It returns T, *T, []T, or []*T.
//  2. It does not return any other type from the same package.
//
// Returns the type name if the conditions are met; otherwise returns an empty string. The return value may be "" or one of the keys in knownTypes.
func getAssociableReturnType(f *FuncSnippet, knownTypes map[string]struct{}) string {
	if f.decl == nil || f.decl.Type == nil || f.decl.Type.Results == nil {
		return ""
	}

	results := f.decl.Type.Results.List
	if len(results) == 0 {
		return ""
	}

	var candidateType string
	foundTypes := make(map[string]bool)

	for _, result := range results {
		typeName := getBaseTypeNameFromExpr(result.Type)
		if typeName == "" {
			continue
		}

		// Check if type is a known user-defined type (either directly or as a pointer)
		if _, exists := knownTypes[typeName]; exists {
			foundTypes[typeName] = true
			if candidateType == "" {
				candidateType = typeName
			}
		}
	}

	// If we found more than one type from the package, we can't associate
	if len(foundTypes) > 1 {
		return ""
	}

	// If we didn't find any type from the package, we can't associate
	if candidateType == "" {
		return ""
	}

	// Check if there are any other types that could be from the same package
	for _, result := range results {
		typeName := getBaseTypeNameFromExpr(result.Type)
		if typeName == "" {
			continue
		}

		// Skip if it's the candidate type we already found
		if typeName == candidateType {
			continue
		}

		// If baseType is from another package, or a builtin, it's fine.
		// Otherwise, if it's from this same package, we can't associate.
		if strings.Contains(typeName, ".") {
			continue
		} else if isBuiltInType(typeName) {
			continue
		} else {
			return ""
		}
	}

	return candidateType
}

// getBaseTypeNameFromExpr extracts the base type name from an ast.Expr, removing pointer indirection and slices. ex: `[]*Foo` -> "Foo".
func getBaseTypeNameFromExpr(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		if baseTypeName := getBaseTypeNameFromExpr(t.X); baseTypeName != "" {
			return baseTypeName
		}
	case *ast.SelectorExpr:
		// For types like io.Reader
		if x, ok := t.X.(*ast.Ident); ok {
			return x.Name + "." + t.Sel.Name
		}
	case *ast.ArrayType:
		if eltName := getBaseTypeNameFromExpr(t.Elt); eltName != "" {
			return eltName
		}
	}
	return ""
}

// isBuiltInType reports whether a type name is built-in (ex: int; error; []bool; *int).
//
// TODO: handle variants like []bool or *int.
func isBuiltInType(typeName string) bool {
	if typeName == "" {
		return false
	}

	// Built-in types that we know are not from the same package
	builtInTypes := map[string]bool{
		"bool":       true,
		"byte":       true,
		"complex64":  true,
		"complex128": true,
		"error":      true,
		"float32":    true,
		"float64":    true,
		"int":        true,
		"int8":       true,
		"int16":      true,
		"int32":      true,
		"int64":      true,
		"rune":       true,
		"string":     true,
		"uint":       true,
		"uint8":      true,
		"uint16":     true,
		"uint32":     true,
		"uint64":     true,
		"uintptr":    true,
	}

	return builtInTypes[typeName]
}
