package gocode

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsPackageIdentifier(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want bool
	}{
		{name: "primary package identifier", id: PackageIdentifier, want: true},
		{name: "per file package identifier", id: PackageIdentifierPerFile("doc.go"), want: true},
		{name: "ordinary identifier", id: "SomeType", want: false},
		{name: "similar prefix", id: "packages:doc.go", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsPackageIdentifier(tc.id))
		})
	}
}

func TestFuncIdentifierFromDecl(t *testing.T) {
	source := dedent(`
		package p

		func Plain() {}
		func init() {}
		func _() {}
		type T struct{}
		func (t *T) Method() {}
		func (t T) _() {}
	`)
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sample.go", source, 0)
	require.NoError(t, err)

	var got []string
	for _, decl := range file.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		got = append(got, FuncIdentifierFromDecl(funcDecl, fset))
	}

	assert.Equal(t, []string{
		"Plain",
		"init:sample.go:4:6",
		"_:sample.go:5:6",
		"*T.Method",
		"T._:sample.go:8:12",
	}, got)
}

func TestFuncIdentifierUse(t *testing.T) {
	tests := []struct {
		name         string
		receiverType string
		funcName     string
		want         string
	}{
		{name: "function", funcName: "Do", want: "Do"},
		{name: "method", receiverType: "*Thing", funcName: "Do", want: "*Thing.Do"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, FuncIdentifierUse(tc.receiverType, tc.funcName))
		})
	}
}

func TestDeparenthesizeIdentifier(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no receiver no change",
			in:   "DoThing",
			want: "DoThing",
		},
		{
			name: "already canonical pointer receiver",
			in:   "*SomeType.SomeMethod",
			want: "*SomeType.SomeMethod",
		},
		{
			name: "pointer receiver in parens",
			in:   "(*SomeType).SomeMethod",
			want: "*SomeType.SomeMethod",
		},
		{
			name: "non-pointer receiver in parens",
			in:   "(SomeType).SomeMethod",
			want: "SomeType.SomeMethod",
		},
		{
			name: "nested parens",
			in:   "((*SomeType)).SomeMethod",
			want: "*SomeType.SomeMethod",
		},
		{
			name: "package-qualified type in parens",
			in:   "(*pkg.SomeType).SomeMethod",
			want: "*pkg.SomeType.SomeMethod",
		},
		{
			name: "generic receiver pointer in parens",
			in:   "(*SomeType[T]).SomeMethod",
			want: "*SomeType.SomeMethod",
		},
		{
			name: "generic receiver non-pointer in parens",
			in:   "(SomeType[T]).SomeMethod",
			want: "SomeType.SomeMethod",
		},
		{
			name: "generic receiver multiple type params and pkg qualifier",
			in:   "(*pkg.SomeType[T, U]).SomeMethod",
			want: "*pkg.SomeType.SomeMethod",
		},
		{
			name: "generic receiver without parens",
			in:   "SomeType[T].SomeMethod",
			want: "SomeType.SomeMethod",
		},
		{
			name: "generic receiver without parens with pkg qualifier",
			in:   "pkg.SomeType[T, U].SomeMethod",
			want: "pkg.SomeType.SomeMethod",
		},
		{
			name: "invalid parse returns unchanged",
			in:   "(*).SomeMethod",
			want: "(*).SomeMethod",
		},
		{
			name: "unbalanced parens returns unchanged",
			in:   "(*SomeType.SomeMethod",
			want: "(*SomeType.SomeMethod",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, DeparenthesizeIdentifier(tc.in))
		})
	}
}
