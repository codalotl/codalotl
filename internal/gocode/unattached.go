package gocode

import (
	"go/ast"
	"go/token"
)

// UnattachedComment represents a top-level comment group that is not attached to any declaration snippet (func/type/var/const) and does not appear in any snippet's
// FullBytes.
//
// It records the raw comment text (newline-terminated), the file it belongs to, a pointer to the next Snippet that follows this comment in the file (if any), and
// AST-related info for position queries.
type UnattachedComment struct {
	FileName string  // file name (no dirs) where the comment was found (ex: "foo.go")
	Comment  string  // the raw comment bytes (including // or /* */), newline-terminated
	Next     Snippet // the next snippet in this file that appears after the comment; nil if none

	// AbovePackage is true if this unattached comment appears before the package clause (i.e., its end offset is at or before the position of the "package" token).
	AbovePackage bool

	fileSet *token.FileSet
	group   *ast.CommentGroup
}

// snippetSpan returns the [start,end) byte offsets for a snippet within its file. The computation mirrors how snippet bytes are extracted in the respective extract*
// helpers.
func snippetSpan(s Snippet) (fileName string, start, end int) {
	switch sn := s.(type) {
	case *FuncSnippet:
		fileName = sn.FileName
		if sn.decl.Doc != nil {
			start = sn.fileSet.Position(sn.decl.Doc.Pos()).Offset
		} else {
			start = sn.fileSet.Position(sn.decl.Pos()).Offset
		}
		if sn.decl.Body != nil {
			end = sn.fileSet.Position(sn.decl.Body.End()).Offset
		} else {
			// No body: end at type end
			end = sn.fileSet.Position(sn.decl.Type.End()).Offset
		}
		return

	case *ValueSnippet:
		fileName = sn.FileName
		if sn.decl.Doc != nil {
			start = sn.fileSet.Position(sn.decl.Doc.Pos()).Offset
		} else {
			start = sn.fileSet.Position(sn.decl.Pos()).Offset
		}
		// End mirrors extractValueSnippet logic for single-spec trailing comments
		end = sn.fileSet.Position(sn.decl.End()).Offset
		if !sn.IsBlock {
			for _, spec := range sn.decl.Specs {
				if vs, ok := spec.(*ast.ValueSpec); ok && vs.Comment != nil {
					cend := sn.fileSet.Position(vs.Comment.End()).Offset
					if cend > end {
						end = cend
					}
				}
			}
		}
		return

	case *TypeSnippet:
		fileName = sn.FileName
		if sn.decl.Doc != nil {
			start = sn.fileSet.Position(sn.decl.Doc.Pos()).Offset
		} else {
			start = sn.fileSet.Position(sn.decl.Pos()).Offset
		}
		end = sn.fileSet.Position(sn.decl.End()).Offset
		if !sn.IsBlock {
			for _, spec := range sn.decl.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok && ts.Comment != nil {
					cend := sn.fileSet.Position(ts.Comment.End()).Offset
					if cend > end {
						end = cend
					}
				}
			}
		}
		return

	case *PackageDocSnippet:
		fileName = sn.FileName
		// Start at doc start; end at end of the package name token
		start = sn.fileSet.Position(sn.file.Doc.Pos()).Offset
		end = sn.fileSet.Position(sn.file.Name.End()).Offset
		return
	default:
		return "", 0, 0
	}
}

// extractUnattachedComments finds top-level comments that are not included in any snippet's FullBytes or bytes span for the given file and returns them, ordered
// by their position. The nextSnippet field is populated by scanning for the next snippet that starts after the comment ends within the same file.
func extractUnattachedComments(file *File, fileSnippets []Snippet) ([]*UnattachedComment, error) {
	// Build snippet spans and a start-ordered list for next-snippet resolution.
	type span struct {
		start int
		end   int
		snip  Snippet
	}
	var spans []span
	for _, s := range fileSnippets {
		fn, start, end := snippetSpan(s)
		if fn != file.FileName {
			continue
		}
		if start == 0 && end == 0 {
			continue
		}
		spans = append(spans, span{start: start, end: end, snip: s})
	}

	// Sort spans by start offset (simple insertion sort due to few snippets per file typically).
	for i := 1; i < len(spans); i++ {
		j := i
		for j > 0 && spans[j-1].start > spans[j].start {
			spans[j-1], spans[j] = spans[j], spans[j-1]
			j--
		}
	}

	// Helper: does [aStart,aEnd) intersect [bStart,bEnd)?
	intersects := func(aStart, aEnd, bStart, bEnd int) bool {
		return aStart < bEnd && bStart < aEnd
	}

	var result []*UnattachedComment
	for _, cg := range file.AST.Comments {
		cStart := file.FileSet.Position(cg.Pos()).Offset
		cEnd := file.FileSet.Position(cg.End()).Offset

		// If the comment is within any snippet span, it is considered attached/inside and thus ignored.
		attached := false
		for _, sp := range spans {
			if intersects(cStart, cEnd, sp.start, sp.end) {
				attached = true
				break
			}
		}
		if attached {
			continue
		}

		// Not attached → collect as UnattachedComment
		comment := ensureNewline(string(file.Contents[cStart:cEnd]))

		// Resolve next snippet in this file: first span whose start > comment end
		var next Snippet
		for _, sp := range spans {
			if sp.start > cEnd {
				next = sp.snip
				break
			}
		}

		// Determine if this comment ends at or before the package token position.
		above := false
		if file.AST != nil {
			pkgStart := file.FileSet.Position(file.AST.Package).Offset
			if cEnd <= pkgStart {
				above = true
			}
		}

		result = append(result, &UnattachedComment{
			FileName:     file.FileName,
			Comment:      comment,
			Next:         next,
			AbovePackage: above,
			fileSet:      file.FileSet,
			group:        cg,
		})
	}

	return result, nil
}
