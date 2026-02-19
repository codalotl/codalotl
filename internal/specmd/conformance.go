package specmd

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"strings"
)

// conformanceDiffType determines whether implSnippet conforms to specSnippet according to SPEC.md's Conformance section. If it does not, it returns the best diff
// type describing the mismatch.
func conformanceDiffType(specSnippet, implSnippet string) (bool, DiffType, error) {
	// Code conformance (ignoring docs) takes precedence over doc-only diffs.
	codeOK, err := codeConforms(specSnippet, implSnippet)
	if err != nil {
		return false, DiffTypeOther, err
	}
	if !codeOK {
		return false, DiffTypeCodeMismatch, nil
	}

	// Only check documentation conformance if the code already conforms.
	docOK, whitespaceOnly, err := docsConform(specSnippet, implSnippet)
	if err != nil {
		return false, DiffTypeOther, err
	}
	if docOK {
		return true, DiffTypeOther, nil
	}
	if whitespaceOnly {
		return false, DiffTypeDocWhitespace, nil
	}
	return false, DiffTypeDocMismatch, nil
}

func codeConforms(specSnippet, implSnippet string) (bool, error) {
	specDecl, err := parseDeclFragment(specSnippet)
	if err != nil {
		return false, fmt.Errorf("specmd: conformance: parse spec decl: %w", err)
	}
	implDecl, err := parseDeclFragment(implSnippet)
	if err != nil {
		return false, fmt.Errorf("specmd: conformance: parse impl decl: %w", err)
	}
	filterImplDeclToSpec(specDecl, implDecl)

	stripCommentsAndBodies(specDecl)
	stripCommentsAndBodies(implDecl)

	specNorm, err := formatDeclNoComments(specDecl)
	if err != nil {
		return false, fmt.Errorf("specmd: conformance: format spec decl: %w", err)
	}
	implNorm, err := formatDeclNoComments(implDecl)
	if err != nil {
		return false, fmt.Errorf("specmd: conformance: format impl decl: %w", err)
	}
	return specNorm == implNorm, nil
}

func docsConform(specSnippet, implSnippet string) (ok bool, whitespaceOnly bool, err error) {
	specDecl, err := parseDeclFragment(specSnippet)
	if err != nil {
		return false, false, fmt.Errorf("specmd: conformance: parse spec decl: %w", err)
	}
	implDecl, err := parseDeclFragment(implSnippet)
	if err != nil {
		return false, false, fmt.Errorf("specmd: conformance: parse impl decl: %w", err)
	}
	filterImplDeclToSpec(specDecl, implDecl)

	anyMismatch := false
	anyNonWhitespaceMismatch := false
	recordMismatch := func(wsOnly bool) {
		anyMismatch = true
		if !wsOnly {
			anyNonWhitespaceMismatch = true
		}
	}

	mismatch := func(wsOnly bool) (bool, bool, error) { return false, wsOnly, nil }

	switch s := specDecl.(type) {
	case *ast.FuncDecl:
		i, _ := implDecl.(*ast.FuncDecl)
		if ok, wsOnly := requiredCommentGroupEqual(s.Doc, i.Doc); !ok {
			return mismatch(wsOnly)
		}
		return true, false, nil
	case *ast.GenDecl:
		i, _ := implDecl.(*ast.GenDecl)
		if ok, wsOnly := requiredCommentGroupEqual(s.Doc, i.Doc); !ok {
			return mismatch(wsOnly)
		}
		// We only need to check docs for the specs that remain after filtering. If code conformed,
		// spec and impl should now have matching structure.
		if i == nil {
			return mismatch(false)
		}
		if len(s.Specs) != len(i.Specs) {
			// Structural mismatch would have shown up as a code mismatch, but be safe.
			return mismatch(false)
		}
		for idx := 0; idx < len(s.Specs); idx++ {
			ss := s.Specs[idx]
			is := i.Specs[idx]
			switch ssp := ss.(type) {
			case *ast.TypeSpec:
				isp, _ := is.(*ast.TypeSpec)
				if isp == nil || isp.Name == nil || ssp.Name == nil || isp.Name.Name != ssp.Name.Name {
					return mismatch(false)
				}
				if ok, wsOnly := requiredCommentGroupEqual(ssp.Doc, isp.Doc); !ok {
					recordMismatch(wsOnly)
				}
				if ok, wsOnly := requiredCommentGroupEqual(ssp.Comment, isp.Comment); !ok {
					recordMismatch(wsOnly)
				}
				// Recurse into struct/interface members.
				if ok, wsOnly, err := typeSpecMemberDocsConform(ssp, isp); err != nil {
					return false, false, err
				} else if !ok {
					recordMismatch(wsOnly)
				}
			case *ast.ValueSpec:
				isp, _ := is.(*ast.ValueSpec)
				if isp == nil || !identListEqual(ssp.Names, isp.Names) {
					return mismatch(false)
				}
				if ok, wsOnly := requiredCommentGroupEqual(ssp.Doc, isp.Doc); !ok {
					recordMismatch(wsOnly)
				}
				if ok, wsOnly := requiredCommentGroupEqual(ssp.Comment, isp.Comment); !ok {
					recordMismatch(wsOnly)
				}
			default:
				// Unexpected spec type; treat as mismatch if it has required docs.
				// (We don't currently expect import specs in Public API blocks.)
			}
		}
		if !anyMismatch {
			return true, false, nil
		}
		if anyNonWhitespaceMismatch {
			return false, false, nil
		}
		return false, true, nil
	default:
		// Public API snippets should only be funcs or gen decls. If we ever get here, do not
		// block conformance on docs (best-effort).
		return true, false, nil
	}
}

func typeSpecMemberDocsConform(specTS, implTS *ast.TypeSpec) (ok bool, whitespaceOnly bool, err error) {
	switch st := specTS.Type.(type) {
	case *ast.StructType:
		it, _ := implTS.Type.(*ast.StructType)
		if it == nil {
			return true, false, nil
		}
		return fieldListDocsConform(st.Fields, it.Fields)
	case *ast.InterfaceType:
		it, _ := implTS.Type.(*ast.InterfaceType)
		if it == nil {
			return true, false, nil
		}
		return fieldListDocsConform(st.Methods, it.Methods)
	default:
		return true, false, nil
	}
}

func fieldListDocsConform(specFL, implFL *ast.FieldList) (ok bool, whitespaceOnly bool, err error) {
	if specFL == nil || len(specFL.List) == 0 {
		return true, false, nil
	}
	if implFL == nil || len(specFL.List) != len(implFL.List) {
		return false, false, nil
	}
	anyMismatch := false
	anyNonWhitespaceMismatch := false
	recordMismatch := func(wsOnly bool) {
		anyMismatch = true
		if !wsOnly {
			anyNonWhitespaceMismatch = true
		}
	}
	for i := 0; i < len(specFL.List); i++ {
		sf := specFL.List[i]
		ifl := implFL.List[i]
		if ok, wsOnly := requiredCommentGroupEqual(sf.Doc, ifl.Doc); !ok {
			recordMismatch(wsOnly)
		}
		if ok, wsOnly := requiredCommentGroupEqual(sf.Comment, ifl.Comment); !ok {
			recordMismatch(wsOnly)
		}
	}
	if !anyMismatch {
		return true, false, nil
	}
	if anyNonWhitespaceMismatch {
		return false, false, nil
	}
	return false, true, nil
}

func requiredCommentGroupEqual(spec, impl *ast.CommentGroup) (ok bool, whitespaceOnly bool) {
	if spec == nil || len(spec.List) == 0 {
		return true, false
	}
	if impl == nil || len(impl.List) == 0 {
		return false, false
	}
	s := commentGroupRawText(spec)
	i := commentGroupRawText(impl)
	if s == i {
		return true, false
	}
	if normalizeDocWhitespace(s) == normalizeDocWhitespace(i) {
		return false, true
	}
	return false, false
}

func commentGroupRawText(cg *ast.CommentGroup) string {
	if cg == nil || len(cg.List) == 0 {
		return ""
	}
	parts := make([]string, 0, len(cg.List))
	for _, c := range cg.List {
		if c == nil {
			continue
		}
		parts = append(parts, c.Text)
	}
	return strings.Join(parts, "\n")
}

func identListEqual(a, b []*ast.Ident) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if a[i] == nil || b[i] == nil || a[i].Name != b[i].Name {
			return false
		}
	}
	return true
}

func parseDeclFragment(code string) (ast.Decl, error) {
	decl, _, _, err := parseDeclFromSnippetBytes([]byte(code))
	if err == nil {
		return decl, nil
	}
	if !snippetStartsWithFuncDecl(code) {
		return nil, err
	}
	// The conformance rules allow spec snippets to omit function bodies; synthesize an empty
	// body so the snippet can be parsed into an *ast.FuncDecl.
	patched := addEmptyFuncBody(code)
	decl, _, _, err2 := parseDeclFromSnippetBytes([]byte(patched))
	if err2 == nil {
		return decl, nil
	}
	return nil, err
}

func snippetStartsWithFuncDecl(code string) bool {
	lines := strings.Split(code, "\n")
	for _, ln := range lines {
		trim := strings.TrimLeft(ln, " \t")
		if trim == "" {
			continue
		}
		if strings.HasPrefix(trim, "//") {
			continue
		}
		if strings.HasPrefix(trim, "/*") {
			// If a snippet starts with a block comment, we conservatively bail out (rare in SPEC.md).
			return false
		}
		return strings.HasPrefix(trim, "func ")
	}
	return false
}

func addEmptyFuncBody(code string) string {
	trimmed := strings.TrimRight(code, " \t\r\n")
	return trimmed + " {}\n"
}

func filterImplDeclToSpec(specDecl, implDecl ast.Decl) {
	specGen, ok := specDecl.(*ast.GenDecl)
	if ok {
		implGen, _ := implDecl.(*ast.GenDecl)
		filterImplGenDeclToSpec(specGen, implGen)
		return
	}
	specFunc, ok := specDecl.(*ast.FuncDecl)
	if ok {
		_, _ = specFunc, implDecl
		return
	}
}

func filterImplGenDeclToSpec(spec, impl *ast.GenDecl) {
	if spec == nil || impl == nil {
		return
	}
	if spec.Tok != impl.Tok {
		return
	}
	switch spec.Tok {
	case token.TYPE:
		specTypes := map[string]*ast.TypeSpec{}
		for _, s := range spec.Specs {
			ts, ok := s.(*ast.TypeSpec)
			if !ok || ts.Name == nil {
				continue
			}
			specTypes[ts.Name.Name] = ts
		}
		out := impl.Specs[:0]
		for _, s := range impl.Specs {
			ts, ok := s.(*ast.TypeSpec)
			if !ok || ts.Name == nil {
				continue
			}
			st := specTypes[ts.Name.Name]
			if st == nil {
				continue
			}
			filterImplTypeSpecMembers(st, ts)
			out = append(out, ts)
		}
		impl.Specs = out
	case token.VAR, token.CONST:
		required := map[string]bool{}
		for _, s := range spec.Specs {
			vs, ok := s.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, n := range vs.Names {
				if n == nil {
					continue
				}
				required[n.Name] = true
			}
		}
		out := impl.Specs[:0]
		for _, s := range impl.Specs {
			vs, ok := s.(*ast.ValueSpec)
			if !ok {
				continue
			}
			filterImplValueSpec(required, vs)
			if len(vs.Names) == 0 {
				continue
			}
			out = append(out, vs)
		}
		impl.Specs = out
	default:
		// No filtering for other decl kinds.
	}
}

func filterImplTypeSpecMembers(specTS, implTS *ast.TypeSpec) {
	if specTS == nil || implTS == nil {
		return
	}
	switch st := specTS.Type.(type) {
	case *ast.StructType:
		it, ok := implTS.Type.(*ast.StructType)
		if !ok {
			return
		}
		filterImplFieldListToSpec(st.Fields, it.Fields)
	case *ast.InterfaceType:
		it, ok := implTS.Type.(*ast.InterfaceType)
		if !ok {
			return
		}
		filterImplFieldListToSpec(st.Methods, it.Methods)
	default:
	}
}

func filterImplFieldListToSpec(specFL, implFL *ast.FieldList) {
	if specFL == nil || implFL == nil {
		return
	}
	requiredNamed := map[string]bool{}
	requiredEmbedded := map[string]bool{}
	for _, f := range specFL.List {
		if f == nil {
			continue
		}
		if len(f.Names) == 0 {
			requiredEmbedded[exprString(f.Type)] = true
			continue
		}
		for _, n := range f.Names {
			if n == nil {
				continue
			}
			requiredNamed[n.Name] = true
		}
	}

	out := implFL.List[:0]
	for _, f := range implFL.List {
		if f == nil {
			continue
		}
		if len(f.Names) == 0 {
			if requiredEmbedded[exprString(f.Type)] {
				out = append(out, f)
			}
			continue
		}
		origNames := f.Names
		f.Names = f.Names[:0]
		for _, n := range origNames {
			if n == nil {
				continue
			}
			if requiredNamed[n.Name] {
				f.Names = append(f.Names, n)
			}
		}
		if len(f.Names) == 0 {
			continue
		}
		out = append(out, f)
	}
	implFL.List = out
}

func filterImplValueSpec(required map[string]bool, vs *ast.ValueSpec) {
	if vs == nil {
		return
	}
	origNames := vs.Names
	origValues := vs.Values
	keepIdx := make([]int, 0, len(origNames))
	newNames := make([]*ast.Ident, 0, len(origNames))
	for i, n := range origNames {
		if n == nil {
			continue
		}
		if !required[n.Name] {
			continue
		}
		keepIdx = append(keepIdx, i)
		newNames = append(newNames, n)
	}
	vs.Names = newNames
	if len(newNames) == 0 {
		vs.Values = nil
		return
	}

	switch {
	case len(origValues) == 0:
		vs.Values = nil
	case len(origValues) == 1:
		// Keep as-is: this covers common patterns like tuple returns.
		vs.Values = origValues
	case len(origValues) == len(origNames):
		newValues := make([]ast.Expr, 0, len(keepIdx))
		for _, i := range keepIdx {
			if i >= 0 && i < len(origValues) {
				newValues = append(newValues, origValues[i])
			}
		}
		vs.Values = newValues
	case len(origValues) > 1 && len(origValues) >= len(origNames):
		newValues := make([]ast.Expr, 0, len(keepIdx))
		for _, i := range keepIdx {
			if i >= 0 && i < len(origValues) {
				newValues = append(newValues, origValues[i])
			}
		}
		vs.Values = newValues
	default:
		// Unknown mapping; leave values unchanged (best-effort).
		vs.Values = origValues
	}
}

func exprString(e ast.Expr) string {
	if e == nil {
		return ""
	}
	var b bytes.Buffer
	_ = format.Node(&b, token.NewFileSet(), e)
	return b.String()
}
