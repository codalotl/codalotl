package gotypes_test

import (
	"testing"

	"go/ast"
	"go/types"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gotypes"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadTypeInfoExternalReflexive(t *testing.T) {
	mod, err := gocode.NewModule(gocode.MustCwd())
	require.NoError(t, err)

	pkg, err := mod.LoadPackageByRelativeDir("internal/gotypes")
	require.NoError(t, err)
	require.True(t, pkg.HasTestPackage())
	pkg = pkg.TestPackage
	assert.NotNil(t, pkg)

	typeInfo, err := gotypes.LoadTypeInfoInto(pkg, true)
	require.NoError(t, err)

	assert.True(t, len(typeInfo.Info.Types) > 0)

	// Find the call to assert.True inside this test
	file := pkg.Files["gotypes_ext_test.go"]
	require.NotNil(t, file)
	require.NotNil(t, file.AST)

	var (
		inTestParse bool
		equalCall   *ast.CallExpr
	)

	ast.Inspect(file.AST, func(n ast.Node) bool {
		switch fn := n.(type) {
		case *ast.FuncDecl:
			inTestParse = fn.Name != nil && fn.Name.Name == "TestLoadTypeInfoExternalReflexive"
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
}
