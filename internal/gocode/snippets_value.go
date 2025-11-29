package gocode

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/token"
	"strings"
)

//
// Snippet interface implementation:
//

var _ Snippet = (*ValueSnippet)(nil) // ValueSnippet implements Snippet

// ValueSnippet describes a const or var declaration (either a single-spec declaration or a parenthesized block). It captures both the rendered bytes as they appear in source and structured
// documentation for the declaration.
//
// Snippet contains the doc comment(s) and the declaration as written and aliases the file's content buffer. BlockDoc holds the block-level doc (for parenthesized blocks). IdentifierDocs
// maps each declared identifier to its own doc (which may come from the block doc, spec doc, end-of-line comment, or a combination thereof).
type ValueSnippet struct {
	Identifiers []string // all identifiers defined by the value block (length is 1 for a single-spec value like "var MyVar int")
	IsVar       bool     // true for var, false for const
	IsBlock     bool     // true for a block (ex: "const ( ... )")
	FileName    string   // file name (no dirs) where the value was defined (ex: "foo.go")
	Snippet     []byte   // the docs + decl as it appears in source; shares the buffer with file's contents
	BlockDoc    string   // empty if not a block; otherwise, the doc above the overall block

	// Identifier -> doc for that identifier. Empty if an identifier has no docs. Multiple identifiers can share the same doc (duplicated strings in the map). Regardless of Doc vs Comment,
	// each comment is newline-terminated. If an identifier has both Doc and Comment, they are concatenated.
	IdentifierDocs map[string]string

	// File set used to parse the decl.
	fileSet *token.FileSet

	// Decl node from parsing the file.
	decl *ast.GenDecl
}

// Implemention of Snippet interface.
func (v *ValueSnippet) HasExported() bool {
	for _, identifier := range v.Identifiers {
		if ast.IsExported(identifier) {
			return true
		}
	}
	return false
}

// Implemention of Snippet interface.
func (v *ValueSnippet) IDs() []string {
	return v.Identifiers
}

// Implemention of Snippet interface.
func (v *ValueSnippet) Test() bool {
	return strings.HasSuffix(v.FileName, "_test.go")
}

// Implemention of Snippet interface.
func (v *ValueSnippet) Bytes() []byte {
	return v.Snippet
}

// Implemention of Snippet interface.
func (v *ValueSnippet) FullBytes() []byte {
	return v.Snippet
}

// Implemention of Snippet interface.
func (v *ValueSnippet) PublicSnippet() ([]byte, error) {
	if !v.HasExported() {
		return nil, nil
	}

	allExported := true
	for _, identifier := range v.Identifiers {
		if !ast.IsExported(identifier) {
			allExported = false
			break
		}
	}
	if allExported {
		return v.Snippet, nil
	}

	// For mixed exported/unexported, we need to filter
	filteredDecl := filterExportedValue(v.decl)
	if filteredDecl == nil {
		return nil, nil
	}

	var buf bytes.Buffer
	if err := format.Node(&buf, v.fileSet, filteredDecl); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// Implemention of Snippet interface.
func (v *ValueSnippet) Docs() []IdentifierDocumentation {
	var docs []IdentifierDocumentation

	// Add block-level documentation if it exists
	if v.BlockDoc != "" {
		docs = append(docs, IdentifierDocumentation{
			Identifier: "",
			Field:      "",
			Doc:        v.BlockDoc,
		})
	}

	// Add identifier-specific documentation
	for _, identifier := range v.Identifiers {
		if doc, exists := v.IdentifierDocs[identifier]; exists && doc != "" {
			docs = append(docs, IdentifierDocumentation{
				Identifier: identifier,
				Field:      "",
				Doc:        doc,
			})
		}
	}

	return docs
}

// Implemention of Snippet interface.
func (v *ValueSnippet) MissingDocs() []IdentifierDocumentation {
	var missing []IdentifierDocumentation

	for _, identifier := range v.Identifiers {
		doc, exists := v.IdentifierDocs[identifier]
		if !exists || doc == "" {
			missing = append(missing, IdentifierDocumentation{
				Identifier: identifier,
				Field:      "",
				Doc:        "",
			})
		}
	}

	return missing
}

// Implemention of Snippet interface.
func (v *ValueSnippet) Position() token.Position {
	if v.decl.Doc != nil {
		return positionWithBaseFilename(v.fileSet.Position(v.decl.Doc.Pos()))
	}
	return positionWithBaseFilename(v.fileSet.Position(v.decl.Pos()))
}

//
// Extraction
//

// extractValueSnippet extracts a ValueSnippet from an ast.GenDecl.
func extractValueSnippet(genDecl *ast.GenDecl, file *File) (*ValueSnippet, error) {
	// Only handle var and const declarations
	if genDecl.Tok != token.VAR && genDecl.Tok != token.CONST {
		panic("unexpected non-var, non-const token")
	}

	// Only create a snippet if there's identifiers:
	if len(genDecl.Specs) == 0 {
		return nil, nil
	}

	fset := file.FileSet

	isVar := genDecl.Tok == token.VAR
	isBlock := genDecl.Lparen.IsValid()

	// Extract block-level documentation
	var doc string
	var blockDoc string
	if genDecl.Doc != nil {
		docStart := fset.Position(genDecl.Doc.Pos()).Offset
		docEnd := fset.Position(genDecl.Doc.End()).Offset
		doc = ensureNewline(string(file.Contents[docStart:docEnd]))
	}
	if isBlock {
		blockDoc = doc
	}

	// Extract all identifiers from all specs
	var identifiers []string
	identifierDocs := make(map[string]string)

	for _, spec := range genDecl.Specs {
		valueSpec := spec.(*ast.ValueSpec)

		// Extract documentation for this spec's identifiers
		var specDoc string
		if !isBlock {
			specDoc = doc
		} else if valueSpec.Doc != nil {
			docStart := fset.Position(valueSpec.Doc.Pos()).Offset
			docEnd := fset.Position(valueSpec.Doc.End()).Offset
			specDoc = ensureNewline(string(file.Contents[docStart:docEnd]))
		}
		if valueSpec.Comment != nil {
			commentStart := fset.Position(valueSpec.Comment.Pos()).Offset
			commentEnd := fset.Position(valueSpec.Comment.End()).Offset
			specDoc += ensureNewline(string(file.Contents[commentStart:commentEnd]))
		}

		for _, name := range valueSpec.Names {
			identifier := name.Name
			if identifier == "_" {
				pos := fset.Position(name.Pos())
				identifier = AnonymousIdentifier(file.FileName, pos.Line, pos.Column)
			}
			identifiers = append(identifiers, identifier)
			identifierDocs[identifier] = specDoc
		}
	}

	// Extract snippet (docs + declaration):
	var snippetStart int
	if genDecl.Doc != nil {
		snippetStart = fset.Position(genDecl.Doc.Pos()).Offset
	} else {
		snippetStart = fset.Position(genDecl.Pos()).Offset
	}
	snippetEnd := fset.Position(genDecl.End()).Offset
	if !isBlock {
		for _, spec := range genDecl.Specs {
			valueSpec := spec.(*ast.ValueSpec)
			if valueSpec.Comment != nil {
				commentEnd := fset.Position(valueSpec.Comment.End()).Offset
				if commentEnd > snippetEnd {
					snippetEnd = commentEnd
				}
			}
		}
	}

	snippet := file.Contents[snippetStart:snippetEnd]

	return &ValueSnippet{
		Identifiers:    identifiers,
		IsVar:          isVar,
		IsBlock:        isBlock,
		FileName:       file.FileName,
		Snippet:        snippet,
		BlockDoc:       blockDoc,
		IdentifierDocs: identifierDocs,
		fileSet:        fset,
		decl:           genDecl,
	}, nil
}
