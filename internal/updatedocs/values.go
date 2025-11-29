package updatedocs

import (
	"fmt"
	"github.com/codalotl/codalotl/internal/gocode"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
)

func updateValueDoc(pkg *gocode.Package, ps *parsedSnippet, options Options) (*gocode.File, *SnippetError, error) {
	if ps.kind != snippetKindVar && ps.kind != snippetKindConst {
		panic("expected value kind")
	}
	if len(ps.ast.Decls) == 0 {
		panic("expected exactly >= 1 decl")
	}

	//
	// We want all identifiers to be part of a single file. So we need to find that file, and ensure all identifiers are in it.
	//

	identMap := identifierToFileMap(pkg)
	identifiersInSnippet := identifiersInFile(ps.ast, ps.fileSet)
	foundFileName := ""
	for _, ident := range identifiersInSnippet {
		if fileName, ok := identMap[ident]; ok {
			if foundFileName == "" {
				foundFileName = fileName
			} else if foundFileName == fileName {
				// ok
			} else {
				return nil, &SnippetError{Snippet: ps.originalSnippet, UserErrorMessage: fmt.Sprintf("Identifers spanned multiple files: %q and %q", foundFileName, fileName)}, nil
			}
		} else {
			return nil, &SnippetError{Snippet: ps.originalSnippet, UserErrorMessage: fmt.Sprintf("Could not find identifier definition for %q", ident)}, nil
		}
	}

	if foundFileName == "" {
		panic("unexpectedly had no foundFileName")
	}

	file := pkg.Files[foundFileName]

	curFileBytes := file.Contents
	curAST := file.AST
	curFileSet := file.FileSet
	var parseError error

	appliedCount := 0

	for _, snippetDecl := range ps.ast.Decls {

		switch snippetDecl := snippetDecl.(type) {
		case *ast.GenDecl:
			if snippetDecl.Tok == token.VAR || snippetDecl.Tok == token.CONST {

				// Match this snippetDecl, which contains 1 or more Specs (each spec is a fieldKey of idents), to a SINGLE decl within source.
				for _, sourceDecl := range curAST.Decls {
					switch sourceDecl := sourceDecl.(type) {
					case *ast.GenDecl:
						someOverlap, fullOverlap := valueIdentifierOverlapInDecls(sourceDecl, snippetDecl)
						if !someOverlap {
							continue // no identifieres in snippetDecl are in sourceDecl, we just go onto the next one.
						} else if someOverlap && !fullOverlap {
							return nil, &SnippetError{Snippet: ps.originalSnippet, UserErrorMessage: "Some, but not all, identifiers in snippet were in a var/const decl in source code."}, nil
						} else if snippetDecl.Tok != sourceDecl.Tok {
							return nil, &SnippetError{Snippet: ps.originalSnippet, UserErrorMessage: "Source and snippet have mismatching var/const."}, nil
						}

						//
						// Here, we know that every identifier in snippetDecl is in sourceDecl (sourceDecl may have some identifiers/specs NOT in the snippet -- that's okay)
						//

						snippetDeclIsBlock := snippetDecl.Lparen.IsValid()
						sourceDeclIsBlock := sourceDecl.Lparen.IsValid()

						if snippetDeclIsBlock && !sourceDeclIsBlock {
							// There's no reason we should attempt to handle this. Snippet should not use a block here.
							return nil, &SnippetError{Snippet: ps.originalSnippet, UserErrorMessage: "Source is not a block (aka: not using parens), but snippet is a block. Make sure snippet matches source."}, nil
						} else if !snippetDeclIsBlock && !sourceDeclIsBlock {

							snippetSpec := snippetDecl.Specs[0].(*ast.ValueSpec)
							sourceSpec := sourceDecl.Specs[0].(*ast.ValueSpec)

							if snippetDecl.Doc != nil && snippetSpec.Comment != nil {
								panic("snippet has both decl and EOL comment, should have been validated elsewhere.")
							}

							// If the existing code has a comment (EOL or Doc), and we we're rejecting updates, then reject it:
							if options.RejectUpdates {
								if snippetDecl.Doc != nil || snippetSpec.Comment != nil {
									if sourceSpec.Comment != nil || sourceDecl.Doc != nil {
										ps.partiallyRejected = true
										continue
									}
								}
							}

							if snippetDecl.Doc != nil {
								// Delete EOL comment first since updating Doc comment will update these offsets, but updating these offsets won't affect Doc offsets:
								if sourceSpec.Comment != nil {
									startOffset := curFileSet.Position(sourceSpec.Comment.Pos()).Offset
									endOffset := curFileSet.Position(sourceSpec.Comment.End()).Offset
									curFileBytes = deleteRangeInBytes(curFileBytes, startOffset, endOffset, false)
								}

								if sourceDecl.Doc == nil {
									startOffset := curFileSet.Position(sourceDecl.Pos()).Offset
									curFileBytes = spliceStringIntoBytes(curFileBytes, commentBlockFromGroup(snippetDecl.Doc, false), startOffset, startOffset)
								} else {
									startOffset := curFileSet.Position(sourceDecl.Doc.Pos()).Offset
									endOffset := curFileSet.Position(sourceDecl.Pos()).Offset
									curFileBytes = spliceStringIntoBytes(curFileBytes, commentBlockFromGroup(snippetDecl.Doc, false), startOffset, endOffset)
								}
							} else if snippetSpec.Comment != nil {
								if sourceSpec.Comment == nil {
									startOffset := curFileSet.Position(sourceSpec.End()).Offset
									curFileBytes = spliceStringIntoBytes(curFileBytes, eolCommentFromGroup(snippetSpec.Comment), startOffset, startOffset)
								} else {
									startOffset := curFileSet.Position(sourceSpec.End()).Offset
									endOffset := curFileSet.Position(sourceSpec.Comment.End()).Offset
									curFileBytes = spliceStringIntoBytes(curFileBytes, eolCommentFromGroup(snippetSpec.Comment), startOffset, endOffset)
								}

								// Delete Doc comment last so that it doesn't affect EOL comment offsets:
								if sourceDecl.Doc != nil {
									startOffset := curFileSet.Position(sourceDecl.Doc.Pos()).Offset
									endOffset := curFileSet.Position(sourceDecl.Doc.End()).Offset
									curFileBytes = deleteRangeInBytes(curFileBytes, startOffset, endOffset, true)
								}
							} else {
								// Just skip this one. We permit snippets to contain multiple values, and maybe only 1 has docs, the others are there for context.
								// If none of the values have docs, we catch this via "no comments updated" error.
								continue
							}

							curAST, curFileSet, parseError = reparseFile(curFileBytes, file.FileName)
							appliedCount++
							if parseError != nil {
								return nil, nil, parseError
							}
						} else if snippetDeclIsBlock && sourceDeclIsBlock {
							// Create a map of snippet specs by their identifier key for easy lookup
							snippetSpecMap := make(map[string]*ast.ValueSpec)
							for _, spec := range snippetDecl.Specs {
								if valueSpec, ok := spec.(*ast.ValueSpec); ok {
									snippetSpecMap[identsKey(valueSpec.Names)] = valueSpec
								}
							}

							// Process each source spec in reverse order, so that modifying file contents doesn't affect byte offsets above it.
							for i := len(sourceDecl.Specs) - 1; i >= 0; i-- {
								sourceSpec := sourceDecl.Specs[i]
								if valueSpec, ok := sourceSpec.(*ast.ValueSpec); ok {
									key := identsKey(valueSpec.Names)
									snippetSpec, exists := snippetSpecMap[key]

									// Skip specs that don't have a matching snippet spec:
									if !exists {
										continue
									}

									if snippetSpec.Comment != nil && snippetSpec.Doc != nil {
										panic("snippet spec has both doc and eol comment. should have been validated elsewhere")
									}

									// If the existing code has a comment (EOL or Doc), and we we're rejecting updates, then reject it:
									if options.RejectUpdates {
										if snippetSpec.Doc != nil || snippetSpec.Comment != nil {
											if valueSpec.Comment != nil || valueSpec.Doc != nil {
												ps.partiallyRejected = true
												continue
											}
										}
									}

									if snippetSpec.Comment != nil {
										if valueSpec.Comment == nil {
											// Insert new EOL comment
											startOffset := curFileSet.Position(valueSpec.End()).Offset
											curFileBytes = spliceStringIntoBytes(curFileBytes, eolCommentFromGroup(snippetSpec.Comment), startOffset, startOffset)
										} else {
											// Update existing EOL comment
											startOffset := curFileSet.Position(valueSpec.End()).Offset
											endOffset := curFileSet.Position(valueSpec.Comment.End()).Offset
											curFileBytes = spliceStringIntoBytes(curFileBytes, eolCommentFromGroup(snippetSpec.Comment), startOffset, endOffset)
										}
										// Delete Doc comment last so that it doesn't affect EOL comment offsets:
										if valueSpec.Doc != nil {
											startOffset := curFileSet.Position(valueSpec.Doc.Pos()).Offset
											endOffset := curFileSet.Position(valueSpec.Doc.End()).Offset
											curFileBytes = deleteRangeInBytes(curFileBytes, startOffset, endOffset, true)
										}
									} else if snippetSpec.Doc != nil {
										// Delete EOL comment first since updating Doc comment will update these offsets, but updating these offsets won't affect Doc offsets:
										if valueSpec.Comment != nil {
											startOffset := curFileSet.Position(valueSpec.Comment.Pos()).Offset
											endOffset := curFileSet.Position(valueSpec.Comment.End()).Offset
											curFileBytes = deleteRangeInBytes(curFileBytes, startOffset, endOffset, false)
										}

										if valueSpec.Doc == nil {
											// Insert new doc comment
											startOffset := curFileSet.Position(valueSpec.Pos()).Offset
											curFileBytes = spliceStringIntoBytes(curFileBytes, commentBlockFromGroup(snippetSpec.Doc, i > 0), startOffset, startOffset)
										} else {
											// Update existing doc comment
											startOffset := curFileSet.Position(valueSpec.Doc.Pos()).Offset
											endOffset := curFileSet.Position(valueSpec.Pos()).Offset
											curFileBytes = spliceStringIntoBytes(curFileBytes, commentBlockFromGroup(snippetSpec.Doc, i > 0), startOffset, endOffset)
										}
									}
								}
							}

							// Handle the declaration-level doc comment
							if snippetDecl.Doc != nil {
								// If the existing code has a doc comment, and we we're rejecting updates, then reject it:
								reject := false
								if options.RejectUpdates {
									if sourceDecl.Doc != nil {
										ps.partiallyRejected = true
										reject = true
									}
								}

								if !reject {
									if sourceDecl.Doc != nil {
										startOffset := curFileSet.Position(sourceDecl.Doc.Pos()).Offset
										endOffset := curFileSet.Position(sourceDecl.Pos()).Offset
										curFileBytes = spliceStringIntoBytes(curFileBytes, commentBlockFromGroup(snippetDecl.Doc, false), startOffset, endOffset)
									} else {
										startOffset := curFileSet.Position(sourceDecl.Pos()).Offset
										curFileBytes = spliceStringIntoBytes(curFileBytes, commentBlockFromGroup(snippetDecl.Doc, false), startOffset, startOffset)
									}
								}

							}

							curAST, curFileSet, parseError = reparseFile(curFileBytes, file.FileName)
							appliedCount++
							if parseError != nil {
								return nil, nil, parseError
							}
						} else {
							// source is block, and snippet is not block (snippetDecl.Doc -> sourceSpec.Doc)
							snippetSpec := snippetDecl.Specs[0].(*ast.ValueSpec)

							// Find the matching spec in the source block
							matchingSpecIndex := -1
							for i, spec := range sourceDecl.Specs {
								if valueSpec, ok := spec.(*ast.ValueSpec); ok {
									if identsKey(valueSpec.Names) == identsKey(snippetSpec.Names) {
										matchingSpecIndex = i
										break
									}
								}
							}

							if matchingSpecIndex == -1 {
								// This shouldn't happen as we already checked for overlap
								return nil, &SnippetError{Snippet: ps.originalSnippet, UserErrorMessage: "Could not find matching identifier in source block"}, nil
							}

							sourceSpec := sourceDecl.Specs[matchingSpecIndex].(*ast.ValueSpec)

							if snippetDecl.Doc != nil && snippetSpec.Comment != nil {
								panic("snippet has both decl and EOL comment, should have been validated elsewhere.")
							}

							// If the existing code has a comment (EOL or Doc), and we we're rejecting updates, then reject it:
							if options.RejectUpdates {
								if snippetDecl.Doc != nil || snippetSpec.Comment != nil {
									if sourceSpec.Comment != nil || sourceSpec.Doc != nil {
										ps.partiallyRejected = true
										continue
									}
								}
							}

							if snippetDecl.Doc != nil {
								// Delete EOL comment first since updating Doc comment will update these offsets, but updating these offsets won't affect Doc offsets:
								if sourceSpec.Comment != nil {
									startOffset := curFileSet.Position(sourceSpec.Comment.Pos()).Offset
									endOffset := curFileSet.Position(sourceSpec.Comment.End()).Offset
									curFileBytes = deleteRangeInBytes(curFileBytes, startOffset, endOffset, false)
								}

								// Apply snippetDecl.Doc to sourceSpec.Doc
								if sourceSpec.Doc == nil {
									startOffset := curFileSet.Position(sourceSpec.Pos()).Offset
									curFileBytes = spliceStringIntoBytes(curFileBytes, commentBlockFromGroup(snippetDecl.Doc, matchingSpecIndex > 0), startOffset, startOffset)
								} else {
									startOffset := curFileSet.Position(sourceSpec.Doc.Pos()).Offset
									endOffset := curFileSet.Position(sourceSpec.Pos()).Offset
									curFileBytes = spliceStringIntoBytes(curFileBytes, commentBlockFromGroup(snippetDecl.Doc, matchingSpecIndex > 0), startOffset, endOffset)
								}
							} else if snippetSpec.Comment != nil {
								// Apply snippetSpec.Comment to sourceSpec.Comment
								if sourceSpec.Comment == nil {
									startOffset := curFileSet.Position(sourceSpec.End()).Offset
									curFileBytes = spliceStringIntoBytes(curFileBytes, eolCommentFromGroup(snippetSpec.Comment), startOffset, startOffset)
								} else {
									startOffset := curFileSet.Position(sourceSpec.End()).Offset
									endOffset := curFileSet.Position(sourceSpec.Comment.End()).Offset
									curFileBytes = spliceStringIntoBytes(curFileBytes, eolCommentFromGroup(snippetSpec.Comment), startOffset, endOffset)
								}

								// Delete Doc comment last so that it doesn't affect EOL comment offsets:
								if sourceSpec.Doc != nil {
									startOffset := curFileSet.Position(sourceSpec.Doc.Pos()).Offset
									endOffset := curFileSet.Position(sourceSpec.Doc.End()).Offset
									curFileBytes = deleteRangeInBytes(curFileBytes, startOffset, endOffset, true)
								}
							} else {
								// Just skip this one. We permit snippets to contain multiple values, and maybe only 1 has docs, the others are there for context.
								// If none of the values have docs, we catch this via "no comments updated" error.
								continue
							}

							curAST, curFileSet, parseError = reparseFile(curFileBytes, file.FileName)
							appliedCount++
							if parseError != nil {
								return nil, nil, parseError
							}
						}

					}
				}
			}
		}
	}

	if appliedCount == 0 {
		if ps.partiallyRejected {
			return nil, nil, nil // Causes rejection snippet error in caller
		} else {
			return nil, &SnippetError{Snippet: ps.originalSnippet, UserErrorMessage: "No comments to apply"}, nil
		}

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
		edits := getFormatEditsForBlockOrStruct(newFile, identifiersInSnippet)
		if len(edits) > 0 {
			newFile, err = ApplyLineEdits(newFile, edits)
			if err != nil {
				return nil, nil, err
			}
		}
	}

	return newFile, nil, nil
}

func reparseFile(contents []byte, fileName string) (*ast.File, *token.FileSet, error) { // TODO: use in types
	fileSet := token.NewFileSet()
	newAST, err := parser.ParseFile(fileSet, fileName, contents, parser.ParseComments)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to reparse file: %w", err)
	}
	return newAST, fileSet, nil
}

// identifierToFileMap returns a map from identifier to file name. For instance, if `type Foo int` is in "foo.go", then the returned map will contain "Foo": "foo.go".
func identifierToFileMap(pkg *gocode.Package) map[string]string {
	result := make(map[string]string)

	// Iterate through all files in the package
	for _, file := range pkg.Files {
		// Get all identifiers in this file
		identifiers := identifiersInFile(file.AST, file.FileSet)

		// Map each identifier to this file's name
		for _, ident := range identifiers {
			result[ident] = file.FileName
		}
	}

	return result
}

// identifiersInFile returns all the top-level identifiers in a file (ex: `type Foo int` -> "Foo"; `func MyFunc()` -> "MyFunc"; `var a, b int` -> {"a", "b"}).
func identifiersInFile(file *ast.File, fset *token.FileSet) []string {
	if file == nil {
		return nil
	}

	var identifiers []string

	// Walk through all declarations in the file
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			// Use helpers from gocode which correctly handle generic receivers
			recvType, funcName := gocode.GetReceiverFuncName(d)
			pos := fset.Position(d.Name.Pos())
			identifiers = append(identifiers, gocode.FuncIdentifier(recvType, funcName, pos.Filename, pos.Line, pos.Column))
		case *ast.GenDecl:
			// Handle type, var, and const declarations
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					// Add type name
					identifiers = append(identifiers, s.Name.Name)
				case *ast.ValueSpec:
					// Add variable/constant names
					for _, name := range s.Names {
						identifiers = append(identifiers, name.Name)
					}
				}
			}
		}
	}

	return identifiers
}

// Returns (ANY identifier in snippetDecl is in sourceDecl, ALL identifiers in snippetDecl are in sourceDecl). An identifier only matches if the whole spec's identifier list matches
// exactly (using identsKey).
//
// Example:
//
//	// source decl:
//	var (
//		A int
//		B, C int
//	)
//
//	// snippet that returns true, true:
//	var (
//		A int
//	)
//
//	// snippet that returns true, false:
//	var (
//		A int // A is in source's decl
//		D int // D isn't in source's decl (it may be in another decl)
//	)
//
//	// snippet that returns false, false:
//	var D int
//
//	// snippet that returns false, false (B isn't paired with C, like it is in source):
//	var B int
func valueIdentifierOverlapInDecls(sourceDecl *ast.GenDecl, snippetDecl *ast.GenDecl) (bool, bool) {
	// Create maps to track identifiers in each declaration
	sourceIdents := make(map[string][]string) // map of spec key to identifiers
	snippetIdents := make(map[string][]string)

	// Process source declaration
	for _, spec := range sourceDecl.Specs {
		if valueSpec, ok := spec.(*ast.ValueSpec); ok {
			key := identsKey(valueSpec.Names)
			sourceIdents[key] = make([]string, len(valueSpec.Names))
			for i, name := range valueSpec.Names {
				sourceIdents[key][i] = name.Name
			}
		}
	}

	// Process snippet declaration
	for _, spec := range snippetDecl.Specs {
		if valueSpec, ok := spec.(*ast.ValueSpec); ok {
			key := identsKey(valueSpec.Names)
			snippetIdents[key] = make([]string, len(valueSpec.Names))
			for i, name := range valueSpec.Names {
				snippetIdents[key][i] = name.Name
			}
		}
	}

	// Check for overlap
	someOverlap := false
	allOverlap := true

	for snippetKey := range snippetIdents {
		found := false
		for sourceKey := range sourceIdents {
			// Check if this spec's identifiers match exactly
			if snippetKey == sourceKey {
				found = true
				someOverlap = true
				break
			}
		}
		if !found {
			allOverlap = false
		}
	}

	return someOverlap, allOverlap
}
