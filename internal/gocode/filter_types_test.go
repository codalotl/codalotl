package gocode

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func mustTypeDecl(t *testing.T, src string) (*ast.GenDecl, *token.FileSet) {
	t.Helper()

	if !strings.Contains(src, "package") {
		src = "package p\n\n" + src
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "input.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal("error when parsing src")
	}
	for _, decl := range file.Decls {
		if gd, ok := decl.(*ast.GenDecl); ok {
			if gd.Tok == token.TYPE {
				return gd, fset
			}
		}
	}
	t.Fatal("could not find type gen decl")

	return nil, nil
}

func mustFormatTypeDecl(t *testing.T, genDecl *ast.GenDecl, fset *token.FileSet) string {
	var buf bytes.Buffer
	cfg := printer.Config{Mode: printer.TabIndent | printer.UseSpaces, Tabwidth: 8}

	if err := cfg.Fprint(&buf, fset, genDecl); err != nil {
		t.Fatal("could not fprint in mustFormatTypeDecl", err)
	}

	return buf.String()
}

func TestFilterTypesBasic(t *testing.T) {
	src := dedent(`
		// T
		type T struct {
			// Name is exported
			Name string
			// age is private
			age int
		}
	`)

	gd, fset := mustTypeDecl(t, src)
	filtered := filterExportedTypes(gd)
	formatted := mustFormatTypeDecl(t, filtered, fset)

	expected := dedent(`
		// T
		type T struct {
			// Name is exported
			Name string
			// contains filtered or unexported fields
		}
	`)

	assert.Equal(t, strings.TrimSpace(expected), strings.TrimSpace(formatted))
}

func TestFilterTypesInterface(t *testing.T) {
	src := dedent(`
		type Foo interface {
			// Bar is exported
			Bar()
			// baz is private
			baz()
		}
	`)
	gd, fset := mustTypeDecl(t, src)
	filtered := filterExportedTypes(gd)
	formatted := mustFormatTypeDecl(t, filtered, fset)

	expected := dedent(`
		type Foo interface {
			// Bar is exported
			Bar()
			// contains filtered or unexported methods
		}
	`)

	assert.Equal(t, strings.TrimSpace(expected), strings.TrimSpace(formatted))
}

func TestFilterTypesAlias(t *testing.T) {
	src := `type ID = string`

	gd, fset := mustTypeDecl(t, src)
	filtered := filterExportedTypes(gd)
	formatted := mustFormatTypeDecl(t, filtered, fset)

	assert.Equal(t, strings.TrimSpace(src), strings.TrimSpace(formatted))
}
