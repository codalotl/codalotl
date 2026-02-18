package gocode

import (
	"go/ast"
	"go/token"
)

// filterExportedValue takes an *ast.GenDecl whose Tok is token.CONST or token.VAR and returns a new *ast.GenDecl containing only exported names. If nothing is exported,
// it returns nil.
func filterExportedValue(gen *ast.GenDecl) *ast.GenDecl {
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
		Doc:    nil, // Comments are handled elsewhere
	}

	for _, sp := range gen.Specs {
		vs, ok := sp.(*ast.ValueSpec)
		if !ok {
			continue
		}

		var keepNames []*ast.Ident
		var keepVals []ast.Expr

		for i, n := range vs.Names {
			if ast.IsExported(n.Name) {
				keepNames = append(keepNames, n)
				// Values may be shorter than Names (iota shorthand),
				// so guard the index.
				if i < len(vs.Values) {
					keepVals = append(keepVals, vs.Values[i])
				}
			}
		}

		if len(keepNames) == 0 {
			continue // skip specs without exported names
		}

		out.Specs = append(out.Specs, &ast.ValueSpec{
			Doc:     vs.Doc,
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
