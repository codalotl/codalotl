package gocode

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
)

// PackageIdentifier is the identifier for the package-level documentation. Ideally, there is a single package doc comment in one file, but multiple files may include
// package comments. To handle this:
//   - PackageIdentifier returns the single package doc wherever it is, as long as there is only one.
//   - If there are multiple, PackageIdentifier returns the "main" comment (the one in doc.go, or if that is missing, in <packagename>.go).
//   - If there are multiple and none are in the canonical location, PackageIdentifier returns the lexicographically first by file name.
//   - In all cases, PackageIdentifierPerFile still works and may identify the same package doc as PackageIdentifier.
const PackageIdentifier = "package"

// PackageIdentifierPerFile returns an identifier for package-level documentation, scoped to a specific file (fileName should have no path). This can be used to
// differentiate package documentation snippets when there are multiple.
func PackageIdentifierPerFile(fileName string) string {
	return fmt.Sprintf("package:%s", fileName)
}

// IsPackageIdentifier reports whether id is a package identifier of any kind (overall or per file).
func IsPackageIdentifier(id string) bool {
	return id == PackageIdentifier || strings.HasPrefix(id, "package:")
}

// AnonymousIdentifier returns a formatted identifier for a "_" identifier. The col should be at the "_", not the start of the decl.
func AnonymousIdentifier(fileName string, line int, col int) string {
	return ambiguousIdentifier("_", fileName, line, col)
}

// Col is the column of the init identifier, not the func declaration.
func InitIdentifier(fileName string, line int, col int) string {
	return ambiguousIdentifier("init", fileName, line, col)
}

// The receiverType is either "T" or "*T".
func AnonymousMethodIdentifier(receiverType string, fileName string, line int, col int) string {
	return ambiguousIdentifier(receiverType+"._", fileName, line, col)
}

// ambiguousIdentifier formats a stable identifier for inherently ambiguous names by appending file name, line, and column in the form "identifier:file:line:col".
func ambiguousIdentifier(identifier string, fileName string, line int, col int) string {
	fileName = filepath.Base(fileName)
	return fmt.Sprintf("%s:%s:%d:%d", identifier, fileName, line, col)
}

// IsAnonymousIdentifier reports whether id is anonymous: either the plain "_" identifier, the form "_:file:line:col" from `var _ int`, or the form "*MyType._:file:line:col"
// from `func (m *MyType) _()`. It does not include init functions.
func IsAnonymousIdentifier(id string) bool {
	// Note: "_" check by itself is just for safety and robustness; this package doesn't currently use _ by itself.
	if id == "_" || strings.HasPrefix(id, "_:") {
		return true
	}

	return strings.Contains(id, "._:")
}

// IsInitIdentifier reports whether id refers to an init function identifier, including disambiguated forms produced by InitIdentifier (values with the "init:" prefix).
func IsInitIdentifier(id string) bool {
	// Note: "init" check by itself is just for safety and robustness; this package doesn't currently use init by itself.
	return id == "init" || strings.HasPrefix(id, "init:")
}

// IsAmbiguousIdentifier reports whether id is an anonymous identifier ("_") or an init identifier, even if disambiguated with "_:file:line:col" syntax. Ambiguous
// identifiers are anonymous identifiers and init() functions.
func IsAmbiguousIdentifier(id string) bool {
	return IsAnonymousIdentifier(id) || IsInitIdentifier(id)
}

// FuncIdentifier returns an identifier for a function declaration.
//
// Normal functions look like "M", "T.M", or "*T.M" (ex: "*myType.DoThing"). Anonymous and ambiguous functions encode file/position information from fset (ex: "_:file.go:12:7";
// "init:file.go:28:7").
func FuncIdentifier(receiverType string, funcName string, fileName string, line int, col int) string {
	if receiverType == "" {
		switch funcName {
		case "init":
			return InitIdentifier(fileName, line, col)
		case "_":
			return AnonymousIdentifier(fileName, line, col)
		default:
			return funcName
		}
	} else {
		if funcName == "_" {
			return AnonymousMethodIdentifier(receiverType, fileName, line, col)
		} else {
			return FuncIdentifierUse(receiverType, funcName)
		}
	}
}

// FuncIdentifierFromDecl builds an identifier for the given function or method declaration.
//
// Normal functions look like "M", "T.M", or "*T.M" (ex: "*myType.DoThing"). Anonymous and ambiguous functions encode file/position information from fset (ex: "_:file.go:12:7";
// "init:file.go:28:7").
func FuncIdentifierFromDecl(funcDecl *ast.FuncDecl, fset *token.FileSet) string {
	pos := fset.Position(funcDecl.Name.Pos())
	receiverType, name := GetReceiverFuncName(funcDecl)
	return FuncIdentifier(receiverType, name, pos.Filename, pos.Line, pos.Column)
}

// FuncIdentifierUse returns a function identifier (ex: "myType.myFunc"; for receiver-less: "myFunc"). These functions must be callable by (in other words, used
// by) other code, so it cannot be an anonymous func or init func.
func FuncIdentifierUse(receiverType string, funcName string) string {
	if receiverType == "" {
		return funcName
	}
	return fmt.Sprintf("%s.%s", receiverType, funcName)
}

// GetReceiverFuncName returns the receiver type and function name of a FuncDecl. If there is no receiver, ("", function name) is returned. Pointer receivers like
// "*MyType" are preserved. Generic receivers such as "func (t *MyType[T]) Foo()" or "func (r MyType[T, U]) Bar()" are normalized by stripping the type parameter
// list, resulting in "*MyType" or "MyType".
func GetReceiverFuncName(funcDecl *ast.FuncDecl) (string, string) {
	if funcDecl.Name == nil {
		panic("funcDecl.Name is nil in GetReceiverFuncName")
	}

	// Function name (always present)
	name := funcDecl.Name.Name

	// Determine receiver type, if any.
	var receiverType string
	if funcDecl.Recv != nil && len(funcDecl.Recv.List) > 0 {
		receiverType = extractReceiverType(funcDecl.Recv.List[0].Type)
	}

	return receiverType, name
}

// DeparenthesizeIdentifier normalizes identifiers like `(*SomeType).SomeMethod` to `*SomeType.SomeMethod`. It also strips any generic type argument lists from the
// receiver side (ex: `(*SomeType[T]).SomeMethod` -> `*SomeType.SomeMethod`, `SomeType[T].SomeMethod` -> `SomeType.SomeMethod`). If ident cannot be parsed or any
// errors are encountered while processing it, ident is returned unchanged.
//
// Normally, using this method isn't needed. You should NOT need it on a regular basis. However, if some LLM insists on using the parenthesized identifier format
// despite prompting, you can pass it through this method.
func DeparenthesizeIdentifier(ident string) string {
	receiverExpr, rest, ok := splitParenthesizedOrGenericReceiver(ident)
	if !ok {
		return ident
	}

	// rest is the selector name (or ambiguous suffix, like "_:file.go:12:7") after the receiver.
	if rest == "" {
		return ident
	}

	expr, err := parser.ParseExpr(receiverExpr)
	if err != nil {
		return ident
	}

	receiverType := extractReceiverType(expr)
	if receiverType == "" {
		return ident
	}

	return receiverType + "." + rest
}

func splitParenthesizedOrGenericReceiver(ident string) (receiverExpr string, rest string, ok bool) {
	// Parenthesized selector form: "(*T).M", "(T).M", including nested parens like "((*T)).M".
	// We intentionally split on the first balanced ")." boundary, which avoids ambiguity with dots
	// that can appear later (ex: file.go in ambiguous identifiers).
	if strings.HasPrefix(ident, "(") {
		depth := 0
		for i := 0; i < len(ident); i++ {
			switch ident[i] {
			case '(':
				depth++
			case ')':
				depth--
				if depth < 0 {
					return "", "", false
				}
				if depth == 0 {
					// We found the matching ')' for the initial '('.
					if i+1 < len(ident) && ident[i+1] == '.' {
						if i+2 >= len(ident) {
							return "", "", false
						}
						return ident[1:i], ident[i+2:], true
					}
				}
			}
		}
		return "", "", false
	}

	// Generic receiver form without parens: "MyType[T].M" or "*pkg.MyType[T, U].M".
	// Only normalize when it looks like a method identifier (final segment is an identifier),
	// and the receiver side contains type arguments.
	lastDot := strings.LastIndex(ident, ".")
	if lastDot == -1 || lastDot == len(ident)-1 {
		return "", "", false
	}

	receiver := ident[:lastDot]
	selector := ident[lastDot+1:]
	if !token.IsIdentifier(selector) {
		return "", "", false
	}
	if !strings.Contains(receiver, "[") {
		return "", "", false
	}

	return receiver, selector, true
}

// extractReceiverType converts an AST expression describing a method receiver type into the canonical string representation used by this package. It preserves leading
// pointer stars and selector expressions but strips any generic type parameter lists, as these are not part of the identifier. Examples:
//
//	*MyType[T]     -> *MyType
//	MyType[T, U]   -> MyType
//	pkg.MyType[T]  -> pkg.MyType
func extractReceiverType(expr ast.Expr) string {
	switch v := expr.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.StarExpr:
		base := extractReceiverType(v.X)
		if base == "" {
			return ""
		}
		return "*" + base
	case *ast.ParenExpr:
		return extractReceiverType(v.X)
	case *ast.IndexExpr: // Go <=1.20 generics (single type param or index expression)
		return extractReceiverType(v.X)
	case *ast.IndexListExpr: // Go 1.21+: multiple type params
		return extractReceiverType(v.X)
	case *ast.SelectorExpr:
		pkg := extractReceiverType(v.X)
		if pkg == "" {
			return v.Sel.Name
		}
		return pkg + "." + v.Sel.Name
	default:
		return ""
	}
}
