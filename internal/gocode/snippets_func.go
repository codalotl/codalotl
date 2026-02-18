package gocode

import (
	"go/ast"
	"go/token"
	"strings"
	"unicode"
)

// RANDOM IDEA:
// If a method exists in order to satisfy an interface, we don't really want to duplicate comments from interface and concrete method.
// The go stdlib tends to skip docs on these. See https://pkg.go.dev/sort#IntSlice
// But in godoc, i think this as a weakness: its unclear from *quickly* looking at a method that it's there for the interface.
// I wonder if we can address that somehow in our system:
// - as long as we can explicitely tie a concrete type to an interface somehow, the method is considered documented.
// - maybe IdentifierDocumentation indicates this somehow?
// - codalotl shouldn't try to document these (unless there's particular notes about a method that differ from interface version)
//
// Snippet interface implementation:
//

var _ Snippet = (*FuncSnippet)(nil) // FuncSnippet implements Snippet

// FuncSnippet holds the parsed documentation and signature for a top-level function or method found in a file. It provides both the raw bytes as they appear in
// the source (for rendering) and structured metadata (for analysis).
//
// Use Bytes to get the doc+signature snippet (no body), FullBytes to get the entire function including its body, HasExported to check export status, and Position
// to locate the declaration in the source.
type FuncSnippet struct {
	Name         string // function name, excluding receiver (ex: "NewSomething"; "doSomething")
	ReceiverType string // ex: "*Foo" or "Foo". For no receiver, ""
	Identifier   string // name for receiverless functions; "type.name" for receivers (ex: "*myType.doSomething")
	FileName     string // file name (no dirs) where the function was defined (ex: "foo.go")

	// the docs and func signature as they appear in source, up to but not including the opening "{" or the space before it. Shares buffer with File's Contents
	Snippet []byte

	// full comment above the function; includes "//" or "/**/"; always \n terminated
	Doc string

	// function signature as it appears in source, from "func" up to but not including the opening " {" (ex: "func (t *MyType) DoThing(a string) (int, error)")
	Sig string

	fileSet  *token.FileSet // fileSet used to parse decl
	decl     *ast.FuncDecl  // decl node from parsing file
	FullFunc []byte         // can be used to move the function elsewhere or examine it in its totality; just an index into f.Contents
}

// Implemention of Snippet interface.
func (f *FuncSnippet) IDs() []string {
	return []string{f.Identifier}
}

// Implemention of Snippet interface.
func (f *FuncSnippet) HasExported() bool {
	if !ast.IsExported(f.Name) {
		return false
	}

	// If there's a receiver type, check if it's exported too
	if indirected := f.IndirectedReceiverType(); indirected != "" {
		if !ast.IsExported(indirected) {
			return false
		}
	}

	return true
}

// Implemention of Snippet interface.
func (f *FuncSnippet) Test() bool {
	return strings.HasSuffix(f.FileName, "_test.go")
}

// Implemention of Snippet interface.
func (f *FuncSnippet) Bytes() []byte {
	return f.Snippet
}

// Implemention of Snippet interface.
func (f *FuncSnippet) PublicSnippet() ([]byte, error) {
	if !f.HasExported() {
		return nil, nil
	}
	return f.Snippet, nil
}

// Implemention of Snippet interface.
func (f *FuncSnippet) FullBytes() []byte {
	return f.FullFunc
}

// Implemention of Snippet interface.
func (f *FuncSnippet) Docs() []IdentifierDocumentation {
	if f.Doc == "" {
		return nil
	}

	return []IdentifierDocumentation{
		{
			Identifier: f.Identifier,
			Field:      "",
			Doc:        f.Doc,
		},
	}
}

// Implemention of Snippet interface.
func (f *FuncSnippet) MissingDocs() []IdentifierDocumentation {
	if f.Doc == "" {
		return []IdentifierDocumentation{
			{
				Identifier: f.Identifier,
				Field:      "",
				Doc:        "",
			},
		}
	}

	return nil
}

// Implemention of Snippet interface.
func (f *FuncSnippet) Position() token.Position {
	if f.decl.Doc != nil {
		return positionWithBaseFilename(f.fileSet.Position(f.decl.Doc.Pos()))
	}
	return positionWithBaseFilename(f.fileSet.Position(f.decl.Pos()))
}

//
// Extraction
//

// extractFuncSnippet extracts a FuncSnippet from an ast.FuncDecl.
func extractFuncSnippet(funcDecl *ast.FuncDecl, file *File) (*FuncSnippet, error) {
	fset := file.FileSet

	receiverType, name := GetReceiverFuncName(funcDecl)
	pos := fset.Position(funcDecl.Name.Pos())
	identifier := FuncIdentifier(receiverType, name, file.FileName, pos.Line, pos.Column)

	// Get function documentation
	doc := ""
	if funcDecl.Doc != nil {
		// Get the raw documentation from the file contents using position information
		docStart := fset.Position(funcDecl.Doc.Pos()).Offset
		docEnd := fset.Position(funcDecl.Doc.End()).Offset
		doc = ensureNewline(string(file.Contents[docStart:docEnd]))
	}

	// Get function signature - from the start of "func" to the opening brace
	funcStart := fset.Position(funcDecl.Pos()).Offset
	funcEnd := fset.Position(funcDecl.Type.End()).Offset
	sig := string(file.Contents[funcStart:funcEnd])
	sig = strings.TrimSpace(sig)

	// Extract snippet (docs + signature)
	var snippetStart int
	if funcDecl.Doc != nil {
		snippetStart = file.FileSet.Position(funcDecl.Doc.Pos()).Offset
	} else {
		snippetStart = funcStart
	}
	snippetEnd := funcEnd
	snippet := file.Contents[snippetStart:snippetEnd]

	// Extract the full function (including body) if it exists
	var fullFunc []byte
	if funcDecl.Body != nil {
		// Function has a body, extract from start of snippet to end of body
		fullFuncEnd := fset.Position(funcDecl.Body.End()).Offset
		fullFunc = file.Contents[snippetStart:fullFuncEnd]
	} else {
		// No body (e.g., interface method), use the same as snippet
		fullFunc = snippet
	}

	return &FuncSnippet{
		Name:         name,
		ReceiverType: receiverType,
		Identifier:   identifier,
		FileName:     file.FileName,
		Snippet:      snippet,
		Doc:          doc,
		Sig:          sig,
		FullFunc:     fullFunc,
		fileSet:      fset,
		decl:         funcDecl,
	}, nil
}

//
// Other
//

// IndirectedReceiverType returns the receiver type without the pointer prefix, if any. Ex: "*Foo" -> "Foo"; "Foo" -> "Foo"; "" -> "".
func (f *FuncSnippet) IndirectedReceiverType() string {
	return strings.TrimPrefix(f.ReceiverType, "*")
}

// IsTestFunc reports whether f describes a top-level Go testing function found in a _test.go file. It recognizes the following forms:
//   - Test: func TestXxx(t *testing.T) with Xxx starting with an uppercase letter and no return values.
//   - Benchmark: func BenchmarkXxx(b *testing.B) with Xxx starting with an uppercase letter and no return values.
//   - Fuzz: func FuzzXxx(f *testing.F) with Xxx starting with an uppercase letter and no return values.
//   - Example: func Example...() with no parameters and no return values.
func (f *FuncSnippet) IsTestFunc() bool {
	// Must be in a test file
	if !f.Test() {
		return false
	}

	// Must not have a receiver
	if f.ReceiverType != "" {
		return false
	}

	// Must have the AST declaration available
	if f.decl == nil {
		return false
	}

	name := f.Name

	// Test functions: func TestXxx(*testing.T)
	if strings.HasPrefix(name, "Test") && len(name) > 4 && unicode.IsUpper(rune(name[4])) {
		return hasTestingParam(f.decl, "T")
	}

	// Benchmark functions: func BenchmarkXxx(*testing.B)
	if strings.HasPrefix(name, "Benchmark") && len(name) > 9 && unicode.IsUpper(rune(name[9])) {
		return hasTestingParam(f.decl, "B")
	}

	// Example functions: func ExampleXxx() with no parameters and no return values
	if strings.HasPrefix(name, "Example") {
		if f.decl.Type == nil {
			return false
		}
		// Must not have parameters
		if f.decl.Type.Params != nil && len(f.decl.Type.Params.List) > 0 {
			return false
		}
		// Must not have return values
		if f.decl.Type.Results != nil && len(f.decl.Type.Results.List) > 0 {
			return false
		}
		return true
	}

	// Fuzz functions: func FuzzXxx(*testing.F)
	if strings.HasPrefix(name, "Fuzz") && len(name) > 4 && unicode.IsUpper(rune(name[4])) {
		return hasTestingParam(f.decl, "F")
	}

	return false
}

// hasTestingParam reports whether the function has exactly one parameter whose type is a pointer to testing.<testingType> (ex: *testing.T; *testing.B). It also
// requires that the function have no return values.
func hasTestingParam(decl *ast.FuncDecl, testingType string) bool {
	if decl.Type == nil || decl.Type.Params == nil {
		return false
	}

	// Test/Benchmark/Fuzz functions must not have return values
	if decl.Type.Results != nil && len(decl.Type.Results.List) > 0 {
		return false
	}

	params := decl.Type.Params.List

	// Must have exactly one parameter field
	if len(params) != 1 {
		return false
	}

	param := params[0]

	// Disallow grouped names like: t, t2 *testing.T (AST represents them as one field with multiple names)
	if len(param.Names) > 1 {
		return false
	}

	// Check if it's a pointer to testing.T/B/F
	starExpr, ok := param.Type.(*ast.StarExpr)
	if !ok {
		return false
	}

	// Check if it's a selector expression (testing.T)
	selExpr, ok := starExpr.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	// The X must be an identifier (the package name)
	pkgIdent, ok := selExpr.X.(*ast.Ident)
	if !ok {
		return false
	}

	// In the AST, if "testing" refers to the standard library package,
	// it will have Obj == nil. If it refers to a local type, Obj != nil.
	if pkgIdent.Obj != nil {
		// This means "testing" refers to something defined in this file/package,
		// not the standard library testing package
		return false
	}

	// Check package name is "testing"
	if pkgIdent.Name != "testing" {
		return false
	}

	// Check type name matches (T, B, or F)
	return selExpr.Sel.Name == testingType
}
