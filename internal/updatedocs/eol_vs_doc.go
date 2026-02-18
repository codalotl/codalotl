package updatedocs

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"strings"
	"unicode/utf8"
)

// enforceEOLVsDocInAST walks the file and modifies the .Doc and .Comment comment nodes for top-level blocks (var/const/type blocks) and struct/interface fields
// so that long comments are .Doc comments and short comments are .Comment EOL comments (a few other rules besides length apply). Because we have another system
// which comprehensively reflows comments (changing their line length to fit in a given width), this method mostly does not do that, except for collapsing doc comments
// if they fit in an EOL comment (since an EOL comment can't have multiple lines).
//
// This function MUST be called with *valid* file positioning information (and thus, before funcs that invalidate it, like reflowDocCommentsInAST).
//
// Important note: this will set the .Doc properties in the file ast, but it will not recalculate any offsets. Therefore, after this call, all offset information
// in file ast is invalid.
//
// A note about the design approach: because this method is called with a snippet, which can contain a subset of fields, and because some changes may be rejected
// due to policy (ex: options.RejectUpdates), it is possible that the end-result of a user's file that they see does not match the policy this function tries to
// impose. Another example of failure: snippet is non-block const, but source code is block, so comments aren't EOL-ized. However, we can always pass the source
// code itself as a snippet, guaranteeing all fields are present, as a second cleanup phase.
func enforceEOLVsDocInAST(file *ast.File, fset *token.FileSet, options Options) {
	// Default tab and width values if not supplied.
	tabWidth := options.ReflowTabWidth
	if tabWidth == 0 {
		tabWidth = 4
	}
	softMax := options.ReflowMaxWidth
	if softMax == 0 {
		softMax = 80
	}

	comments := file.Comments

	// Get all eolVsDocField first, because doing so needs accurate position information, and updating any comments destroys it:
	fieldMap := make(map[string]*eolVsDocField) // key in `var x,y int` -> "x&y"; in structs, `type foo struct {bar int}` -> "foo.bar"
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}

		// Only handle const/var/type:
		if !(genDecl.Tok == token.VAR || genDecl.Tok == token.CONST || genDecl.Tok == token.TYPE) {
			continue
		}

		// For blocks (parens), handle the specs:
		if genDecl.Lparen.IsValid() {
			// Build groups of comment fields for the declaration.
			groups := getEolVsDocFieldsForDeclBlock(genDecl, fset, comments, tabWidth, softMax)

			// Decide EOL vs Doc for every group.
			for _, g := range groups {
				decideEOLVsDocForGroup(g, softMax)
				for _, fld := range g {
					fieldMap[fld.identifierKey] = fld
				}
			}
		}

		// For struct/interface types specifically, get fields:
		// NOTE: interfaces are interesting. They have a symmetry with structs (which often want EOL comments) but they're also like functions, which are
		// always desired to be .Doc. We're going to force them to always be .Doc.
		if genDecl.Tok == token.TYPE {
			indentLevel := 0
			if genDecl.Lparen.IsValid() {
				indentLevel = 1
			}
			for _, spec := range genDecl.Specs {
				spec := spec.(*ast.TypeSpec)
				if structType, ok := spec.Type.(*ast.StructType); ok {
					structGroups := getEolVsDocFieldsForStructType(spec.Name.Name, structType, fset, comments, indentLevel, tabWidth, softMax)
					for _, g := range structGroups {
						decideEOLVsDocForGroup(g, softMax)
						for _, fld := range g {
							fieldMap[fld.identifierKey] = fld
						}
					}
				} else if ifaceType, ok := spec.Type.(*ast.InterfaceType); ok {
					// Collect interface elements; we always prefer Doc for interface methods/embeddings.
					ifaceFields := getEolVsDocFieldsForInterfaceType(spec.Name.Name, ifaceType, fset, comments, indentLevel, tabWidth, softMax)
					for _, fld := range ifaceFields {
						// shouldBeEOL already defaults to false; keep it that way to enforce Doc.
						fieldMap[fld.identifierKey] = fld
					}
				}
			}
		}
	}

	// Now update all comments:
	applyEOLVsDocChangesToDecls(file, fieldMap)
	applyEOLVsDocChangesToStructs(file, fieldMap)
	applyEOLVsDocChangesToInterfaces(file, fieldMap)
}

// applyEOLVsDocChangesToStructs iterates through the file's type declarations and updates any struct fields' .Doc and .Comment based on the decisions made in the
// provided fieldMap.
func applyEOLVsDocChangesToStructs(file *ast.File, fieldMap map[string]*eolVsDocField) {
	// Helper to create an *ast.CommentGroup from a raw string.
	createCommentGroup := func(comment string) *ast.CommentGroup {
		if comment == "" {
			return nil
		}
		comment = strings.TrimSuffix(comment, "\n")
		lines := strings.Split(comment, "\n")
		var list []*ast.Comment
		for _, line := range lines {
			list = append(list, &ast.Comment{Text: strings.TrimSpace(line)})
		}
		return &ast.CommentGroup{List: list}
	}

	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}

		if genDecl.Tok != token.TYPE {
			continue
		}

		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}

			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}

			var walkStruct func(structPath string, st *ast.StructType)
			walkStruct = func(structPath string, st *ast.StructType) {
				if st.Fields == nil {
					return
				}
				for _, field := range st.Fields.List {
					if field.Doc != nil && field.Comment != nil {
						continue
					}
					if field.Doc == nil && field.Comment == nil {
						continue
					}

					key := structPath + "." + fieldKey(field)
					fieldInfo, ok := fieldMap[key]
					if !ok {
						// This can happen for anonymous nested structs which we recurse into.
						if st, ok := field.Type.(*ast.StructType); ok {
							nestedPath := structPath + "."
							if len(field.Names) > 0 {
								nestedPath += field.Names[0].Name
							} else {
								// Fallback for embedded struct, might need a better key.
								if t, ok := field.Type.(*ast.Ident); ok {
									nestedPath += t.Name
								}
							}
							walkStruct(nestedPath, st)
						}
						continue
					}

					if fieldInfo.shouldBeEOL && field.Doc != nil && field.Comment == nil {
						newCG := &ast.CommentGroup{List: []*ast.Comment{{Text: strings.TrimSpace(fieldInfo.reflowedDocComment)}}}
						removeCommentFromFile(file, field.Doc)
						field.Doc = nil
						field.Comment = newCG
					} else if !fieldInfo.shouldBeEOL && field.Doc == nil && field.Comment != nil {
						// Preserve exact spelling for directives like `//go:embed` by using the
						// original comment token text (not CommentGroup.Text()).
						newCG := createCommentGroup(commentBlockFromGroup(field.Comment, false))
						removeCommentFromFile(file, field.Comment)
						field.Comment = nil
						field.Doc = newCG
					}

					// Recurse for nested structs
					var nestedStruct *ast.StructType
					if st, ok := field.Type.(*ast.StructType); ok {
						nestedStruct = st
					} else if pt, ok := field.Type.(*ast.StarExpr); ok {
						if st, ok := pt.X.(*ast.StructType); ok {
							nestedStruct = st
						}
					}
					if nestedStruct != nil {
						nestedPath := structPath + "." + identsKey(field.Names)
						walkStruct(nestedPath, nestedStruct)
					}
				}
			}
			walkStruct(typeSpec.Name.Name, structType)
		}
	}
}

// applyEOLVsDocChangesToInterfaces iterates through the file's interface type declarations and updates any methods' .Doc and .Comment based on the decisions in
// fieldMap (which, for interfaces, enforces Doc always).
func applyEOLVsDocChangesToInterfaces(file *ast.File, fieldMap map[string]*eolVsDocField) {
	// Helper to create an *ast.CommentGroup from a raw string.
	createCommentGroup := func(comment string) *ast.CommentGroup {
		if comment == "" {
			return nil
		}
		comment = strings.TrimSuffix(comment, "\n")
		lines := strings.Split(comment, "\n")
		var list []*ast.Comment
		for _, line := range lines {
			list = append(list, &ast.Comment{Text: strings.TrimSpace(line)})
		}
		return &ast.CommentGroup{List: list}
	}

	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}

		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}

			ifaceType, ok := typeSpec.Type.(*ast.InterfaceType)
			if !ok || ifaceType.Methods == nil {
				continue
			}

			for _, field := range ifaceType.Methods.List {
				if field.Doc != nil && field.Comment != nil {
					continue
				}
				if field.Doc == nil && field.Comment == nil {
					continue
				}

				key := typeSpec.Name.Name + "." + fieldKey(field)
				fieldInfo, ok := fieldMap[key]
				if !ok {
					continue
				}

				// Enforce Doc always for interfaces: if we currently have an EOL comment, convert to Doc.
				if !fieldInfo.shouldBeEOL && field.Doc == nil && field.Comment != nil {
					// Preserve exact spelling for directives like `//go:...` by using the
					// original comment token text (not CommentGroup.Text()).
					newCG := createCommentGroup(commentBlockFromGroup(field.Comment, false))
					removeCommentFromFile(file, field.Comment)
					field.Comment = nil
					field.Doc = newCG
				} else if fieldInfo.shouldBeEOL && field.Doc != nil && field.Comment == nil {
					// This branch should never be taken for interfaces (we never set shouldBeEOL), but handle defensively.
					newCG := &ast.CommentGroup{List: []*ast.Comment{{Text: strings.TrimSpace(fieldInfo.reflowedDocComment)}}}
					removeCommentFromFile(file, field.Doc)
					field.Doc = nil
					field.Comment = newCG
				}
			}
		}
	}
}

// applyEOLVsDocChangesToDecls iterates through the file's declarations and updates the .Doc and .Comment fields of specs based on the decisions made in the provided
// fieldMap.
func applyEOLVsDocChangesToDecls(file *ast.File, fieldMap map[string]*eolVsDocField) {
	// Helper to create an *ast.CommentGroup from a raw string. The input must already
	// start with "//" for every line and be newline terminated (except for potential
	// EOL single-line comments which are not newline-terminated after trimming).
	createCommentGroup := func(comment string) *ast.CommentGroup {
		if comment == "" {
			return nil
		}

		comment = strings.TrimSuffix(comment, "\n")

		lines := strings.Split(comment, "\n")
		var list []*ast.Comment
		for _, line := range lines {
			list = append(list, &ast.Comment{Text: strings.TrimSpace(line)})
		}
		return &ast.CommentGroup{List: list}
	}

	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}

		// Only handle const/var/type blocks (with parens).
		if !(genDecl.Tok == token.VAR || genDecl.Tok == token.CONST || genDecl.Tok == token.TYPE) || !genDecl.Lparen.IsValid() {
			continue
		}

		// Iterate over specs again to mutate their comment placement.
		for _, spec := range genDecl.Specs {
			switch s := spec.(type) {
			case *ast.ValueSpec:
				// If the field has both doc and EOL comment, just ignore it (it's unclear what a good policy is here, so just avoid mistakes):
				if s.Doc != nil && s.Comment != nil {
					continue
				}

				// If the spec has no comment, nothing to do:
				if s.Doc == nil && s.Comment == nil {
					continue
				}

				key := identsKey(s.Names)
				field, ok := fieldMap[key]
				if !ok {
					panic("unexpected lack of key in applyEOLVsDocChangesToDecls")
				}
				if field.reflowedDocComment == "" {
					panic("unexpected empty comment in field.reflowedDocComment in applyEOLVsDocChangesToDecls")
				}

				if field.shouldBeEOL && s.Doc != nil && s.Comment == nil {
					newCG := &ast.CommentGroup{List: []*ast.Comment{{Text: strings.TrimSpace(field.reflowedDocComment)}}} // NOTE: ast.Comment's Text wants NON-\n terminated strings.

					removeCommentFromFile(file, s.Doc)
					s.Doc = nil
					s.Comment = newCG
				} else if !field.shouldBeEOL && s.Doc == nil && s.Comment != nil {
					// Preserve exact spelling for directives like `//go:embed` by using the
					// original comment token text (not CommentGroup.Text()).
					newCG := createCommentGroup(commentBlockFromGroup(s.Comment, false))

					removeCommentFromFile(file, s.Comment)
					s.Comment = nil
					s.Doc = newCG
				}
			case *ast.TypeSpec:
				// If the field has both doc and EOL comment, just ignore it (it's unclear what a good policy is here, so just avoid mistakes):
				if s.Doc != nil && s.Comment != nil {
					continue
				}

				// If the spec has no comment, nothing to do:
				if s.Doc == nil && s.Comment == nil {
					continue
				}

				key := s.Name.Name
				field, ok := fieldMap[key]
				if !ok {
					panic("unexpected lack of key in applyEOLVsDocChangesToDecls")
				}
				if field.reflowedDocComment == "" {
					panic("unexpected empty comment in field.reflowedDocComment in applyEOLVsDocChangesToDecls")
				}

				if field.shouldBeEOL && s.Doc != nil && s.Comment == nil {
					newCG := &ast.CommentGroup{List: []*ast.Comment{{Text: strings.TrimSpace(field.reflowedDocComment)}}} // NOTE: ast.Comment's Text wants NON-\n terminated strings.

					if s.Doc != nil {
						removeCommentFromFile(file, s.Doc)
					}

					s.Doc = nil
					s.Comment = newCG
				} else if !field.shouldBeEOL && s.Doc == nil && s.Comment != nil {
					// Preserve exact spelling for directives like `//go:...` by using the
					// original comment token text (not CommentGroup.Text()).
					newCG := createCommentGroup(commentBlockFromGroup(s.Comment, false))

					if s.Comment != nil {
						removeCommentFromFile(file, s.Comment)
					}

					s.Comment = nil
					s.Doc = newCG
				}
			}
		}
	}
}

// getEolVsDocFieldsForStructType returns groups for a struct type, recursively.
//   - structPath is the dot notation path of the (nested) struct. ex: `type foo struct { bar struct { baz int } }` -> "foo.bar" (for the bar struct).
//   - indentLevel is the indent level of the struct (either type or field), and is 0-based, so a top-level `type foo struct { ... }` should be called with indentLevel=0.
//
// Returns groups of []*eolVsDocField. Empty structs (no fields) produce zero groups. Non-empty structs produce at least one group, or more if separated by separator
// comments. Nested structs are separate group(s).
func getEolVsDocFieldsForStructType(structPath string, structType *ast.StructType, fset *token.FileSet, allComments []*ast.CommentGroup, indentLevel int, tabWidth int, softMaxCols int) [][]*eolVsDocField {
	if structType.Fields == nil || len(structType.Fields.List) == 0 {
		return nil
	}

	// Helper: compute code metrics
	codeMetrics := func(field *ast.Field) (int, bool) {
		start := fset.Position(field.Pos())
		end := fset.Position(field.End())
		multiline := start.Line != end.Line

		minLen := 0
		if !multiline {
			var buf bytes.Buffer

			for i, name := range field.Names {
				if i > 0 {
					buf.WriteString(", ")
				}
				buf.WriteString(name.Name)
			}
			buf.WriteByte(' ')
			err := printer.Fprint(&buf, fset, field.Type)
			if err != nil {
				fmt.Printf("WARNING: got err when printing field %s: %v", buf.String(), err)
			}

			if field.Tag != nil {
				buf.WriteByte(' ')
				buf.WriteString(field.Tag.Value) // The Value contains the `` characters.
			}

			s := strings.TrimSpace(buf.String())
			minLen = utf8.RuneCountInString(s)
		}

		return minLen, multiline
	}

	// Build a set of *attached* comment groups.
	attached := make(map[*ast.CommentGroup]struct{})
	for _, field := range structType.Fields.List {
		if field.Doc != nil {
			attached[field.Doc] = struct{}{}
		}
		if field.Comment != nil {
			attached[field.Comment] = struct{}{}
		}
	}

	// Collect floating comment groups that lie *inside* the StructType.
	var floating []*ast.CommentGroup
	for _, cg := range allComments {
		if _, ok := attached[cg]; ok {
			continue // attached – ignore
		}
		// The structType.Pos() is the position of the "struct" keyword. The fields start after the opening brace.
		// Similarly, structType.End() is after the closing brace.
		if cg.Pos() >= structType.Fields.Opening && cg.End() <= structType.Fields.Closing {
			floating = append(floating, cg)
		}
	}

	var groups [][]*eolVsDocField
	var current []*eolVsDocField

	for i, field := range structType.Fields.List {
		// Detect nested struct types, including pointers to structs.
		var nestedStruct *ast.StructType
		if st, ok := field.Type.(*ast.StructType); ok {
			nestedStruct = st
		} else if pt, ok := field.Type.(*ast.StarExpr); ok {
			if st, ok := pt.X.(*ast.StructType); ok {
				nestedStruct = st
			}
		}

		// Handle nested anonymous structs recursively.
		if nestedStruct != nil {
			var fieldName string
			if len(field.Names) > 0 {
				fieldName = field.Names[0].Name // Use first name for path.
			} else {
				continue
			}

			nestedPath := structPath + "." + fieldName

			nestedGroups := getEolVsDocFieldsForStructType(nestedPath, nestedStruct, fset, allComments, indentLevel+1, tabWidth, softMaxCols)
			groups = append(groups, nestedGroups...)
		}

		identKey := structPath + "." + fieldKey(field)

		var cg *ast.CommentGroup
		if !hasNoDocs(field.Doc) {
			cg = field.Doc
		} else if !hasNoDocs(field.Comment) {
			cg = field.Comment
		}
		forceDoc := commentGroupForcesDoc(cg)

		var reflowed string
		if cg != nil {
			raw := commentBlockFromGroup(cg, false)
			reflowed = reflowDocComment(raw, 1, tabWidth, softMaxCols)
		}

		// Comment metrics
		var runeCount int
		isMultilineComment := false
		if reflowed != "" {
			runeCount = utf8.RuneCountInString(strings.TrimSpace(reflowed))
			isMultilineComment = strings.Count(reflowed, "\n") > 1
		}

		// Code metrics
		minLen, codeMultiline := codeMetrics(field)

		current = append(current, &eolVsDocField{
			identifierKey:      identKey,
			isMultiline:        isMultilineComment,
			commentLength:      runeCount,
			minCodeLength:      minLen,
			codeIsMultiline:    codeMultiline,
			indentInSpaces:     (indentLevel + 1) * tabWidth,
			reflowedDocComment: reflowed,
			forceDoc:           forceDoc,
			shouldBeEOL:        false,
		})

		// Determine if we should terminate the current group.
		isLastField := i == len(structType.Fields.List)-1
		if !isLastField {
			nextPos := structType.Fields.List[i+1].Pos()
			for _, fcg := range floating {
				if fcg.Pos() > field.End() && fcg.End() < nextPos {
					groups = append(groups, current)
					current = nil
					break
				}
			}
		}
	}

	if len(current) > 0 {
		groups = append(groups, current)
	}

	return groups
}

// getEolVsDocFieldsForInterfaceType returns a flat slice of fields for an interface type. Identifier keys are of the form "<InterfaceName>.<MethodOrEmbedded>".
// Policy: prefer Doc for interface methods and embedded interfaces, but for type terms (ex: int; ~int; unions like "~int | ~float64"), mark shouldBeEOL=true (reason:
// as of 2025/08/12, Go doesn't attach leading comments to .Doc for type terms).
//
// NOTE/BUG: there are plain type terms that are unhandled (ex: maps, pointers, and identifiers). Without using go/types, we cannot reliably distinguish `type interface { Foo }`
// meaning Foo as a constraint or Foo as an embedded interface. In these cases, a Doc will be used.
func getEolVsDocFieldsForInterfaceType(interfacePath string, ifaceType *ast.InterfaceType, fset *token.FileSet, allComments []*ast.CommentGroup, indentLevel int, tabWidth int, softMaxCols int) []*eolVsDocField {
	if ifaceType.Methods == nil || len(ifaceType.Methods.List) == 0 {
		return nil
	}

	fields := make([]*eolVsDocField, 0, len(ifaceType.Methods.List))

	// Helper: compute minimal code length and multiline for a method/embedded line.
	codeMetrics := func(field *ast.Field) (int, bool) {
		start := fset.Position(field.Pos())
		end := fset.Position(field.End())
		multiline := start.Line != end.Line
		minLen := 0
		if !multiline {
			var buf bytes.Buffer
			if len(field.Names) > 0 {
				// method name(s)
				for i, name := range field.Names {
					if i > 0 {
						buf.WriteString(", ")
					}
					buf.WriteString(name.Name)
				}
				buf.WriteByte(' ')
			}
			_ = printer.Fprint(&buf, fset, field.Type)
			s := strings.TrimSpace(buf.String())
			minLen = utf8.RuneCountInString(s)
		}
		return minLen, multiline
	}

	// Helper: recognize predeclared basic types which can appear as plain type terms in interfaces.
	basicTypes := map[string]struct{}{
		"bool": {}, "byte": {}, "complex64": {}, "complex128": {}, "error": {},
		"float32": {}, "float64": {}, "int": {}, "int8": {}, "int16": {}, "int32": {}, "int64": {},
		"rune": {}, "string": {}, "uint": {}, "uint8": {}, "uint16": {}, "uint32": {}, "uint64": {}, "uintptr": {},
		"any": {}, // useful in constraints
	}

	for _, field := range ifaceType.Methods.List {
		identKey := interfacePath + "." + fieldKey(field)

		var cg *ast.CommentGroup
		if !hasNoDocs(field.Doc) {
			cg = field.Doc
		} else if !hasNoDocs(field.Comment) {
			cg = field.Comment
		}
		forceDoc := commentGroupForcesDoc(cg)

		var reflowed string
		if cg != nil {
			raw := commentBlockFromGroup(cg, false)
			reflowed = reflowDocComment(raw, 1, tabWidth, softMaxCols)
		}

		runeCount := 0
		isMultilineComment := false
		if reflowed != "" {
			runeCount = utf8.RuneCountInString(strings.TrimSpace(reflowed))
			isMultilineComment = strings.Count(reflowed, "\n") > 1
		}

		minLen, codeMultiline := codeMetrics(field)

		// As of 2025/08/12, go doesn't attach leading comments to .Doc for type terms, so make them EOL (which are attached).
		// Determine if this interface element is a type term (as opposed to a method or embedded interface).
		// We treat:
		//   - UnaryExpr with TILDE
		//   - BinaryExpr with OR
		//   - ParenExpr
		//   - Plain basic identifiers (e.g., int, string, bool, ...)
		// as type terms for EOL purposes.
		isTypeTerm := false
		if len(field.Names) == 0 { // only unnamed elements can be embedded interfaces or type terms
			switch t := field.Type.(type) {
			case *ast.UnaryExpr:
				if t.Op == token.TILDE {
					isTypeTerm = true
				}
			case *ast.BinaryExpr:
				if t.Op == token.OR {
					isTypeTerm = true
				}
			case *ast.ParenExpr:
				// Parenthesized type terms – conservatively treat as a type term.
				isTypeTerm = true
			case *ast.Ident:
				if _, ok := basicTypes[t.Name]; ok {
					isTypeTerm = true
				}
			}
		}

		fields = append(fields, &eolVsDocField{
			identifierKey:      identKey,
			isMultiline:        isMultilineComment,
			commentLength:      runeCount,
			minCodeLength:      minLen,
			codeIsMultiline:    codeMultiline,
			indentInSpaces:     (indentLevel + 1) * tabWidth,
			reflowedDocComment: reflowed,
			forceDoc:           forceDoc,
			shouldBeEOL:        isTypeTerm && !forceDoc,
		})
	}

	return fields
}

// getEolVsDocFieldsForDeclBlock returns grouped []*eolVsDocField. Groups are determined by "floating" comments -- comments separating specs that are not attached
// to any spec via .Doc or .Comment. All comments in the file are supplied in allComments (which is just file.Comments). Each spec's .Doc or .Comment will be reflowed
// by reflowDocComment and set as reflowedDocComment.
func getEolVsDocFieldsForDeclBlock(decl *ast.GenDecl, fset *token.FileSet, allComments []*ast.CommentGroup, tabWidth int, softMaxCols int) [][]*eolVsDocField {
	// Only const/var declarations are expected.
	if decl.Tok != token.VAR && decl.Tok != token.CONST && decl.Tok != token.TYPE {
		panic("getEolVsDocFieldsForDeclBlock called with non-value/non-type decl")
	}

	// Only do this for blocks
	if !decl.Lparen.IsValid() {
		return nil
	}

	// Helper: compute code metrics – minimum code length (for single-line specs) and whether the
	// code spans multiple lines.
	// codeMetrics returns the minimal printed length of the *code* portion of
	// the spec (excluding any Doc or trailing Comment) and whether the code
	// spans multiple lines.
	codeMetrics := func(spec ast.Spec) (int, bool) {
		start := fset.Position(spec.Pos())
		end := fset.Position(spec.End())
		multiline := start.Line != end.Line
		minLen := 0

		if !multiline {
			var buf bytes.Buffer

			// We want to ignore comments when measuring the code length. For
			// *ast.Value/TypeSpec we can achieve this by making a shallow copy with its
			// Doc and Comment fields cleared before printing.
			var copySpec ast.Spec

			if v, ok := spec.(*ast.ValueSpec); ok {
				copyValueSpec := *v
				copyValueSpec.Doc = nil
				copyValueSpec.Comment = nil
				copySpec = &copyValueSpec
			} else if v, ok := spec.(*ast.TypeSpec); ok {
				copyValueSpec := *v
				copyValueSpec.Doc = nil
				copyValueSpec.Comment = nil
				copySpec = &copyValueSpec
			}

			_ = printer.Fprint(&buf, token.NewFileSet(), copySpec)

			s := strings.TrimSpace(buf.String())
			minLen = utf8.RuneCountInString(s)
		}

		return minLen, multiline
	}

	// Build a set of *attached* comment groups (Doc or Comment) so that we can
	// distinguish them from *floating* comments present in allComments.
	attached := make(map[*ast.CommentGroup]struct{})
	for _, spec := range decl.Specs {
		if vs, ok := spec.(*ast.ValueSpec); ok {
			if vs.Doc != nil {
				attached[vs.Doc] = struct{}{}
			}
			if vs.Comment != nil {
				attached[vs.Comment] = struct{}{}
			}
		}
		if ts, ok := spec.(*ast.TypeSpec); ok {
			if ts.Doc != nil {
				attached[ts.Doc] = struct{}{}
			}
			if ts.Comment != nil {
				attached[ts.Comment] = struct{}{}
			}
		}
	}
	if decl.Doc != nil {
		attached[decl.Doc] = struct{}{}
	}

	// Collect floating comment groups that lie *inside* the GenDecl and are not
	// attached to any spec. We will use their positions as group separators.
	var floating []*ast.CommentGroup
	for _, cg := range allComments {
		if _, ok := attached[cg]; ok {
			continue // attached – ignore
		}
		if cg.Pos() >= decl.Pos() && cg.End() <= decl.End() {
			floating = append(floating, cg)
		}
	}

	// Iterate through specs, build fields, and split into groups when a floating
	// comment appears between consecutive specs.
	var groups [][]*eolVsDocField
	var current []*eolVsDocField

	for i, spec := range decl.Specs {
		var identKey string
		var cg *ast.CommentGroup
		if vs, ok := spec.(*ast.ValueSpec); ok {
			identKey = identsKey(vs.Names)

			// Choose comment group (prefer Doc over Comment).
			if !hasNoDocs(vs.Doc) {
				cg = vs.Doc
			} else if !hasNoDocs(vs.Comment) {
				cg = vs.Comment
			}
		} else if ts, ok := spec.(*ast.TypeSpec); ok {
			identKey = ts.Name.Name

			// Choose comment group (prefer Doc over Comment).
			if !hasNoDocs(ts.Doc) {
				cg = ts.Doc
			} else if !hasNoDocs(ts.Comment) {
				cg = ts.Comment
			}
		}

		var reflowed string
		if cg != nil {
			raw := commentBlockFromGroup(cg, false)
			reflowed = reflowDocComment(raw, 1, tabWidth, softMaxCols)
		}
		forceDoc := commentGroupForcesDoc(cg)

		// Comment metrics
		var runeCount int
		isMultilineComment := false
		if reflowed != "" {
			runeCount = utf8.RuneCountInString(strings.TrimSpace(reflowed))
			isMultilineComment = strings.Count(reflowed, "\n") > 1
		}

		// Code metrics
		minLen, codeMultiline := codeMetrics(spec)

		current = append(current, &eolVsDocField{
			identifierKey:      identKey,
			isMultiline:        isMultilineComment,
			commentLength:      runeCount,
			minCodeLength:      minLen,
			codeIsMultiline:    codeMultiline,
			indentInSpaces:     tabWidth,
			reflowedDocComment: reflowed,
			forceDoc:           forceDoc,
			shouldBeEOL:        false, // leave unset – caller decides later
		})

		// Determine if we should terminate the current group.
		isLastSpec := i == len(decl.Specs)-1
		if !isLastSpec {
			nextPos := decl.Specs[i+1].Pos()
			// Any floating comment strictly between current spec and next spec?
			for _, fcg := range floating {
				if fcg.Pos() > spec.End() && fcg.End() < nextPos {
					// Separator found – close current group.
					groups = append(groups, current)
					current = nil
					break
				}
			}
		}
	}

	if len(current) > 0 {
		groups = append(groups, current)
	}

	return groups
}

// NOTE: commentLength is ideally "displayed characters in a fixed-width font" but is currently coded as "runes". Use github.com/mattn/go-runewidth to fix.
type eolVsDocField struct {
	identifierKey string // name(s) of fields/specs (ex: `var x, y int`, will be "x&y" -- see identsKey). Structs have dot notation prefixes to their field key
	isMultiline   bool   // true if reflowedDocComment contains multiple lines (2+ \n's)
	commentLength int    // length of reflowedDocComment (even if multiline), including leading "//" but excluding the last \n. 0 if no comment

	// length of the code, starting from first non-whitespace char to the last, assuming minimal spaces between tokens (ex: `x int` -> 5, and `x int` -> 5; `X, Y int`
	// -> 8 (2 mandatory spaces))
	minCodeLength int

	// true if the code is multiline (ex: a field which is an inline nested struct that spans several lines). Avoid EOL for multiline fields
	codeIsMultiline bool

	// indent of the field, in space-equivalents; should be equal for all fields in group
	indentInSpaces int

	// comment, \n-terminated, starting with "//", if the comment were to be placed as a Doc comment. Result of reflowing the original comment. If this is multiline,
	// we know already we must keep it a doc comment. If it's single line, we can measure the stripped length to see if it fits EOL.
	reflowedDocComment string

	// If true, this comment must stay as a leading comment (Doc) even if it would otherwise be eligible to become an EOL comment. This is used for compiler directives
	// like `//go:embed` which break when moved to EOL position.
	forceDoc bool

	// will be set to true if this comment should be EOL, otherwise set to false if this comment should be a Doc.
	shouldBeEOL bool
}

// decideEOLVsDocForGroup sets each field's shouldBeEOL. All fields belong to the same group. Every property in fields is pre-calculated (except shouldBeEOL). indentInSpaces
// is the indent level of the fields measured in spaces. Rules to set shouldBeEOL:
//   - If isMultiline or codeIsMultiline, set shouldBeEOL=false
//   - If there are three consecutive fields where an EOL comment would fit (if we align those fields), set them to shouldBeEOL=true. (No comment counts as EOL)
//   - If there's only 2 max fields, and all would fit if we align, set them to shouldBeEOL=true.
//   - Otherwise, shouldBeEOL=false
func decideEOLVsDocForGroup(fields []*eolVsDocField, softMaxCols int) {
	// Early exit if no work.
	if len(fields) == 0 {
		return
	}

	indentInSpaces := fields[0].indentInSpaces

	if softMaxCols == 0 {
		softMaxCols = 80
	}

	// BUG: minCodeLength is an incorrect abstraction (but ~works in practice for most cases).
	// The Go formatter will align things by column. This can result in cases where the longest minCodeLength
	// is actually shorter than the longest line when aligned by column. The net result of this bug is lines that could be too long.
	// Example:
	//	var x         someVeryLongType // 22 min length
	//	var mediumVar mediumType       // 24 min length

	// Helper closure: given indices into fields [start,end] inclusive, return true if every
	// field in that slice fits on one line when comments are aligned to the
	// longest code segment among the same slice.
	fits := func(start, end int) bool {
		// Calculate max code length in slice.
		maxCode := 0
		for i := start; i <= end; i++ {
			if fields[i].minCodeLength > maxCode {
				maxCode = fields[i].minCodeLength
			}
		}
		// Check each field.
		for i := start; i <= end; i++ {
			lineLen := indentInSpaces + maxCode + 1 + fields[i].commentLength // +1 space between code and "//"
			if lineLen > softMaxCols {
				return false
			}
		}
		return true
	}

	// Helper slice marking eligibility (non-multiline comment and non-multiline code).
	eligible := make([]bool, len(fields))
	for i, f := range fields {
		eligible[i] = !(f.isMultiline || f.codeIsMultiline || f.forceDoc)
		// initialise default – explicit for clarity
		fields[i].shouldBeEOL = false
	}

	// Special-case small groups (≤2 total fields).
	if len(fields) <= 2 {
		allEligible := true
		for _, e := range eligible {
			if !e {
				allEligible = false
				break
			}
		}
		if allEligible && fits(0, len(fields)-1) {
			for i := range fields {
				fields[i].shouldBeEOL = true
			}
		}
		return
	}

	// Traverse runs of consecutive *eligible* fields.
	for i := 0; i < len(fields); {
		if !eligible[i] {
			i++
			continue
		}

		// Start of run.
		runStart := i
		for i < len(fields) && eligible[i] {
			i++
		}
		runEnd := i - 1
		runLen := runEnd - runStart + 1

		if runLen < 3 {
			// Need at least 3 consecutive eligible fields to even consider EOL.
			continue
		}

		// If the entire run fits, mark them all.
		if fits(runStart, runEnd) {
			for j := runStart; j <= runEnd; j++ {
				fields[j].shouldBeEOL = true
			}
			continue
		}

		// Otherwise, slide a window of size 3 over the run and mark any window
		// that fits. We deliberately use the minimal window size to satisfy the
		// rule. Overlapping windows are fine – marking a field multiple times
		// is idempotent.
		for ws := runStart; ws <= runEnd-2; ws++ {
			we := ws + 2 // window end index (inclusive)
			if fits(ws, we) {
				for j := ws; j <= we; j++ {
					fields[j].shouldBeEOL = true
				}
			}
		}
	}
}

func commentGroupForcesDoc(cg *ast.CommentGroup) bool {
	if cg == nil {
		return false
	}
	for _, c := range cg.List {
		if commentTextForcesDoc(c.Text) {
			return true
		}
	}
	return false
}

func commentTextForcesDoc(text string) bool {
	t := strings.TrimSpace(text)

	if strings.HasPrefix(t, "//line ") || strings.HasPrefix(t, "//line\t") {
		return true
	}

	// Re-use the existing directive/pragma detection used elsewhere in this
	// package (build tags, //go:<directive>, cgo pragmas, nolint directives,
	// generated markers, etc.).
	return shouldPreserveComment(t)
}
