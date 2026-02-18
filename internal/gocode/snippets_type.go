package gocode

import (
	"bytes"
	"go/ast"
	"go/printer"
	"go/token"
	"slices"
	"strings"
)

//
// Snippet interface implementation:
//

var _ Snippet = (*TypeSnippet)(nil) // TypeSnippet implements Snippet

// TypeSnippet represents a single type declaration or type block as it appears in source. It records the raw source (documentation plus declaration), the defined
// identifiers, and any per-identifier and per-field documentation. TypeSnippet implements Snippet and can render a public-only form that elides unexported members.
type TypeSnippet struct {
	Identifiers []string // all identifiers defined by the type block (length will be 1 if it's a single-spec type like "type MyType int")
	IsBlock     bool     // true if a block (ex: "type ( ... )")
	FileName    string   // file name (no dirs) where the value was defined (ex: "foo.go")
	Snippet     []byte   // the docs + decl as it appears in source; shares buffer with File's Contents
	BlockDoc    string   // "" if not a block; otherwise, the doc above the overall block

	// identifier -> doc for that identifier. "" if no docs for an identifier. Multiple identifiers can share the same doc (duplicated strings in the map per identifier).
	// Regardless of doc vs comment, each comment is \n-terminated. An identifier with both doc and comment has its comments concatenated. Field documentation (within
	// structs and interfaces) is not present in this map.
	IdentifierDocs map[string]string

	// nil for non-structs/interfaces. For structs/interfaces, contains field-key -> doc, with "" for no docs (all fields must be in the map). Multiple fields can share
	// the same doc (duplicated strings). Always \n-terminated if docs are non-empty. Field-key is constructed by taking the identifier, followed by a dot, followed
	// by the field name. Nested fields are supported with additional dots. ex: "MyType.MyNestedStruct.MyField".
	FieldDocs map[string]string

	fieldDocIdentifiers []string       // ordered keys for FieldDocs so that order in Snippet.Docs/MissingDocs is deterministic
	fileSet             *token.FileSet // fileSet used to parse decl
	decl                *ast.GenDecl   // decl node from parsing file

	// Thoughts: struct/interface types are singularly important (as with functions). So, it might make sense to build more ergonomic abstractions around these.
	// But without a specific use-case, I don't want to. Note also that there's a danger of just reimplementing the AST, which can be fraught.
}

// Implemention of Snippet interface.
func (t *TypeSnippet) HasExported() bool {
	return slices.ContainsFunc(t.Identifiers, ast.IsExported)
}

// Implemention of Snippet interface.
func (t *TypeSnippet) IDs() []string {
	return t.Identifiers
}

// Implemention of Snippet interface.
func (t *TypeSnippet) Test() bool {
	return strings.HasSuffix(t.FileName, "_test.go")
}

// Implemention of Snippet interface.
func (t *TypeSnippet) Bytes() []byte {
	return t.Snippet
}

// Implemention of Snippet interface.
func (t *TypeSnippet) FullBytes() []byte {
	return t.Snippet
}

// Implemention of Snippet interface.
func (t *TypeSnippet) PublicSnippet() ([]byte, error) {
	if !t.HasExported() {
		return nil, nil
	}

	var buf bytes.Buffer
	cfg := printer.Config{Mode: printer.TabIndent | printer.UseSpaces, Tabwidth: 8}

	filtered := filterExportedTypes(t.decl)

	if err := cfg.Fprint(&buf, t.fileSet, filtered); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// Implemention of Snippet interface.
func (t *TypeSnippet) Docs() []IdentifierDocumentation {
	var docs []IdentifierDocumentation

	// Add block-level documentation if it exists
	if t.BlockDoc != "" {
		docs = append(docs, IdentifierDocumentation{
			Identifier: "",
			Field:      "",
			Doc:        t.BlockDoc,
		})
	}

	// Add identifier-specific documentation
	for _, identifier := range t.Identifiers {
		if doc, exists := t.IdentifierDocs[identifier]; exists && doc != "" {
			docs = append(docs, IdentifierDocumentation{
				Identifier: identifier,
				Field:      "",
				Doc:        doc,
			})
		}
	}

	// Add field documentation in deterministic order
	for _, fieldKey := range t.fieldDocIdentifiers {
		if doc := t.FieldDocs[fieldKey]; doc != "" {
			// Extract identifier and field from fieldKey (format: "TypeName.FieldName")
			parts := strings.Split(fieldKey, ".")
			if len(parts) >= 2 {
				identifier := parts[0]
				field := strings.Join(parts[1:], ".")
				docs = append(docs, IdentifierDocumentation{
					Identifier: identifier,
					Field:      field,
					Doc:        doc,
				})
			}
		}
	}

	return docs
}

// Implemention of Snippet interface.
func (t *TypeSnippet) MissingDocs() []IdentifierDocumentation {
	var missing []IdentifierDocumentation

	for _, identifier := range t.Identifiers {
		doc, exists := t.IdentifierDocs[identifier]
		if !exists || doc == "" {
			missing = append(missing, IdentifierDocumentation{
				Identifier: identifier,
				Field:      "",
				Doc:        "",
			})
		}
	}

	// Add missing field documentation in deterministic order
	for _, fieldKey := range t.fieldDocIdentifiers {
		if doc := t.FieldDocs[fieldKey]; doc == "" {
			// Extract identifier and field from fieldKey (format: "TypeName.FieldName")
			parts := strings.Split(fieldKey, ".")
			if len(parts) >= 2 {
				identifier := parts[0]
				field := strings.Join(parts[1:], ".")
				missing = append(missing, IdentifierDocumentation{
					Identifier: identifier,
					Field:      field,
					Doc:        "",
				})
			}
		}
	}

	return missing
}

// Implemention of Snippet interface.
func (t *TypeSnippet) Position() token.Position {
	if t.decl.Doc != nil {
		return positionWithBaseFilename(t.fileSet.Position(t.decl.Doc.Pos()))
	}
	return positionWithBaseFilename(t.fileSet.Position(t.decl.Pos()))
}

//
// Extraction
//

// extractFieldDocs recursively extracts field documentation from struct and interface types. It returns a map of field-key -> doc where the field-key uses "TypeName.FieldName".
// For nested fields, it uses dot notation like "TypeName.NestedType.FieldName". It also returns an ordered slice of field keys to preserve deterministic order.
func extractFieldDocs(typeName string, typeExpr ast.Expr, fset *token.FileSet, fileContents []byte) (map[string]string, []string) {
	fieldDocs := make(map[string]string)
	var fieldOrder []string

	switch t := typeExpr.(type) {
	case *ast.StructType:
		if t.Fields == nil {
			return fieldDocs, fieldOrder
		}

		for _, field := range t.Fields.List {
			// Get field documentation
			var fieldDoc string
			if field.Doc != nil {
				docStart := fset.Position(field.Doc.Pos()).Offset
				docEnd := fset.Position(field.Doc.End()).Offset
				fieldDoc += ensureNewline(string(fileContents[docStart:docEnd]))
			}
			if field.Comment != nil {
				commentStart := fset.Position(field.Comment.Pos()).Offset
				commentEnd := fset.Position(field.Comment.End()).Offset
				fieldDoc += ensureNewline(string(fileContents[commentStart:commentEnd]))
			}

			// Handle field names
			if len(field.Names) > 0 {
				// Named fields
				for _, name := range field.Names {
					fieldKey := typeName + "." + name.Name
					fieldDocs[fieldKey] = fieldDoc
					fieldOrder = append(fieldOrder, fieldKey)
				}
			} else {
				// Embedded field
				fieldName := getEmbeddedFieldName(field.Type)
				if fieldName != "" {
					fieldKey := typeName + "." + fieldName
					fieldDocs[fieldKey] = fieldDoc
					fieldOrder = append(fieldOrder, fieldKey)
				}
			}

			// Recursively handle nested struct/interface types:
			var nestedExpr ast.Expr
			if nestedStruct, ok := field.Type.(*ast.StructType); ok {
				nestedExpr = nestedStruct
			} else if nestedInterface, ok := field.Type.(*ast.InterfaceType); ok {
				nestedExpr = nestedInterface
			}
			if nestedExpr != nil {
				for _, name := range field.Names {
					nestedTypeName := typeName + "." + name.Name
					nestedDocs, nestedOrder := extractFieldDocs(nestedTypeName, nestedExpr, fset, fileContents)
					for k, v := range nestedDocs {
						fieldDocs[k] = v
					}
					fieldOrder = append(fieldOrder, nestedOrder...)
				}
			}
		}

	case *ast.InterfaceType:
		if t.Methods == nil {
			return fieldDocs, fieldOrder
		}

		for _, method := range t.Methods.List {
			// Get method documentation
			var methodDoc string
			if method.Doc != nil {
				docStart := fset.Position(method.Doc.Pos()).Offset
				docEnd := fset.Position(method.Doc.End()).Offset
				methodDoc += ensureNewline(string(fileContents[docStart:docEnd]))
			}
			if method.Comment != nil {
				commentStart := fset.Position(method.Comment.Pos()).Offset
				commentEnd := fset.Position(method.Comment.End()).Offset
				methodDoc += ensureNewline(string(fileContents[commentStart:commentEnd]))
			}

			// Handle method names
			if len(method.Names) > 0 {
				// Named methods
				for _, name := range method.Names {
					fieldKey := typeName + "." + name.Name
					fieldDocs[fieldKey] = methodDoc
					fieldOrder = append(fieldOrder, fieldKey)
				}
			} else {
				// Embedded interface
				fieldName := getEmbeddedFieldName(method.Type)
				if fieldName != "" {
					fieldKey := typeName + "." + fieldName
					fieldDocs[fieldKey] = methodDoc
					fieldOrder = append(fieldOrder, fieldKey)
				}
			}
		}
	}

	return fieldDocs, fieldOrder
}

// getEmbeddedFieldName returns the field name derived from an embedded field's type expression.
func getEmbeddedFieldName(typeExpr ast.Expr) string {
	switch t := typeExpr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		if pkg, ok := t.X.(*ast.Ident); ok {
			return pkg.Name + "." + t.Sel.Name
		}
	case *ast.StarExpr:
		if embedded := getEmbeddedFieldName(t.X); embedded != "" {
			return "*" + embedded
		}
	}
	return ""
}

// extractTypeSnippet extracts a TypeSnippet from an ast.GenDecl.
func extractTypeSnippet(genDecl *ast.GenDecl, file *File) (*TypeSnippet, error) {
	// Only handle type declarations
	if genDecl.Tok != token.TYPE {
		panic("unexpected non-type token")
	}

	// Only create a snippet if there's identifiers:
	if len(genDecl.Specs) == 0 {
		return nil, nil
	}

	fset := file.FileSet
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
	fieldDocs := make(map[string]string)
	var fieldDocIdentifiers []string

	for _, spec := range genDecl.Specs {
		typeSpec := spec.(*ast.TypeSpec)

		identifier := typeSpec.Name.Name
		if identifier == "_" {
			pos := fset.Position(typeSpec.Name.Pos())
			identifier = AnonymousIdentifier(file.FileName, pos.Line, pos.Column)
		}

		identifiers = append(identifiers, identifier)

		// Extract documentation for this spec's identifier
		var specDoc string
		if !isBlock {
			specDoc = doc
		} else if typeSpec.Doc != nil {
			// Get doc comment from file contents
			docStart := fset.Position(typeSpec.Doc.Pos()).Offset
			docEnd := fset.Position(typeSpec.Doc.End()).Offset
			specDoc = ensureNewline(string(file.Contents[docStart:docEnd]))
		}
		if typeSpec.Comment != nil {
			// Get end-of-line comment from file contents
			commentStart := fset.Position(typeSpec.Comment.Pos()).Offset
			commentEnd := fset.Position(typeSpec.Comment.End()).Offset
			specDoc += ensureNewline(string(file.Contents[commentStart:commentEnd]))
		}

		// Assign the doc to the identifier
		identifierDocs[identifier] = specDoc

		// Extract field documentation for struct and interface types
		typeFieldDocs, typeFieldOrder := extractFieldDocs(identifier, typeSpec.Type, fset, file.Contents)
		for k, v := range typeFieldDocs {
			fieldDocs[k] = v
		}
		fieldDocIdentifiers = append(fieldDocIdentifiers, typeFieldOrder...)
	}

	// FieldDocs should be nil for non-struct/interface types
	if len(fieldDocs) == 0 {
		fieldDocs = nil
		fieldDocIdentifiers = nil
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
			typeSpec := spec.(*ast.TypeSpec)
			if typeSpec.Comment != nil {
				commentEnd := fset.Position(typeSpec.Comment.End()).Offset
				if commentEnd > snippetEnd {
					snippetEnd = commentEnd
				}
			}
		}
	}
	snippet := file.Contents[snippetStart:snippetEnd]

	return &TypeSnippet{
		Identifiers:         identifiers,
		IsBlock:             isBlock,
		FileName:            file.FileName,
		Snippet:             snippet,
		BlockDoc:            blockDoc,
		IdentifierDocs:      identifierDocs,
		FieldDocs:           fieldDocs,
		fieldDocIdentifiers: fieldDocIdentifiers,
		fileSet:             fset,
		decl:                genDecl,
	}, nil
}
