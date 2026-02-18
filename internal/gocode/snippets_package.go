package gocode

import (
	"fmt"
	"go/ast"
	"go/token"
	"sort"
	"strings"
)

var _ Snippet = (*PackageDocSnippet)(nil) // PackageDocSnippet implements Snippet

// PackageDocSnippet holds the package-level documentation comment extracted from a single file, along with the trailing package clause. It is always considered
// exported.
//
// Snippet contains the raw bytes from the start of the package doc comment through the end of the "package ..." line and aliases the file's content buffer. Doc
// contains just the comment text and is newline-terminated.
type PackageDocSnippet struct {
	Identifier string         // identifier, always in PackageIdentifierPerFile format (ex: "package:foo.go")
	FileName   string         // file name (no dirs) where the package doc was defined (ex: "foo.go")
	Snippet    []byte         // the package doc as it appears in source; shares the buffer with File's Contents
	Doc        string         // full comment above the package statement; includes "//" or "/**/"; always \n-terminated
	fileSet    *token.FileSet // fileSet used to parse the file
	file       *ast.File      // file node produced by parsing
}

// Implemention of Snippet interface.
func (p *PackageDocSnippet) HasExported() bool {
	// Package documentation is always considered exported
	return true
}

// Implemention of Snippet interface.
func (p *PackageDocSnippet) IDs() []string {
	return []string{p.Identifier}
}

// Implemention of Snippet interface.
func (p *PackageDocSnippet) Test() bool {
	return strings.HasSuffix(p.FileName, "_test.go")
}

// Implemention of Snippet interface.
func (p *PackageDocSnippet) Bytes() []byte {
	return p.Snippet
}

// Implemention of Snippet interface.
func (p *PackageDocSnippet) FullBytes() []byte {
	return p.Snippet
}

// Implemention of Snippet interface.
func (p *PackageDocSnippet) PublicSnippet() ([]byte, error) {
	// Package documentation is always public
	return p.Snippet, nil
}

// Implemention of Snippet interface.
func (p *PackageDocSnippet) Docs() []IdentifierDocumentation {
	if p.Doc == "" {
		return nil
	}
	return []IdentifierDocumentation{
		{
			Identifier: p.Identifier,
			Field:      "",
			Doc:        p.Doc,
		},
	}
}

// Implemention of Snippet interface.
func (p *PackageDocSnippet) MissingDocs() []IdentifierDocumentation {
	return nil
}

// Implemention of Snippet interface.
func (p *PackageDocSnippet) Position() token.Position {
	return positionWithBaseFilename(p.fileSet.Position(p.file.Doc.Pos()))
}

// extractPackageDocSnippet extracts a package documentation snippet from a file.
func extractPackageDocSnippet(file *File) (*PackageDocSnippet, error) {
	if file.AST == nil || file.AST.Doc == nil {
		return nil, nil
	}

	// Get the snippet from doc start to end of package statement
	snippetStart := file.FileSet.Position(file.AST.Doc.Pos()).Offset
	snippetEnd := file.FileSet.Position(file.AST.Name.End()).Offset
	snippet := file.Contents[snippetStart:snippetEnd]

	// Get the raw documentation from the file contents
	docStart := file.FileSet.Position(file.AST.Doc.Pos()).Offset
	docEnd := file.FileSet.Position(file.AST.Doc.End()).Offset
	doc := ensureNewline(string(file.Contents[docStart:docEnd]))

	return &PackageDocSnippet{
		Identifier: PackageIdentifierPerFile(file.FileName),
		FileName:   file.FileName,
		Snippet:    snippet,
		Doc:        doc,
		fileSet:    file.FileSet,
		file:       file.AST,
	}, nil
}

// primaryPackageDocSnippet selects the canonical package documentation snippet for p. When multiple package doc comments exist across files, it prefers, in order:
//  1. doc.go
//  2. <package name>.go
//  3. the lexicographically first file.
//
// It returns nil if the package has no package documentation.
func primaryPackageDocSnippet(p *Package) *PackageDocSnippet {
	if len(p.PackageDocSnippets) == 0 {
		return nil
	}

	// If there's only one, return it:
	if len(p.PackageDocSnippets) == 1 {
		return p.PackageDocSnippets[0]
	}

	// First priority: doc.go:
	for _, ps := range p.PackageDocSnippets {
		if ps.FileName == "doc.go" {
			return ps
		}
	}

	// Second priority: <packageName>.go:
	targetFileName := fmt.Sprintf("%s.go", p.Name)
	for _, ps := range p.PackageDocSnippets {
		if ps.FileName == targetFileName {
			return ps
		}
	}

	// Otherwise, sort by lexographical fileName and return the first.
	sortedSnippets := make([]*PackageDocSnippet, len(p.PackageDocSnippets))
	copy(sortedSnippets, p.PackageDocSnippets)
	sort.Slice(sortedSnippets, func(i, j int) bool {
		return sortedSnippets[i].FileName < sortedSnippets[j].FileName
	})
	return sortedSnippets[0]
}
