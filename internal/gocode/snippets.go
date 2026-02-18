package gocode

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
)

// Snippet is a single package-level declaration (func, type, value/const, or package doc). Implementations include FuncSnippet, TypeSnippet, ValueSnippet, and PackageDocSnippet.
// A snippet exposes the identifiers it defines, its source bytes in various views, and the documentation state.
type Snippet interface {
	// IDs returns the identifiers defined by the snippet. Note: It is named IDs instead of Identifiers to avoid aliasing conflicts with implementing structs.
	IDs() []string

	// HasExported reports whether anything in the snippet is exported.
	HasExported() bool

	// Test reports whether the snippet is in a test file.
	Test() bool

	// Bytes returns the snippet's bytes, both exported and unexported. A function's snippet bytes are its docs + signature; for types/values, it's the full code.
	Bytes() []byte

	// PublicSnippet returns the snippet for public documentation (similar to godoc). Unexported fields or variables are elided. If nothing is public in the snippet,
	// nil is returned.
	PublicSnippet() ([]byte, error)

	// FullBytes returns all bytes (docs + signature + function bodies, including exported and unexported fields).
	FullBytes() []byte

	// Docs returns non-blank documentation for all identifiers and their fields/methods within a declaration. If a value spec defines multiple identifiers with the
	// same docs, each identifier will have its own entry, duplicating the doc.
	Docs() []IdentifierDocumentation

	// MissingDocs returns the identifiers and fields that are missing documentation. ex: if a struct type is missing all docs, one MissingDocumentation entry is for
	// the struct itself, and each field has its own entry. If there is no missing documentation, nil is returned.
	MissingDocs() []IdentifierDocumentation

	// Position returns the position of the snippet in the file. Can be used to get the file name (ex: Position().Filename).
	Position() token.Position
}

// IDIsDocumented returns whether an identifier has any documentation in the snippet (anyDocs) and whether it is fully documented in the snippet (fullDocs). This
// is useful because snippets can be blocks (ex: var ( ... )) that define many identifiers.
//
// There are three places that might have docs (depending on the kind of snippet):
//  1. the block.
//  2. the spec (or decl for non-blocks).
//  3. fields (for structs/interfaces).
//
// AnyDocs requires docs in at least one of those places. FullDocs requires docs in all fields/methods (for structs/interfaces). In addition: if !blockDocsAllSpecs,
// FullDocs requires docs for all specs; block docs are irrelevant/optional. If blockDocsAllSpecs, FullDocs requires docs for all specs or in block docs. This allows,
// ex, const blocks to be fully documented with a single comment before const ().
func IDIsDocumented(snippet Snippet, identifier string, blockDocsAllSpecs bool) (anyDocs bool, fullDocs bool) {

	hasBlock := false // true if block and block is doc'ed

	// set anyDocs, hasBlock:
	for _, d := range snippet.Docs() {
		if d.Identifier == identifier {
			anyDocs = true
		} else if d.Identifier == "" {
			anyDocs = true
			hasBlock = true
		}
	}

	// If there's no docs, we can't have full docs:
	if !anyDocs {
		return false, false
	}

	// calculate missingField, missingSpec:
	missingField := false
	missingSpec := false // non-field, non-block. direct identifier doc.
	for _, d := range snippet.MissingDocs() {
		if d.Identifier == identifier {
			if d.Field != "" {
				missingField = true
			} else {
				missingSpec = true
			}
		}
	}

	// set fullDocs:
	if missingField {
		fullDocs = false
	} else {
		if blockDocsAllSpecs {
			fullDocs = hasBlock || !missingSpec
		} else {
			fullDocs = !missingSpec
		}
	}

	return
}

// IdentifierDocumentation describes documentation text associated with an identifier in a declaration. For block-level comments, Identifier is empty. For struct/interface
// members, Field names the member and may use dotted paths for nested fields. Doc, when present, is newline-terminated.
type IdentifierDocumentation struct {
	// the identifier in the decl (ex: the var name; the type name; the func name); can be blank for block docs
	Identifier string

	// only for struct/interface types: field in the struct (or method in interface); for nested fields, uses dot notation (ex: "Field.SubField")
	Field string

	// the doc; if present, always \n terminated (even if EOL comment)
	Doc string
}

// extractSnippets accepts a parsed file (file.AST is set) and extracts funcs, values, types, and packageDoc.
func extractSnippets(file *File) ([]*FuncSnippet, []*ValueSnippet, []*TypeSnippet, *PackageDocSnippet, error) {
	if file.AST == nil {
		return nil, nil, nil, nil, fmt.Errorf("file AST is nil")
	}

	var funcSnippets []*FuncSnippet
	var valueSnippets []*ValueSnippet
	var typeSnippets []*TypeSnippet

	// Extract package documentation snippet
	packageDocSnippet, err := extractPackageDocSnippet(file)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to extract package doc snippet: %w", err)
	}

	// Iterate through all declarations in the file
	for _, decl := range file.AST.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			// Extract function snippet
			funcSnippet, err := extractFuncSnippet(d, file)
			if err != nil {
				return nil, nil, nil, nil, fmt.Errorf("failed to extract function snippet: %w", err)
			}
			if funcSnippet != nil {
				funcSnippets = append(funcSnippets, funcSnippet)
			}
		case *ast.GenDecl:
			// Check if it's a value declaration (VAR or CONST)
			if d.Tok == token.VAR || d.Tok == token.CONST {
				// Extract value snippet
				valueSnippet, err := extractValueSnippet(d, file)
				if err != nil {
					return nil, nil, nil, nil, fmt.Errorf("failed to extract value snippet: %w", err)
				}
				if valueSnippet != nil {
					valueSnippets = append(valueSnippets, valueSnippet)
				}
			} else if d.Tok == token.TYPE {
				// Extract type snippet
				typeSnippet, err := extractTypeSnippet(d, file)
				if err != nil {
					return nil, nil, nil, nil, fmt.Errorf("failed to extract type snippet: %w", err)
				}
				if typeSnippet != nil {
					typeSnippets = append(typeSnippets, typeSnippet)
				}
			}
		}
	}

	return funcSnippets, valueSnippets, typeSnippets, packageDocSnippet, nil
}

// ensureNewline ensures s ends with exactly one newline ('\n').
func ensureNewline(s string) string {
	if len(s) == 0 {
		return "\n"
	}

	lastPos := len(s) - 1

	if s[lastPos] == '\n' {
		// Already ends with newline, trim any extra newlines
		i := lastPos
		for i > 0 && s[i-1] == '\n' {
			i--
		}
		if i < lastPos {
			return s[:i+1]
		}
		return s
	}

	// No newline at the end, add one
	return s + "\n"
}

func positionWithBaseFilename(position token.Position) token.Position {
	position.Filename = filepath.Base(position.Filename)
	return position
}
