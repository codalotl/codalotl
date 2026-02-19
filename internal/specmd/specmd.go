package specmd

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/codalotl/codalotl/internal/updatedocs"

	"github.com/codalotl/codalotl/internal/gocode"
)

// Spec represents a SPEC.md on disk.
type Spec struct {
	AbsPath string // absolute file path of SPEC.md
	Body    string // Full contents of the file
}

// Read reads the path to create a Spec. If the path is not a "SPEC.md" file (case-sensitive), an error is returned. The file is NOT parsed, nor verified to be markdown.
func Read(path string) (*Spec, error) {
	if filepath.Base(path) != "SPEC.md" {
		return nil, fmt.Errorf("specmd: Read: path must be a SPEC.md file: %q", path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("specmd: Read: abs path: %w", err)
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("specmd: Read: read file: %w", err)
	}
	return &Spec{
		AbsPath: abs,
		Body:    string(b),
	}, nil
}

// Validate parses Body as a markdown file, and ensures each Go code block has valid code without syntax errors. The code is not checked for type errors. The first
// error encountered is returned; nil if no errors.
func (s *Spec) Validate() error {
	if s == nil {
		return errors.New("specmd: Validate: nil Spec")
	}
	md, err := parseMarkdown([]byte(s.Body))
	if err != nil {
		return err
	}
	for _, b := range md.allGoFences {
		_, err := parseGoFileFragment(b.code)
		if err != nil {
			return err
		}
	}
	return nil
}

// GoCodeBlocks returns all multi-line Go code blocks in a ```go``` fence.
//   - These must be triple-backtick and multi-line, not inline `single-backtick` code spans.
//   - The fences MUST be tagged with `go`. Go code in triple-backtick fences without the Go tag is not included.
//
// If there are any problems parsing the markdown or if there are malformed code blocks (e.g. no closing triple-backticks), an error is returned. The Go code itself
// is not checked for errors.
func (s *Spec) GoCodeBlocks() ([]string, error) {
	if s == nil {
		return nil, errors.New("specmd: GoCodeBlocks: nil Spec")
	}
	md, err := parseMarkdown([]byte(s.Body))
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(md.allGoFences))
	for _, b := range md.allGoFences {
		if !b.multiLine {
			continue
		}
		out = append(out, b.code)
	}
	return out, nil
}

// PublicAPIGoCodeBlocks returns those Go code blocks that are part of the public API of a package. This is determined by:
//   - If the code block has {api} in the info string. This includes things like {api, other_tag}.
//   - If the code block is in any headered section that includes "public api" (case-insensitive).
//   - If the code block is in any nested headered section of the above "public api". E.g., `## Public API\n### Types\n<code block>`.
//
// Errors are returned for the same reasons as GoCodeBlocks.
func (s *Spec) PublicAPIGoCodeBlocks() ([]string, error) {
	if s == nil {
		return nil, errors.New("specmd: PublicAPIGoCodeBlocks: nil Spec")
	}
	md, err := parseMarkdown([]byte(s.Body))
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(md.goFencesInPublicAPI))
	for _, b := range md.goFencesInPublicAPI {
		if !b.multiLine {
			continue
		}
		out = append(out, b.code)
	}
	return out, nil
}

// FormatGoCodeBlocks runs each Go code block through the equivalent of `gofmt`, updating the file on disk and s.Body.
//
// If reflowWidth is 0, documentation is not reflowed. If reflowWidth is > 0, documentation in each code block is reflowed to the specified width.
//
// If any Go code block has erroneous Go code (e.g. syntax error), it is ignored. The other Go code blocks are still formatted.
//
// It returns true if any modifications to the SPEC.md were made. An error is returned for file I/O issues or for invalid markdown. Go code with syntax errors do
// not cause errors.
func (s *Spec) FormatGoCodeBlocks(reflowWidth int) (bool, error) {
	if s == nil {
		return false, errors.New("specmd: FormatGoCodeBlocks: nil Spec")
	}
	if s.AbsPath == "" {
		return false, errors.New("specmd: FormatGoCodeBlocks: empty AbsPath")
	}
	if reflowWidth < 0 {
		return false, fmt.Errorf("specmd: FormatGoCodeBlocks: reflowWidth must be >= 0: %d", reflowWidth)
	}
	src := []byte(s.Body)
	md, err := parseMarkdown(src)
	if err != nil {
		return false, err
	}
	var reflower *goCodeBlockReflower
	if reflowWidth > 0 {
		r, err := newGoCodeBlockReflower(reflowWidth)
		if err != nil {
			return false, err
		}
		reflower = r
		defer reflower.close()
	}
	// Format every go fenced code block (not just public API), but ignore invalid Go.
	var edits []textEdit
	for _, b := range md.allGoFences {
		formatted, ok := gofmtFragment(b.code)
		if !ok {
			continue
		}
		out := formatted
		if reflower != nil {
			reflowed, ok, err := reflower.reflow(out)
			if err != nil {
				return false, err
			}
			if ok {
				out = reflowed
			}
		}
		if out != b.code {
			edits = append(edits, textEdit{
				start:       b.contentStart,
				end:         b.contentEnd,
				replacement: []byte(out),
			})
		}
	}
	if len(edits) == 0 {
		return false, nil
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	updated := applyTextEdits(src, edits)
	if err := os.WriteFile(s.AbsPath, updated, 0o644); err != nil {
		return false, fmt.Errorf("specmd: FormatGoCodeBlocks: write file: %w", err)
	}
	s.Body = string(updated)
	return true, nil
}

type DiffType int

const (
	// Unknown difference
	DiffTypeOther DiffType = iota

	// At least one snippet ID is missing in the implementation.
	DiffTypeImplMissing

	// All IDs are present in the impl, but they span different snippets. E.g., one var block in SPEC, but impl has separate `var` decls.
	DiffTypeIDMismatch

	// Both spec and impl have the same snippet, but code is different. E.g. diff function args; diff var values. Any docs are NOT considered. Whitespace is also not
	// considered.
	DiffTypeCodeMismatch

	// Docs between the two are mismatched.
	DiffTypeDocMismatch

	// Whitespace is different (e.g. SPEC uses spaces but impl uses tabs).
	DiffTypeDocWhitespace
)

// SpecDiff represents a difference from the SPEC.md and the actual implementation in .go files. Each diff corresponds to one `gocode.Snippet`. Note that one code
// block may contain multiple `gocode.Snippet` values, and one `gocode.Snippet` may contain multiple IDs. A correspondence between snippet and impl is made only
// by exact ID matches.
type SpecDiff struct {
	// The IDs of the snippet. Often this is just one string (e.g., a function name). It can be multiple IDs for things like var blocks. These IDs will match a snippet
	// in the SPEC.md exactly.
	IDs []string

	SpecSnippet string // The snippet in the SPEC. May be "" if missing.
	SpecLine    int    // The line number in the SPEC.
	ImplSnippet string // The snippet in the actual implementation. May be "" if missing.
	ImplFile    string // The .go file containing the impl.
	ImplLine    int    // The line number.

	// DiffType represents the reason the specs differ. DiffTypeOther is a fallback if no pre-contemplated reason is discovered; otherwise, we prefer to return a lower
	// iota value (e.g. DiffTypeCodeMismatch is returned if there's both a DiffTypeCodeMismatch and a DiffTypeDocMismatch).
	DiffType DiffType
}

// ImplemenationDiffs finds differences between the public API declared in the SPEC.md and the actual public API in the corresponding Go package. It only checks
// those identifiers defined in the SPEC.md - if the public API is a strict superset, no differences are returned. If no differences are found, nil is returned.
//   - Only PublicAPIGoCodeBlocks are checked.
//   - If PublicAPIGoCodeBlocks contains method bodies, they are ignored (we're only checking the interface).
//   - That being said, variable declarations must match (and an anonymous function can be assigned to a variable - it is checked in this case).
//   - If the corresponding Go package cannot be loaded (ex: syntax error; no Go files), an error is returned.
func (s *Spec) ImplemenationDiffs() ([]SpecDiff, error) {
	if s == nil {
		return nil, errors.New("specmd: ImplemenationDiffs: nil Spec")
	}
	if s.AbsPath == "" {
		return nil, errors.New("specmd: ImplemenationDiffs: empty AbsPath")
	}
	// Parse markdown once so we can compute SPEC line numbers.
	md, err := parseMarkdown([]byte(s.Body))
	if err != nil {
		return nil, err
	}
	var specDecls []specDecl
	for _, b := range md.goFencesInPublicAPI {
		if !b.multiLine {
			continue
		}
		decls, err := parseSpecDeclsFromCodeBlock(b.code, b.contentStartLine)
		if err != nil {
			return nil, err
		}
		specDecls = append(specDecls, decls...)
	}
	if len(specDecls) == 0 {
		return nil, nil
	}
	pkg, err := loadImplPackageForSpec(s.AbsPath)
	if err != nil {
		return nil, err
	}
	var diffs []SpecDiff
	for _, sd := range specDecls {
		diff := SpecDiff{
			IDs:         append([]string(nil), sd.IDs...),
			SpecSnippet: sd.Snippet,
			SpecLine:    sd.SpecLine,
			DiffType:    DiffTypeOther,
		}
		implSnippet, implPos, implBytes, implDocRaw, implNorm, implErr := findImplForSpecDecl(pkg, sd)
		if implErr != nil {
			return nil, implErr
		}
		diff.ImplSnippet = implSnippet
		diff.ImplFile = implPos.file
		diff.ImplLine = implPos.line
		if implPos.missing {
			diff.DiffType = DiffTypeImplMissing
			diffs = append(diffs, diff)
			continue
		}
		if implPos.idMismatch {
			diff.DiffType = DiffTypeIDMismatch
			diffs = append(diffs, diff)
			continue
		}
		if sd.NormalizedCode != implNorm {
			diff.DiffType = DiffTypeCodeMismatch
			diffs = append(diffs, diff)
			continue
		}
		_ = implBytes
		if sd.DocRaw != implDocRaw {
			if normalizeDocWhitespace(sd.DocRaw) == normalizeDocWhitespace(implDocRaw) {
				diff.DiffType = DiffTypeDocWhitespace
			} else {
				diff.DiffType = DiffTypeDocMismatch
			}
			diffs = append(diffs, diff)
			continue
		}
		// No diff for this snippet.
	}
	if len(diffs) == 0 {
		return nil, nil
	}
	return diffs, nil
}

type implSnippetPos struct {
	file       string
	line       int
	missing    bool
	idMismatch bool
}

func findImplForSpecDecl(pkg *gocode.Package, sd specDecl) (implSnippet string, pos implSnippetPos, implBytes []byte, implDocRaw string, implNormCode string, fnErr error) {
	if len(sd.IDs) == 0 {
		return "", implSnippetPos{missing: true}, nil, "", "", nil
	}
	var snippets []gocode.Snippet
	for _, id := range sd.IDs {
		sn := pkg.GetSnippet(id)
		if sn == nil {
			return "", implSnippetPos{missing: true}, nil, "", "", nil
		}
		snippets = append(snippets, sn)
	}
	first := snippets[0]
	for _, sn := range snippets[1:] {
		if sn != first {
			// All IDs exist, but they point at different decl blocks.
			return "", implSnippetPos{idMismatch: true}, nil, "", "", nil
		}
	}
	p := first.Position()
	pos.file = filepath.Base(p.Filename)
	pos.line = p.Line
	implBytes = first.FullBytes()
	implSnippet = string(first.Bytes())
	decl, fset, wrapper, err := parseDeclFromSnippetBytes(implBytes)
	if err != nil {
		return "", implSnippetPos{}, nil, "", "", fmt.Errorf("specmd: ImplemenationDiffs: parse impl snippet %v: %w", sd.IDs, err)
	}
	implDocRaw = rawDocForDecl(decl, fset, wrapper)
	stripCommentsAndBodies(decl)
	implNormCode, err = formatDeclNoComments(decl)
	if err != nil {
		return "", implSnippetPos{}, nil, "", "", fmt.Errorf("specmd: ImplemenationDiffs: format impl decl %v: %w", sd.IDs, err)
	}
	return implSnippet, pos, implBytes, implDocRaw, implNormCode, nil
}
func loadImplPackageForSpec(specAbsPath string) (*gocode.Package, error) {
	mod, err := gocode.NewModule(specAbsPath)
	if err != nil {
		return nil, fmt.Errorf("specmd: ImplemenationDiffs: load module: %w", err)
	}
	specDir := filepath.Dir(specAbsPath)
	relDir, err := filepath.Rel(mod.AbsolutePath, specDir)
	if err != nil {
		return nil, fmt.Errorf("specmd: ImplemenationDiffs: compute package relative dir: %w", err)
	}
	if relDir == "" {
		relDir = "."
	}
	pkg, err := mod.LoadPackageByRelativeDir(relDir)
	if err != nil {
		return nil, fmt.Errorf("specmd: ImplemenationDiffs: load package %q: %w", relDir, err)
	}
	return pkg, nil
}

type specDecl struct {
	IDs            []string
	Snippet        string
	SpecLine       int
	DocRaw         string
	NormalizedCode string
}

func parseSpecDeclsFromCodeBlock(code string, codeStartLine int) ([]specDecl, error) {
	f, fset, wrapper, err := parseGoFileFragmentWithSource(code)
	if err != nil {
		return nil, err
	}
	var decls []specDecl
	for _, d := range f.Decls {
		ids := idsForDecl(d, fset)
		if len(ids) == 0 {
			continue
		}
		startPos := d.Pos()
		if doc := docGroupForDecl(d); doc != nil {
			startPos = doc.Pos()
		}
		wrapperPos := fset.Position(startPos)
		specLine := codeStartLine + (wrapperPos.Line - 3) // wrapper is "package p\n\n" (2 lines); first code line is wrapper line 3.
		if specLine < 1 {
			specLine = 1
		}
		snippet, err := specSnippetForDecl(d, fset, wrapper)
		if err != nil {
			return nil, err
		}
		docRaw := rawDocForDecl(d, fset, wrapper)
		stripCommentsAndBodies(d)
		norm, err := formatDeclNoComments(d)
		if err != nil {
			return nil, err
		}
		decls = append(decls, specDecl{
			IDs:            ids,
			Snippet:        snippet,
			SpecLine:       specLine,
			DocRaw:         docRaw,
			NormalizedCode: norm,
		})
	}
	return decls, nil
}
func idsForDecl(d ast.Decl, fset *token.FileSet) []string {
	switch dd := d.(type) {
	case *ast.FuncDecl:
		return []string{gocode.FuncIdentifierFromDecl(dd, fset)}
	case *ast.GenDecl:
		var ids []string
		for _, s := range dd.Specs {
			switch ss := s.(type) {
			case *ast.TypeSpec:
				ids = append(ids, ss.Name.Name)
			case *ast.ValueSpec:
				for _, n := range ss.Names {
					ids = append(ids, n.Name)
				}
			}
		}
		return ids
	default:
		return nil
	}
}
func docGroupForDecl(d ast.Decl) *ast.CommentGroup {
	switch dd := d.(type) {
	case *ast.FuncDecl:
		return dd.Doc
	case *ast.GenDecl:
		return dd.Doc
	default:
		return nil
	}
}
func specSnippetForDecl(d ast.Decl, fset *token.FileSet, wrapper []byte) (string, error) {
	start := d.Pos()
	if doc := docGroupForDecl(d); doc != nil {
		start = doc.Pos()
	}
	startOff, err := offsetForPos(fset, start)
	if err != nil {
		return "", err
	}
	switch dd := d.(type) {
	case *ast.FuncDecl:
		// For functions, ignore bodies: slice only up to the end of the signature.
		endOff, err := offsetForPos(fset, dd.Type.End())
		if err != nil {
			return "", err
		}
		if endOff < startOff {
			endOff = startOff
		}
		return string(wrapper[startOff:endOff]), nil
	default:
		endOff, err := offsetForPos(fset, d.End())
		if err != nil {
			return "", err
		}
		if endOff < startOff {
			endOff = startOff
		}
		return string(wrapper[startOff:endOff]), nil
	}
}
func parseDeclFromSnippetBytes(snippet []byte) (decl ast.Decl, fset *token.FileSet, wrapper []byte, err error) {
	wrapper = makeWrappedGoFileSource(string(snippet))
	fset = token.NewFileSet()
	f, err := parser.ParseFile(fset, "snippet.go", wrapper, parser.ParseComments|parser.AllErrors)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(f.Decls) != 1 {
		return nil, nil, nil, fmt.Errorf("expected exactly 1 decl, found %d", len(f.Decls))
	}
	return f.Decls[0], fset, wrapper, nil
}
func parseGoFileFragment(code string) (*ast.File, error) {
	f, _, _, err := parseGoFileFragmentWithSource(code)
	return f, err
}
func parseGoFileFragmentWithSource(code string) (*ast.File, *token.FileSet, []byte, error) {
	wrapper := makeWrappedGoFileSource(code)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "spec.go", wrapper, parser.ParseComments|parser.AllErrors)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("specmd: go syntax error: %w", err)
	}
	return f, fset, wrapper, nil
}
func makeWrappedGoFileSource(code string) []byte {
	// We intentionally treat code blocks as Go source fragments without a package clause.
	// Using a deterministic package name ensures stable parse positions.
	var b bytes.Buffer
	b.WriteString("package p\n\n")
	b.WriteString(code)
	return b.Bytes()
}
func stripCommentsAndBodies(d ast.Decl) {
	ast.Inspect(d, func(n ast.Node) bool {
		switch nn := n.(type) {
		case *ast.FuncDecl:
			nn.Doc = nil
			nn.Body = nil
		case *ast.GenDecl:
			nn.Doc = nil
		case *ast.TypeSpec:
			nn.Doc = nil
			nn.Comment = nil
		case *ast.ValueSpec:
			nn.Doc = nil
			nn.Comment = nil
		case *ast.Field:
			nn.Doc = nil
			nn.Comment = nil
		}
		return true
	})
}
func formatDeclNoComments(d ast.Decl) (string, error) {
	var buf bytes.Buffer
	if err := format.Node(&buf, token.NewFileSet(), d); err != nil {
		return "", err
	}
	return buf.String(), nil
}
func rawDocForDecl(d ast.Decl, fset *token.FileSet, wrapper []byte) string {
	doc := docGroupForDecl(d)
	if doc == nil {
		return ""
	}
	startOff, err := offsetForPos(fset, doc.Pos())
	if err != nil {
		return ""
	}
	endOff, err := offsetForPos(fset, doc.End())
	if err != nil {
		return ""
	}
	if startOff < 0 || endOff < startOff || endOff > len(wrapper) {
		return ""
	}
	return string(wrapper[startOff:endOff])
}
func normalizeDocWhitespace(s string) string {
	// Canonicalize whitespace for doc whitespace-only diffs.
	return strings.Join(strings.Fields(s), " ")
}
func offsetForPos(fset *token.FileSet, pos token.Pos) (int, error) {
	f := fset.File(pos)
	if f == nil {
		return 0, errors.New("no file for position")
	}
	return f.Offset(pos), nil
}
func gofmtFragment(code string) (formatted string, ok bool) {
	_, _, _, err := parseGoFileFragmentWithSource(code)
	if err != nil {
		return "", false
	}
	formattedFile, err := format.Source(makeWrappedGoFileSource(code))
	if err != nil {
		return "", false
	}
	// Strip "package p" and the following blank line.
	const prefix = "package p\n\n"
	if !bytes.HasPrefix(formattedFile, []byte(prefix)) {
		return "", false
	}
	frag := formattedFile[len(prefix):]
	return string(frag), true
}

type goCodeBlockReflower struct {
	tempDir     string
	goFilePath  string
	mod         *gocode.Module
	pkg         *gocode.Package
	reflowWidth int
}

func newGoCodeBlockReflower(reflowWidth int) (*goCodeBlockReflower, error) {
	dir, err := os.MkdirTemp("", "specmd-reflow-*")
	if err != nil {
		return nil, fmt.Errorf("specmd: FormatGoCodeBlocks: create temp dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	goMod := strings.Join([]string{
		"module example.com/specmdtmp",
		"",
		"go 1.24.4",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		cleanup()
		return nil, fmt.Errorf("specmd: FormatGoCodeBlocks: write temp go.mod: %w", err)
	}
	mod, err := gocode.NewModule(dir)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("specmd: FormatGoCodeBlocks: load temp module: %w", err)
	}
	return &goCodeBlockReflower{
		tempDir:     dir,
		goFilePath:  filepath.Join(dir, "spec.go"),
		mod:         mod,
		reflowWidth: reflowWidth,
	}, nil
}

func (r *goCodeBlockReflower) close() {
	if r == nil || r.tempDir == "" {
		return
	}
	_ = os.RemoveAll(r.tempDir)
}

func (r *goCodeBlockReflower) reflow(code string) (string, bool, error) {
	if r == nil {
		return "", false, nil
	}
	if err := os.WriteFile(r.goFilePath, makeWrappedGoFileSource(code), 0o644); err != nil {
		return "", false, fmt.Errorf("specmd: FormatGoCodeBlocks: write temp file: %w", err)
	}
	var pkg *gocode.Package
	var err error
	if r.pkg == nil {
		pkg, err = r.mod.LoadPackageByRelativeDir(".")
	} else {
		pkg, err = r.pkg.Reload()
	}
	if err != nil {
		return "", false, fmt.Errorf("specmd: FormatGoCodeBlocks: load temp package: %w", err)
	}
	r.pkg = pkg
	newPkg, _, err := updatedocs.ReflowAllDocumentation(pkg, updatedocs.Options{
		Reflow:         true,
		ReflowMaxWidth: r.reflowWidth,
	})
	if err != nil {
		return "", false, fmt.Errorf("specmd: FormatGoCodeBlocks: reflow docs: %w", err)
	}
	if newPkg != nil {
		r.pkg = newPkg
	}
	updated, err := os.ReadFile(r.goFilePath)
	if err != nil {
		return "", false, fmt.Errorf("specmd: FormatGoCodeBlocks: read temp file: %w", err)
	}
	frag, ok := unwrapWrappedGoFileSource(updated)
	return frag, ok, nil
}

func unwrapWrappedGoFileSource(src []byte) (string, bool) {
	if !bytes.HasPrefix(src, []byte("package p")) {
		return "", false
	}
	// Strip the package clause line, plus any immediately following blank lines.
	i := bytes.IndexByte(src, '\n')
	if i == -1 {
		return "", false
	}
	j := i + 1
	for j < len(src) && (src[j] == '\n' || src[j] == '\r') {
		j++
	}
	return string(src[j:]), true
}

type textEdit struct {
	start       int
	end         int
	replacement []byte
}

func applyTextEdits(src []byte, edits []textEdit) []byte {
	updated := append([]byte(nil), src...)
	for _, e := range edits {
		if e.start < 0 {
			e.start = 0
		}
		if e.end < e.start {
			e.end = e.start
		}
		if e.end > len(updated) {
			e.end = len(updated)
		}
		var b bytes.Buffer
		b.Grow(len(updated) - (e.end - e.start) + len(e.replacement))
		b.Write(updated[:e.start])
		b.Write(e.replacement)
		b.Write(updated[e.end:])
		updated = b.Bytes()
	}
	return updated
}
