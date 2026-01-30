package updatedocs

import (
	"fmt"
	"github.com/codalotl/codalotl/internal/gocode"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
)

func updateTypeDoc(pkg *gocode.Package, ps *parsedSnippet, options Options) (*gocode.File, *SnippetError, error) {
	if ps.kind != snippetKindType {
		panic("expected type kind")
	}
	if len(ps.ast.Decls) != 1 {
		panic("expected exactly 1 decl")
	}

	genDecl, isGenDecl := ps.ast.Decls[0].(*ast.GenDecl)
	if !isGenDecl {
		panic("expected gen decl")
	}
	if genDecl.Tok != token.TYPE {
		panic("expected type token")
	}
	if len(genDecl.Specs) == 0 {
		panic("expected at least one spec")
	}

	snippetTypeSpec := genDecl.Specs[0].(*ast.TypeSpec)
	typeName := snippetTypeSpec.Name.Name

	identMap := identifierToFileMap(pkg)
	foundFile := identMap[typeName]
	if foundFile == "" {
		return nil, &SnippetError{Snippet: ps.originalSnippet, UserErrorMessage: fmt.Sprintf("Could not find type definition for %q", typeName)}, nil
	}

	file := pkg.Files[foundFile]

	curFileBytes := file.Contents
	curAST := file.AST
	curFileSet := file.FileSet

	appliedCount := 0

	for {
		newBytes, shouldContinue, snippetErr, err := updateTypeDocOneComment(curFileBytes, curAST, curFileSet, genDecl, snippetTypeSpec, ps, options)
		if err != nil || snippetErr != nil {
			// NOTE: if we get a snippet error, even if we've "applied" something, it's only to unpersisted bytes, so it's fine to throw it away.
			return nil, snippetErr, err
		}

		if !shouldContinue {
			break
		}

		if newBytes != nil {
			appliedCount++

			// Create new FileSet and parse the updated contents
			curFileSet = token.NewFileSet()
			curAST, err = parser.ParseFile(curFileSet, file.FileName, newBytes, parser.ParseComments)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to reparse file: %w", err)
			}
			curFileBytes = newBytes
		}
	}

	if appliedCount == 0 {
		return nil, &SnippetError{Snippet: ps.originalSnippet, UserErrorMessage: "No comments to apply"}, nil
	}

	// Format the file contents using gofmt-style to fix all empty lines and misaligned comments.
	formattedBytes, err := format.Source(curFileBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to format file: %w", err)
	}

	newFile := file.Clone()
	err = newFile.PersistNewContents(formattedBytes, true)
	if err != nil {
		return nil, nil, err
	}

	// if reflow, do further newline manipulation so that, for instance, multiple consts in a block with EOL comments don't have newlines between them:
	if options.Reflow {
		edits := getFormatEditsForBlockOrStruct(newFile, []string{typeName})
		if len(edits) > 0 {
			newFile, err = ApplyLineEdits(newFile, edits)
			if err != nil {
				return nil, nil, err
			}
		}
	}

	return newFile, nil, nil
}

// updateTypeDocOneComment returns new file contents by applying a single comment from ps to contents. If it does so, it *mutates* ps.ast by deleting the applied comment, which is okay
// because we don't need ps.ast's byte positioning information. If there are no comments to apply (ex: no comments in snippet ast; TODO any more?), return values are (nil, false, nil,
// nil). If a comment was applied, it returns (new contents, true, nil, nil). If a comment was rejected, it returns (nil, true, nil, nil).
func updateTypeDocOneComment(contents []byte, contentsAST *ast.File, fileSet *token.FileSet, genDecl *ast.GenDecl, snippetTypeSpec *ast.TypeSpec, ps *parsedSnippet, options Options) ([]byte, bool, *SnippetError, error) {

	// Here's how Go handles comments on types:
	// There's non-block types like this:
	//   type Foo int
	// and then there's block types like this:
	//   type (
	//     Foo int
	//   )
	// For non-block types:
	// - the comment above is in genDecl.Doc
	// - the typeSpec.Doc is nil
	// For block types:
	// - the comment above the 'type (' is in genDecl.Doc
	// - the comment above the spec is in typeSpec.Doc
	// For both:
	// - any comment suffixes are in typeSpec.Comment
	// However, if the type Expr has a line comment (ex: type Foo struct { // comment), the comment is not in the typeSpec.Comment. Howver, it is if the line comment is at the end (ex: "} // comment")

	snippetGenDeclComment := commentBlockFromGroup(genDecl.Doc, false)
	snippetEolComment := commentBlockFromGroup(snippetTypeSpec.Comment, false)
	snippetIsBlock := genDecl.Lparen.IsValid()

	typeName := snippetTypeSpec.Name.Name

	var newFileBytes []byte

	goFileAST := contentsAST
	for _, decl := range goFileAST.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok == token.TYPE {
				for _, spec := range d.Specs {
					fileTypeSpec := spec.(*ast.TypeSpec)

					if fileTypeSpec.Name.Name == typeName {
						// Make sure the types are compatible:
						// NOTE: because we only apply one comment at a time, this validation is run multiple times for the same type. If need to optimize, we can.
						if !typesSameShape(fileTypeSpec.Type, snippetTypeSpec.Type) {
							return nil, false, &SnippetError{Snippet: ps.originalSnippet, UserErrorMessage: "Source type does not match type in snippet."}, nil
						}

						fileIsBlock := d.Lparen.IsValid()

						if snippetIsBlock && !fileIsBlock {
							// There's no reason we should attempt to handle this. Snippet should not use a block here.
							return nil, false, &SnippetError{Snippet: ps.originalSnippet, UserErrorMessage: "Source is not a block (aka: not using parens), but snippet is a block. Make sure snippet matches source."}, nil
						} else if !snippetIsBlock && fileIsBlock {
							// This is plausible to handle because AI may have seen package documentation (which normalizes blocks -> non blocks), it may attempt to comment it as a standalone type.
							// Beyond that, it is possible to handle using some straightforward logic.
							// That being said, it's currently not implemented.
							return nil, false, &SnippetError{Snippet: ps.originalSnippet, UserErrorMessage: "Source is a block (aka: not using parens), but snippet is not a block. Make sure snippet matches source."}, nil
						} else if !fileIsBlock && !snippetIsBlock {

							// Handle application of gen decl comment:
							if snippetGenDeclComment != "" {
								genDecl.Doc = nil // consume the documentation

								// If the existing code has a comment (EOL or Doc), and we we're rejecting updates, then reject it:
								if !hasNoDocs(fileTypeSpec.Comment) || !hasNoDocs(d.Doc) {
									if options.RejectUpdates {
										ps.partiallyRejected = true
										return nil, true, nil, nil
									}
								}

								// Delete EOL comment if it exists:
								if !hasNoDocs(fileTypeSpec.Comment) {
									startOffset := fileSet.Position(fileTypeSpec.Comment.Pos()).Offset
									endOffset := fileSet.Position(fileTypeSpec.Comment.End()).Offset
									contents = deleteRangeInBytes(contents, startOffset, endOffset, false)
								}

								// Splice the main comment in:
								if hasNoDocs(d.Doc) {
									startOffset := fileSet.Position(d.Pos()).Offset // Get offset of "type" keyword:
									newFileBytes = spliceStringIntoBytes(contents, snippetGenDeclComment, startOffset, startOffset)
								} else {
									// Get offsets of comment and "type" keyword:
									startOffset := fileSet.Position(d.Doc.Pos()).Offset
									endOffset := fileSet.Position(d.Pos()).Offset

									newFileBytes = spliceStringIntoBytes(contents, snippetGenDeclComment, startOffset, endOffset)
								}

								return newFileBytes, true, nil, nil
							}

							// Handle application of EOL comment:
							if snippetEolComment != "" {
								snippetTypeSpec.Comment = nil // consume the documentation

								// If the existing code has a comment (EOL or Doc), and we we're rejecting updates, then reject it:
								if !hasNoDocs(fileTypeSpec.Comment) || !hasNoDocs(d.Doc) {
									if options.RejectUpdates {
										ps.partiallyRejected = true
										return nil, true, nil, nil
									}
								}

								if hasNoDocs(fileTypeSpec.Comment) {
									// Get offset of "type" keyword:
									startOffset := fileSet.Position(fileTypeSpec.End()).Offset

									newFileBytes = spliceStringIntoBytes(contents, snippetEolComment, startOffset, startOffset)
								} else {
									startOffset := fileSet.Position(fileTypeSpec.End()).Offset
									endOffset := fileSet.Position(fileTypeSpec.Comment.End()).Offset
									newFileBytes = spliceStringIntoBytes(contents, snippetEolComment, startOffset, endOffset)
								}

								// delete Doc comment if it exists
								if !hasNoDocs(d.Doc) {
									startOffset := fileSet.Position(d.Doc.Pos()).Offset
									endOffset := fileSet.Position(d.Doc.End()).Offset
									newFileBytes = deleteRangeInBytes(newFileBytes, startOffset, endOffset, true)
								}

								return newFileBytes, true, nil, nil
							}

							switch t := snippetTypeSpec.Type.(type) {
							case *ast.StructType:
								sourceStruct, ok := fileTypeSpec.Type.(*ast.StructType)
								if !ok {
									return nil, false, nil, fmt.Errorf("source type isn't a struct but snippet is. How did validation miss this?")
								}

								newContents, shouldContinue, err := updateStructTypeDoc(contents, fileSet, sourceStruct, t, ps, options)
								if err != nil {
									return nil, false, nil, err
								}
								if shouldContinue {
									return newContents, true, nil, nil
								}
							case *ast.InterfaceType:
								sourceInterface, ok := fileTypeSpec.Type.(*ast.InterfaceType)
								if !ok {
									return nil, false, nil, fmt.Errorf("source type isn't an interface but snippet is. How did validation miss this?")
								}

								newContents, shouldContinue, err := updateInterfaceTypeDoc(contents, fileSet, sourceInterface, t, ps, options)
								if err != nil {
									return nil, false, nil, err
								}
								if shouldContinue {
									return newContents, true, nil, nil
								}
							}
						} else if fileIsBlock && snippetIsBlock {
							// Handling when both snippet and source use a type block.
							// We iterate through EACH TypeSpec in the snippet. As soon as we
							// successfully apply one comment, we return so the outer loop can
							// re-parse the file and continue processing remaining comments.

							// First, handle the block-level doc comment (same for all specs).
							if snippetGenDeclComment != "" {
								genDecl.Doc = nil // consume
								if !hasNoDocs(d.Doc) && options.RejectUpdates {
									ps.partiallyRejected = true
									return nil, true, nil, nil
								}
								if hasNoDocs(d.Doc) {
									start := fileSet.Position(d.Pos()).Offset
									newFileBytes = spliceStringIntoBytes(contents, snippetGenDeclComment, start, start)
								} else {
									start := fileSet.Position(d.Doc.Pos()).Offset
									end := fileSet.Position(d.Pos()).Offset
									newFileBytes = spliceStringIntoBytes(contents, snippetGenDeclComment, start, end)
								}
								return newFileBytes, true, nil, nil
							}

							// Next, iterate over each TypeSpec in the snippet block.
							for _, rawSpec := range genDecl.Specs {
								snSpec := rawSpec.(*ast.TypeSpec)
								if (snSpec.Doc == nil || len(snSpec.Doc.List) == 0) && (snSpec.Comment == nil || len(snSpec.Comment.List) == 0) {
									continue // nothing to apply for this spec
								}

								// Find matching source spec within the same block.
								var srcSpec *ast.TypeSpec
								var srcIdx int
								for i, fs := range d.Specs {
									ts := fs.(*ast.TypeSpec)
									if ts.Name.Name == snSpec.Name.Name {
										srcSpec = ts
										srcIdx = i
										break
									}
								}

								if srcSpec == nil {
									return nil, false, nil, fmt.Errorf("could not find matching type spec in source block")
								}

								// Validate type shape matches.
								if !typesSameShape(srcSpec.Type, snSpec.Type) {
									return nil, false, &SnippetError{Snippet: ps.originalSnippet, UserErrorMessage: "Source type does not match type in snippet."}, nil
								}

								// Apply spec-level doc comment if present.
								if snSpec.Doc != nil {
									docBlock := commentBlockFromGroup(snSpec.Doc, srcIdx > 0)
									snSpec.Doc = nil // consume

									if (!hasNoDocs(srcSpec.Doc) || !hasNoDocs(srcSpec.Comment)) && options.RejectUpdates {
										ps.partiallyRejected = true
										return nil, true, nil, nil
									}

									// Remove existing EOL comment if any.
									if !hasNoDocs(srcSpec.Comment) {
										start := fileSet.Position(srcSpec.Comment.Pos()).Offset
										end := fileSet.Position(srcSpec.Comment.End()).Offset
										contents = deleteRangeInBytes(contents, start, end, false)
									}

									if hasNoDocs(srcSpec.Doc) {
										start := fileSet.Position(srcSpec.Pos()).Offset
										newFileBytes = spliceStringIntoBytes(contents, docBlock, start, start)
									} else {
										start := fileSet.Position(srcSpec.Doc.Pos()).Offset
										end := fileSet.Position(srcSpec.Pos()).Offset
										newFileBytes = spliceStringIntoBytes(contents, docBlock, start, end)
									}

									return newFileBytes, true, nil, nil
								}

								// Apply EOL comment if present.
								if snSpec.Comment != nil {
									eol := eolCommentFromGroup(snSpec.Comment)
									snSpec.Comment = nil

									if (!hasNoDocs(srcSpec.Comment) || !hasNoDocs(srcSpec.Doc)) && options.RejectUpdates {
										ps.partiallyRejected = true
										return nil, true, nil, nil
									}

									if hasNoDocs(srcSpec.Comment) {
										insert := fileSet.Position(srcSpec.End()).Offset
										newFileBytes = spliceStringIntoBytes(contents, eol, insert, insert)
									} else {
										start := fileSet.Position(srcSpec.End()).Offset
										end := fileSet.Position(srcSpec.Comment.End()).Offset
										newFileBytes = spliceStringIntoBytes(contents, eol, start, end)
									}

									// Remove existing doc comment if any.
									if !hasNoDocs(srcSpec.Doc) {
										start := fileSet.Position(srcSpec.Doc.Pos()).Offset
										end := fileSet.Position(srcSpec.Doc.End()).Offset
										newFileBytes = deleteRangeInBytes(newFileBytes, start, end, true)
									}

									return newFileBytes, true, nil, nil
								}
							}
						} else {
							panic("unreachable")
						}

						// Once we've considered the type, we know we're done. We'd have returned earlier if we made a change, we we know we didn't make a change.
						return nil, false, nil, nil
					}
				}
			}
		}
	}

	return nil, false, nil, fmt.Errorf("did not find type decl")
}

// updateStructTypeDoc recursively handles struct field documentation updates. If a change was made, returns (new contents, true, nil). If a change was rejected, returns (nil, true,
// nil). Otherwise, returns (nil, false, nil or error) -- aka, don't continue, we're done.
func updateStructTypeDoc(contents []byte, fileSet *token.FileSet, sourceStruct *ast.StructType, snippetStruct *ast.StructType, ps *parsedSnippet, options Options) ([]byte, bool, error) {
	if snippetStruct.Fields == nil {
		return nil, false, nil
	}

	// Create a map of source field key to their fields
	sourceFields := make(map[string]*ast.Field)
	sourceFieldIndexes := make(map[string]int)
	for i, field := range sourceStruct.Fields.List {
		key := fieldKey(field)
		sourceFields[key] = field
		sourceFieldIndexes[key] = i
	}

	// Iterate through snippet fields and apply documentation
	for _, field := range snippetStruct.Fields.List {
		key := fieldKey(field)
		sourceField := sourceFields[key]
		if sourceField == nil {
			return nil, false, fmt.Errorf("couldn't find source field. How did validation miss this?")
		}
		sourceFieldIndex := sourceFieldIndexes[key]

		// Handle field comments
		if field.Comment != nil {
			fieldComment := field.Comment
			field.Comment = nil // consume the comment

			// If the existing code has a comment (EOL or Doc), and we we're rejecting updates, then reject it:
			if sourceField.Comment != nil || !hasNoDocs(sourceField.Doc) {
				if options.RejectUpdates {
					ps.partiallyRejected = true
					return nil, true, nil
				}
			}

			// Determine if the struct is defined on a single line, ex: `type C struct { Z int }`
			needNewlineAfterComment := false
			if sourceStruct != nil {
				fieldEndLine := fileSet.Position(sourceField.End()).Line
				structEndLine := fileSet.Position(sourceStruct.End()).Line
				if fieldEndLine == structEndLine {
					needNewlineAfterComment = true
				}
			}

			commentText := eolCommentFromGroup(fieldComment)
			if needNewlineAfterComment {
				commentText += "\n"
			}

			// There's a line comment. Delete doc comment and inject this line comment.
			if sourceField.Comment == nil {
				startOffset := fileSet.Position(sourceField.End()).Offset
				contents = spliceStringIntoBytes(contents, commentText, startOffset, startOffset)
			} else {
				startOffset := fileSet.Position(sourceField.End()).Offset
				endOffset := fileSet.Position(sourceField.Comment.End()).Offset
				contents = spliceStringIntoBytes(contents, commentText, startOffset, endOffset)
			}

			if !hasNoDocs(sourceField.Doc) {
				startOffset := fileSet.Position(sourceField.Doc.Pos()).Offset
				endOffset := fileSet.Position(sourceField.Doc.End()).Offset
				contents = deleteRangeInBytes(contents, startOffset, endOffset, true)
			}

			return contents, true, nil
		} else if field.Doc != nil {
			fieldDoc := field.Doc
			field.Doc = nil // consume the comment

			// If the existing code has a comment (EOL or Doc), and we we're rejecting updates, then reject it:
			if sourceField.Comment != nil || !hasNoDocs(sourceField.Doc) {
				if options.RejectUpdates {
					ps.partiallyRejected = true
					return nil, true, nil
				}
			}

			// Delete line comment:
			if !hasNoDocs(sourceField.Comment) {
				startOffset := fileSet.Position(sourceField.Comment.Pos()).Offset
				endOffset := fileSet.Position(sourceField.Comment.End()).Offset
				contents = deleteRangeInBytes(contents, startOffset, endOffset, false)
			}

			// Determine if we need to add a leading newline before the doc comment (single-line struct case, ex: `type C struct { Z int }`).
			needNewlineBeforeComment := false
			if sourceStruct != nil {
				openingLine := fileSet.Position(sourceStruct.Fields.Opening).Line
				fieldLine := fileSet.Position(sourceField.Pos()).Line
				if openingLine == fieldLine {
					needNewlineBeforeComment = true
				}
			}

			commentText := commentBlockFromGroup(fieldDoc, needNewlineBeforeComment || sourceFieldIndex > 0)

			if sourceField.Doc == nil {
				startOffset := fileSet.Position(sourceField.Pos()).Offset
				contents = spliceStringIntoBytes(contents, commentText, startOffset, startOffset)
			} else {
				startOffset := fileSet.Position(sourceField.Doc.Pos()).Offset
				endOffset := fileSet.Position(sourceField.Pos()).Offset
				contents = spliceStringIntoBytes(contents, commentText, startOffset, endOffset)
			}

			return contents, true, nil
		}

		// Recursively handle nested struct fields
		if sourceStructType, ok := sourceField.Type.(*ast.StructType); ok {
			if snippetStructType, ok := field.Type.(*ast.StructType); ok {
				newContents, shouldContinue, err := updateStructTypeDoc(contents, fileSet, sourceStructType, snippetStructType, ps, options)
				if err != nil {
					return nil, false, err
				}
				if shouldContinue {
					return newContents, true, nil
				}
			}
		}
	}

	return nil, false, nil
}

// updateInterfaceTypeDoc handles interface documentation updates. If a change was made, returns (new contents, true, nil). If a change was rejected, returns (nil, true, nil). Otherwise
// returns (nil, false, nil or error).
func updateInterfaceTypeDoc(contents []byte, fileSet *token.FileSet, sourceInterface *ast.InterfaceType, snippetInterface *ast.InterfaceType, ps *parsedSnippet, options Options) ([]byte, bool, error) {
	if snippetInterface.Methods == nil {
		return nil, false, nil
	}

	// Build a lookup of source methods/embedded interfaces keyed by fieldKey.
	sourceMethods := make(map[string]*ast.Field)
	sourceMethodIndexes := make(map[string]int)
	for i, m := range sourceInterface.Methods.List {
		key := fieldKey(m)
		sourceMethods[key] = m
		sourceMethodIndexes[key] = i
	}

	// Iterate through snippet methods/embedded interfaces and apply documentation.
	for _, method := range snippetInterface.Methods.List {
		key := fieldKey(method)
		srcMethod := sourceMethods[key]
		if srcMethod == nil {
			return nil, false, fmt.Errorf("couldn't find source method. How did validation miss this?")
		}
		srcMethodIndex := sourceMethodIndexes[key]

		// End-of-line comment processing.
		if method.Comment != nil {
			mComment := method.Comment
			method.Comment = nil // mark as consumed

			// If existing code already has docs and we reject updates, bail out.
			if srcMethod.Comment != nil || !hasNoDocs(srcMethod.Doc) {
				if options.RejectUpdates {
					ps.partiallyRejected = true
					return nil, true, nil
				}
			}

			// Determine if we need a newline AFTER the inserted EOL comment (single-line interface case, ex: `type C interface { Bar() }`)
			needNewlineAfterComment := false
			if sourceInterface != nil {
				methodEndLine := fileSet.Position(srcMethod.End()).Line
				interfaceEndLine := fileSet.Position(sourceInterface.Methods.Closing).Line
				if methodEndLine == interfaceEndLine {
					needNewlineAfterComment = true
				}
			}

			commentText := eolCommentFromGroup(mComment)
			if needNewlineAfterComment {
				commentText += "\n"
			}

			// Replace or insert EOL comment.
			if srcMethod.Comment == nil {
				insertPos := fileSet.Position(srcMethod.End()).Offset
				contents = spliceStringIntoBytes(contents, commentText, insertPos, insertPos)
			} else {
				start := fileSet.Position(srcMethod.End()).Offset
				end := fileSet.Position(srcMethod.Comment.End()).Offset
				contents = spliceStringIntoBytes(contents, commentText, start, end)
			}

			// Remove any existing doc comment if present.
			if !hasNoDocs(srcMethod.Doc) {
				start := fileSet.Position(srcMethod.Doc.Pos()).Offset
				end := fileSet.Position(srcMethod.Doc.End()).Offset
				contents = deleteRangeInBytes(contents, start, end, true)
			}

			return contents, true, nil
		}

		// Block doc comment processing.
		if method.Doc != nil {
			mDoc := method.Doc
			method.Doc = nil // consume

			// Reject updates if required.
			if srcMethod.Comment != nil || !hasNoDocs(srcMethod.Doc) {
				if options.RejectUpdates {
					ps.partiallyRejected = true
					return nil, true, nil
				}
			}

			// Remove existing EOL comment if any.
			if srcMethod.Comment != nil {
				start := fileSet.Position(srcMethod.Comment.Pos()).Offset
				end := fileSet.Position(srcMethod.Comment.End()).Offset
				contents = deleteRangeInBytes(contents, start, end, false)
			}

			// Determine if newline needed before comment (single-line interface, ex: `type C interface { Bar() }`).
			needNewlineBeforeComment := false
			if sourceInterface != nil {
				openingLine := fileSet.Position(sourceInterface.Methods.Opening).Line
				methodLine := fileSet.Position(srcMethod.Pos()).Line
				if openingLine == methodLine {
					needNewlineBeforeComment = true
				}
			}

			commentText := commentBlockFromGroup(mDoc, needNewlineBeforeComment || srcMethodIndex > 0)

			if srcMethod.Doc == nil {
				insertPos := fileSet.Position(srcMethod.Pos()).Offset
				contents = spliceStringIntoBytes(contents, commentText, insertPos, insertPos)
			} else {
				start := fileSet.Position(srcMethod.Doc.Pos()).Offset
				end := fileSet.Position(srcMethod.Pos()).Offset
				contents = spliceStringIntoBytes(contents, commentText, start, end)
			}

			return contents, true, nil
		}
	}

	return nil, false, nil
}

// identsKey generates a key for names in a struct field. ex: Foo, Bar int -> "Foo&Bar"; A int -> "A". Useful to map between two structs' fields. TODO: can idents be _? If so, what
// breaks? Like multiple _ in a var block.
func identsKey(idents []*ast.Ident) string {
	if len(idents) == 0 {
		panic("expected some idents")
	}
	if len(idents) == 1 {
		return idents[0].Name
	} else {
		var names []string
		for _, n := range idents {
			names = append(names, n.Name)
		}
		return strings.Join(names, "&")
	}
}

func fieldKey(field *ast.Field) string {
	if len(field.Names) > 0 {
		return identsKey(field.Names)
	}

	switch t := field.Type.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr: // package selector
		if pkg, ok := t.X.(*ast.Ident); ok {
			return fmt.Sprintf("%s.%s", pkg.Name, t.Sel.Name)
		}
		panic("fieldKey: unexpectedly didn't find ident in selector expression")
	case *ast.StarExpr: // pointer
		switch x := t.X.(type) {
		case *ast.Ident: // *Bar
			return "*" + x.Name
		case *ast.SelectorExpr: // *pkg.Bar
			if pkg, ok := x.X.(*ast.Ident); ok {
				return fmt.Sprintf("*%s.%s", pkg.Name, x.Sel.Name)
			}
		case *ast.IndexExpr: // *Generic[T]
			if ident, ok := x.X.(*ast.Ident); ok {
				return "*" + ident.Name
			}
			if sel, ok := x.X.(*ast.SelectorExpr); ok {
				if pkg, ok := sel.X.(*ast.Ident); ok {
					return fmt.Sprintf("*%s.%s", pkg.Name, sel.Sel.Name)
				}
			}
			panic("fieldKey: unhandled IndexExpr case in StarExpr")
		case *ast.IndexListExpr: // *Generic[A, B]
			if ident, ok := x.X.(*ast.Ident); ok {
				return "*" + ident.Name
			}
			if sel, ok := x.X.(*ast.SelectorExpr); ok {
				if pkg, ok := sel.X.(*ast.Ident); ok {
					return fmt.Sprintf("*%s.%s", pkg.Name, sel.Sel.Name)
				}
			}
			panic("fieldKey: unhandled IndexListExpr case in StarExpr")
		}
		panic("fieldKey: unexpectedly didn't find case in start expression")
	case *ast.IndexExpr: // Generic[T]
		// Handle instantiated generic types by ignoring their type parameters and using the underlying type name
		if ident, ok := t.X.(*ast.Ident); ok {
			return ident.Name
		}
		if sel, ok := t.X.(*ast.SelectorExpr); ok {
			if pkg, ok := sel.X.(*ast.Ident); ok {
				return fmt.Sprintf("%s.%s", pkg.Name, sel.Sel.Name)
			}
		}
		panic("fieldKey: unhandled IndexExpr case")
	case *ast.IndexListExpr: // Generic[A, B]
		// Same logic as IndexExpr but for multiple type parameters
		if ident, ok := t.X.(*ast.Ident); ok {
			return ident.Name
		}
		if sel, ok := t.X.(*ast.SelectorExpr); ok {
			if pkg, ok := sel.X.(*ast.Ident); ok {
				return fmt.Sprintf("%s.%s", pkg.Name, sel.Sel.Name)
			}
		}
		panic("fieldKey: unhandled IndexListExpr case")
	case *ast.UnaryExpr: // ~T (approximate type constraint)
		if t.Op == token.TILDE {
			switch x := t.X.(type) {
			case *ast.Ident:
				return "~" + x.Name
			case *ast.SelectorExpr:
				if pkg, ok := x.X.(*ast.Ident); ok {
					return fmt.Sprintf("~%s.%s", pkg.Name, x.Sel.Name)
				}
			}
		}
	case *ast.BinaryExpr: // union constraints like A|B
		if t.Op == token.OR {
			// helper closure to compute key for an arbitrary expression
			var exprKey func(side string, e ast.Expr) string
			exprKey = func(side string, e ast.Expr) string {
				switch ex := e.(type) {
				case *ast.Ident:
					return ex.Name
				case *ast.SelectorExpr:
					if pkg, ok := ex.X.(*ast.Ident); ok {
						return fmt.Sprintf("%s.%s", pkg.Name, ex.Sel.Name)
					}
				case *ast.IndexExpr: // Generic[T]
					if ident, ok := ex.X.(*ast.Ident); ok {
						return ident.Name
					}
					if sel, ok := ex.X.(*ast.SelectorExpr); ok {
						if pkg, ok := sel.X.(*ast.Ident); ok {
							return fmt.Sprintf("%s.%s", pkg.Name, sel.Sel.Name)
						}
					}
				case *ast.IndexListExpr: // Generic[A, B]
					if ident, ok := ex.X.(*ast.Ident); ok {
						return ident.Name
					}
					if sel, ok := ex.X.(*ast.SelectorExpr); ok {
						if pkg, ok := sel.X.(*ast.Ident); ok {
							return fmt.Sprintf("%s.%s", pkg.Name, sel.Sel.Name)
						}
					}
				case *ast.StarExpr:
					if ident, ok := ex.X.(*ast.Ident); ok {
						return "*" + ident.Name
					}
					if sel, ok := ex.X.(*ast.SelectorExpr); ok {
						if pkg, ok := sel.X.(*ast.Ident); ok {
							return fmt.Sprintf("*%s.%s", pkg.Name, sel.Sel.Name)
						}
					}
					if idx, ok := ex.X.(*ast.IndexExpr); ok {
						// treat pointer to instantiated generic as pointer to base type
						return "*" + exprKey(side, idx.X)
					}
					if idxl, ok := ex.X.(*ast.IndexListExpr); ok {
						return "*" + exprKey(side, idxl.X)
					}
				case *ast.UnaryExpr:
					if ex.Op == token.TILDE {
						return "~" + exprKey(side, ex.X)
					}
				case *ast.ParenExpr:
					return exprKey(side, ex.X)
				case *ast.BinaryExpr:
					if ex.Op == token.OR {
						// Support nested union chains, flattening via recursion
						left := exprKey(side, ex.X)
						right := exprKey(side, ex.Y)
						return left + "|" + right
					}
				}
				// Include detailed information to aid debugging when we hit an unhandled operand
				panic(fmt.Errorf(
					"fieldKey: unhandled binary expr operand: side=%s e_kind=%T e_str=%q bin_kind=%T bin_str=%q",
					side,
					e,
					types.ExprString(e),
					t,
					types.ExprString(t),
				))
			}
			leftKey := exprKey("left", t.X)
			rightKey := exprKey("right", t.Y)
			return leftKey + "|" + rightKey
		}
		// if not OR, fallthrough to panic
	}
	panic("fieldKey: unexpectedly didn't find case for field.Type")
}

// typesSameShape returns true if source and snippet are both types of the same kind (ex: both int; both struct) and, in the case of struct, snippet's struct fields are a subset of
// source's fields. This func is intended to be used for the purpose of applying documentation to source via a snippet. We allow fields to be elided in the snippet if there are no docs
// we intend to apply with it. However, we want to make sure that the snippet is compatible with the source.
//
// Examples, with stylized syntax for brevity: typesSameShape(int, int) -> true typesSameShape(int, int64) -> false typesSameShape(int, struct{}) -> false typesSameShape(int, myIntType)
// -> false // even if 'type myIntType int' typesSameShape(struct{}, struct{}) -> true typesSameShape(struct{Foo int, Bar string}, struct{Foo int, Bar string}) -> true typesSameShape(struct{Foo
// int, Bar string}, struct{Bar string}) -> true typesSameShape(struct{Foo int, Bar string}, struct{}) -> true typesSameShape(struct{Foo int, Bar string}, struct{Baz int}) -> false
// // Baz is not present in source's struct typesSameShape(struct{Foo int, Bar string}, struct{Foo string}) -> false // Foo is a different type typesSameShape(struct{Foo int, Bar string},
// struct{Foo int, Bar string, Baz int}) -> false // Baz is not present in source's struct typesSameShape(struct{Foo struct{Bar string Baz string}}, struct{Foo struct{Bar string}})
// -> true // nested structs are handled recursively typesSameShape(struct{Foo int}, Bar) -> false // even if Bar is type struct {Foo int}, they are different types typesSameShape(interface{},
// interface{}) -> true typesSameShape(interface{Foo()}, interface{}) -> true typesSameShape(interface{Foo()}, interface{Foo()}) -> true typesSameShape(interface{Foo(), Bar()}, interface{Foo()})
// -> true typesSameShape(interface{Foo()}, interface{Foo(), Bar()}) -> false typesSameShape(interface{Foo(int)}, interface{Foo(int)}) -> true typesSameShape(interface{Foo(int)}, interface{Foo(string)})
// -> false typesSameShape(interface{Foo(int)}, interface{Foo(int, string)}) -> false typesSameShape(interface{Foo(int, string)}, interface{Foo(int)}) -> false typesSameShape(struct{A,
// B int}, struct{A, B int}) -> true // multiple field names for same field must match exactly typesSameShape(struct{A, B int}, struct{A int}) -> false // see above
func typesSameShape(source ast.Expr, snippet ast.Expr) bool {
	// Handle basic type comparisons
	switch s := source.(type) {
	case *ast.Ident:
		// If source is an identifier, snippet must be the same identifier
		if snippetIdent, ok := snippet.(*ast.Ident); ok {
			return s.Name == snippetIdent.Name
		}
		return false

	case *ast.SelectorExpr:
		// If source is a selector expression, snippet must be the same selector
		if snippetSel, ok := snippet.(*ast.SelectorExpr); ok {
			// Compare both package and name
			return typesSameShape(s.X, snippetSel.X) && s.Sel.Name == snippetSel.Sel.Name
		}
		return false

	case *ast.StarExpr:
		// If source is a pointer, snippet must also be a pointer
		if snippetStar, ok := snippet.(*ast.StarExpr); ok {
			// Compare element types
			return typesSameShape(s.X, snippetStar.X)
		}
		return false

	case *ast.StructType:
		// If source is a struct, snippet must also be a struct
		snippetStruct, ok := snippet.(*ast.StructType)
		if !ok {
			return false
		}

		// If snippet has no fields, it's always a valid subset
		if snippetStruct.Fields == nil || len(snippetStruct.Fields.List) == 0 {
			return true
		}

		// If source has no fields but snippet does, it's not a valid subset
		if s.Fields == nil || len(s.Fields.List) == 0 {
			return false
		}

		// Create a map of source field to their types
		sourceFieldSets := make(map[string]ast.Expr)
		for _, field := range s.Fields.List {
			sourceFieldSets[fieldKey(field)] = field.Type
		}

		// Check that each snippet field exists in source with the same type
		for _, field := range snippetStruct.Fields.List {
			sourceType, ok := sourceFieldSets[fieldKey(field)]
			if !ok {
				return false
			}
			if !typesSameShape(sourceType, field.Type) {
				return false
			}
		}
		return true

	case *ast.ArrayType:
		// If source is an array, snippet must also be an array
		if snippetArray, ok := snippet.(*ast.ArrayType); ok {
			// Check if both are arrays (not slices)
			sourceIsArray := s.Len != nil
			snippetIsArray := snippetArray.Len != nil

			// If one is a slice and the other is an array, they're not the same shape
			if sourceIsArray != snippetIsArray {
				return false
			}

			// If both are arrays, compare their lengths
			if sourceIsArray && snippetIsArray {
				// The length can be a basic literal (e.g. "5"), an identifier (e.g. "n"),
				// or a constant arithmetic expression (e.g. "n+1"). We consider the two
				// array types to have the same shape only if the *expressions* representing
				// their lengths are structurally equivalent.
				if !exprEqual(s.Len, snippetArray.Len) {
					return false // Different array lengths
				}
			}

			// Compare element types
			return typesSameShape(s.Elt, snippetArray.Elt)
		}
		return false

	case *ast.MapType:
		// If source is a map, snippet must also be a map
		if snippetMap, ok := snippet.(*ast.MapType); ok {
			// Compare key and value types
			return typesSameShape(s.Key, snippetMap.Key) && typesSameShape(s.Value, snippetMap.Value)
		}
		return false

	case *ast.InterfaceType:
		// If source is an interface, snippet must also be an interface
		if snippetInterface, ok := snippet.(*ast.InterfaceType); ok {
			// Empty interface is always a valid subset
			if snippetInterface.Methods == nil || len(snippetInterface.Methods.List) == 0 {
				return true
			}
			// If source has no methods but snippet does, it's not a valid subset
			if s.Methods == nil || len(s.Methods.List) == 0 {
				return false
			}

			// Create a map of source method names to their types
			sourceMethods := make(map[string]*ast.FuncType)
			for _, method := range s.Methods.List {
				for _, name := range method.Names {
					if funcType, ok := method.Type.(*ast.FuncType); ok {
						sourceMethods[name.Name] = funcType
					}
				}
			}

			// Check that each snippet method exists in source with the same signature
			for _, method := range snippetInterface.Methods.List {
				for _, name := range method.Names {
					sourceFunc, exists := sourceMethods[name.Name]
					if !exists {
						return false
					}
					if funcType, ok := method.Type.(*ast.FuncType); ok {
						// Compare parameter and result lists
						if !compareFieldList(sourceFunc.Params, funcType.Params) {
							return false
						}
						if !compareFieldList(sourceFunc.Results, funcType.Results) {
							return false
						}
					} else {
						return false
					}
				}
			}
			return true
		}
		return false

	case *ast.ChanType:
		// If source is a channel, snippet must also be a channel
		if snippetChan, ok := snippet.(*ast.ChanType); ok {
			// Compare direction and element type
			return s.Dir == snippetChan.Dir && typesSameShape(s.Value, snippetChan.Value)
		}
		return false

	case *ast.FuncType:
		// If source is a function type, snippet must also be a function type
		if snippetFunc, ok := snippet.(*ast.FuncType); ok {
			// Compare parameters and results
			if !compareFieldList(s.Params, snippetFunc.Params) {
				return false
			}
			return compareFieldList(s.Results, snippetFunc.Results)
		}
		return false

	default:
		// For other types, they must be exactly the same
		return fmt.Sprintf("%T", source) == fmt.Sprintf("%T", snippet)
	}
}

// Helper function to compare function parameter/result lists.
func compareFieldList(source, snippet *ast.FieldList) bool {
	if source == nil && snippet == nil {
		return true
	}
	if source == nil || snippet == nil {
		return false
	}
	if len(source.List) != len(snippet.List) {
		return false
	}
	for i, sourceField := range source.List {
		snippetField := snippet.List[i]
		if !typesSameShape(sourceField.Type, snippetField.Type) {
			return false
		}
	}
	return true
}

// exprEqual returns true if two expressions are structurally equivalent.
func exprEqual(expr1, expr2 ast.Expr) bool {
	switch t1 := expr1.(type) {
	case *ast.BasicLit:
		t2, ok := expr2.(*ast.BasicLit)
		if !ok {
			return false
		}
		return t1.Value == t2.Value
	case *ast.Ident:
		t2, ok := expr2.(*ast.Ident)
		if !ok {
			return false
		}
		return t1.Name == t2.Name
	case *ast.SelectorExpr:
		t2, ok := expr2.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		return exprEqual(t1.X, t2.X) && t1.Sel.Name == t2.Sel.Name
	case *ast.StarExpr:
		t2, ok := expr2.(*ast.StarExpr)
		if !ok {
			return false
		}
		return exprEqual(t1.X, t2.X)
	case *ast.ArrayType:
		t2, ok := expr2.(*ast.ArrayType)
		if !ok {
			return false
		}
		return exprEqual(t1.Len, t2.Len) && typesSameShape(t1.Elt, t2.Elt)
	case *ast.MapType:
		t2, ok := expr2.(*ast.MapType)
		if !ok {
			return false
		}
		return typesSameShape(t1.Key, t2.Key) && typesSameShape(t1.Value, t2.Value)
	case *ast.InterfaceType:
		t2, ok := expr2.(*ast.InterfaceType)
		if !ok {
			return false
		}
		return t1.Methods == nil && t2.Methods == nil || compareFieldList(t1.Methods, t2.Methods)
	case *ast.ChanType:
		t2, ok := expr2.(*ast.ChanType)
		if !ok {
			return false
		}
		return t1.Dir == t2.Dir && typesSameShape(t1.Value, t2.Value)
	case *ast.FuncType:
		t2, ok := expr2.(*ast.FuncType)
		if !ok {
			return false
		}
		return compareFieldList(t1.Params, t2.Params) && compareFieldList(t1.Results, t2.Results)
	case *ast.BinaryExpr:
		t2, ok := expr2.(*ast.BinaryExpr)
		if !ok {
			return false
		}
		return t1.Op == t2.Op && exprEqual(t1.X, t2.X) && exprEqual(t1.Y, t2.Y)
	case *ast.CallExpr:
		t2, ok := expr2.(*ast.CallExpr)
		if !ok {
			return false
		}
		if !exprEqual(t1.Fun, t2.Fun) {
			return false
		}
		if len(t1.Args) != len(t2.Args) {
			return false
		}
		for i := range t1.Args {
			if !exprEqual(t1.Args[i], t2.Args[i]) {
				return false
			}
		}
		return true
	case *ast.ParenExpr:
		t2, ok := expr2.(*ast.ParenExpr)
		if !ok {
			return false
		}
		return exprEqual(t1.X, t2.X)
	case *ast.UnaryExpr:
		t2, ok := expr2.(*ast.UnaryExpr)
		if !ok {
			return false
		}
		return t1.Op == t2.Op && exprEqual(t1.X, t2.X)
	default:
		return false
	}
}
