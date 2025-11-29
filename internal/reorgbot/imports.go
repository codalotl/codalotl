package reorgbot

import (
	"github.com/codalotl/codalotl/internal/gocode"
	"bytes"
	"go/ast"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
)

// aliasedImport represents an import that has an explicit alias (including dot import), e.g.,
//
//	alias "path/to/pkg"
//	.     "path/to/pkg"
//
// Path is kept as a literal (including quotes) to preserve exact formatting.
type aliasedImport struct {
	Alias       string // e.g., "foo" or "."
	PathLiteral string // e.g., "\"path/to/pkg\""
}

// key returns a unique key for the aliased import.
func (ai aliasedImport) key() string {
	return ai.Alias + " " + ai.PathLiteral
}

// render returns the aliased import as a string for use in import declarations.
func (ai aliasedImport) render() string {
	return ai.Alias + " " + ai.PathLiteral
}

// importPlanner encapsulates import analysis and rendering across a reorganization phase (tests vs non-tests). It maintains state to ensure certain imports appear at least once.
type importPlanner struct {
	pkg                        *gocode.Package            // package being reorganized
	onlyTests                  bool                       // true if reorganizing test files, false otherwise
	aliasedImportsByFile       map[string][]aliasedImport // maps original filenames to their aliased imports
	preExistingSideEffectPaths []string                   // sorted list of side-effect import paths from original files
	ensuredSideEffectByPath    map[string]bool            // tracks which side-effect imports have been ensured in the new organization
	idToOrigFile               map[string]string          // maps snippet IDs to their original filenames
	idToAliasedImportsFromOrig map[string][]aliasedImport // maps snippet IDs to aliased imports from their original files
}

// newImportPlanner creates a new importPlanner for a reorganization phase.
func newImportPlanner(pkg *gocode.Package, idToSnippet map[string]gocode.Snippet, onlyTests bool) *importPlanner {
	p := &importPlanner{
		pkg:                        pkg,
		onlyTests:                  onlyTests,
		aliasedImportsByFile:       make(map[string][]aliasedImport),
		ensuredSideEffectByPath:    make(map[string]bool),
		idToOrigFile:               make(map[string]string),
		idToAliasedImportsFromOrig: make(map[string][]aliasedImport),
	}

	// Scan files for aliased and side-effect imports in this phase.
	preExistingSideEffectSet := make(map[string]struct{})
	for fname, f := range pkg.Files {
		if f == nil || f.AST == nil {
			continue
		}
		if imps := extractAliasedImportsFromFile(f); len(imps) > 0 {
			p.aliasedImportsByFile[fname] = imps
		}
		isTestFile := strings.HasSuffix(fname, "_test.go")
		if isTestFile == onlyTests {
			for _, pathLit := range extractSideEffectImportsFromFile(f) {
				preExistingSideEffectSet[pathLit] = struct{}{}
			}
		}
	}
	for pth := range preExistingSideEffectSet {
		p.preExistingSideEffectPaths = append(p.preExistingSideEffectPaths, pth)
	}
	sort.Strings(p.preExistingSideEffectPaths)

	// Build id -> originating file and originating aliased imports.
	for id, s := range idToSnippet {
		orig := snippetFileName(s)
		p.idToOrigFile[id] = orig
		if imps := p.aliasedImportsByFile[orig]; len(imps) > 0 {
			p.idToAliasedImportsFromOrig[id] = imps
		}
	}

	return p
}

// snippetFileName returns the base file name where the snippet originated.
func snippetFileName(s gocode.Snippet) string {
	if s == nil {
		return ""
	}
	// Position().Filename may be absolute; use base to match keys in pkg.Files
	return filepath.Base(s.Position().Filename)
}

// extractAliasedImportsFromFile returns all aliased (including dot) imports in f. Side-effect
// imports (alias "_") are ignored.
func extractAliasedImportsFromFile(f *gocode.File) []aliasedImport {
	if f == nil || f.AST == nil {
		return nil
	}
	var out []aliasedImport
	for _, spec := range f.AST.Imports {
		if spec == nil || spec.Path == nil {
			continue
		}
		if spec.Name == nil {
			continue // not aliased
		}
		alias := spec.Name.Name
		if alias == "_" {
			continue // ignore side-effect imports
		}
		out = append(out, aliasedImport{Alias: alias, PathLiteral: spec.Path.Value})
	}
	return out
}

// extractSideEffectImportsFromFile returns all side-effect (blank identifier) import path literals
// from the given file, e.g., "\"net/http/pprof\"". Returns nil if none.
func extractSideEffectImportsFromFile(f *gocode.File) []string {
	if f == nil || f.AST == nil {
		return nil
	}
	var out []string
	for _, spec := range f.AST.Imports {
		if spec == nil || spec.Path == nil {
			continue
		}
		if spec.Name == nil {
			continue
		}
		if spec.Name.Name != "_" {
			continue
		}
		out = append(out, spec.Path.Value)
	}
	return out
}

// writeImports writes all import sections for a destination file into buf.
// It preserves original imports, adds aliased imports from moved snippets, ensures go:embed
// and phase side-effect imports are present at least once across files.
func (p *importPlanner) writeImports(buf *bytes.Buffer, destFileName string, ids []string, idToSnippet map[string]gocode.Snippet, idToPreComments map[string][]string) {
	// Preserve original import declarations (if any)
	if origFile := p.pkg.Files[destFileName]; origFile != nil {
		if imp := extractImportDeclBytes(origFile); len(imp) > 0 {
			buf.Write(imp)
			// Ensure at least one blank line after imports before snippets
			buf.WriteString("\n")
		}
	}

	// Add aliased imports from moved snippets, deduplicated against existing aliases in destination
	{
		present := make(map[string]struct{})
		if existing := p.aliasedImportsByFile[destFileName]; len(existing) > 0 {
			for _, ai := range existing {
				present[ai.key()] = struct{}{}
			}
		}

		addSet := make(map[string]aliasedImport)
		for _, id := range ids {
			orig := p.idToOrigFile[id]
			if orig == "" || orig == destFileName {
				continue // not moved or unknown
			}
			for _, ai := range p.idToAliasedImportsFromOrig[id] {
				k := ai.key()
				if _, ok := present[k]; ok {
					continue
				}
				addSet[k] = ai
			}
		}

		if len(addSet) > 0 {
			keys := make([]string, 0, len(addSet))
			for k := range addSet {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			buf.WriteString("import (\n")
			for _, k := range keys {
				ai := addSet[k]
				buf.WriteString("\t")
				buf.WriteString(ai.render())
				buf.WriteString("\n")
			}
			buf.WriteString(")\n\n")
		}
	}

	// Ensure special imports: go:embed and phase side-effects
	{
		hasGoEmbed := false
		for _, id := range ids {
			s := idToSnippet[id]
			if s == nil {
				continue
			}
			// NOTE: we could possibly narrow this further, by only considering var's documentation.
			if bytes.Contains(s.Bytes(), []byte("//go:embed")) {
				hasGoEmbed = true
				break
			}
			if pres := idToPreComments[id]; len(pres) > 0 {
				for _, c := range pres {
					if strings.Contains(c, "//go:embed") {
						hasGoEmbed = true
						break
					}
				}
				if hasGoEmbed {
					break
				}
			}
		}

		var extra []string
		bufSoFar := buf.Bytes()
		containsPath := func(pathLiteral string) bool { // pathLiteral includes quotes
			return bytes.Contains(bufSoFar, []byte(pathLiteral))
		}

		if hasGoEmbed && !containsPath("\"embed\"") {
			extra = append(extra, "_ \"embed\"")
		}

		for _, pth := range p.preExistingSideEffectPaths {
			// Avoid duplicating embed: it is handled separately above when //go:embed is present
			if pth == "\"embed\"" {
				continue
			}
			if p.ensuredSideEffectByPath[pth] {
				continue
			}
			if containsPath(pth) {
				p.ensuredSideEffectByPath[pth] = true
				continue
			}
			extra = append(extra, "_ "+pth)
			p.ensuredSideEffectByPath[pth] = true
		}

		if len(extra) > 0 {
			buf.WriteString("import (\n")
			for _, line := range extra {
				buf.WriteString("\t")
				buf.WriteString(line)
				buf.WriteString("\n")
			}
			buf.WriteString(")\n\n")
		}
	}
}

// extractImportDeclBytes returns the exact bytes of all import declarations in f, in
// the order they appeared in the original file. If no imports exist or the file is
// not parsed, it returns nil.
func extractImportDeclBytes(f *gocode.File) []byte {
	if f == nil || f.AST == nil || f.FileSet == nil || len(f.Contents) == 0 {
		return nil
	}

	var sections [][]byte
	for _, decl := range f.AST.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		start := f.FileSet.Position(gd.Pos()).Offset
		end := f.FileSet.Position(gd.End()).Offset
		if start < 0 || end <= start || end > len(f.Contents) {
			continue
		}
		sections = append(sections, f.Contents[start:end])
	}

	if len(sections) == 0 {
		return nil
	}

	var out bytes.Buffer
	for _, s := range sections {
		out.Write(s)
		// Ensure each import block ends with exactly one newline to keep them distinct
		if !strings.HasSuffix(string(s), "\n") {
			out.WriteByte('\n')
		}
	}
	return out.Bytes()
}
