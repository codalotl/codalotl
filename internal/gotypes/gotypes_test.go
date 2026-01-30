package gotypes

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"go/ast"
	"go/types"

	"github.com/codalotl/codalotl/internal/gocode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadTypeInfoMainReflexive(t *testing.T) {
	mod, err := gocode.NewModule(gocode.MustCwd())
	require.NoError(t, err)

	pkg, err := mod.LoadPackageByRelativeDir("internal/gotypes")
	require.NoError(t, err)

	typeInfo, err := LoadTypeInfoInto(pkg, true)
	require.NoError(t, err)

	assert.True(t, len(typeInfo.Info.Types) > 0)

	// Find the call to assert.True inside this test
	file := pkg.Files["gotypes.go"]
	require.NotNil(t, file)
	require.NotNil(t, file.AST)

	var (
		inTestParse bool
		loadCall    *ast.CallExpr
	)

	ast.Inspect(file.AST, func(n ast.Node) bool {
		switch fn := n.(type) {
		case *ast.FuncDecl:
			inTestParse = fn.Name != nil && fn.Name.Name == "LoadTypeInfoInto"
			return true
		}
		if !inTestParse {
			return true
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "packages" && sel.Sel.Name == "Load" {
				loadCall = call
				return false
			}
		}
		return true
	})

	require.NotNil(t, loadCall)
	// Check the function type of packages.Load and ensure it returns ([]*Package, error)
	funTV, ok := typeInfo.Info.Types[loadCall.Fun]
	require.True(t, ok)
	sig, ok := funTV.Type.(*types.Signature)
	require.True(t, ok)
	sigStr := types.TypeString(sig, func(p *types.Package) string { return p.Name() })
	normalized := strings.ReplaceAll(sigStr, "packages.", "")
	assert.Contains(t, normalized, "([]*Package, error)")
}

func TestLoadTypeInfoTestingReflexive(t *testing.T) {
	mod, err := gocode.NewModule(gocode.MustCwd())
	require.NoError(t, err)

	pkg, err := mod.LoadPackageByRelativeDir("internal/gotypes")
	require.NoError(t, err)

	typeInfo, err := LoadTypeInfoInto(pkg, true)
	require.NoError(t, err)

	assert.True(t, len(typeInfo.Info.Types) > 0)

	// Find the call to assert.True inside this test
	file := pkg.Files["gotypes_test.go"]
	require.NotNil(t, file)
	require.NotNil(t, file.AST)

	var (
		inTestParse bool
		equalCall   *ast.CallExpr
	)

	ast.Inspect(file.AST, func(n ast.Node) bool {
		switch fn := n.(type) {
		case *ast.FuncDecl:
			inTestParse = fn.Name != nil && fn.Name.Name == "TestLoadTypeInfoTestingReflexive"
			return true
		}
		if !inTestParse {
			return true
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "assert" && sel.Sel.Name == "True" {
				equalCall = call
				return false
			}
		}
		return true
	})

	require.NotNil(t, equalCall)
	tv, ok := typeInfo.Info.Types[equalCall]
	require.True(t, ok)
	assert.True(t, types.Identical(tv.Type, types.Typ[types.Bool]))

	s := pkg.GetSnippet("TestLoadTypeInfoTestingReflexive")
	require.NotNil(t, s)
	assert.Equal(t, "gotypes_test.go", s.Position().Filename) // ensure this is just the base name, not an abs path. (packages loads based on abs path)
}

func TestLoadTypeInfoIncludesImports(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)

	moduleDir := filepath.Dir(filename)

	mod, err := gocode.NewModule(moduleDir)
	require.NoError(t, err)

	pkg, err := mod.LoadPackageByImportPath("github.com/codalotl/codalotl/internal/gotypes/testdata/uses")
	require.NoError(t, err)

	typeInfo, err := LoadTypeInfoInto(pkg, false)
	require.NoError(t, err)

	var constructorSel *ast.SelectorExpr
	var fieldSel *ast.SelectorExpr

	ast.Inspect(pkg.Files["uses.go"].AST, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		if pkgIdent, ok := sel.X.(*ast.Ident); ok && pkgIdent.Name == "lib" && sel.Sel.Name == "NewFoo" {
			constructorSel = sel
		}

		if sel.Sel.Name == "Value" {
			fieldSel = sel
		}

		return true
	})

	require.NotNil(t, constructorSel)
	require.NotNil(t, fieldSel)

	constructorObj, ok := typeInfo.Info.Uses[constructorSel.Sel]
	require.True(t, ok)
	assert.Equal(t, "NewFoo", constructorObj.Name())
	if assert.NotNil(t, constructorObj.Pkg()) {
		assert.Equal(t, "github.com/codalotl/codalotl/internal/gotypes/testdata/lib", constructorObj.Pkg().Path())
	}

	selection, ok := typeInfo.Info.Selections[fieldSel]
	require.True(t, ok)
	assert.Equal(t, types.FieldVal, selection.Kind())
	assert.Equal(t, "Value", selection.Obj().Name())
	assert.Equal(t, "github.com/codalotl/codalotl/internal/gotypes/testdata/lib", selection.Obj().Pkg().Path())
}
