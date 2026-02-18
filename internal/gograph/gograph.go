package gograph

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/importer"
	"go/token"
	"go/types"
	"path/filepath"

	"github.com/codalotl/codalotl/internal/gocode"
)

type Graph struct {
	pkg        *gocode.Package
	info       *types.Info
	checkedPkg *types.Package
	fset       *token.FileSet

	// All package-level identifiers in the graph (see intraUses for explanation of identifiers). All identifiers in intraUses must be here. This set also contains identifiers
	// that lack edges.
	identifiers map[string]struct{}

	// intraUses is populated from info.Uses. Keys to the map are defining package-level identifiers (ex: `type Foo struct{}` -> "Foo"). Values are a set of intra-package-level
	// identifiers that the defining identifier references. Methods are identified with "receiver.funcName" style (ex: "*Foo.Bar" or "Foo.Bar"). Even for recursive funcs,
	// there are never self-references (ex: "A" -> {"A"} is disallowed), but the graph can form cycles. There is no semantic difference between "x" -> {} and the "x"
	// key not being in the map. Examples:
	//   - If `type Foo struct { B Bar; Q Qux }`, then "Foo" -> {"Bar", "Qux"}
	//   - If `var x Foo`, then "x" -> {"Foo"}
	//   - If `var x int`, then "x" -> {}
	//   - If `func foo() { bar() }`, then "foo" -> {"bar"}
	//   - If `func (v *Foo) Bar(b *Baz) { b.Qux() }`, then "*Foo.Bar" -> {"Foo", "Baz", "*Baz.Qux"} // methods reference their receiver type; functions reference args/returns;
	//     the body references a method
	intraUses map[string]map[string]struct{}

	// crossPackageUses is like intraUses but for cross-package uses. Maps defining-package-level-identifier -> set of cross package refs.
	crossPackageUses map[string]map[ExternalID]struct{}

	// testIdentifiers are all identifiers defined in _test.go files (f.IsTest). Includes TestXxx/BenchXxx/etc funcs, as well as test file helpers/types/etc.
	testIdentifiers map[string]struct{}
}

type ExternalID struct {
	ImportPath string // import path from the perspective of the using package. There should be a matching `import "myproj/codeai/gocode"` in the file
	ID         string // identifier being referenced
}

// stubImporter returns empty stub packages for ALL non-stdlib imports
type stubImporter struct {
	stdImporter types.Importer
}

func (s *stubImporter) Import(path string) (*types.Package, error) {
	// Does this path resolve to a directory under $GOROOT/src ? —
	// i.e. is it a standard-library package?
	if info, err := build.Default.Import(path, "", build.FindOnly); err == nil && info.Goroot {
		return s.stdImporter.Import(path) // load compiled archive
	}

	name := filepath.Base(path)         // "fmt"
	pkg := types.NewPackage(path, name) // empty scope
	pkg.MarkComplete()                  // tell checker not to ask for it again
	return pkg, nil                     // no error » continue checking
}

func getFilesAndSet(pkg *gocode.Package) ([]*ast.File, *token.FileSet, error) {
	var files []*ast.File
	var fset *token.FileSet
	for _, f := range pkg.Files {
		files = append(files, f.AST)

		if fset == nil {
			fset = f.FileSet
		} else {
			if fset != f.FileSet {
				return nil, nil, fmt.Errorf("inconsistent file set")
			}
		}

	}

	return files, fset, nil
}

func NewGoGraph(pkg *gocode.Package) (*Graph, error) {
	g := &Graph{
		pkg:              pkg,
		testIdentifiers:  make(map[string]struct{}),
		identifiers:      make(map[string]struct{}),
		crossPackageUses: make(map[string]map[ExternalID]struct{}),
	}

	// Gather all ast.Files. Make sure they all have the same fset (they will by default; if user modifies a File manually and reparses just that file, it will not):
	// If there's a fset mismatch, reload the package and try one more time.
	files, fset, err := getFilesAndSet(pkg)
	if err != nil {
		pkg, err = pkg.Reload()
		if err != nil {
			return nil, err
		}

		files, fset, err = getFilesAndSet(pkg)
		if err != nil {
			return nil, err
		}
	}
	g.fset = fset

	// Create types.Info to store type checking results
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Defs:  make(map[*ast.Ident]types.Object),
		Uses:  make(map[*ast.Ident]types.Object),
		// Implicits:  make(map[ast.Node]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Scopes:     make(map[ast.Node]*types.Scope),
	}
	g.info = info

	// Create types.Config with a custom importer that stubs external packages
	cfg := &types.Config{
		Importer: &stubImporter{stdImporter: importer.Default()},
		Error: func(err error) {
			// Silently ignore errors - we're analyzing a single package
		},
	}

	// Type check the package:
	// Check returns a package and an error.
	// Error is expected to be non-nil, because any imported code is not actually imported.
	// Even though an error is present, info is still populated with Defs and Uses.
	checkedPkg, _ := cfg.Check(pkg.ImportPath, fset, files, info)
	g.checkedPkg = checkedPkg

	g.populateIdentifiersAndUses()

	return g, nil
}

func (g *Graph) WithoutTestIdentifiers() *Graph {
	idents := make([]string, 0, len(g.testIdentifiers))
	for ident := range g.testIdentifiers {
		idents = append(idents, ident)
	}
	return g.WithoutIdentifiers(idents)
}

func (g *Graph) WithoutIdentifiers(idents []string) *Graph {
	newG := &Graph{
		pkg:              g.pkg,
		info:             g.info,
		checkedPkg:       g.checkedPkg,
		fset:             g.fset,
		testIdentifiers:  make(map[string]struct{}),
		intraUses:        make(map[string]map[string]struct{}),
		identifiers:      make(map[string]struct{}),
		crossPackageUses: make(map[string]map[ExternalID]struct{}),
	}

	toRemove := make(map[string]struct{}, len(idents))
	for _, ident := range idents {
		toRemove[ident] = struct{}{}
	}

	for def, uses := range g.intraUses {
		if _, remove := toRemove[def]; remove {
			continue
		}

		newUses := make(map[string]struct{})
		for use := range uses {
			if _, remove := toRemove[use]; remove {
				continue
			}
			newUses[use] = struct{}{}
		}

		if len(newUses) > 0 {
			newG.intraUses[def] = newUses
		}
	}

	for ident := range g.testIdentifiers {
		if _, remove := toRemove[ident]; !remove {
			newG.testIdentifiers[ident] = struct{}{}
		}
	}

	for ident := range g.identifiers {
		if _, remove := toRemove[ident]; !remove {
			newG.identifiers[ident] = struct{}{}
		}
	}

	// Copy crossPackageUses, excluding definitions that are removed.
	for def, refs := range g.crossPackageUses {
		if _, remove := toRemove[def]; remove {
			continue
		}

		newRefs := make(map[ExternalID]struct{})
		for ref := range refs {
			newRefs[ref] = struct{}{}
		}

		if len(newRefs) > 0 {
			newG.crossPackageUses[def] = newRefs
		}
	}

	return newG
}

func (g *Graph) populateIdentifiersAndUses() {
	g.intraUses = make(map[string]map[string]struct{})
	if g.checkedPkg == nil || g.pkg == nil {
		return
	}

	if g.checkedPkg.Scope() != nil {
		for _, name := range g.checkedPkg.Scope().Names() {
			// Use canonical naming for types. Generic type parameters are omitted so Vector is
			// used instead of Vector[T].
			if obj := g.checkedPkg.Scope().Lookup(name); obj != nil {
				if typeName, ok := obj.(*types.TypeName); ok {
					g.identifiers[g.canonicalTypeName(typeName)] = struct{}{}
					continue
				}
			}
			g.identifiers[name] = struct{}{}
		}
	}

	for _, file := range g.pkg.Files {
		if file.AST == nil {
			continue
		}

		for _, decl := range file.AST.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				switch d.Tok {
				case token.TYPE:
					g.handleTypeSpec(d, file.IsTest)
				case token.VAR, token.CONST:
					g.handleValueSpec(d, file.IsTest)
				}
			case *ast.FuncDecl:
				g.handleFuncDecl(d, file.IsTest)
			}
		}
	}
}

func (g *Graph) handleFuncDecl(funcDecl *ast.FuncDecl, isTestFile bool) {
	if funcDecl.Name == nil {
		return // Should not happen for top-level functions
	}

	var defObj types.Object

	receiverType, funcName := gocode.GetReceiverFuncName(funcDecl)
	namePosition := g.fset.Position(funcDecl.Name.Pos())

	defObj = g.info.Defs[funcDecl.Name]

	// If we have type information, derive receiver type from it to ensure we
	// capture generic parameters accurately.
	if defObj != nil {
		if fn, ok := defObj.(*types.Func); ok {
			if sig, ok := fn.Type().(*types.Signature); ok {
				if recv := sig.Recv(); recv != nil {
					receiverType = g.receiverTypeString(recv.Type())
				}
			}
		}
	}

	defKey := gocode.FuncIdentifier(receiverType, funcName, namePosition.Filename, namePosition.Line, namePosition.Column)

	g.identifiers[defKey] = struct{}{}
	if isTestFile {
		g.testIdentifiers[defKey] = struct{}{}
	}

	allUses := make(map[string]struct{})
	allCrossUses := make(map[ExternalID]struct{})

	// Dependencies from receiver type
	if funcDecl.Recv != nil && len(funcDecl.Recv.List) > 0 {
		// The receiver is a field list, we find uses in its type.
		uses := g.findUses(funcDecl.Recv.List[0].Type, defObj)
		for use := range uses {
			allUses[use] = struct{}{}
		}
		cross := g.findCrossPackageUses(funcDecl.Recv.List[0].Type)
		for ref := range cross {
			allCrossUses[ref] = struct{}{}
		}
	}

	// Dependencies from type parameters, parameters, and results
	if funcDecl.Type != nil {
		uses := g.findUses(funcDecl.Type, defObj)
		for use := range uses {
			allUses[use] = struct{}{}
		}
		cross := g.findCrossPackageUses(funcDecl.Type)
		for ref := range cross {
			allCrossUses[ref] = struct{}{}
		}
	}

	// Dependencies from function body
	if funcDecl.Body != nil {
		uses := g.findUses(funcDecl.Body, defObj)
		for use := range uses {
			allUses[use] = struct{}{}
		}
		cross := g.findCrossPackageUses(funcDecl.Body)
		for ref := range cross {
			allCrossUses[ref] = struct{}{}
		}
	}

	if len(allUses) > 0 {
		g.addUses(defKey, allUses)
	}
	if len(allCrossUses) > 0 {
		g.addCrossPackageUses(defKey, allCrossUses)
	}
}

func (g *Graph) handleTypeSpec(genDecl *ast.GenDecl, isTestFile bool) {
	for _, spec := range genDecl.Specs {
		typeSpec, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}

		var defKey string
		var defObj types.Object

		if typeSpec.Name.Name == "_" {
			defKey = g.anonymousIdentifier(typeSpec.Name.Pos())
			defObj = g.info.Defs[typeSpec.Name]
		} else {
			var ok bool
			defObj, ok = g.info.Defs[typeSpec.Name]
			if !ok {
				continue
			}
			if typeNameObj, okTN := defObj.(*types.TypeName); okTN {
				defKey = g.canonicalTypeName(typeNameObj)
			} else {
				defKey = defObj.Name()
			}
		}

		g.identifiers[defKey] = struct{}{}
		if isTestFile {
			g.testIdentifiers[defKey] = struct{}{}
		}

		uses := g.findUses(typeSpec, defObj)
		crossUses := g.findCrossPackageUses(typeSpec)
		if len(uses) > 0 {
			g.addUses(defKey, uses)
		}
		if len(crossUses) > 0 {
			g.addCrossPackageUses(defKey, crossUses)
		}
	}
}

func (g *Graph) handleValueSpec(genDecl *ast.GenDecl, isTestFile bool) {
	for _, spec := range genDecl.Specs {
		valueSpec, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}

		hasValues := len(valueSpec.Values) > 0

		for i, name := range valueSpec.Names {
			var defKey string
			var defObj types.Object

			if name.Name == "_" {
				defKey = g.anonymousIdentifier(name.Pos())
				defObj = g.info.Defs[name]
			} else {
				var ok bool
				defObj, ok = g.info.Defs[name]
				if !ok {
					continue
				}
				if typeNameObj, okTN := defObj.(*types.TypeName); okTN {
					defKey = g.canonicalTypeName(typeNameObj)
				} else {
					defKey = defObj.Name()
				}
			}

			g.identifiers[defKey] = struct{}{}
			if isTestFile {
				g.testIdentifiers[defKey] = struct{}{}
			}

			allUses := make(map[string]struct{})
			allCrossUses := make(map[ExternalID]struct{})

			hasExplicitType := valueSpec.Type != nil

			// Dependencies from explicit type
			if hasExplicitType {
				uses := g.findUses(valueSpec.Type, nil) // No defObj for type part
				for use := range uses {
					allUses[use] = struct{}{}
				}
				cross := g.findCrossPackageUses(valueSpec.Type)
				for ref := range cross {
					allCrossUses[ref] = struct{}{}
				}
			}

			// Dependencies from initialization values
			if hasValues {
				// Case: var a, b = f() which returns two values. Here len(valueSpec.Values) is 1.
				// It's a single CallExpr. All LHS identifiers depend on it.
				// Case: var a, b = 1, 2. Here len(valueSpec.Values) is 2.
				if len(valueSpec.Values) > 1 {
					if i < len(valueSpec.Values) {
						uses := g.findUses(valueSpec.Values[i], defObj)
						for use := range uses {
							allUses[use] = struct{}{}
						}
						cross := g.findCrossPackageUses(valueSpec.Values[i])
						for ref := range cross {
							allCrossUses[ref] = struct{}{}
						}
					}
				} else { // len(valueSpec.Values) == 1
					uses := g.findUses(valueSpec.Values[0], defObj)
					for use := range uses {
						allUses[use] = struct{}{}
					}
					cross := g.findCrossPackageUses(valueSpec.Values[0])
					for ref := range cross {
						allCrossUses[ref] = struct{}{}
					}
				}
			}

			// For constants with implicit type and value (like iota).
			if !hasExplicitType && !hasValues {
				if defObj != nil {
					if constObj, ok := defObj.(*types.Const); ok {
						if namedType, ok := constObj.Type().(*types.Named); ok {
							if typeName := namedType.Obj(); typeName != nil && typeName.Pkg() == g.checkedPkg {
								allUses[typeName.Name()] = struct{}{}
							}
						}
					}
				}
			}

			if len(allUses) > 0 {
				g.addUses(defKey, allUses)
			}
			if len(allCrossUses) > 0 {
				g.addCrossPackageUses(defKey, allCrossUses)
			}
		}
	}
}

// objectKey returns a unique, predictable key for a types.Object. For methods, it returns a key of the form "ReceiverType.MethodName". For other objects, it returns
// the object's name.
func (g *Graph) objectKey(obj types.Object) string {
	// For methods, generate an identifier that uses the canonical receiver
	// type representation produced by receiverTypeString so that generic type
	// parameters are omitted (e.g. "*Pair" instead of "*Pair[T]").
	if fn, ok := obj.(*types.Func); ok {
		if sig, ok := fn.Type().(*types.Signature); ok {
			if recv := sig.Recv(); recv != nil {
				receiverType := g.receiverTypeString(recv.Type())
				return gocode.FuncIdentifierUse(receiverType, fn.Name())
			}
		}
	}

	// All other objects: just use the name.
	return obj.Name()
}

// anonymousIdentifier returns an identifier for an anonymous var/type/func (name of "_").
func (g *Graph) anonymousIdentifier(p token.Pos) string {
	pos := g.fset.Position(p)
	return gocode.AnonymousIdentifier(filepath.Base(pos.Filename), pos.Line, pos.Column)
}

// findUses inspects a node (nil allowed) and returns the set of package-level identifiers it uses. In addition to named types, this includes package-level functions/methods,
// variables, and constants. For interface method calls, the dependency is recorded on the interface type rather than the method. The `defObj` is the object being
// defined, which should be excluded from its own use list.
func (g *Graph) findUses(n ast.Node, defObj types.Object) map[string]struct{} {
	uses := make(map[string]struct{})
	if n == nil {
		return uses
	}

	ast.Inspect(n, func(node ast.Node) bool {
		// Field accesses are not in `Uses`, they are in `Selections`.
		// This block considers x.y, and adds to uses x's named type.
		// NOTE: it is not obvious that we need this. We could delete this type of dependency.
		if selExpr, ok := node.(*ast.SelectorExpr); ok {
			if sel, ok := g.info.Selections[selExpr]; ok && sel.Kind() == types.FieldVal {
				// It's a field access. The dependency is on the type of the receiver.
				recv := sel.Recv()
				if ptr, ok := recv.(*types.Pointer); ok {
					recv = ptr.Elem() // unwrap *T → T
				}
				if named, ok := recv.(*types.Named); ok {
					if typeName := named.Obj(); typeName != nil && typeName.Pkg() == g.checkedPkg && typeName.Parent() == g.checkedPkg.Scope() {
						uses[g.canonicalTypeName(typeName)] = struct{}{}
					}
				}
			}
		}

		ident, ok := node.(*ast.Ident)
		if !ok {
			return true
		}

		useObj := g.info.Uses[ident]
		if useObj == nil {
			return true
		}

		// Don't count the identifier being defined (self-reference)
		if defObj != nil && useObj == defObj {
			return true
		}

		// We only care about uses within the same package
		if useObj.Pkg() != g.checkedPkg {
			return true
		}

		// For method calls on interfaces, we want to depend on the interface, not the method.
		if fn, ok := useObj.(*types.Func); ok {
			if sig, ok := fn.Type().(*types.Signature); ok {
				if recv := sig.Recv(); recv != nil {
					if _, isInterface := recv.Type().Underlying().(*types.Interface); isInterface {
						if namedType, ok := recv.Type().(*types.Named); ok {
							if typeName := namedType.Obj(); typeName != nil {
								uses[g.canonicalTypeName(typeName)] = struct{}{}
							}
						}
						return true // We've recorded the interface dependency, so we can skip the method itself.
					}
				}
			}
		}

		// We only want package-level objects or methods.
		isPackageLevel := useObj.Parent() == g.checkedPkg.Scope()
		isMethod := false
		if fn, ok := useObj.(*types.Func); ok {
			if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
				isMethod = true
			}
		}

		if !isPackageLevel && !isMethod {
			return true
		}

		switch obj := useObj.(type) {
		case *types.TypeName:
			if _, isTypeParam := obj.Type().(*types.TypeParam); isTypeParam {
				return true // It's a type parameter like T, so skip it.
			}
			uses[g.canonicalTypeName(obj)] = struct{}{}
		case *types.Var, *types.Const:
			uses[obj.Name()] = struct{}{}
		case *types.Func:
			uses[g.objectKey(obj)] = struct{}{}
		}

		return true
	})
	return uses
}

func (g *Graph) addUses(defKey string, uses map[string]struct{}) {
	if _, ok := g.intraUses[defKey]; !ok {
		g.intraUses[defKey] = make(map[string]struct{})
	}
	for useKey := range uses {
		g.identifiers[useKey] = struct{}{}
		if useKey != defKey {
			g.intraUses[defKey][useKey] = struct{}{}
		}
	}
}

// addCrossPackageUses records cross-package dependencies for a defining identifier. A dependency is represented by an ExternalID capturing the import path and the
// referenced identifier in the other package.
func (g *Graph) addCrossPackageUses(defKey string, refs map[ExternalID]struct{}) {
	if len(refs) == 0 {
		return
	}

	if g.crossPackageUses == nil {
		g.crossPackageUses = make(map[string]map[ExternalID]struct{})
	}

	if _, ok := g.crossPackageUses[defKey]; !ok {
		g.crossPackageUses[defKey] = make(map[ExternalID]struct{})
	}

	for ref := range refs {
		g.crossPackageUses[defKey][ref] = struct{}{}
	}
}

// findCrossPackageUses walks an AST node and returns the set of identifiers that are referenced from other packages (i.e. their types.Object has a different package
// than the one currently being analysed).
func (g *Graph) findCrossPackageUses(n ast.Node) map[ExternalID]struct{} {
	refs := make(map[ExternalID]struct{})
	if n == nil {
		return refs
	}

	// Helper to record a reference in the map.
	addRef := func(pkgPath, identStr string) {
		if pkgPath == "" || identStr == "" {
			return
		}
		ref := ExternalID{ImportPath: pkgPath, ID: identStr}
		refs[ref] = struct{}{}
	}

	ast.Inspect(n, func(node ast.Node) bool {
		switch v := node.(type) {
		case *ast.SelectorExpr:
			if pkgIdent, ok := v.X.(*ast.Ident); ok {
				if pkgObj, ok := g.info.Uses[pkgIdent].(*types.PkgName); ok {
					imported := pkgObj.Imported()
					if imported != nil && imported != g.checkedPkg {
						addRef(imported.Path(), v.Sel.Name)
					}
				}
			}
		case *ast.Ident:
			ident := v
			useObj := g.info.Uses[ident]
			if useObj == nil {
				// If unresolved, nothing to do here – it might be the selector part handled
				// above or simply an unresolved identifier.
				return true
			}

			// Only consider objects that belong to a different package.
			pkg := useObj.Pkg()
			if pkg == nil || pkg == g.checkedPkg {
				return true
			}

			// Build the identifier string in a way that is consistent with intra-package keys.
			var identStr string
			switch obj := useObj.(type) {
			case *types.TypeName:
				identStr = g.canonicalTypeName(obj)
			case *types.Func:
				identStr = g.objectKey(obj)
			case *types.Var, *types.Const:
				identStr = obj.Name()
			default:
				identStr = useObj.Name()
			}

			addRef(pkg.Path(), identStr)
			return true
		default:
			// Continue traversing
		}
		return true
	})

	return refs
}

// AllIdentifiers returns all identifiers (nodes) in the graph. This includes:
//   - All package-level identifiers from the checked package scope
//   - All identifiers that appear in dependency relationships (including methods and anonymous identifiers)
//
// The returned slice contains unique identifier strings with no duplicates.
func (g *Graph) AllIdentifiers() []string {
	result := make([]string, 0, len(g.identifiers))
	for id := range g.identifiers {
		result = append(result, id)
	}
	return result
}

// canonicalTypeName returns a stable identifier for a named type. Generic type parameters are ignored because a type name is unique within its package, so `type Vector[T Number] struct{}`
// and its instantiations all canonicalise to "Vector".
func (g *Graph) canonicalTypeName(obj *types.TypeName) string {
	if obj == nil {
		return ""
	}

	// A type name is unique within a package, so we can simply return the
	// base name without including any generic type parameters (e.g. return
	// "Vector" instead of "Vector[T]").
	return obj.Name()
}

// receiverTypeString returns a canonical string for a receiver type, including a leading '*' for pointer receivers. Generic type parameters are omitted, so for
// a method on *streamReader[T] this returns "*streamReader".
func (g *Graph) receiverTypeString(t types.Type) string {
	ptrPrefix := ""
	if pt, ok := t.(*types.Pointer); ok {
		ptrPrefix = "*"
		t = pt.Elem()
	}

	if named, ok := t.(*types.Named); ok {
		if typeName := named.Obj(); typeName != nil {
			return ptrPrefix + g.canonicalTypeName(typeName)
		}
	}

	// Fall back to the default type string, though this should rarely be
	// needed for methods we care about.
	return ptrPrefix + types.TypeString(t, func(p *types.Package) string { return "" })
}
