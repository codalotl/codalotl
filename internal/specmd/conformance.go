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

// codeConforms reports whether implSnippet satisfies the code portion of specSnippet under SPEC.md conformance rules. It ignores comments and function declaration
// bodies, filters implementation-only declarations or members that the spec allows, and returns parse or formatting errors. Function literals inside value declarations
// remain part of the compared AST.
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

// docsConform reports whether implSnippet contains all comments required by specSnippet in the same AST locations. It ignores implementation comments that have
// no corresponding spec comment and reports whitespaceOnly when the only required-comment mismatch is normalized whitespace. Both snippets must parse as single
// declarations; function declarations without bodies are accepted.
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
		return fieldListDocsConform(st.Fields, it.Fields, true)
	case *ast.InterfaceType:
		it, _ := implTS.Type.(*ast.InterfaceType)
		if it == nil {
			return true, false, nil
		}
		// Recursive nested-struct rules are only for struct fields, not interface methods.
		return fieldListDocsConform(st.Methods, it.Methods, false)
	default:
		return true, false, nil
	}
}

// fieldListDocsConform reports whether implementation field or method documentation satisfies the comments required by specFL. A nil or empty spec list conforms;
// otherwise the lists are compared positionally and must have the same length. Comments present in the spec must match the implementation in the same doc-comment
// or end-of-line position, while absent spec comments impose no requirement. If every mismatch differs only by normalized documentation whitespace, ok is false
// and whitespaceOnly is true.
func fieldListDocsConform(specFL, implFL *ast.FieldList, recurseNestedStructs bool) (ok bool, whitespaceOnly bool, err error) {
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
		if recurseNestedStructs {
			if ok, wsOnly, err := nestedStructDocsConform(sf.Type, ifl.Type); err != nil {
				return false, false, err
			} else if !ok {
				recordMismatch(wsOnly)
			}
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

// nestedStructDocsConform reports whether documentation on anonymous structs inside implType satisfies the requirements in the corresponding parts of specType.
// It recurses through matching parenthesized, pointer, array, map, and channel type expressions, including map keys and values; mismatched container shapes do not
// conform, and non-struct leaf types impose no documentation requirement. If every mismatch differs only by normalized documentation whitespace, ok is false and
// whitespaceOnly is true.
func nestedStructDocsConform(specType, implType ast.Expr) (ok bool, whitespaceOnly bool, err error) {
	if specType == nil || implType == nil {
		return true, false, nil
	}
	switch st := specType.(type) {
	case *ast.ParenExpr:
		it, ok := implType.(*ast.ParenExpr)
		if !ok {
			return false, false, nil
		}
		return nestedStructDocsConform(st.X, it.X)
	case *ast.StarExpr:
		it, ok := implType.(*ast.StarExpr)
		if !ok {
			return false, false, nil
		}
		return nestedStructDocsConform(st.X, it.X)
	case *ast.ArrayType:
		it, ok := implType.(*ast.ArrayType)
		if !ok {
			return false, false, nil
		}
		return nestedStructDocsConform(st.Elt, it.Elt)
	case *ast.MapType:
		it, ok := implType.(*ast.MapType)
		if !ok {
			return false, false, nil
		}
		// Recurse into both key and value types; map keys may be anonymous structs if comparable.
		keyOK, keyWS, err := nestedStructDocsConform(st.Key, it.Key)
		if err != nil {
			return false, false, err
		}
		valOK, valWS, err := nestedStructDocsConform(st.Value, it.Value)
		if err != nil {
			return false, false, err
		}
		if keyOK && valOK {
			return true, false, nil
		}
		wsOnly := true
		if !keyOK {
			wsOnly = wsOnly && keyWS
		}
		if !valOK {
			wsOnly = wsOnly && valWS
		}
		return false, wsOnly, nil
	case *ast.ChanType:
		it, ok := implType.(*ast.ChanType)
		if !ok {
			return false, false, nil
		}
		return nestedStructDocsConform(st.Value, it.Value)
	case *ast.StructType:
		it, ok := implType.(*ast.StructType)
		if !ok {
			return false, false, nil
		}
		return fieldListDocsConform(st.Fields, it.Fields, true)
	default:
		return true, false, nil
	}
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

// snippetStartsWithFuncDecl reports whether code begins with a function declaration after leading whitespace and comments. It recognizes line and block comments,
// requires func to be followed by whitespace or end of input, and returns false for an unterminated leading block comment.
func snippetStartsWithFuncDecl(code string) bool {
	// Determine whether this fragment begins with a function declaration, ignoring any leading
	// whitespace and comments. This lets us accept SPEC snippets that omit function bodies.
	b := []byte(code)
	i := 0
	for {
		// Skip whitespace.
		for i < len(b) {
			switch b[i] {
			case ' ', '\t', '\n', '\r':
				i++
			default:
				goto nonWS
			}
		}
		return false
	nonWS:
		// Skip line comments.
		if i+1 < len(b) && b[i] == '/' && b[i+1] == '/' {
			i += 2
			for i < len(b) && b[i] != '\n' {
				i++
			}
			continue
		}
		// Skip block comments.
		if i+1 < len(b) && b[i] == '/' && b[i+1] == '*' {
			i += 2
			for i+1 < len(b) && !(b[i] == '*' && b[i+1] == '/') {
				i++
			}
			if i+1 >= len(b) {
				// Unterminated comment; treat as not-a-func snippet.
				return false
			}
			i += 2
			continue
		}
		break
	}
	rest := b[i:]
	if len(rest) < 4 || string(rest[:4]) != "func" {
		return false
	}
	if len(rest) == 4 {
		return true
	}
	switch rest[4] {
	case ' ', '\t', '\n', '\r':
		return true
	default:
		return false
	}
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

// filterImplGenDeclToSpec mutates impl so it retains only the specs and members required by spec, preserving retained implementation order. It is a no-op for nil
// declarations, token mismatches, and declaration kinds other than type, var, and const.
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

// filterImplTypeSpecMembers mutates implTS so its struct fields or interface methods retain only members required by specTS. Struct filtering recurses into anonymous
// nested struct types; interface filtering does not. Nil type specs, mismatched type kinds, and non-struct or non-interface type specs are no-ops.
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
		filterImplFieldListToSpec(st.Fields, it.Fields, true)
	case *ast.InterfaceType:
		it, ok := implTS.Type.(*ast.InterfaceType)
		if !ok {
			return
		}
		// Recursive nested-struct rules are only for struct fields, not interface methods.
		filterImplFieldListToSpec(st.Methods, it.Methods, false)
	default:
	}
}

// filterImplFieldListToSpec mutates implFL so it contains only fields or methods required by specFL. Named fields keep only implementation names present in the
// spec, embedded fields are matched by formatted type expression, and retained entries keep implementation order. When recurseNestedStructs is true, anonymous struct
// types inside matched fields are filtered recursively.
func filterImplFieldListToSpec(specFL, implFL *ast.FieldList, recurseNestedStructs bool) {
	if specFL == nil || implFL == nil {
		return
	}
	requiredNamed := map[string]*ast.Field{}
	requiredEmbedded := map[string]*ast.Field{}
	for _, f := range specFL.List {
		if f == nil {
			continue
		}
		if len(f.Names) == 0 {
			requiredEmbedded[exprString(f.Type)] = f
			continue
		}
		for _, n := range f.Names {
			if n == nil {
				continue
			}
			requiredNamed[n.Name] = f
		}
	}

	out := implFL.List[:0]
	for _, f := range implFL.List {
		if f == nil {
			continue
		}
		if len(f.Names) == 0 {
			specField := requiredEmbedded[exprString(f.Type)]
			if specField != nil {
				// Embedded fields can't be unnamed struct literals, so no recursion needed here.
				out = append(out, f)
			}
			continue
		}
		origNames := f.Names
		f.Names = f.Names[:0]
		var firstSpecField *ast.Field
		for _, n := range origNames {
			if n == nil {
				continue
			}
			specField := requiredNamed[n.Name]
			if specField != nil {
				if firstSpecField == nil {
					firstSpecField = specField
				}
				f.Names = append(f.Names, n)
			}
		}
		if len(f.Names) == 0 {
			continue
		}
		if recurseNestedStructs && firstSpecField != nil {
			// Apply "extra fields are ok" recursively for nested structs.
			filterImplNestedStructTypes(firstSpecField.Type, f.Type)
		}
		out = append(out, f)
	}
	implFL.List = out
}

// filterImplNestedStructTypes mutates implType by removing extra fields from anonymous struct types that correspond to anonymous structs in specType. It recurses
// through matching parenthesized, pointer, array, map, and channel type expressions, including both map keys and values, and stops when the expression shapes differ.
// Nil expressions and non-struct leaf types are no-ops.
func filterImplNestedStructTypes(specType, implType ast.Expr) {
	if specType == nil || implType == nil {
		return
	}
	switch st := specType.(type) {
	case *ast.ParenExpr:
		it, ok := implType.(*ast.ParenExpr)
		if !ok {
			return
		}
		filterImplNestedStructTypes(st.X, it.X)
	case *ast.StarExpr:
		it, ok := implType.(*ast.StarExpr)
		if !ok {
			return
		}
		filterImplNestedStructTypes(st.X, it.X)
	case *ast.ArrayType:
		it, ok := implType.(*ast.ArrayType)
		if !ok {
			return
		}
		filterImplNestedStructTypes(st.Elt, it.Elt)
	case *ast.MapType:
		it, ok := implType.(*ast.MapType)
		if !ok {
			return
		}
		// Recurse into both key and value types; map keys may be anonymous structs if comparable.
		filterImplNestedStructTypes(st.Key, it.Key)
		filterImplNestedStructTypes(st.Value, it.Value)
	case *ast.ChanType:
		it, ok := implType.(*ast.ChanType)
		if !ok {
			return
		}
		filterImplNestedStructTypes(st.Value, it.Value)
	case *ast.StructType:
		it, ok := implType.(*ast.StructType)
		if !ok {
			return
		}
		filterImplFieldListToSpec(st.Fields, it.Fields, true)
	default:
	}
}

// filterImplValueSpec removes names from vs that are not required and keeps matching initializer expressions when their positions can be mapped. It mutates vs in
// place and leaves ambiguous value mappings unchanged.
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
