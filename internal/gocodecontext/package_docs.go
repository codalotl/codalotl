package gocodecontext

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"sort"
	"strings"

	"github.com/codalotl/codalotl/internal/gocode"
)

// PublicPackageDocumentation returns a godoc-like documentation string for the package:
//   - Grouped by file (with file comment markers, ex: `// user.go:`).
//   - Each file only has public snippets (no imports; no package-private vars/etc).
//   - Snippets are sorted by the order they appear in the actual file.
//   - Functions only have docs+signatures (even if not documented).
//   - blocks and structs with unexported elements have those elements elided.
//   - No "floating" comments.
//
// If identifiers are present, documentation is limited identifiers:
//   - If any identifier is a type, we also include all public methods on that type.
//   - Most identifiers are just their name. Methods are identified like "*SomePtrType.SomeMethod" or "SomeType.SomeMethod".
//
// Returns an error if pkg is a test package.
func PublicPackageDocumentation(pkg *gocode.Package, identifiers ...string) (string, error) {
	if pkg == nil {
		return "", fmt.Errorf("nil package")
	}

	if pkg.IsTestPackage() {
		return "", fmt.Errorf("cannot generate public documentation for test package %q", pkg.ImportPath)
	}

	// Expand identifiers to include methods for any type identifiers.
	var ids []string
	if len(identifiers) > 0 {
		expanded := make(map[string]struct{}, len(identifiers))
		for _, id := range identifiers {
			if id == "" {
				continue
			}
			expanded[id] = struct{}{}
			// If the identifier is a type, include all exported methods on that type (pointer or value receiver).
			if sn := pkg.GetSnippet(id); sn != nil {
				if _, ok := sn.(*gocode.TypeSnippet); ok {
					typeName := id
					for _, s := range pkg.Snippets() {
						fn, ok := s.(*gocode.FuncSnippet)
						if !ok {
							continue
						}
						// Match either "T" or "*T" receiver, but only exported methods.
						if fn.HasExported() && fn.IndirectedReceiverType() == typeName {
							expanded[fn.Identifier] = struct{}{}
						}
					}
				}
			}
		}
		ids = make([]string, 0, len(expanded))
		for id := range expanded {
			ids = append(ids, id)
		}
	}

	perFile := pkg.SnippetsByFile(ids)
	if len(perFile) == 0 {
		return "", nil
	}

	var fileNames []string
	for fileName := range perFile {
		if strings.HasSuffix(fileName, "_test.go") {
			continue
		}
		fileNames = append(fileNames, fileName)
	}

	sort.Strings(fileNames)

	var buf bytes.Buffer

	for _, fileName := range fileNames {
		snippets := perFile[fileName]

		var publicSnippets [][]byte
		for _, snip := range snippets {
			if snip == nil {
				continue
			}

			if snip.Test() {
				continue
			}

			if !snip.HasExported() {
				continue
			}

			publicBytes, err := snip.PublicSnippet()
			if err != nil {
				return "", fmt.Errorf("public snippet for %s in %s: %w", strings.Join(snip.IDs(), ","), fileName, err)
			}
			if len(publicBytes) == 0 {
				continue
			}

			publicSnippets = append(publicSnippets, publicBytes)
		}

		if len(publicSnippets) == 0 {
			continue
		}

		buf.WriteString("// ")
		buf.WriteString(fileName)
		buf.WriteString(":\n\n")

		for _, snippetBytes := range publicSnippets {
			buf.Write(snippetBytes)
			if len(snippetBytes) == 0 || snippetBytes[len(snippetBytes)-1] != '\n' {
				buf.WriteByte('\n')
			}
			buf.WriteByte('\n')
		}

	}

	return buf.String(), nil
}

// InternalPackageSignatures returns a string for the package:
//   - Grouped by file (with file comment markers, ex: `// user.go:`).
//   - Each file has public and private snippets; includes imports; includes "floating" comments if includeDocs.
//   - Snippets are sorted by the order they appear in the actual file.
//   - All functions/methods are just signatures (no bodies).
//
// If tests, we only do test files. Otherwise, no test files are included. If pkg is a _test package, tests MUST be true.
//
// If includeDocs, all documentation comments are included. Otherwise, all comments are stripped (including floaters).
func InternalPackageSignatures(pkg *gocode.Package, tests bool, includeDocs bool) (string, error) {
	if pkg == nil {
		return "", fmt.Errorf("nil package")
	}

	if pkg.IsTestPackage() && !tests {
		return "", fmt.Errorf("tests must be true for test package %q", pkg.ImportPath)
	}

	var fileNames []string
	for name, file := range pkg.Files {
		if file == nil {
			continue
		}
		if tests {
			if !file.IsTest {
				continue
			}
		} else {
			if file.IsTest {
				continue
			}
		}
		fileNames = append(fileNames, name)
	}

	sort.Strings(fileNames)

	if len(fileNames) == 0 {
		return "", nil
	}

	var buf bytes.Buffer

	for _, name := range fileNames {
		file := pkg.Files[name]
		content, err := signaturesForFile(file, includeDocs)
		if err != nil {
			return "", fmt.Errorf("format signatures for %s: %w", name, err)
		}

		content = strings.TrimSpace(content)
		if content == "" {
			continue
		}

		buf.WriteString("// ")
		buf.WriteString(name)
		buf.WriteString(":\n\n")
		buf.WriteString(content)
		buf.WriteString("\n\n")
	}

	result := buf.String()
	if !includeDocs {
		result = removeBlankLines(result)
	}

	return result, nil
}

func signaturesForFile(file *gocode.File, includeDocs bool) (string, error) {
	if file == nil {
		return "", fmt.Errorf("nil file")
	}

	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file.FileName, file.Contents, parser.ParseComments)
	if err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}

	var bodyRanges []commentPosRange

	for _, decl := range parsed.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			if fn.Body != nil {
				bodyRanges = append(bodyRanges, commentPosRange{
					start: fn.Body.Pos(),
					end:   fn.Body.End(),
				})
				fn.Body = nil
			}
		}
	}

	if !includeDocs {
		stripAllComments(parsed)
	} else {
		filterCommentsOutsideBodies(parsed, bodyRanges)
	}

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, parsed); err != nil {
		return "", fmt.Errorf("render: %w", err)
	}

	return buf.String(), nil
}

func stripAllComments(f *ast.File) {
	if f == nil {
		return
	}

	f.Doc = nil

	ast.Inspect(f, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.GenDecl:
			node.Doc = nil
			for _, spec := range node.Specs {
				switch s := spec.(type) {
				case *ast.ImportSpec:
					s.Doc = nil
					s.Comment = nil
				case *ast.ValueSpec:
					s.Doc = nil
					s.Comment = nil
				case *ast.TypeSpec:
					s.Doc = nil
					s.Comment = nil
				}
			}
		case *ast.FuncDecl:
			node.Doc = nil
		case *ast.TypeSpec:
			node.Doc = nil
			node.Comment = nil
		case *ast.ValueSpec:
			node.Doc = nil
			node.Comment = nil
		case *ast.ImportSpec:
			node.Doc = nil
			node.Comment = nil
		case *ast.Field:
			node.Doc = nil
			node.Comment = nil
		}
		return true
	})

	f.Comments = nil
}

type commentPosRange struct {
	start token.Pos
	end   token.Pos
}

func filterCommentsOutsideBodies(f *ast.File, ranges []commentPosRange) {
	if f == nil || len(f.Comments) == 0 || len(ranges) == 0 {
		return
	}

	var filtered []*ast.CommentGroup
	for _, cg := range f.Comments {
		if commentWithinRanges(cg, ranges) {
			continue
		}
		filtered = append(filtered, cg)
	}
	f.Comments = filtered
}

func commentWithinRanges(cg *ast.CommentGroup, ranges []commentPosRange) bool {
	if cg == nil {
		return false
	}

	for _, rng := range ranges {
		if cg.Pos() >= rng.start && cg.End() <= rng.end {
			return true
		}
	}

	return false
}

func removeBlankLines(s string) string {
	if s == "" {
		return ""
	}

	lines := strings.Split(s, "\n")
	var kept []string
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		kept = append(kept, line)
	}

	if len(kept) == 0 {
		return ""
	}

	result := strings.Join(kept, "\n")
	if !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	return result
}
