package updatedocs

import (
	"fmt"
	"go/format"
	"sort"
	"strings"

	"github.com/codalotl/codalotl/internal/gocode"
)

type EditOp string

const (
	EditOpRemoveBlankLine      EditOp = "remove_blank_line"
	EditOpInsertBlankLineAbove EditOp = "insert_blank_line_above"

	// Upserts a comment at the end of the line. Comment must be single-line. If Line is blank or a comment, it just replaces that line with the comment.
	EditOpSetEOLComment EditOp = "set_eol_comment"

	// Removes the comment at the end of the line. If no comment, no op. If the line is only a comment, removes that comment AND line.
	EditOpRemoveEOLComment EditOp = "remove_eol_comment"
)

type LineEdit struct {
	EditOp EditOp

	// 1-based line number
	Line int

	// either "" or starts with "//". Multiline comments are separated fine (separated by \n). Can end in \n (either is fine, it's automatically handled). Comment should have no leading
	// whitespace (indentation handled automatically).
	Comment string
}

type LineEditError struct {
	LineEdit        // the offending edit
	Message  string // why LineEdit failed
}

func (e *LineEditError) Error() string {
	return e.Message
}

// NOTE: I'm not sure if we want to accept a file or a package. For now, it's going to be file-based, but we can change to package if it's more convenient.
// (we'd may want a package because any time we edit a file, the package loses a coherent FileSet).
// (we could also do both -- ApplyLineEditsToFile and ApplyLineEdits)

// ApplyLineEdits applies edits to file and returns a new File, without mutating file. An error is returned if any edit could not be applied or if another fatal error occurred (ex:
// I/O error). All edits are applied simultaneously -- Line refers to the original line number in the original file, so the caller DOESN'T need to calculate what the Line *will* be
// after the first edit. If an edit is nonsensical (ex: remove blank line on a line that isn't blank), or if two edits refer to the same line, an error will be returned. If an error
// occurred due to a bad LineEdit, the error will be *LineEditError. After all edits are made, gofmt will be applied to the file (ex: if you insert two blank lines, they'll be collapsed
// to one).
//
// BUG: Edits whose Line is beyond the end of the file are silently ignored instead of returning an error.
func ApplyLineEdits(file *gocode.File, edits []LineEdit) (*gocode.File, error) {

	// Sort edits stably by line
	sort.SliceStable(edits, func(i, j int) bool {
		return edits[i].Line < edits[j].Line
	})

	// If duplicate line, error
	if len(edits) > 1 {
		for i := 0; i < len(edits)-1; i++ {
			if edits[i].Line == edits[i+1].Line {
				return nil, &LineEditError{
					LineEdit: edits[i],
					Message:  "duplicate edit for same line",
				}
			}
		}
	}

	// This removes warnings for staticcheck
	var contents []byte
	if file != nil {
		contents = file.Contents
	}

	lines := strings.Split(string(contents), "\n")
	var outLines []string

	// Build a map of line number -> column where the first line comment (// ...) starts:
	lineToCommentCol := make(map[int]int)
	if file != nil && file.AST != nil && file.FileSet != nil {
		for _, cg := range file.AST.Comments {
			for _, c := range cg.List {
				if !strings.HasPrefix(c.Text, "//") {
					// Only interested in // comments for EOL processing.
					continue
				}
				pos := file.FileSet.Position(c.Slash)
				// Keep the left-most comment on the line (smallest column).
				if existing, ok := lineToCommentCol[pos.Line]; !ok || pos.Column < existing {
					lineToCommentCol[pos.Line] = pos.Column
				}
			}
		}
	}

	editIdx := 0
	for i, line := range lines {
		currentLine := i + 1
		var edit *LineEdit
		if editIdx < len(edits) && edits[editIdx].Line == currentLine {
			edit = &edits[editIdx]
			editIdx++
		}

		if edit != nil {
			switch edit.EditOp {
			case EditOpInsertBlankLineAbove:
				outLines = append(outLines, "")
				outLines = append(outLines, line)

			case EditOpRemoveBlankLine:
				if strings.TrimSpace(line) != "" {
					return nil, &LineEditError{
						LineEdit: *edit,
						Message:  "not a blank line",
					}
				}
				// Just don't append the line

			case EditOpSetEOLComment:
				if edit.Comment == "" || !strings.HasPrefix(edit.Comment, "//") {
					return nil, &LineEditError{
						LineEdit: *edit,
						Message:  "comment must be non-empty and start with //",
					}
				}
				trimmedComment := strings.TrimSpace(edit.Comment)
				if strings.Contains(trimmedComment, "\n") {
					return nil, &LineEditError{
						LineEdit: *edit,
						Message:  "comment must be single-line when used with EditOpSetEOLComment",
					}
				}
				// Determine the portion of the line before any existing comment.
				codePart := line
				if col, ok := lineToCommentCol[currentLine]; ok {
					idx := min(max(col-1, 0), len(line)) // convert 1-based to 0-based, and apply bounds
					codePart = line[:idx]
				}

				codePart = strings.TrimRight(codePart, " \t")

				if strings.TrimSpace(codePart) == "" {
					// Blank line or comment-only line – replace entire line with the new comment.
					outLines = append(outLines, trimmedComment)
				} else {
					outLines = append(outLines, codePart+" "+trimmedComment)
				}

			case EditOpRemoveEOLComment:
				if col, ok := lineToCommentCol[currentLine]; ok {
					idx := min(max(col-1, 0), len(line)) // convert 1-based to 0-based, and apply bounds
					codePart := strings.TrimRight(line[:idx], " \t")

					if strings.TrimSpace(codePart) == "" {
						// Line was only a comment – remove it entirely (do not append).
					} else {
						outLines = append(outLines, codePart)
					}
				} else {
					// No comment on this line – leave it untouched.
					outLines = append(outLines, line)
				}

			default:
				return nil, &LineEditError{
					LineEdit: *edit,
					Message:  fmt.Sprintf("unknown edit op %q", edit.EditOp),
				}
			}
		} else {
			outLines = append(outLines, line)
		}
	}

	output := strings.Join(outLines, "\n")
	formatted, err := format.Source([]byte(output))
	if err != nil {
		return nil, fmt.Errorf("error formatting generated code: %w\n---\n%s", err, output)
	}

	newFile := file.Clone()
	err = newFile.PersistNewContents(formatted, true)
	if err != nil {
		return nil, fmt.Errorf("could not create File from formatted source: %w", err)
	}

	return newFile, nil
}
