package gorenamer

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"strings"
	"unicode"

	"github.com/codalotl/codalotl/internal/goclitools"
	"github.com/codalotl/codalotl/internal/gocode"
)

// renameFunc abstracts the external CLI rename tool. It is set to goclitools.Rename by default, but tests may override it to stub external behavior.
var renameFunc = goclitools.Rename

type IdentifierRename struct {
	From   string
	To     string
	DeclID string // ID from gocode.

	// Context is the full-line context where the From identifier appears. It includes whitespace but excludes newlines. But if that is ambiguous, Context may have extra
	// preceding lines (newline separated).
	Context string

	// (basename)
	FileName string

	// On input, should be nil. Will be set on output to any error
	Err error
}

// Rename performs the renames in pkg. It returns (successful renames, failed renames, error). An error is returned for a fatal error like I/O failure.
//
// If an element of renames fails to work (without triggering overall failure), its Err field will be set with the reason for failure. Possible reasons include:
//   - invalid identifier in From or To
//   - could not find DeclID
//   - could not find context, or the context provided is ambiguous.
//   - Doing the rename would introduce a conflicting variable name.
func Rename(pkg *gocode.Package, renames []IdentifierRename) ([]IdentifierRename, []IdentifierRename, error) {
	var succeeded []IdentifierRename
	var failed []IdentifierRename

	// Phase 1: validate and resolve line numbers for all renames up-front using context.
	type pending struct {
		req     IdentifierRename
		lineNum int
	}
	pendings := make([]pending, 0, len(renames))

	for _, r := range renames {
		// Validate identifiers
		if !isValidIdentifier(r.From) {
			r.Err = fmt.Errorf("invalid identifier in From: %q", r.From)
			failed = append(failed, r)
			continue
		}
		if !isValidIdentifier(r.To) {
			r.Err = fmt.Errorf("invalid identifier in To: %q", r.To)
			failed = append(failed, r)
			continue
		}

		// Look for snippet in the package
		snippet := pkg.GetSnippet(r.DeclID)
		if snippet == nil {
			r.Err = fmt.Errorf("could not find DeclID")
			failed = append(failed, r)
			continue
		}

		// Ensure file is known
		if pkg.Files[r.FileName] == nil {
			r.Err = fmt.Errorf("could not find FileName: %q", r.FileName)
			failed = append(failed, r)
			continue
		}

		// Derive the target line from context within the snippet. We only compute line now; column will be computed later.
		lineNum, findErr := locateLineFromContext(snippet, r.Context, r.From)
		if findErr != nil {
			r.Err = findErr
			failed = append(failed, r)
			continue
		}
		pendings = append(pendings, pending{req: r, lineNum: lineNum})
	}

	// Phase 2: For each pending rename, compute the column with the current file/AST state, perform the rename, then reload the package.
	for _, p := range pendings {
		r := p.req

		// Resolve absolute file path from the CURRENT package state
		file := pkg.Files[r.FileName]
		if file == nil {
			r.Err = fmt.Errorf("could not find FileName: %q", r.FileName)
			failed = append(failed, r)
			continue
		}
		absPath := file.AbsolutePath
		if absPath == "" {
			r.Err = fmt.Errorf("could not find FileName: %q", r.FileName)
			failed = append(failed, r)
			continue
		}

		// Compute column against the latest AST
		colNum, colErr := findDefColumnInFile(file, r.From, p.lineNum)
		if colErr != nil {
			r.Err = colErr
			failed = append(failed, r)
			continue
		}

		// Perform the rename via CLI tool wrapper.
		if err := renameFunc(absPath, p.lineNum, colNum, r.To); err != nil {
			r.Err = err
			failed = append(failed, r)
			continue
		}

		// Reload only the modified file from disk and reparse to refresh AST/positions for subsequent renames.
		newContents, readErr := os.ReadFile(absPath)
		if readErr != nil {
			return succeeded, failed, readErr
		}
		file.Contents = newContents
		if _, perr := file.Parse(nil); perr != nil {
			return succeeded, failed, perr
		}

		succeeded = append(succeeded, r)
	}

	return succeeded, failed, nil
}

// locateLineFromContext mirrors locateFromInContext but only resolves the absolute file line number from the snippet context. It does not consult the AST and intentionally
// ignores column resolution so that we can compute all line numbers up-front before executing any renames that may mutate file contents.
func locateLineFromContext(snippet gocode.Snippet, ctx string, from string) (int, error) {
	if strings.TrimSpace(ctx) == "" {
		return 0, fmt.Errorf("context invalid")
	}

	snippetContents := string(snippet.FullBytes())

	// Normalize newlines to \n for all code snippets:
	snippetContents = strings.ReplaceAll(snippetContents, "\r\n", "\n")
	snippetContents = strings.ReplaceAll(snippetContents, "\r", "\n")
	ctx = strings.ReplaceAll(ctx, "\r\n", "\n")
	ctx = strings.ReplaceAll(ctx, "\r", "\n")

	snippetLines := strings.Split(snippetContents, "\n")

	// Get context lines. The last line must be a complete line in the snippet somewhere:
	ctxLines := strings.Split(ctx, "\n")
	if len(ctxLines) == 0 {
		return 0, fmt.Errorf("context invalid")
	}
	lastCtxLine := ctxLines[len(ctxLines)-1]

	// Sanity check: the last line should contain from. NOTE: this is neither necessary nor sufficient.
	if !strings.Contains(lastCtxLine, from) {
		return 0, fmt.Errorf("context does not contain from")
	}

	candidateLineIdxs := make([]int, 0)
	for i := range snippetLines {
		if snippetLines[i] != lastCtxLine {
			continue
		}
		ok := true
		for back := 1; back < len(ctxLines); back++ {
			prevIdx := i - back
			if prevIdx < 0 || snippetLines[prevIdx] != ctxLines[len(ctxLines)-1-back] {
				ok = false
				break
			}
		}
		if ok {
			candidateLineIdxs = append(candidateLineIdxs, i)
		}
	}

	// Fallback 1: If exact match not found, allow missing leading whitespace on the FIRST context line only.
	if len(candidateLineIdxs) == 0 {
		trimLeft := func(s string) string { return strings.TrimLeft(s, " \t") }
		for i := range snippetLines {
			// Anchor on exact match of the last context line
			if snippetLines[i] != lastCtxLine {
				continue
			}
			ok := true
			for back := 1; back < len(ctxLines); back++ {
				prevIdx := i - back
				expected := ctxLines[len(ctxLines)-1-back]
				if prevIdx < 0 {
					ok = false
					break
				}
				// Only the first context line (earliest) is compared ignoring leading indentation.
				if back == len(ctxLines)-1 {
					if trimLeft(snippetLines[prevIdx]) != trimLeft(expected) {
						ok = false
						break
					}
				} else {
					if snippetLines[prevIdx] != expected {
						ok = false
						break
					}
				}
			}
			if ok {
				candidateLineIdxs = append(candidateLineIdxs, i)
			}
		}
	}

	// Special-case single-line context: allow missing leading whitespace on that single first line.
	if len(candidateLineIdxs) == 0 && len(ctxLines) == 1 {
		trimLeft := func(s string) string { return strings.TrimLeft(s, " \t") }
		trimmed := trimLeft(ctxLines[0])
		for i := range snippetLines {
			if trimLeft(snippetLines[i]) == trimmed {
				candidateLineIdxs = append(candidateLineIdxs, i)
			}
		}
	}

	// Fallback 2: If any context line uses leading spaces, ignore leading indentation entirely for all lines.
	if len(candidateLineIdxs) == 0 {
		hasLeadingSpaces := false
		for _, ln := range ctxLines {
			// examine only the leading whitespace run
			i := 0
			for i < len(ln) && (ln[i] == ' ' || ln[i] == '\t') {
				if ln[i] == ' ' {
					hasLeadingSpaces = true
				}
				i++
			}
			if hasLeadingSpaces {
				break
			}
		}
		if hasLeadingSpaces {
			trimLeft := func(s string) string { return strings.TrimLeft(s, " \t") }
			trimmedCtx := make([]string, len(ctxLines))
			for i := range ctxLines {
				trimmedCtx[i] = trimLeft(ctxLines[i])
			}
			for i := range snippetLines {
				if trimLeft(snippetLines[i]) != trimmedCtx[len(trimmedCtx)-1] {
					continue
				}
				ok := true
				for back := 1; back < len(trimmedCtx); back++ {
					prevIdx := i - back
					if prevIdx < 0 || trimLeft(snippetLines[prevIdx]) != trimmedCtx[len(trimmedCtx)-1-back] {
						ok = false
						break
					}
				}
				if ok {
					candidateLineIdxs = append(candidateLineIdxs, i)
				}
			}
		}
	}

	if len(candidateLineIdxs) == 0 {
		return 0, fmt.Errorf("could not find context")
	}
	if len(candidateLineIdxs) > 1 {
		return 0, fmt.Errorf("context is ambiguous")
	}

	snippetLineIdx := candidateLineIdxs[0]
	fileLineNo := snippet.Position().Line + snippetLineIdx
	return fileLineNo, nil
}

// findDefColumnInFile searches the file AST for the definition of identifier `from` that appears on the specified 1-based line, and returns its 1-based column.
func findDefColumnInFile(file *gocode.File, from string, fileLineNo int) (int, error) {
	if file == nil || file.FileSet == nil || file.AST == nil {
		return 0, fmt.Errorf("file not parsed")
	}

	fset := file.FileSet
	var col int
	found := false

	ast.Inspect(file.AST, func(n ast.Node) bool {
		if n == nil || found {
			return false
		}
		switch d := n.(type) {
		case *ast.FuncDecl:
			if d.Name != nil && d.Name.Name == from {
				pos := fset.Position(d.Name.Pos())
				if pos.Line == fileLineNo {
					col = pos.Column
					found = true
					return false
				}
			}

			// Receiver variable name (e.g., func (r T) ...)
			if d.Recv != nil {
				for _, fld := range d.Recv.List {
					for _, nm := range fld.Names {
						if nm != nil && nm.Name == from {
							pos := fset.Position(nm.Pos())
							if pos.Line == fileLineNo {
								col = pos.Column
								found = true
								return false
							}
						}
					}
				}
			}

			// Parameter names
			if d.Type != nil && d.Type.Params != nil {
				for _, fld := range d.Type.Params.List {
					for _, nm := range fld.Names {
						if nm != nil && nm.Name == from {
							pos := fset.Position(nm.Pos())
							if pos.Line == fileLineNo {
								col = pos.Column
								found = true
								return false
							}
						}
					}
				}
			}

			// Named result parameters
			if d.Type != nil && d.Type.Results != nil {
				for _, fld := range d.Type.Results.List {
					for _, nm := range fld.Names {
						if nm != nil && nm.Name == from {
							pos := fset.Position(nm.Pos())
							if pos.Line == fileLineNo {
								col = pos.Column
								found = true
								return false
							}
						}
					}
				}
			}
		case *ast.TypeSpec:
			if d.Name != nil && d.Name.Name == from {
				pos := fset.Position(d.Name.Pos())
				if pos.Line == fileLineNo {
					col = pos.Column
					found = true
					return false
				}
			}

			// Struct fields and interface methods inside this type
			switch t := d.Type.(type) {
			case *ast.StructType:
				if t.Fields != nil {
					for _, fld := range t.Fields.List {
						for _, nm := range fld.Names {
							if nm != nil && nm.Name == from {
								pos := fset.Position(nm.Pos())
								if pos.Line == fileLineNo {
									col = pos.Column
									found = true
									return false
								}
							}
						}
					}
				}
			case *ast.InterfaceType:
				if t.Methods != nil {
					for _, m := range t.Methods.List {
						for _, nm := range m.Names {
							if nm != nil && nm.Name == from {
								pos := fset.Position(nm.Pos())
								if pos.Line == fileLineNo {
									col = pos.Column
									found = true
									return false
								}
							}
						}
					}
				}
			}
		case *ast.ValueSpec:
			for _, name := range d.Names {
				if name != nil && name.Name == from {
					pos := fset.Position(name.Pos())
					if pos.Line == fileLineNo {
						col = pos.Column
						found = true
						return false
					}
				}
			}
		case *ast.AssignStmt:
			// Short variable definition: :=
			if d.Tok == token.DEFINE {
				for _, lhs := range d.Lhs {
					if ident, ok := lhs.(*ast.Ident); ok && ident != nil && ident.Name == from {
						pos := fset.Position(ident.Pos())
						if pos.Line == fileLineNo {
							col = pos.Column
							found = true
							return false
						}
					}
				}
			}
		case *ast.RangeStmt:
			// Range statement variables (Key, Value) can be identifiers introduced by :=
			// or assignments to existing identifiers. Match either position on the line.
			if d.Key != nil {
				if ident, ok := d.Key.(*ast.Ident); ok && ident.Name == from {
					pos := fset.Position(ident.Pos())
					if pos.Line == fileLineNo {
						col = pos.Column
						found = true
						return false
					}
				}
			}
			if d.Value != nil {
				if ident, ok := d.Value.(*ast.Ident); ok && ident.Name == from {
					pos := fset.Position(ident.Pos())
					if pos.Line == fileLineNo {
						col = pos.Column
						found = true
						return false
					}
				}
			}
		case *ast.FuncLit:
			// Parameters and named results for function literals
			if d.Type != nil && d.Type.Params != nil {
				for _, fld := range d.Type.Params.List {
					for _, nm := range fld.Names {
						if nm != nil && nm.Name == from {
							pos := fset.Position(nm.Pos())
							if pos.Line == fileLineNo {
								col = pos.Column
								found = true
								return false
							}
						}
					}
				}
			}
			if d.Type != nil && d.Type.Results != nil {
				for _, fld := range d.Type.Results.List {
					for _, nm := range fld.Names {
						if nm != nil && nm.Name == from {
							pos := fset.Position(nm.Pos())
							if pos.Line == fileLineNo {
								col = pos.Column
								found = true
								return false
							}
						}
					}
				}
			}
		case *ast.LabeledStmt:
			if d.Label != nil && d.Label.Name == from {
				pos := fset.Position(d.Label.Pos())
				if pos.Line == fileLineNo {
					col = pos.Column
					found = true
					return false
				}
			}
		}
		return true
	})

	if !found {
		return 0, fmt.Errorf("could not find defining AST for identifier on line")
	}
	return col, nil
}

func isValidIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if r != '_' && !unicode.IsLetter(r) {
				return false
			}
			continue
		}
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
