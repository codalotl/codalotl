package updatedocs

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// stripBackticks takes code with optional wrapping triple backticks and returns the code without wrapping backticks.
//
// An error is returned when:
//   - the snippet starts with backticks but the opener is not exactly ```.
//   - there is no newline immediately after the opening backticks.
//   - a language is specified but is not "go".
//   - the fenced block is empty (for example, "```go\n```")
//   - a matching closing ``` delimiter is missing.
func stripBackticks(snippet string) (string, error) {
	trimmed := strings.TrimSpace(snippet)

	// Fast path – no wrapper.
	if !strings.HasPrefix(trimmed, "`") {
		return snippet, nil
	}

	// Opener must be exactly ```
	if !strings.HasPrefix(trimmed, "```") {
		return "", errors.New("snippet starts with backticks but not exactly three")
	}

	firstNL := strings.IndexByte(trimmed, '\n')
	if firstNL == -1 {
		return "", errors.New("missing newline after opening backticks")
	}
	opener := trimmed[:firstNL]           // "```" or "```go"
	lang := strings.TrimSpace(opener[3:]) // anything after the three back-ticks
	if lang != "" && lang != "go" {
		return "", fmt.Errorf("unsupported language: %s", lang)
	}

	lastNL := strings.LastIndexByte(trimmed, '\n')
	if lastNL == -1 || lastNL == firstNL {
		return "", errors.New("missing closing backticks")
	}
	closer := strings.TrimSpace(trimmed[lastNL+1:])
	if closer != "```" {
		return "", errors.New("closing delimiter must be exactly three backticks")
	}

	code := trimmed[firstNL+1 : lastNL] // body between the two newline markers

	// Ensure exactly one trailing newline.
	if !strings.HasSuffix(code, "\n") {
		code += "\n"
	}
	return code, nil
}

// snippetKind describes the semantic category of a validated snippet.
type snippetKind int

const (
	snippetKindUnknown snippetKind = iota
	snippetKindPackageDoc
	snippetKindFunc
	snippetKindType
	snippetKindVar
	snippetKindConst
)

func (k snippetKind) String() string {
	switch k {
	case snippetKindPackageDoc:
		return "packageDoc"
	case snippetKindFunc:
		return "func"
	case snippetKindType:
		return "type"
	case snippetKindVar:
		return "var"
	case snippetKindConst:
		return "const"
	default:
		return "unknown"
	}
}

type rejectionError struct {
	error
}

// parseValidateSnippet parses *snippet* as if it were a Go source file and returns the parsed *ast.File, the token.FileSet used during parsing, plus the snippet's
// kind if it satisfies the UpdateDocumentation rules. Otherwise it returns an error.
//
// parseValidateSnippet does NOT know about the target source file and so cannot validate that it can be safely matched to a source file. It only validates that
// the snippet could be valid for *some* source file in a package with pkgName.
func parseValidateSnippet(pkgName, snippet string, options Options) (*ast.File, *token.FileSet, snippetKind, error) {
	fset := token.NewFileSet()

	var neededPackageInjected bool

	// Start by parsing the file:
	file, err := parser.ParseFile(fset, "snippet.go", snippet, parser.ParseComments)

	// Auto-fix some errors:
	if err != nil {
		if strings.Contains(err.Error(), "expected 'package'") {
			neededPackageInjected = true

			// Parse again with package injected:
			snippet = "package " + pkgName + "\n" + snippet
			fset = token.NewFileSet() // reset the fileSet, because the failed parse still causes offsets to increase.
			file, err = parser.ParseFile(fset, "snippet.go", snippet, parser.ParseComments)
		}

		if err != nil && strings.Contains(err.Error(), "expected '}', found 'EOF'") {
			snippet = strings.TrimRight(snippet, " \n\r\t") + "}\n"
			fset = token.NewFileSet() // reset the fileSet, because the failed parse still causes offsets to increase.
			file, err = parser.ParseFile(fset, "snippet.go", snippet, parser.ParseComments)
		}

		if err != nil {
			return nil, nil, snippetKindUnknown, err
		}
	}

	// Make sure the package name matches the package:
	if !neededPackageInjected {
		if file.Name == nil {
			// Actually unreachable – parser would have errored – but keep for clarity.
			return nil, nil, snippetKindUnknown, errors.New("snippet missing package clause")
		}
		if file.Name.Name != pkgName {
			return nil, nil, snippetKindUnknown, errors.New("package name mismatch")
		}
	}

	hasDecls := len(file.Decls) > 0
	hasDocComment := hasPackageDocComment(file)

	// A snippet can't document both the package and have decls:
	if hasDocComment && hasDecls {
		return nil, nil, snippetKindUnknown, errors.New("package doc comment snippet may not contain other declarations")
	}

	var kind snippetKind

	// Package doc only snippet
	if !hasDecls {
		if !hasDocComment {
			return nil, nil, snippetKindUnknown, errors.New("snippet with only package clause must include a package doc comment")
		}
		kind = snippetKindPackageDoc
	} else {
		kind, err = classifyAndValidateDecls(file)
		if err != nil {
			return nil, nil, snippetKindUnknown, err
		}
	}

	// Convert EOL <-> Doc comments based on policies. NOTE: this invalidates offsets.
	if options.Reflow {
		enforceEOLVsDocInAST(file, fset, options)
	}

	// Convert function EOL comments to doc comments. NOTE: this invalidates offsets.
	if kind == snippetKindFunc {
		if err := convertFunctionDeclEOLComments(file, fset); err != nil {
			return nil, nil, snippetKindUnknown, err
		}
	}

	// Reflow all doc comments. NOTE: this invalidates offsets.
	if options.Reflow {
		reflowDocCommentsInAST(file, options)
	}

	return file, fset, kind, nil
}

// hasPackageDocComment reports whether f contains a comment group that ends *before* the `package` keyword.
func hasPackageDocComment(f *ast.File) bool {
	for _, cg := range f.Comments {
		if cg.End() < f.Package {
			return true
		}
	}
	return false
}

// classifyAndValidateDecls validates that f (known to have no package-doc comment) matches exactly one of the allowed declaration forms and returns the corresponding
// snippetKind.
func classifyAndValidateDecls(f *ast.File) (snippetKind, error) {
	var funcCnt, typeCnt, varCnt, constCnt int

	for _, d := range f.Decls {
		switch decl := d.(type) {
		case *ast.FuncDecl:
			funcCnt++
		case *ast.GenDecl:
			switch decl.Tok {
			case token.TYPE:
				if len(decl.Specs) == 0 {
					return snippetKindUnknown, errors.New("snippet contains type block with no specs")
				}
				typeCnt++
			case token.VAR:
				if len(decl.Specs) == 0 {
					return snippetKindUnknown, errors.New("snippet contains type var with no specs")
				}
				varCnt++
			case token.CONST:
				if len(decl.Specs) == 0 {
					return snippetKindUnknown, errors.New("snippet contains type const with no specs")
				}
				constCnt++
			default:
				return snippetKindUnknown, errors.New("snippet contains unsupported declaration kind")
			}
		default:
			return snippetKindUnknown, errors.New("snippet contains unsupported declaration kind")
		}
	}

	total := funcCnt + typeCnt + varCnt + constCnt
	if total == 0 {
		return snippetKindUnknown, errors.New("snippet must contain at least one declaration")
	}

	var kind snippetKind

	switch {
	case funcCnt == 1 && typeCnt+varCnt+constCnt == 0:
		kind = snippetKindFunc
	case typeCnt == 1 && funcCnt+varCnt+constCnt == 0:
		kind = snippetKindType
	case varCnt >= 1 && funcCnt+typeCnt+constCnt == 0:
		kind = snippetKindVar
	case constCnt >= 1 && funcCnt+typeCnt+varCnt == 0:
		kind = snippetKindConst
	default:
		var kinds []string
		if funcCnt > 0 {
			kinds = append(kinds, fmt.Sprintf("%d func", funcCnt))
		}
		if typeCnt > 0 {
			if typeCnt == 1 {
				kinds = append(kinds, "1 type")
			} else {
				kinds = append(kinds, fmt.Sprintf("%d types", typeCnt))
			}
		}
		if varCnt > 0 {
			if varCnt == 1 {
				kinds = append(kinds, "1 var")
			} else {
				kinds = append(kinds, fmt.Sprintf("%d vars", varCnt))
			}
		}
		if constCnt > 0 {
			if constCnt == 1 {
				kinds = append(kinds, "1 const")
			} else {
				kinds = append(kinds, fmt.Sprintf("%d consts", constCnt))
			}
		}
		return snippetKindUnknown, fmt.Errorf("snippet contains %s; allowed forms are: single func, single type, vars only, or consts only", strings.Join(kinds, ", "))
	}

	if err := validSnippetComments(f); err != nil {
		return snippetKindUnknown, err
	}

	return kind, nil
}

// validSnippetComments returns an error if the snippet has invalid comments for the purpose of documenting. In particular:
//   - a declaration/spec must NOT have both a Doc (leading) comment and an end-of-line (EOL) comment on the same declaration.
//   - for struct types, each field must NOT have both a Doc and an EOL comment.
//
// Examples of invalid comments:
//
//	// doc comment
//	type Foo int // end-of-line comment (shouldn't have both doc and eol at same time)
//
//	type Foo struct {
//		// Bar doc
//		Bar int // end-of-line comment (shouldn't have both doc and eol at the same time)
//	}
//
// TODO: I originally thought "multi-line entities should NOT have EOL comments" was important to validate -- it still may be! I never implemented it.
func validSnippetComments(file *ast.File) error {
	for _, decl := range file.Decls {

		switch d := decl.(type) {
		case *ast.GenDecl:

			if d.Tok == token.TYPE {
				if d.Lparen.IsValid() {
					// type block
					// d can always have a Doc

					for _, spec := range d.Specs {
						typeSpec := spec.(*ast.TypeSpec)

						// Detect both Doc and EOL comment:
						if typeSpec.Comment != nil && typeSpec.Doc != nil {
							return fmt.Errorf("type %s has both doc comment and end-of-line comment", typeSpec.Name.Name)
						}

						if err := validCommentsInType(typeSpec.Type); err != nil {
							return err
						}
					}
				} else {
					// single type declaration
					if len(d.Specs) != 1 {
						panic("unexpected length of specs for non-block type decl")
					}

					typeSpec := d.Specs[0].(*ast.TypeSpec)

					// Detect both Doc and EOL comment:
					if typeSpec.Comment != nil && d.Doc != nil {
						return fmt.Errorf("type %s has both doc comment and end-of-line comment", typeSpec.Name.Name)
					}

					if err := validCommentsInType(typeSpec.Type); err != nil {
						return err
					}
				}
			} else if d.Tok == token.VAR || d.Tok == token.CONST {
				if d.Lparen.IsValid() {
					// var/const block
					// d can always have a Doc

					for _, spec := range d.Specs {
						valueSpec := spec.(*ast.ValueSpec)

						// Detect both Doc and EOL comment:
						if valueSpec.Comment != nil && valueSpec.Doc != nil {
							if len(valueSpec.Names) > 0 {
								return fmt.Errorf("%s %s has both doc comment and end-of-line comment", d.Tok.String(), valueSpec.Names[0].Name)
							}
							return fmt.Errorf("%s declaration has both doc comment and end-of-line comment", d.Tok.String())
						}
					}
				} else {
					// single type declaration
					if len(d.Specs) != 1 {
						panic("unexpected length of specs for non-block type decl")
					}

					valueSpec := d.Specs[0].(*ast.ValueSpec)

					// Detect both Doc and EOL comment:
					if valueSpec.Comment != nil && d.Doc != nil {
						if len(valueSpec.Names) > 0 {
							return fmt.Errorf("%s %s has both doc comment and end-of-line comment", d.Tok.String(), valueSpec.Names[0].Name)
						}
						return fmt.Errorf("%s declaration has both doc comment and end-of-line comment", d.Tok.String())
					}
				}
			}
		}
	}

	return nil
}

// validCommentsInType detects invalid comments in types. Currently only structs are handled. Each field must not contain both a Comment and a Doc.
func validCommentsInType(expr ast.Expr) error {
	structType, ok := expr.(*ast.StructType)
	if !ok {
		return nil
	}

	if structType.Fields != nil {
		for _, f := range structType.Fields.List {

			if f.Doc != nil && f.Comment != nil {
				if len(f.Names) > 0 {
					return fmt.Errorf("struct field %s has both doc comment and end-of-line comment", f.Names[0].Name)
				}
				return fmt.Errorf("struct field has both doc comment and end-of-line comment")
			}

			if err := validCommentsInType(f.Type); err != nil {
				return err
			}
		}
	}

	return nil
}

// reflowDocCommentsInAST reflows .Doc comments on package, top-level var/const/type decls and specs, and types' fields in type decls (ex: struct fields).
//
// Caveat: it does not reflow struct/interface fields in non-type decls (ex: anonymous struct types in vars; inside functions).
//
// Important note: this will set the .Doc properties in the file ast, but it will not recalculate any offsets. Therefore, after this call, all offset information
// in file ast is invalid.
func reflowDocCommentsInAST(file *ast.File, options Options) {
	const (
		indentLevel = 0 // Package-level declarations have no indentation
	)

	// Helper function to reflow a comment group
	reflowCommentGroup := func(cg *ast.CommentGroup) *ast.CommentGroup {
		if cg == nil {
			return nil
		}

		// Extract the comment text
		var commentText strings.Builder
		for _, comment := range cg.List {
			commentText.WriteString(comment.Text)
			commentText.WriteByte('\n')
		}

		// Reflow the comment
		reflowed := reflowDocComment(commentText.String(), indentLevel, options.ReflowTabWidth, options.ReflowMaxWidth)
		if strings.TrimSpace(reflowed) == "" {
			return nil
		}

		// Parse the reflowed comment back into ast.Comment nodes
		lines := strings.Split(strings.TrimSuffix(reflowed, "\n"), "\n")
		newComments := make([]*ast.Comment, 0, len(lines))

		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				newComments = append(newComments, &ast.Comment{
					Text: line,
				})
			}
		}

		if len(newComments) > 0 {
			return &ast.CommentGroup{List: newComments}
		}
		return nil
	}

	// Helper function to recursively reflow comments in types (handles nested structs)
	var reflowCommentsInType func(expr ast.Expr)
	reflowCommentsInType = func(expr ast.Expr) {
		switch t := expr.(type) {
		case *ast.StructType:
			if t.Fields != nil {
				for _, field := range t.Fields.List {
					field.Doc = reflowCommentGroup(field.Doc)
					// Recursively handle nested types
					reflowCommentsInType(field.Type)
				}
			}
		case *ast.InterfaceType:
			if t.Methods != nil {
				for _, method := range t.Methods.List {
					method.Doc = reflowCommentGroup(method.Doc)
				}
			}
		case *ast.ArrayType:
			reflowCommentsInType(t.Elt)
		case *ast.MapType:
			reflowCommentsInType(t.Key)
			reflowCommentsInType(t.Value)
		case *ast.ChanType:
			reflowCommentsInType(t.Value)
		case *ast.StarExpr:
			reflowCommentsInType(t.X)
		}
	}

	// Reflow package doc comment
	file.Doc = reflowCommentGroup(file.Doc)

	// Walk through all declarations
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			// Reflow declaration-level doc comment
			d.Doc = reflowCommentGroup(d.Doc)

			// Handle specs within the declaration
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					s.Doc = reflowCommentGroup(s.Doc)
					// Use the recursive helper to handle all nested types
					reflowCommentsInType(s.Type)

				case *ast.ValueSpec:
					s.Doc = reflowCommentGroup(s.Doc)
				}
			}

		case *ast.FuncDecl:
			d.Doc = reflowCommentGroup(d.Doc)
		}
	}
}

// convertFunctionDeclEOLComments converts an EOL func comment to a doc comment, only if the func has no body.
func convertFunctionDeclEOLComments(file *ast.File, fset *token.FileSet) error {
	for _, decl := range file.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}

		// Only convert if function has no body (signature only)
		if funcDecl.Body != nil {
			continue
		}

		// Find EOL comment for this function declaration
		eolComment := findEOLCommentForNode(file, funcDecl, fset)
		if eolComment != nil {
			if funcDecl.Doc != nil {
				return fmt.Errorf("func %s has both doc comment and end-of-line comment", funcDecl.Name.Name)
			}
			funcDecl.Doc = eolComment
			removeCommentFromFile(file, eolComment)
		}
	}
	return nil
}

// findEOLCommentForNode finds an end-of-line comment associated with the given AST node. It returns the comment group if found, nil otherwise.
func findEOLCommentForNode(file *ast.File, node ast.Node, fset *token.FileSet) *ast.CommentGroup {
	nodeEndPos := fset.Position(node.End())

	// Look for a comment that starts on the same line as the node ends
	for _, cg := range file.Comments {
		commentStartPos := fset.Position(cg.Pos())

		// Check if the comment is on the same line as the node ends
		if commentStartPos.Line == nodeEndPos.Line && cg.Pos() > node.End() {
			return cg
		}
	}
	return nil
}

// removeCommentFromFile removes a comment group from the file's comment list.
func removeCommentFromFile(file *ast.File, commentToRemove *ast.CommentGroup) {
	for i, cg := range file.Comments {
		if cg == commentToRemove {
			// Remove this comment from the slice
			file.Comments = append(file.Comments[:i], file.Comments[i+1:]...)
			break
		}
	}
}
