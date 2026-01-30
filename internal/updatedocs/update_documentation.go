package updatedocs

import (
	"bytes"
	"fmt"
	"github.com/codalotl/codalotl/internal/gocode"
	"go/ast"
	"go/token"
	"strings"
)

type SnippetError struct {
	Snippet           string // the snippet passed to UpdateDocumentation
	UserErrorMessage  string // human or AI-readable message on why the snippet is invalid
	Err               error  // TODO: remove this. Actual full error received when dealing with the snippet; potentially nil, if there's no underlying error
	PartiallyRejected bool   // at least part of (maybe all of) this snippet was not applied due to Reject flags (ex: RejectUpdates) in the Options
}

type parsedSnippet struct {
	originalSnippet   string
	unwrappedSnippet  string
	ast               *ast.File
	fileSet           *token.FileSet
	kind              snippetKind
	partiallyRejected bool // set to true during application if any rejected components of snippet
}

type Options struct {
	//
	// Reflow options:
	//

	Reflow         bool // true -> reflow Doc comments to the specified ReflowMaxWidth (put more text per line, or less)
	ReflowTabWidth int  // only if Reflow; width of tab measured in spaces; if 0, defaults to 4
	ReflowMaxWidth int  // only if Reflow; when to wrap text; if 0, defaults to 80

	// If RejectUpdates is true, we will not replace any existing documentation for a symbol. Note that a single snippet may update some docs but others may be rejected (ex: a struct type
	// with many fields; a value block with many specs).
	RejectUpdates bool
}

// UpdateDocumentation updates documentation in pkg based on the supplied snippets and writes any changes to disk. Snippets may be raw Go declarations or wrapped in triple backticks,
// and can target package docs, functions/methods, types (including fields/methods on structs/interfaces), and vars/consts (single decls or blocks).
//
// Each snippet is unwrapped, parsed, and validated for general Go correctness, then matched to the package and applied. Comment text may be reflowed when options.Reflow is true (defaults:
// ReflowMaxWidth=80, ReflowTabWidth=4). If options.RejectUpdates is true, existing documentation for matching symbols will not be replaced; such cases are reported as partial rejections.
//
// The function can partially succeed:
//   - newPkg is a reloaded Package when at least one file was updated; otherwise it is nil and callers should continue using the original pkg.
//   - updatedFiles lists the filenames that were written, even if err is non-nil.
//   - snippetErrors contains per-snippet failures (e.g., unwrap/parse issues, mismatches) and partial rejections; these do not produce an overall error.
//   - err is reserved for fatal conditions (e.g., I/O). Some writes may already have succeeded.
//
// Passing no snippets returns an error.
func UpdateDocumentation(pkg *gocode.Package, snippets []string, options ...Options) (*gocode.Package, []string, []SnippetError, error) {
	if len(snippets) == 0 {
		return nil, nil, nil, fmt.Errorf("no snippets supplied")
	}

	// Extract options and set defaults:
	var opts Options
	if len(options) > 0 {
		opts = options[0]
	}
	if opts.Reflow {
		if opts.ReflowMaxWidth == 0 {
			opts.ReflowMaxWidth = 80
		}
		if opts.ReflowTabWidth == 0 {
			opts.ReflowTabWidth = 4
		}
	}

	// snippetErrors will be our return value for snippet validation
	var snippetErrors []SnippetError

	// Our first pass will populate parsedSnippets
	var parsedSnippets []parsedSnippet

	// Unwrap each snippet from triple backtick wrapping, and parse/validate each of them, collecting parsedSnippets and snippetErrors.
	// Note that validation ensures the snippet is valid for *some* source code, not necessarily ours. Further validation happens as snippets
	// are applied to the pkg.
	for _, originalSnippet := range snippets {
		snippet, err := stripBackticks(originalSnippet)
		if err != nil {
			snippetErrors = append(snippetErrors, SnippetError{
				Snippet:          originalSnippet,
				UserErrorMessage: fmt.Sprintf("Snippet could not be unwrapped from backticks: %v", err),
				Err:              err,
			})
		} else {
			parsed, fset, kind, err := parseValidateSnippet(pkg.Name, snippet, opts)
			if err != nil {
				se := SnippetError{
					Snippet:          originalSnippet,
					UserErrorMessage: err.Error(),
					Err:              err,
				}

				if _, ok := err.(rejectionError); ok {
					se.PartiallyRejected = true
				}
				snippetErrors = append(snippetErrors, se)
			} else {
				parsedSnippets = append(parsedSnippets, parsedSnippet{
					originalSnippet:  originalSnippet,
					unwrappedSnippet: snippet,
					ast:              parsed,
					fileSet:          fset,
					kind:             kind,
				})
			}
		}
	}

	if len(parsedSnippets) == 0 {
		return nil, nil, snippetErrors, nil
	}

	var updatedFiles []string

	for _, ps := range parsedSnippets {

		var updatedFile *gocode.File
		var snippetErr *SnippetError
		var err error

		// call updateXxxDoc:
		// - Any error we get back is fatal (ex: IO error)
		// - If we get back a snippetErr, we cannot have updated the file.
		// - If a file was updated, it must be returned.
		// - As a 4th implicit return value, we may or may not set ps.partiallyRejected to indicate some (or all) of the snippet was rejected due to (for example) not options.RejectUpdates.
		switch ps.kind {
		case snippetKindPackageDoc:
			updatedFile, snippetErr, err = updatePackageDoc(pkg, &ps, opts)
		case snippetKindFunc:
			updatedFile, snippetErr, err = updateFunctionDoc(pkg, &ps, opts)
		case snippetKindType:
			updatedFile, snippetErr, err = updateTypeDoc(pkg, &ps, opts)
		case snippetKindVar, snippetKindConst:
			updatedFile, snippetErr, err = updateValueDoc(pkg, &ps, opts)
		default:
			panic("unsupported kind")
		}
		if err != nil {
			return nil, updatedFiles, snippetErrors, err
		} else if snippetErr != nil {
			if ps.partiallyRejected {
				snippetErr.PartiallyRejected = true
			}
			snippetErrors = append(snippetErrors, *snippetErr)

			if updatedFile != nil {
				panic("unexpected updatedFile not nil")
			}
		} else {

			if ps.partiallyRejected {
				// TODO: consider if updatedFile implies we definitely updated the file, meaning the snippet rejection was partial and not full.
				snippetErrors = append(snippetErrors, SnippetError{
					Snippet:           ps.originalSnippet,
					UserErrorMessage:  "Part or all of snippet was not applied due to options restrictions.",
					PartiallyRejected: true,
				})
			}

			if updatedFile != nil {
				pkg.Files[updatedFile.FileName] = updatedFile
				updatedFiles = appendSet(updatedFiles, updatedFile.FileName) // save the new file in pkg.Files so it can be re-updated in future snippets
			}
		}

	}

	// Bypass reloading the package if nothing was updated:
	if len(updatedFiles) == 0 {
		return nil, nil, snippetErrors, nil
	}

	// Reload so that all files share a unified file set:
	newPkg, err := pkg.Reload()
	if err != nil {
		return nil, updatedFiles, snippetErrors, err
	}

	return newPkg, updatedFiles, snippetErrors, nil
}

// hasNoDocs returns true if group is nil or contains no comment lines.
func hasNoDocs(group *ast.CommentGroup) bool {
	return group == nil || len(group.List) == 0
}

// commentBlockFromGroup returns the comment block, newline (\n) terminated. No docs return "". leadingNewline puts "\n" at the front, so that the string can be inserted as a Doc comment,
// giving breathing room from whatever is above it (multiple newlines are eventually collapsed to one by a gofmt step in this package). There is no leading whitespace inserted here
// (that's fixed, again, by gofmt).
func commentBlockFromGroup(group *ast.CommentGroup, leadingNewline bool) string {
	if hasNoDocs(group) {
		return ""
	}

	var buf bytes.Buffer

	if leadingNewline {
		buf.WriteRune('\n')
	}

	// Write the new actual comment to buf, which will be a \n terminated comment block.
	for _, c := range group.List {
		commentLine := ensureNewline(c.Text)
		_, err := buf.WriteString(commentLine)
		if err != nil {
			panic(fmt.Errorf("unexpected error during buffer string write"))
		}
	}

	return buf.String()
}

// eolCommentFromGroup returns "" for no EOL comment, a non-newline-terminated string (ex: " // some comment"), or panics if this is unexpectedly multiline. The returned string always
// has exactly one leading space before the original comment text, which may begin with "//" or "/*".
func eolCommentFromGroup(group *ast.CommentGroup) string {
	if hasNoDocs(group) {
		return ""
	}
	if len(group.List) != 1 {
		panic("unexpected multi-line EOL comment")
	}
	return " " + strings.TrimSpace(group.List[0].Text)
}

// spliceStringIntoBytes returns a copy of b with b[spliceStart:spliceEnd] replaced by str. It panics if spliceStart < 0, spliceEnd < spliceStart, or spliceEnd > len(b).
func spliceStringIntoBytes(b []byte, str string, spliceStart, spliceEnd int) []byte {
	if spliceStart < 0 || spliceEnd < spliceStart || spliceEnd > len(b) {
		panic("invalid splice range")
	}

	newLen := len(b) - (spliceEnd - spliceStart) + len(str)
	out := make([]byte, newLen)

	copy(out, b[:spliceStart])                      // prefix
	copy(out[spliceStart:], str)                    // injected string — copy reads directly from the string, no allocation
	copy(out[spliceStart+len(str):], b[spliceEnd:]) // suffix

	return out
}

// deleteRangeInBytes returns a copy of b with b[spliceStart:spliceEnd] deleted. If deleteLeftWhitespace, it will also delete whitespace at spliceStart-1 and leftwards until it gets
// to a non-whitespace character, or AFTER it deletes a single newline. This allows it to delete .Doc comments, including the line they are on. It panics if spliceStart < 0, spliceEnd
// < spliceStart, or spliceEnd > len(b).
func deleteRangeInBytes(b []byte, spliceStart, spliceEnd int, deleteLeftWhitespace bool) []byte {
	if spliceStart < 0 || spliceEnd < spliceStart || spliceEnd > len(b) {
		panic("invalid splice range")
	}

	if deleteLeftWhitespace {
		for spliceStart > 0 {
			newStart := spliceStart - 1
			if b[newStart] == ' ' || b[newStart] == '\t' {
				spliceStart--
				continue
			}
			if b[newStart] == '\n' {
				spliceStart--
				break
			}
			break
		}
	}

	return spliceStringIntoBytes(b, "", spliceStart, spliceEnd)
}

// ensureNewline ensures s ends in exactly 1 newline ('\n').
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

// appendSet appends item to set only if set doesn't already contain it.
func appendSet(set []string, item string) []string {
	for _, existing := range set {
		if existing == item {
			return set
		}
	}
	return append(set, item)
}
