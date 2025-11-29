package updatedocs

import (
	"fmt"
	"go/ast"

	"github.com/codalotl/codalotl/internal/gocode"
)

func updateFunctionDoc(pkg *gocode.Package, ps *parsedSnippet, options Options) (*gocode.File, *SnippetError, error) {
	if ps.kind != snippetKindFunc {
		panic("expected function kind")
	}
	if len(ps.ast.Decls) != 1 {
		panic("expected exactly 1 decl")
	}

	snippetFuncDecl, isSnippetFuncDecl := ps.ast.Decls[0].(*ast.FuncDecl)
	if !isSnippetFuncDecl {
		panic("expected func decl")
	}

	commentBlock := commentBlockFromGroup(snippetFuncDecl.Doc, false)
	snippetFuncIdentifier := gocode.FuncIdentifierFromDecl(snippetFuncDecl, ps.fileSet)

	// Find the file which contains the function:
	// NOTE: Even if we find the file, we don't guarantee that the func sig exactly matches. This is because identifiersInFile et al have a function identifier
	// as, for example, "*Customer.SendInvoid", whereas the full sig has parameters that must also match. In theory, we could unify these things.
	identMap := identifierToFileMap(pkg)
	identifiersInSnippet := identifiersInFile(ps.ast, ps.fileSet)
	if len(identifiersInSnippet) != 1 {
		panic(fmt.Sprintf("unexpected number of identifiers in snippet: %v", identifiersInSnippet))
	}
	foundFileName := ""
	funcNotFoundSnippetError := &SnippetError{Snippet: ps.originalSnippet, UserErrorMessage: fmt.Sprintf("Could not find function definition for %q", identifiersInSnippet[0])}
	if fileName, ok := identMap[identifiersInSnippet[0]]; ok {
		foundFileName = fileName
	} else {
		return nil, funcNotFoundSnippetError, nil
	}

	file := pkg.Files[foundFileName]

	var newFileBytes []byte

	sourceAST := file.AST
	for _, decl := range sourceAST.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			sourceCandidateIdentifier := gocode.FuncIdentifierFromDecl(d, file.FileSet)
			if sourceCandidateIdentifier == snippetFuncIdentifier {
				// If there's no comment, return the file unmodified.
				// We do this here instead of above to correctly return funcNotFoundSnippetError errors.
				if commentBlock == "" {
					return nil, nil, nil
				}

				if hasNoDocs(d.Doc) {
					// Get position of "func":
					funcPosition := file.FileSet.Position(d.Pos())
					funcOffset := funcPosition.Offset

					newFileBytes = spliceStringIntoBytes(file.Contents, commentBlock, funcOffset, funcOffset)

				} else {

					if options.RejectUpdates {
						ps.partiallyRejected = true
						return nil, nil, nil
					}

					// Get positions of comment and "func":
					existingCommentPosition := file.FileSet.Position(d.Doc.Pos())
					existingCommentOffset := existingCommentPosition.Offset
					funcPosition := file.FileSet.Position(d.Pos())
					funcOffset := funcPosition.Offset

					newFileBytes = spliceStringIntoBytes(file.Contents, commentBlock, existingCommentOffset, funcOffset)
				}

				newFile := file.Clone()
				err := newFile.PersistNewContents(newFileBytes, true)
				if err != nil {
					return nil, nil, err
				}

				return newFile, nil, nil
			}
		}
	}

	return nil, funcNotFoundSnippetError, nil
}
