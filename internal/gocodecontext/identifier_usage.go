package gocodecontext

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/codalotl/codalotl/internal/goclitools"
	"github.com/codalotl/codalotl/internal/gocode"
	"go/ast"
	"go/token"
)

const (
	maxSnippetContexts = 2
	maxSnippetLines    = 150
)

// IdentifierUsage returns usages of identifier as defined in packageAbsDir (i.e., the abs dir of a package).
//
// By default (includeIntraPackageUsages=false), none of the returned usages will be from within the defining package itself. All usages will be from other packages
// that import the defining package. Usages from packageAbsDir's own _test package will not be included.
//
// If includeIntraPackageUsages=true, usages within the defining package (same import path) will also be returned. Usages from the defining package's own _test package
// are still excluded.
//
// The second return value is a string representation of these references, suitable for an LLM:
//   - It will include all references in some manner.
//   - Some references may include the SnippetFullBytes.
//   - Specifics are an implementation detail. Callers should just pass this opaque blob to an LLM.
//
// If packageAbsDir is invalid, or identifier is not defined in packageAbsDir, an error will be returned.
func IdentifierUsage(packageAbsDir string, identifier string, includeIntraPackageUsages bool) ([]IdentifierUsageRef, string, error) {
	mod, err := gocode.NewModule(packageAbsDir)
	if err != nil {
		return nil, "", fmt.Errorf("load module: %w", err)
	}

	if !filepath.IsAbs(packageAbsDir) {
		return nil, "", fmt.Errorf("package dir must be absolute: %q", packageAbsDir)
	}

	var pkg *gocode.Package
	clean := filepath.Clean(packageAbsDir)
	if rel, relErr := filepath.Rel(mod.AbsolutePath, clean); relErr == nil {
		if rel == "." {
			rel = ""
		}
		if p, perr := mod.LoadPackageByRelativeDir(rel); perr == nil {
			pkg = p
		}
	}
	if pkg == nil {
		return nil, "", fmt.Errorf("could not resolve package from %q (expected absolute dir, module-relative dir, or import path)", packageAbsDir)
	}
	sn := pkg.GetSnippet(identifier)
	if sn == nil {
		return nil, "", fmt.Errorf("identifier %q not found in package at %q", identifier, packageAbsDir)
	}

	defAbsPath, defLine, defCol, err := definitionPosition(pkg, sn, identifier)
	if err != nil {
		return nil, "", fmt.Errorf("locate definition for %q: %w", identifier, err)
	}

	// Get references via gopls
	refs, err := goclitools.References(defAbsPath, defLine, defCol)
	if err != nil {
		return nil, "", fmt.Errorf("gopls references failed: %w", err)
	}

	var out []IdentifierUsageRef
	defDir := filepath.Clean(pkg.AbsolutePath())
	for _, r := range refs {
		// gopls includes the declaration as a "reference"; exclude it.
		if filepath.Clean(r.AbsPath) == filepath.Clean(defAbsPath) && r.Line == defLine && r.ColumnStart == defCol {
			continue
		}

		// Load only the package that contains this reference (if it belongs to the current module).
		usingDir := filepath.Dir(r.AbsPath)
		if rel, relErr := filepath.Rel(mod.AbsolutePath, usingDir); relErr == nil {
			if rel == "." {
				rel = ""
			}
			// Only load if it's within the module (avoid paths like ../../ outside root).
			if rel != "" && !strings.HasPrefix(rel, "..") {
				_, _ = mod.LoadPackageByRelativeDir(rel)
			} else if rel == "" {
				// Module root can itself be a package.
				_, _ = mod.LoadPackageByRelativeDir(rel)
			}
		}

		usingPkg, usingFile, inTestPkg := packageForFile(mod, r.AbsPath)
		if usingPkg == nil || usingFile == nil {
			// Could be outside the current module or otherwise unmapped; ignore.
			continue
		}

		inDefDir := filepath.Clean(filepath.Dir(r.AbsPath)) == defDir
		if inDefDir && !includeIntraPackageUsages {
			// Excludes both the defining package and the defining directory's _test package.
			continue
		}
		if inDefDir && includeIntraPackageUsages {
			// Keep intra-package usages (same import path) but exclude the directory's _test package.
			if usingPkg.ImportPath != pkg.ImportPath || inTestPkg {
				continue
			}
		}

		// Ensure the using package directly imports the target package import path, unless it's the defining package itself.
		if usingPkg.ImportPath != pkg.ImportPath {
			if _, ok := usingPkg.ImportPaths[pkg.ImportPath]; !ok {
				// Some files may belong to packages that don't import importPath (unlikely for a real ref), skip defensively.
				continue
			}
		}

		// Determine the snippet that contains this reference.
		byteOffset := lineColToOffset(usingFile.Contents, r.Line, r.ColumnStart)
		snippet := snippetForOffset(usingPkg, usingFile, byteOffset, inTestPkg)
		if snippet == nil {
			// Fallback: we couldn't resolve the snippet; skip this usage rather than returning partial data.
			continue
		}

		fullLine := extractFullLine(usingFile.Contents, r.Line)
		out = append(out, IdentifierUsageRef{
			ImportPath:       usingPkg.ImportPath,
			AbsFilePath:      r.AbsPath,
			Line:             r.Line,
			Column:           r.ColumnStart,
			FullLine:         fullLine,
			SnippetFullBytes: string(snippet.FullBytes()),
		})
	}
	return out, formatIdentifierUsageSummary(mod, out), nil
}

// IdentifierUsageRef holds a single usage location for an identifier.
// This matches the SPEC fields for IdentifierUsageRef.
type IdentifierUsageRef struct {
	ImportPath       string // using package's import path
	AbsFilePath      string // using file's absolute path
	Line             int    // line (1 based) where the usage occurs
	Column           int    // column (1 based) where the usage occurs
	FullLine         string // the full line in the file that uses the identifier (including \n if it exists)
	SnippetFullBytes string // the full bytes of the gocode Snippet that uses identifier
}

// definitionPosition returns absolute filename + position (1-based line/column) of the identifier inside its defining package file.
func definitionPosition(pkg *gocode.Package, sn gocode.Snippet, identifier string) (string, int, int, error) {
	fileName, err := snippetFileName(sn)
	if err != nil {
		return "", 0, 0, err
	}
	f := pkg.Files[fileName]
	if f == nil || f.AST == nil || f.FileSet == nil {
		return "", 0, 0, fmt.Errorf("file %q not found or not parsed in package %q", fileName, pkg.ImportPath)
	}

	var pos token.Pos
	switch s := sn.(type) {
	case *gocode.FuncSnippet:
		wantRecv := s.ReceiverType
		wantName := s.Name
		ast.Inspect(f.AST, func(n ast.Node) bool {
			fd, ok := n.(*ast.FuncDecl)
			if !ok {
				return true
			}
			rt, name := gocode.GetReceiverFuncName(fd)
			if name == wantName && rt == wantRecv {
				pos = fd.Name.NamePos
				return false
			}
			return true
		})
	case *gocode.TypeSnippet:
		// Find the type spec with matching name.
		ast.Inspect(f.AST, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			if ts.Name != nil && ts.Name.Name == identifier {
				pos = ts.Name.NamePos
				return false
			}
			return true
		})
	case *gocode.ValueSnippet:
		ast.Inspect(f.AST, func(n ast.Node) bool {
			vs, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for _, name := range vs.Names {
				if name != nil && name.Name == identifier {
					pos = name.NamePos
					return false
				}
			}
			return true
		})
	default:
		// Package docs or unknown snippet types: use the package name position.
		if f.AST != nil && f.AST.Name != nil {
			pos = f.AST.Name.NamePos
		}
	}
	if pos == token.NoPos {
		return "", 0, 0, errors.New("unable to find declaration position")
	}
	tpos := f.FileSet.Position(pos)
	abs := filepath.Join(pkg.AbsolutePath(), fileName)
	return abs, tpos.Line, tpos.Column, nil
}

func snippetFileName(sn gocode.Snippet) (string, error) {
	switch s := sn.(type) {
	case *gocode.FuncSnippet:
		return s.FileName, nil
	case *gocode.TypeSnippet:
		return s.FileName, nil
	case *gocode.ValueSnippet:
		return s.FileName, nil
	case *gocode.PackageDocSnippet:
		return s.FileName, nil
	default:
		return "", fmt.Errorf("unsupported snippet type %T", sn)
	}
}

// packageForFile returns the gocode.Package that owns absPath and the File inside that package.
// If the file belongs to a test package (package name suffix _test), it returns that test package and inTestPkg=true.
func packageForFile(mod *gocode.Module, absPath string) (*gocode.Package, *gocode.File, bool) {
	dir := filepath.Dir(absPath)
	base := filepath.Base(absPath)
	for _, p := range mod.Packages {
		if filepath.Clean(p.AbsolutePath()) != filepath.Clean(dir) {
			continue
		}
		// Prefer main package file map.
		if f := p.Files[base]; f != nil {
			return p, f, false
		}
		// Test package?
		if p.TestPackage != nil {
			if f := p.TestPackage.Files[base]; f != nil {
				return p.TestPackage, f, true
			}
		}
	}
	return nil, nil, false
}

// lineColToOffset converts a 1-based (line, col) to a byte offset within contents.
func lineColToOffset(contents []byte, line int, col int) int {
	if line <= 0 || col <= 0 {
		return 0
	}
	l := 1
	i := 0
	for i < len(contents) && l < line {
		if contents[i] == '\n' {
			l++
		}
		i++
	}
	// i is at start byte of desired line (or EOF).
	return i + (col - 1)
}

// extractFullLine returns the full line at 1-based line number, preserving a trailing newline if present.
func extractFullLine(contents []byte, line int) string {
	if line <= 0 {
		return ""
	}
	l := 1
	start := 0
	for i := 0; i < len(contents) && l < line; i++ {
		if contents[i] == '\n' {
			l++
			start = i + 1
		}
	}
	// Find end of line
	end := len(contents)
	for i := start; i < len(contents); i++ {
		if contents[i] == '\n' {
			end = i + 1 // include newline
			break
		}
	}
	return string(contents[start:end])
}

// snippetForOffset finds the gocode.Snippet that encloses byteOffset in usingFile.
// It falls back to AST mapping if byte search fails.
func snippetForOffset(usingPkg *gocode.Package, usingFile *gocode.File, byteOffset int, inTestPkg bool) gocode.Snippet {
	// First attempt: use AST to find the enclosing top-level declaration.
	if s := snippetViaAST(usingPkg, usingFile, byteOffset); s != nil {
		return s
	}
	// Fallback: search snippet bytes within the file contents.
	sb := usingPkg.SnippetsByFile(nil)
	list := sb[usingFile.FileName]
	for _, sn := range list {
		fb := sn.FullBytes()
		if len(fb) == 0 {
			continue
		}
		idx := bytes.Index(usingFile.Contents, fb)
		if idx < 0 {
			continue
		}
		if byteOffset >= idx && byteOffset < idx+len(fb) {
			return sn
		}
	}
	return nil
}

func snippetViaAST(usingPkg *gocode.Package, usingFile *gocode.File, byteOffset int) gocode.Snippet {
	if usingFile.FileSet == nil || usingFile.AST == nil {
		return nil
	}
	tf := usingFile.FileSet.File(usingFile.AST.Pos())
	if tf == nil {
		return nil
	}
	pos := tf.Pos(byteOffset)
	// Check top-level decls for containment.
	for _, d := range usingFile.AST.Decls {
		if !(pos >= d.Pos() && pos <= d.End()) {
			continue
		}
		switch dd := d.(type) {
		case *ast.FuncDecl:
			id := gocode.FuncIdentifierFromDecl(dd, usingFile.FileSet)
			if sn := usingPkg.GetSnippet(id); sn != nil {
				return sn
			}
		case *ast.GenDecl:
			switch dd.Tok {
			case token.TYPE:
				// Pick any type name in the decl; they all map to the same snippet if in a block, or to the single snippet.
				for _, sp := range dd.Specs {
					if ts, ok := sp.(*ast.TypeSpec); ok && ts.Name != nil {
						if sn := usingPkg.GetSnippet(ts.Name.Name); sn != nil {
							return sn
						}
					}
				}
			case token.VAR, token.CONST:
				for _, sp := range dd.Specs {
					if vs, ok := sp.(*ast.ValueSpec); ok {
						for _, nm := range vs.Names {
							if nm != nil {
								if sn := usingPkg.GetSnippet(nm.Name); sn != nil {
									return sn
								}
							}
						}
					}
				}
			}
		}
	}
	return nil
}

func formatIdentifierUsageSummary(mod *gocode.Module, usages []IdentifierUsageRef) string {
	var b strings.Builder
	moduleRoot := mod.AbsolutePath
	b.WriteString("--- References ---\n\n")

	if len(usages) == 0 {
		b.WriteString("No references found.\n")
		return b.String()
	}

	for _, usage := range usages {
		displayPath := formatUsagePath(moduleRoot, usage.AbsFilePath)
		b.WriteString(displayPath)
		b.WriteString("\n")

		line := strings.TrimRight(usage.FullLine, "\n")
		if line == "" {
			line = "(blank line)"
		}
		fmt.Fprintf(&b, "%d:\t%s\n\n", usage.Line, line)
	}

	snippets := selectSnippetContexts(moduleRoot, usages)
	if len(snippets) == 0 {
		return strings.TrimRight(b.String(), "\n")
	}

	b.WriteString("--- A handful of examples of usage ---\n\n")
	for idx, sc := range snippets {
		b.WriteString(sc.path)
		b.WriteString("\n")
		b.WriteString(sc.snippet)
		if !strings.HasSuffix(sc.snippet, "\n") {
			b.WriteString("\n")
		}
		if idx < len(snippets)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

type snippetContext struct {
	path    string
	snippet string
	length  int
}

func selectSnippetContexts(moduleRoot string, usages []IdentifierUsageRef) []snippetContext {
	seen := make(map[string]snippetContext)
	for _, usage := range usages {
		snippet := usage.SnippetFullBytes
		if snippet == "" {
			continue
		}
		if _, ok := seen[snippet]; ok {
			continue
		}
		lineCount := strings.Count(snippet, "\n") + 1
		if lineCount > maxSnippetLines {
			continue
		}
		path := formatUsagePath(moduleRoot, usage.AbsFilePath)
		seen[snippet] = snippetContext{
			path:    path,
			snippet: snippet,
			length:  len(snippet),
		}
	}

	if len(seen) == 0 {
		return nil
	}

	list := make([]snippetContext, 0, len(seen))
	for _, sc := range seen {
		list = append(list, sc)
	}

	sort.Slice(list, func(i, j int) bool {
		if list[i].length != list[j].length {
			return list[i].length < list[j].length
		}
		return list[i].path < list[j].path
	})
	if len(list) > maxSnippetContexts {
		list = list[:maxSnippetContexts]
	}
	return list
}

func formatUsagePath(moduleRoot, abs string) string {
	rel, err := filepath.Rel(moduleRoot, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(abs)
	}
	if rel == "" {
		return filepath.ToSlash(filepath.Base(abs))
	}
	return filepath.ToSlash(rel)
}
