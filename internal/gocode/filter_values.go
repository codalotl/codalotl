package gocode

import (
	"go/ast"
	"go/token"
)

// filterExportedValue takes an *ast.GenDecl whose Tok is token.CONST or token.VAR and returns a new *ast.GenDecl containing public specs. Declaration docs remain
// attached to the declaration. If preserveMixed is true, specs with at least one exported name are kept intact. Fully unexported specs are still elided. If preserveMixed
// is false, only exported names are kept within mixed specs. If nothing is exported, it returns nil.
func filterExportedValue(gen *ast.GenDecl, preserveMixed bool) *ast.GenDecl {
	if gen == nil {
		return gen
	}
	if gen.Tok != token.CONST && gen.Tok != token.VAR {
		panic("unexpected token type")
	}

	out := &ast.GenDecl{
		Tok:    gen.Tok,
		TokPos: gen.TokPos,
		Lparen: gen.Lparen,
		Rparen: gen.Rparen,
		Doc:    gen.Doc,
	}

	for _, sp := range gen.Specs {
		vs, ok := sp.(*ast.ValueSpec)
		if !ok {
			continue
		}

		var keepNames []*ast.Ident
		var keepVals []ast.Expr

		for _, n := range vs.Names {
			if ast.IsExported(n.Name) {
				keepNames = append(keepNames, n)
			}
		}

		if len(keepNames) == 0 {
			continue // skip specs without exported names
		}

		if preserveMixed {
			clone := *vs
			out.Specs = append(out.Specs, &clone)
			continue
		}

		for i, n := range vs.Names {
			if ast.IsExported(n.Name) {
				// Values may be shorter than Names (iota shorthand),
				// so guard the index.
				if i < len(vs.Values) {
					keepVals = append(keepVals, vs.Values[i])
				}
			}
		}

		docPos := keepNames[0].Pos()
		out.Specs = append(out.Specs, &ast.ValueSpec{
			Doc:     cloneCommentGroupBefore(vs.Doc, docPos),
			Comment: vs.Comment,
			Type:    vs.Type,   // nil for plain consts, OK to copy
			Names:   keepNames, // exported only
			Values:  keepVals,  // may be nil
		})
	}

	if len(out.Specs) == 0 {
		return nil
	}
	return out
}

func cloneCommentGroupBefore(group *ast.CommentGroup, pos token.Pos) *ast.CommentGroup {
	if group == nil {
		return nil
	}

	comments := make([]*ast.Comment, len(group.List))
	end := pos - 1
	for i := len(group.List) - 1; i >= 0; i-- {
		comment := group.List[i]
		slash := end - token.Pos(len(comment.Text))
		if slash <= token.NoPos {
			slash = pos
		}
		comments[i] = &ast.Comment{
			Slash: slash,
			Text:  comment.Text,
		}
		end = slash - 1
	}
	return &ast.CommentGroup{List: comments}
}
