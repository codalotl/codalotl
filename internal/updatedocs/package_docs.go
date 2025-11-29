package updatedocs

import (
	"bytes"
	"path/filepath"

	"github.com/codalotl/codalotl/internal/gocode"
)

// updatePackageDoc updates package documentation (comment above the package keyword) based on the parsedSnippet. If successful, it will save to disk a new or existing .go file with
// updated docs, and return the updated File. This helper does not construct or return a SnippetError; callers handle snippet-level failures. If there was a program or I/O error, we'll
// return an overall error.
func updatePackageDoc(pkg *gocode.Package, ps *parsedSnippet, options Options) (*gocode.File, *SnippetError, error) {
	if ps.kind != snippetKindPackageDoc {
		panic("expected package doc kind")
	}

	commentBlock := commentBlockFromGroup(ps.ast.Doc, false)
	if commentBlock == "" {
		panic("expected parsed snippet to contain a doc")
	}

	var buf bytes.Buffer

	// Write the new actual comment to buf, which will be a \n terminated comment block.
	buf.WriteString(commentBlock)

	var file *gocode.File
	snippet := pkg.GetSnippet(gocode.PackageIdentifier)
	if snippet != nil {
		docSnippet := snippet.(*gocode.PackageDocSnippet)
		file = pkg.Files[docSnippet.FileName]
	}

	if file != nil {
		// Get the byte offset of the package keyword within the file:
		packagePosition := file.FileSet.Position(file.AST.Package)
		packageOffset := packagePosition.Offset

		if len(file.Contents) > 0 && file.Contents[0] == '/' {
			if options.RejectUpdates {
				ps.partiallyRejected = true
				return nil, nil, nil
			}
		}

		// Write the rest of the file starting with they 'package' keyword:
		_, err := buf.Write(file.Contents[packageOffset:])
		if err != nil {
			return nil, nil, err
		}

		newFile := file.Clone()
		err = newFile.PersistNewContents(buf.Bytes(), true)
		if err != nil {
			return nil, nil, err
		}

		return newFile, nil, nil
	} else {
		const docFileName = "doc.go"
		newFile := &gocode.File{
			FileName:         docFileName,
			RelativeFileName: filepath.Join(pkg.RelativeDir, docFileName),
			AbsolutePath:     filepath.Join(pkg.Module.AbsolutePath, pkg.RelativeDir, docFileName),
			Contents:         []byte(ps.unwrappedSnippet),
			PackageName:      pkg.Name,
			IsTest:           false,
			AST:              ps.ast,
			FileSet:          ps.fileSet,
		}

		err := newFile.PersistContents(false)
		if err != nil {
			return nil, nil, err
		}

		return newFile, nil, nil
	}
}
