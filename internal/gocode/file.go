package gocode

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
)

// genRE matches the standard "Code generated … DO NOT EDIT." header at the start of a line. It is used by (*File).IsCodeGenerated to detect generated files, and
// runs in multiline mode so "^" anchors to line starts within the whole file.
var genRE = regexp.MustCompile(`(?m)^//\s*Code generated .* DO NOT EDIT\.?`)

// File represents a Go source file loaded into memory. It stores basic file metadata, the raw contents, and parse artifacts (AST and FileSet). File is intentionally
// low‑level; higher‑level concepts (ex: identifiers and documentation) live on Package. A File is ready to Parse when FileName and Contents are set.
type File struct {
	FileName         string         // the .go filename (no directory)
	RelativeFileName string         // filename relative to the module. ex: 'codeai/gocode/gocode.go'
	AbsolutePath     string         // ex: '/path/to/foo.go'
	Contents         []byte         // full file contents
	PackageName      string         // the package name declared at the top of the file
	IsTest           bool           // whether the file is a _test.go file
	AST              *ast.File      // once parsed, we store the AST here; parsing does not mutate the AST
	FileSet          *token.FileSet // the FileSet used to parse the file, potentially used during PersistAST
}

// NOTE: ReadFile(absolutePath string) (*File, error) could makes sense, but is hampered by needing to know the module path. We could pass that in, but API suffers. We could find module, but performance suffers.

// Clone clones and returns f. AST and FileSet pointers are re-used.
func (f *File) Clone() *File {
	cloned := *f
	return &cloned
}

// Parse parses f.Contents and sets the AST field and the FileSet. fset can be a token.NewFileSet() used to parse the rest of the package, or a single-use token.NewFileSet()
// made for just this file. If fset is nil, a new one is created and saved to f.FileSet.
func (f *File) Parse(fset *token.FileSet) (*ast.File, error) {
	if fset == nil {
		fset = token.NewFileSet()
	}
	ast, err := parser.ParseFile(fset, f.FileName, f.Contents, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file %s: %w", f.FileName, err)
	}
	f.AST = ast
	f.FileSet = fset
	return ast, nil
}

// PersistContents writes the contents of the File to disk at AbsolutePath, overwriting any existing file. If reparse, a new f.FileSet is created and used to Parse
// the contents to f.AST; otherwise, f.FileSet and f.AST are left unchanged.
//
// An error is returned for I/O failure or failure to reparse the file.
//
// To persist f without mutating f, use f.Clone().PersistContents(reparse).
func (f *File) PersistContents(reparse bool) error {
	err := os.WriteFile(f.AbsolutePath, f.Contents, 0644)
	if err != nil {
		return fmt.Errorf("failed to write contents to file %s: %w", f.FileName, err)
	}

	if reparse {
		_, err := f.Parse(nil)
		return err
	}

	return nil
}

// PersistNewContents updates f.Contents to newContents and persists using f.PersistContents(reparse).
func (f *File) PersistNewContents(newContents []byte, reparse bool) error {
	f.Contents = newContents
	return f.PersistContents(reparse)
}

// IsCodeGenerated reports whether the File was created by code generation tools.
func (f *File) IsCodeGenerated() bool {
	return genRE.Match(f.Contents)
}
