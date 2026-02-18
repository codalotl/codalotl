package updatedocs

import (
	"github.com/codalotl/codalotl/internal/gocode"
	"go/ast"
	"go/token"
	"sort"
	"strings"
)

// blockFormattingEntity is a spec, field, or floating comment within a block/struct/interface.
type blockFormattingEntity struct {
	// true if entity is just a floating comment. If it is, entityStartLine == codeStartLine == start of comment, endLine == end of comment, hasDoc == true
	isFloater bool

	entityStartLine int  // start line of the code or doc
	codeStartLine   int  // start line of the code
	endLine         int  // end line of the code
	hasDoc          bool // true if .Doc != nil or if floater
	docLine         int  // if hasDoc, first line of .Doc; otherwise 0
}

// getFormatEditsForBlockOrStruct returns edits to file so that blocks and type structs/interfaces are nicely formatted (ex: consts in a block with EOL comments
// don't have newlines between them). Only those decls with identifiers are modified. File is assumed to be gofmt'ed already.
//
// Rules:
//   - If two adjacent one-line specs/fields have EOL comments (or no comment), then no blank lines should be between them.
//   - There should be no blank line at the beginning, nor at the end, of a value block or struct.
//   - Floating comments (not attached to spec/field) must have leading/trailing blanks (except at beginning/end of block - the rules above take precedence).
//   - If a field/spec has a .Doc comment, there should be a blank line above the comment, and below the spec/field (the rules above take precedence).
func getFormatEditsForBlockOrStruct(file *gocode.File, identifiers []string) []LineEdit {
	if file == nil || file.AST == nil || file.FileSet == nil {
		return nil
	}

	// Return value:
	var edits []LineEdit

	// Build quick-lookup set of identifiers we care about.
	identSet := make(map[string]struct{}, len(identifiers))
	for _, id := range identifiers {
		identSet[id] = struct{}{}
	}

	lines := strings.Split(string(file.Contents), "\n")

	// Build attached for checking if a comment is attached to .Doc or .Comment:
	detatched := getDetatchedCommentsInFile(file)

	for _, decl := range file.AST.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}

		// const/var blocks
		if (gd.Tok == token.CONST || gd.Tok == token.VAR) && gd.Lparen.IsValid() {
			relevant := false
			for _, sp := range gd.Specs {
				vs := sp.(*ast.ValueSpec)
				for _, n := range vs.Names {
					if _, ok := identSet[n.Name]; ok {
						relevant = true
						break
					}
				}
				if relevant {
					break
				}
			}
			if relevant {
				var blockEntities []blockFormattingEntity
				for _, raw := range gd.Specs {
					spec := raw.(*ast.ValueSpec)
					docLine := 0
					codeStartLine := file.FileSet.Position(spec.Pos()).Line
					entityStartLine := codeStartLine
					if spec.Doc != nil {
						docLine = file.FileSet.Position(spec.Doc.Pos()).Line
						entityStartLine = docLine
					}
					blockEntities = append(blockEntities, blockFormattingEntity{
						entityStartLine: entityStartLine,
						codeStartLine:   codeStartLine,
						endLine:         file.FileSet.Position(spec.End()).Line,
						hasDoc:          spec.Doc != nil,
						docLine:         docLine,
					})
				}

				// Add floating comments within the block parentheses as entities.
				addFloatingCommentEntities(file, gd.Lparen, gd.Rparen, detatched, &blockEntities)

				edits = append(edits, editsForLinesAndBlockEntities(lines, blockEntities)...)
			}
		}

		// NOTE: type blocks could in theory be handled here.

		//
		// struct types
		//

		// create processStructType as a variable so that it can be recursively called:
		var processStructType func(structType *ast.StructType)
		processStructType = func(structType *ast.StructType) {
			var blockEntities []blockFormattingEntity
			for _, field := range structType.Fields.List {
				docLine := 0
				codeStartLine := file.FileSet.Position(field.Pos()).Line
				entityStartLine := codeStartLine
				if field.Doc != nil {
					docLine = file.FileSet.Position(field.Doc.Pos()).Line
					entityStartLine = docLine
				}
				blockEntities = append(blockEntities, blockFormattingEntity{
					entityStartLine: entityStartLine,
					codeStartLine:   codeStartLine,
					endLine:         file.FileSet.Position(field.End()).Line,
					hasDoc:          field.Doc != nil,
					docLine:         docLine,
				})
			}

			// Add floating comments within the struct braces as entities.
			addFloatingCommentEntities(file, structType.Fields.Opening, structType.Fields.Closing, detatched, &blockEntities)

			edits = append(edits, editsForLinesAndBlockEntities(lines, blockEntities)...)

			// Now recursively handle struct types:
			for _, field := range structType.Fields.List {
				nestedStructType, ok := field.Type.(*ast.StructType)
				if !ok {
					continue
				}
				processStructType(nestedStructType)
			}
		}

		//
		// interface types (do not recurse like structs)
		//
		processInterfaceType := func(interfaceType *ast.InterfaceType) {
			if interfaceType == nil || interfaceType.Methods == nil {
				return
			}

			var blockEntities []blockFormattingEntity
			for _, field := range interfaceType.Methods.List {
				docLine := 0
				codeStartLine := file.FileSet.Position(field.Pos()).Line
				entityStartLine := codeStartLine
				if field.Doc != nil {
					docLine = file.FileSet.Position(field.Doc.Pos()).Line
					entityStartLine = docLine
				}
				blockEntities = append(blockEntities, blockFormattingEntity{
					entityStartLine: entityStartLine,
					codeStartLine:   codeStartLine,
					endLine:         file.FileSet.Position(field.End()).Line,
					hasDoc:          field.Doc != nil,
					docLine:         docLine,
				})
			}

			// Add floating comments within the interface braces as entities.
			addFloatingCommentEntities(file, interfaceType.Methods.Opening, interfaceType.Methods.Closing, detatched, &blockEntities)

			edits = append(edits, editsForLinesAndBlockEntities(lines, blockEntities)...)
		}

		if gd.Tok == token.TYPE {
			for _, sp := range gd.Specs {
				ts := sp.(*ast.TypeSpec)

				if _, ok := identSet[ts.Name.Name]; !ok {
					continue
				}

				if structType, ok := ts.Type.(*ast.StructType); ok {
					processStructType(structType)
					continue
				}
				if interfaceType, ok := ts.Type.(*ast.InterfaceType); ok {
					processInterfaceType(interfaceType)
					continue
				}
			}
		}
	}

	return edits
}

// getDetatchedCommentsInFile returns a set of all comments in a file that are 1. top level (not in a function) and 2. not attached to .Doc or .Comment of any decl,
// spec, or field.
func getDetatchedCommentsInFile(file *gocode.File) map[*ast.CommentGroup]bool {
	if file == nil || file.AST == nil || file.FileSet == nil {
		return nil
	}

	// Collect all comment groups that are explicitly attached to nodes' Doc/Comment fields.
	attached := map[*ast.CommentGroup]bool{}

	// Track function declaration ranges to exclude any comments within functions.
	var funcRanges [][2]token.Pos

	if file.AST.Doc != nil {
		attached[file.AST.Doc] = true
	}

	ast.Inspect(file.AST, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.GenDecl:
			if t.Doc != nil {
				attached[t.Doc] = true
			}
			for _, sp := range t.Specs {
				switch s := sp.(type) {
				case *ast.ValueSpec:
					if s.Doc != nil {
						attached[s.Doc] = true
					}
					if s.Comment != nil {
						attached[s.Comment] = true
					}
				case *ast.TypeSpec:
					if s.Doc != nil {
						attached[s.Doc] = true
					}
					if s.Comment != nil {
						attached[s.Comment] = true
					}
				case *ast.ImportSpec:
					if s.Doc != nil {
						attached[s.Doc] = true
					}
					if s.Comment != nil {
						attached[s.Comment] = true
					}
				}
			}
		case *ast.FuncDecl:
			if t.Doc != nil {
				attached[t.Doc] = true
			}
			// Record full func span (signature + body) to filter comments within.
			funcRanges = append(funcRanges, [2]token.Pos{t.Pos(), t.End()})
			return false // stop recursion into function
		case *ast.Field:
			// Covers struct fields, interface methods, and function params/results.
			if t.Doc != nil {
				attached[t.Doc] = true
			}
			if t.Comment != nil {
				attached[t.Comment] = true
			}
		}
		return true
	})

	// Helper: whether a comment group lies entirely within any recorded function range.
	inFunc := func(cg *ast.CommentGroup) bool {
		for _, r := range funcRanges {
			if cg.Pos() >= r[0] && cg.End() <= r[1] {
				return true
			}
		}
		return false
	}

	// Build the set of detached, top-level comments (exclude those inside functions).
	detatched := map[*ast.CommentGroup]bool{}
	for _, cg := range file.AST.Comments {
		if attached[cg] {
			continue
		}
		if inFunc(cg) {
			continue
		}
		detatched[cg] = true
	}

	return detatched
}

// addFloatingCommentEntities adds floating comment entities within (start, end) that are not attached in the correct spot.
func addFloatingCommentEntities(file *gocode.File, start, end token.Pos, detatched map[*ast.CommentGroup]bool, entities *[]blockFormattingEntity) {
	if file == nil || file.AST == nil || file.FileSet == nil {
		return
	}
	for cg := range detatched {
		if cg.Pos() > start && cg.End() < end {
			startLine := file.FileSet.Position(cg.Pos()).Line
			endLine := file.FileSet.Position(cg.End()).Line
			*entities = append(*entities, blockFormattingEntity{
				isFloater:       true,
				entityStartLine: startLine,
				codeStartLine:   startLine,
				endLine:         endLine,
				hasDoc:          true,
				docLine:         startLine,
			})
		}
	}

	sortEntitiesBySourceOrder(*entities)
}

// sortEntitiesBySourceOrder sorts entities by their starting line (and end line as tiebreaker).
func sortEntitiesBySourceOrder(entities []blockFormattingEntity) {
	sort.Slice(entities, func(i, j int) bool {
		if entities[i].entityStartLine == entities[j].entityStartLine {
			return entities[i].endLine < entities[j].endLine
		}
		return entities[i].entityStartLine < entities[j].entityStartLine
	})
}

// editsForLinesAndBlockEntities returns edits (in the form of blank line insertions and deletions) to nicely format value blocks and structs. See getFormatEditsForBlockOrStruct.
//
// Lines are the lines in the file, and each item in blockEntities is a spec, field, or floating comment. This function is called on a per-block basis (so all blockEntities
// belong to the same block or struct). Note that .startLine is 1-based, while lines is 0-based. This function relies on the fact that lines is already gofmt, so
// it doesn't have to deal with many types of formatting issues.
func editsForLinesAndBlockEntities(lines []string, blockEntities []blockFormattingEntity) []LineEdit {
	var edits []LineEdit

	if len(blockEntities) == 0 || len(lines) == 0 {
		return nil
	}

	// isBlank returns true if line is blank. lineNo is the 1-based line number. It permits sloppy/oob indexing.
	isBlank := func(lineNo int) bool {
		if lineNo <= 0 || lineNo > len(lines) {
			return false
		}
		return strings.TrimSpace(lines[lineNo-1]) == ""
	}

	// edited[lineNo]=true means we already edited that line
	edited := map[int]bool{}

	// Remove leading blank line:
	if isBlank(blockEntities[0].entityStartLine - 1) {
		line := blockEntities[0].entityStartLine - 1
		edits = append(edits, LineEdit{
			EditOp: EditOpRemoveBlankLine,
			Line:   line,
		})
		edited[line] = true
	}

	// Remove trailing blank line:
	lastEntityIdx := len(blockEntities) - 1
	if isBlank(blockEntities[lastEntityIdx].endLine + 1) {
		line := blockEntities[lastEntityIdx].endLine + 1
		edits = append(edits, LineEdit{
			EditOp: EditOpRemoveBlankLine,
			Line:   line,
		})
		edited[line] = true
	}

	for i, entity := range blockEntities {
		hasNext := i+1 < len(blockEntities)

		// If two adjacent 1-line entities don't have doc comments, then no blanks should be between them:
		if hasNext {
			nextEntity := blockEntities[i+1]
			if !entity.hasDoc && !nextEntity.hasDoc && (entity.entityStartLine == entity.endLine) && (nextEntity.entityStartLine == nextEntity.endLine) {
				line := entity.endLine + 1
				if isBlank(line) {
					edits = append(edits, LineEdit{
						EditOp: EditOpRemoveBlankLine,
						Line:   line,
					})
					edited[line] = true
					continue
				}
			}
		}

		if entity.hasDoc {
			// entities with Doc comments should have a blank line above the comment:
			if i > 0 {
				if !isBlank(entity.entityStartLine-1) && !edited[entity.entityStartLine] {
					edits = append(edits, LineEdit{
						EditOp: EditOpInsertBlankLineAbove,
						Line:   entity.entityStartLine,
					})
					edited[entity.entityStartLine] = true
				}
			}

			// entities with Doc comments should have a blank line below the entity:
			if hasNext {
				line := entity.endLine + 1
				if !isBlank(line) && !edited[line] {
					edits = append(edits, LineEdit{
						EditOp: EditOpInsertBlankLineAbove,
						Line:   line,
					})
					edited[line] = true
				}
			}
		}
	}

	return edits
}
