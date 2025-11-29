package gograph

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGograph_Integration(t *testing.T) {
	t.SkipNow()
	m, err := gocode.NewModule(gocode.MustCwd())
	assert.NoError(t, err)

	err = m.LoadAllPackages()
	assert.NoError(t, err)

	p := m.Packages["axi/codeai/gocode"]
	// p := m.Packages["axi/stock"]
	// p := m.Packages["axi/codeai/llmcomplete"]
	assert.NotNil(t, p)

	g, err := NewGoGraph(p)
	assert.NotNil(t, g)
	assert.NoError(t, err)

	var buf bytes.Buffer

	buf.WriteString("\n--- Identifiers ---\n")
	for _, k := range g.AllIdentifiers() {
		buf.WriteString(fmt.Sprintf("  - %s\n", k))
	}
	t.Log(buf.String())

	buf.Reset()
	for k, v := range g.intraUses {
		buf.WriteString(fmt.Sprintf("%s:\n", k))
		for k2 := range v {
			buf.WriteString(fmt.Sprintf("  - %s\n", k2))
		}
	}
	t.Log(buf.String())

	// Print weakly connected components
	components := g.WeaklyConnectedComponents()
	buf.Reset()
	buf.WriteString("\n--- Weakly Connected Components ---\n")
	for i, component := range components {
		buf.WriteString(fmt.Sprintf("Component %d:\n", i+1))
		var nodes []string
		for node := range component {
			nodes = append(nodes, node)
		}
		sort.Strings(nodes)
		for _, node := range nodes {
			buf.WriteString(fmt.Sprintf("  - %s\n", node))
		}
	}
	t.Log(buf.String())

	// Print strongly connected components
	components = g.StronglyConnectedComponents()
	buf.Reset()
	buf.WriteString("\n--- Strongly Connected Components (size > 1) ---\n")
	var sccCount int
	for _, component := range components {
		if len(component) > 1 {
			buf.WriteString(fmt.Sprintf("Component %d:\n", sccCount+1))
			sccCount++
			var nodes []string
			for node := range component {
				nodes = append(nodes, node)
			}
			sort.Strings(nodes)
			for _, node := range nodes {
				buf.WriteString(fmt.Sprintf("  - %s\n", node))
			}
		}
	}
	t.Log(buf.String())
}

func TestNewGoGraphIntraUsesTableDriven(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		expected map[string][]string
	}{
		{
			name: "simple struct dependencies",
			src: dedent(`
				type Bar1 struct{}
				type Bar2 int
				type Bar3 struct{}
				type Bar4 struct{}
				type Bar5 struct{}
				type Bar6 struct{}
				type Bar7 struct{}
				type Bar8 struct{}
				type Bar9 struct{}
				type Bar10 struct{}
				type Bar11 struct{}
				type Bar12 struct{}
				type Bar13 struct{}
				type Bar14 struct{}
				type Bar15 struct{}
				type BarUnused struct{} // not referenced
				type Foo struct {
					B1  Bar1
					B2  *Bar2
					B3  []Bar3
					B4  []*Bar4
					B5  map[Bar5]bool
					B6  map[int]Bar6
					B7  [2]Bar7
					B8  [2]*Bar8
					B9  chan Bar9
					B10 chan *Bar10
					B11 func(Bar11)
					B12 func() Bar12
					B13 interface{ M(Bar13) }
					B14 interface{ M() Bar14 }
					B15 struct{ F Bar15 }
				}`),
			expected: map[string][]string{"Foo": {"Bar1", "Bar2", "Bar3", "Bar4", "Bar5", "Bar6", "Bar7", "Bar8", "Bar9", "Bar10", "Bar11", "Bar12", "Bar13", "Bar14", "Bar15"}},
		},
		{
			name: "embedded struct dependencies",
			src: dedent(`
				type A struct { B; C }
				type B struct {}
				type C struct { B B }`),
			expected: map[string][]string{"A": {"B", "C"}, "C": {"B"}},
		},
		{
			name: "simple type references",
			src: dedent(`
				type A int
				type B A
				type C map[string]A
				type D *A
				type E []B
			`),
			expected: map[string][]string{"B": {"A"}, "C": {"A"}, "D": {"A"}, "E": {"B"}},
		},
		{
			name: "type alias",
			src: dedent(`
				type A int
				type B = A
			`),
			expected: map[string][]string{"B": {"A"}},
		},
		{
			name: "other package references",
			src: dedent(`
				import "other"
				type A int
				type B struct {
					X A
					Y other.H
				}`),
			expected: map[string][]string{"B": {"A"}},
		},
		{
			name: "recursive",
			src: dedent(`
				type Node struct {
					N *Node
				}`),
			expected: map[string][]string{},
		},
		{
			name: "interfaces",
			src: dedent(`
				type A struct {}
				type B struct {}
				type C struct {}
				type I interface {
					X() A
					Y(B)
					Z(*C)
				}`),
			expected: map[string][]string{"I": {"A", "B", "C"}},
		},
		{
			name: "interfaces embedding",
			src: dedent(`
				type D interface {F()}
				type I interface {
					D
				}`),
			expected: map[string][]string{"I": {"D"}},
		},
		{
			name: "function types",
			src: dedent(`
				type B struct{}
				type Z func(arg B)
				type Y func() B
				type X func(args ...B)`),
			expected: map[string][]string{"Z": {"B"}, "Y": {"B"}, "X": {"B"}},
		},
		{
			name: "generics in types",
			src: dedent(`
				type Number interface {
					~int | ~float
				}
				type Vector[T Number] struct {
					X, Y T
				}
			`),
			expected: map[string][]string{"Vector": {"Number"}},
		},
		{
			name: "generics2 in types",
			src: dedent(`
				type B struct {}
				type List[T any] struct {
					items []T
				}
				type ListOfB List[B]
			`),
			expected: map[string][]string{"ListOfB": {"List", "B"}},
		},
		{
			name: "generics in functions",
			src: dedent(`
				type A struct {}
				type B struct {}
				type streamable interface { A | B }
				type streamReader[T streamable] struct {
					foo int
				}
				func (s *streamReader[T]) Recv() (T, error) {
					var x T
					return x, err
				}
			`),
			expected: map[string][]string{"streamable": {"A", "B"}, "streamReader": {"streamable"}, "*streamReader.Recv": {"streamReader"}},
		},
		{
			name: "single variable",
			src: dedent(`
				type B int
				var x B
			`),
			expected: map[string][]string{"x": {"B"}},
		},
		{
			name: "single const",
			src: dedent(`
				type B int
				const x B = 1
			`),
			expected: map[string][]string{"x": {"B"}},
		},
		{
			name: "multi var",
			src: dedent(`
				type A struct{}
				type B struct{}
				var (
					v1 A
					v2 B
				)
			`),
			expected: map[string][]string{"v1": {"A"}, "v2": {"B"}},
		},
		{
			name: "multi const",
			src: dedent(`
				type A string
				type B int
				const (
					c1 A = "a"
					c2 B = 1
				)
			`),
			expected: map[string][]string{"c1": {"A"}, "c2": {"B"}},
		},
		{
			name: "var depends on var",
			src: dedent(`
				type A struct{}
				var v1 A
				var v2 = v1
			`),
			expected: map[string][]string{"v1": {"A"}, "v2": {"v1"}},
		},
		{
			name: "const depends on const",
			src: dedent(`
				const c1 = 1
				const c2 = c1
			`),
			expected: map[string][]string{"c2": {"c1"}},
		},
		{
			name: "const depends on typed const",
			src: dedent(`
				type A int
				const c1 A = 1
				const c2 = c1
			`),
			expected: map[string][]string{"c1": {"A"}, "c2": {"c1"}},
		},
		{
			name: "const iota",
			src: dedent(`
				type A int
				const (
					ZA A = iota
					YA
					XA

				)
			`),
			expected: map[string][]string{"ZA": {"A"}, "YA": {"A"}, "XA": {"A"}},
		},
		{
			name: "var with func",
			src: dedent(`
				const b = 8
				var log = func(s string) { fmt.Println(s, b) }
			`),
			expected: map[string][]string{"log": {"b"}},
		},
		{
			name: "basic func",
			src: dedent(`
				type A int
				type B int
				const bb B = 1
				func foo() B {return bb}
				func bar(a A) B {
					var x A
					bar(a + x)
					return foo()
				}
			`),
			expected: map[string][]string{"bb": {"B"}, "foo": {"B", "bb"}, "bar": {"A", "B", "foo"}},
		},
		{
			name: "method with dependencies",
			src: dedent(`
				type A struct{}
				type B struct{}
				type C struct{}
				func foo() {}
				func (a *A) MyMethod(b B) C {
					foo()
					return C{}
				}`),
			expected: map[string][]string{"*A.MyMethod": {"A", "B", "C", "foo"}},
		},
		{
			name: "pointer methods vs value methods",
			src: dedent(`
				type A struct{}
				func foo() {
					var a *A
					a.PtrMethod()
					a.ValueMethod()
				}
				func (a *A) PtrMethod() {}
				func (a A) ValueMethod() {}
				`),
			expected: map[string][]string{"foo": {"A", "*A.PtrMethod", "A.ValueMethod"}, "*A.PtrMethod": {"A"}, "A.ValueMethod": {"A"}},
		},
		{
			name: "function uses struct literal",
			src: dedent(`
				type A struct{ Val B }
				type B struct{}
				func foo() A {
					return A{ Val: B{} }
				}`),
			expected: map[string][]string{"A": {"B"}, "foo": {"A", "B"}},
		},
		{
			name: "mutually recursive functions",
			src: dedent(`
				func f1() { f2() }
				func f2() { f1() }`),
			expected: map[string][]string{"f1": {"f2"}, "f2": {"f1"}},
		},
		{
			name: "function calling method",
			src: dedent(`
				type S struct{}
				func (s *S) MyMethod() {}
				func usesMethod() {
					var s S
					s.MyMethod()
				}`),
			expected: map[string][]string{"*S.MyMethod": {"S"}, "usesMethod": {"S", "*S.MyMethod"}},
		},
		{
			name: "function calling non-pointer method",
			src: dedent(`
				type S struct{}
				func (s S) MyMethod() {}
				func usesMethod() {
					var s S
					s.MyMethod()
				}`),
			expected: map[string][]string{"S.MyMethod": {"S"}, "usesMethod": {"S", "S.MyMethod"}},
		},
		{
			name: "method expression",
			src: dedent(`
				type S struct{}
				func (s S) M() {}
				func usesMethodExpr() {
					_ = S.M
				}`),
			expected: map[string][]string{"S.M": {"S"}, "usesMethodExpr": {"S", "S.M"}},
		},
		{
			name: "generic function",
			src: dedent(`
				type Number interface{ ~int }
				func add[T Number](a, b T) T {
					return a + b
				}`),
			expected: map[string][]string{"add": {"Number"}},
		},
		{
			name: "type assertion",
			src: dedent(`
				type A struct{}
				func foo(v interface{}) {
					_ = v.(A)
				}`),
			expected: map[string][]string{"foo": {"A"}},
		},
		{
			name: "type switch",
			src: dedent(`
				type A struct{}
				type B struct{}
				func foo(v any) {
					switch v.(type) {
					case A:
					case *B:
					}
				}`),
			expected: map[string][]string{"foo": {"A", "B"}},
		},
		{
			name: "field access",
			src: dedent(`
				type A struct{B int}
				func foo() int {
					var a A
					return a.B
				}`),
			expected: map[string][]string{"foo": {"A"}},
		},
		{
			name: "interface calling",
			src: dedent(`
				type I interface { Foo() }
				type s struct {}
				func (z s) Foo() {}
				func use(i I) {
					i.Foo()
				}
				func call() {
					use(s{})
				}`),
			expected: map[string][]string{"s.Foo": {"s"}, "use": {"I"}, "call": {"use", "s"}},
		},
		{
			name: "anonymous function literal in goroutine",
			src: dedent(`
				type Task struct{}
				type Result struct{}
				
				func process() {
					tasks := make(chan Task)
					
					go func() {
						for t := range tasks {
							_ = Result{}
							_ = t
						}
					}()
				}
			`),
			expected: map[string][]string{
				"process": {"Result", "Task"}, // Should track types used in anonymous function
			},
		},
		{
			name: "anonymous identifiers",
			src: dedent(`
				type I interface { Foo() }
				type s struct{}
				type t struct{}
				func (z *s) Foo() {}
				var _ I = (*s)(nil)
				var _ t = t{}
				func _() { var _ s }
				func _() {} // no deps, does not show up in uses
				type _ struct {t}
				func (z *s) _() {}
			`),
			expected: map[string][]string{"*s.Foo": {"s"}, "_:test.go:7:5": {"I", "s"}, "_:test.go:8:5": {"t"}, "_:test.go:9:6": {"s"}, "_:test.go:11:6": {"t"}, "*s._:test.go:12:13": {"s"}},
		},
		{
			name: "anonymous identifiers mixed with non-anon idents",
			src: dedent(`
				var _, x = foo()
				var y, _ = foo()
				func foo() (int, int) { return 1, 2}
			`),
			expected: map[string][]string{"_:test.go:3:5": {"foo"}, "x": {"foo"}, "_:test.go:4:8": {"foo"}, "y": {"foo"}},
		},
		{
			name: "init - basic",
			src: dedent(`
				func init() { foo() }
				func foo() {}
			`),
			expected: map[string][]string{"init:test.go:3:6": {"foo"}},
		},
		{
			name: "init - multiple",
			src: dedent(`
				func init() { foo() }
				func init() { bar() }
				func init() {}
				func foo() {}
				func bar() {}
			`),
			expected: map[string][]string{"init:test.go:3:6": {"foo"}, "init:test.go:4:6": {"bar"}},
		},
		{
			name: "via selector's type - non ptr type",
			src: dedent(`
				type a struct{b int}
				func newFoo() a {return a{2}}
				func bar() int { x := newFoo(); return x.b }
			`),
			expected: map[string][]string{"newFoo": {"a"}, "bar": {"newFoo", "a"}},
		},
		{
			name: "via selector's type - ptr type",
			src: dedent(`
				type a struct{b int}
				func newFoo() *a {return &a{2}}
				func bar() int { x := newFoo(); return x.b }
			`),
			expected: map[string][]string{"newFoo": {"a"}, "bar": {"newFoo", "a"}},
		},
		{
			name: "generic alias instantiation",
			src: dedent(`
				type Number interface{ ~int | ~float64 }
				type Vector[T Number] struct{ X, Y T }
				type VecInt = Vector[int]
			`),
			expected: map[string][]string{"Vector": {"Number"}, "VecInt": {"Vector"}},
		},
		{
			name: "generic var instantiation",
			src: dedent(`
				type Number interface{ ~int | ~float64 }
				type Vector[T Number] struct{ X, Y T }
				var v Vector[int]
			`),
			expected: map[string][]string{"Vector": {"Number"}, "v": {"Vector"}},
		},
		{
			name: "generic struct embedding generic type",
			src: dedent(`
				type Number interface{ ~int }
				type Vector[T Number] struct{ X, Y T }
				type Point[T Number] struct { Vec Vector[T] }
			`),
			expected: map[string][]string{"Vector": {"Number"}, "Point": {"Vector", "Number"}},
		},
		{
			name: "generic method value receiver",
			src: dedent(`
				type Number interface{ ~int }
				type Pair[T Number] struct{ A, B T }
				func (p Pair[T]) Sum() T { return p.A + p.B }
			`),
			expected: map[string][]string{"Pair": {"Number"}, "Pair.Sum": {"Pair"}},
		},
		{
			name: "generic method pointer receiver",
			src: dedent(`
				type Number interface{ ~int }
				type Pair[T Number] struct{ A, B T }
				func (p *Pair[T]) Scale(f T) { p.A = p.A * f; p.B = p.B * f }
			`),
			expected: map[string][]string{"Pair": {"Number"}, "*Pair.Scale": {"Pair"}},
		},
		{
			name: "generic function with constraint",
			src: dedent(`
				type Number interface{ ~int }
				type Vector[T Number] struct{ X, Y T }
				func makeVec[T Number](x, y T) Vector[T] { return Vector[T]{X: x, Y: y} }
			`),
			expected: map[string][]string{"Vector": {"Number"}, "makeVec": {"Vector", "Number"}},
		},
		{
			name: "generic multiple type params",
			src: dedent(`
				type Key interface { ~string | ~int }
				type Map[K Key, V any] struct {
					data map[K]V
				}
			`),
			expected: map[string][]string{"Map": {"Key"}},
		},
		{
			name: "nested generics",
			src: dedent(`
				type Number interface { ~int }
				type Vector[T Number] struct { X, Y T }
				type Matrix[T Number] struct { Rows []Vector[T] }
			`),
			expected: map[string][]string{"Vector": {"Number"}, "Matrix": {"Vector", "Number"}},
		},
		{
			name: "generic function uses instantiation",
			src: dedent(`
				type Number interface { ~int }
				type Vector[T Number] struct { X, Y T }
				func useVec() {
					var v Vector[int]
					_ = v
				}
			`),
			expected: map[string][]string{"Vector": {"Number"}, "useVec": {"Vector"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := newTestPackage(t, tt.src)
			g, err := NewGoGraph(pkg)
			require.NoError(t, err)

			// Convert g.intraUses to map[string][]string for comparison
			actual := make(map[string][]string)
			for k, v := range g.intraUses {
				deps := make([]string, 0, len(v))
				for dep := range v {
					deps = append(deps, dep)
				}
				sort.Strings(deps) // Sort the dependencies
				// Only include entries that have dependencies (edges)
				if len(deps) > 0 {
					actual[k] = deps
				}
			}

			// Sort expected dependencies for comparison
			expected := make(map[string][]string)
			for k, v := range tt.expected {
				deps := make([]string, len(v))
				copy(deps, v)
				sort.Strings(deps)
				// Only include entries that have dependencies (edges)
				if len(deps) > 0 {
					expected[k] = deps
				}
			}

			assert.Equal(t, expected, actual)
		})
	}
}

func TestNewGoGraphCrossPackageUsesTableDriven(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		expected map[string][]ExternalID
	}{
		{
			name: "basic use",
			src: dedent(`
				import "foo"
				func bar() {
					foo.Bar()
				}
			`),
			expected: map[string][]ExternalID{"bar": {{"foo", "Bar"}}},
		},
		{
			name: "basic use with multisegment import path",
			src: dedent(`
				import "myproj/some/foo"
				func bar() {
					foo.Bar()
				}
			`),
			expected: map[string][]ExternalID{"bar": {{"myproj/some/foo", "Bar"}}},
		},
		{
			name: "basic use with renamed import path",
			src: dedent(`
				import xxx "myproj/some/foo"
				func bar() {
					xxx.Bar()
				}
			`),
			expected: map[string][]ExternalID{"bar": {{"myproj/some/foo", "Bar"}}},
		},
		{
			name: "basic use with multiple imports, one renamed",
			src: dedent(`
				import "myproj/some/foo"
				import otherfoo "myproj/other/foo"
				func bar() {
					foo.Bar()
				}
				func bar2() {
					otherfoo.Bar()
				}
			`),
			expected: map[string][]ExternalID{"bar": {{"myproj/some/foo", "Bar"}}, "bar2": {{"myproj/other/foo", "Bar"}}},
		},
		// Without full type checking, we don't know which package Bar() belongs to:
		// I think in order to get this to work properly, all imports would need to be type-checked, probably discarding our stub importer.
		// It might be able to be hacked to work without that, by using gocode to read "myproj/some/foo" to see if it has a Bar(). Note that Go
		// enforces no name collisions, meaning `import .` cannot conflict with names in the current package, and multiple `import .` cannot conflict.
		{
			name: "basic use with dot notation",
			src: dedent(`
				import . "myproj/some/foo"
				func bar() {
					Bar()
				}
			`),
			expected: map[string][]ExternalID{},
		},
		{
			name: "embed in struct",
			src: dedent(`
				import "foo"
				import "foo2"
				type f struct {
					foo.Bar
					*foo2.Bar2
				}
			`),
			expected: map[string][]ExternalID{"f": {{"foo", "Bar"}, {"foo2", "Bar2"}}},
		},
		{
			name: "in function params",
			src: dedent(`
				import "foo"
				func bar(x foo.T) {}
			`),
			expected: map[string][]ExternalID{"bar": {{"foo", "T"}}},
		},
		{
			name: "stdlib",
			src: dedent(`
				import "fmt"
				func bar() { fmt.Println("hi") }
			`),
			expected: map[string][]ExternalID{"bar": {{"fmt", "Println"}}},
		},
		{
			name: "struct field type reference",
			src: dedent(`
				import "foo"
				type My struct {
					Field foo.Bar
				}
			`),
			expected: map[string][]ExternalID{"My": {{"foo", "Bar"}}},
		},
		{
			name: "pointer struct field type reference with alias import",
			src: dedent(`
				import alias "foo/bar"
				type MyPtr struct {
					P *alias.Baz
				}
			`),
			expected: map[string][]ExternalID{"MyPtr": {{"foo/bar", "Baz"}}},
		},
		{
			name: "function return type and body reference",
			src: dedent(`
				import "foo"
				func Make() foo.Bar {
					return foo.Bar{}
				}
			`),
			expected: map[string][]ExternalID{"Make": {{"foo", "Bar"}}},
		},
		{
			name: "var initialization with external type literal",
			src: dedent(`
				import "foo"
				var x = foo.Bar{}
			`),
			expected: map[string][]ExternalID{"x": {{"foo", "Bar"}}},
		},
		// NOTE: Since we're using a stub importer, no types are resolved from other packages, and g.info does not know what type "x" is.
		// If we want to handle this, I think the only sol'n is to actually type check imported packages.
		// For now, I'm leaving this test case here as a breadcrumb as to how things work now and why.
		{
			name: "method call - (isnt detected)",
			src: dedent(`
				import "foo"
				func bar() {
					var x foo.T
					x.Baz()
				}
			`),
			expected: map[string][]ExternalID{"bar": {{"foo", "T"}}}, // We'd like {"foo", "T.Baz"} to also be there.
		},
		{
			name: "type alias to external type",
			src: dedent(`
				import "foo"
				type Alias = foo.Bar
			`),
			expected: map[string][]ExternalID{"Alias": {{"foo", "Bar"}}},
		},
		{
			name: "generic variable instantiation",
			src: dedent(`
				import "foo"
				var x foo.Vector[int]
			`),
			expected: map[string][]ExternalID{"x": {{"foo", "Vector"}}},
		},
		{
			name: "generic alias to external type",
			src: dedent(`
				import "foo"
				type VecInt = foo.Vector[int]
			`),
			expected: map[string][]ExternalID{"VecInt": {{"foo", "Vector"}}},
		},
		{
			name: "generic struct field",
			src: dedent(`
				import "foo"
				type S struct {
					V foo.Vector[int]
				}
			`),
			expected: map[string][]ExternalID{"S": {{"foo", "Vector"}}},
		},
		{
			name: "generic function parameter and return",
			src: dedent(`
				import "foo"
				func process(v foo.Vector[int]) foo.Vector[int] {
					return v
				}
			`),
			expected: map[string][]ExternalID{"process": {{"foo", "Vector"}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := newTestPackage(t, tt.src)
			g, err := NewGoGraph(pkg)
			require.NoError(t, err)

			// Convert g.crossPackageUses to map[string][]crossPackageRef for comparison
			actual := make(map[string][]ExternalID)
			for def, refs := range g.crossPackageUses {
				slice := make([]ExternalID, 0, len(refs))
				for ref := range refs {
					slice = append(slice, ref)
				}
				sort.Slice(slice, func(i, j int) bool {
					if slice[i].ImportPath == slice[j].ImportPath {
						return slice[i].ID < slice[j].ID
					}
					return slice[i].ImportPath < slice[j].ImportPath
				})
				if len(slice) > 0 {
					actual[def] = slice
				}
			}

			// Sort expected dependencies for comparison
			expected := make(map[string][]ExternalID)
			for def, refs := range tt.expected {
				sorted := make([]ExternalID, len(refs))
				copy(sorted, refs)
				sort.Slice(sorted, func(i, j int) bool {
					if sorted[i].ImportPath == sorted[j].ImportPath {
						return sorted[i].ID < sorted[j].ID
					}
					return sorted[i].ImportPath < sorted[j].ImportPath
				})
				if len(sorted) > 0 {
					expected[def] = sorted
				}
			}

			assert.Equal(t, expected, actual)
		})
	}
}

func newTestPackage(t *testing.T, src string) *gocode.Package {
	t.Helper()
	return newTestPackageWithFiles(t, map[string]string{"test.go": src})
}

func newTestPackageWithFiles(t *testing.T, files map[string]string) *gocode.Package {
	t.Helper()
	dir, err := os.MkdirTemp("", "gographtest_module")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	// Create a dummy go.mod
	err = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)
	require.NoError(t, err)

	// Create a package directory inside the module
	pkgDir := filepath.Join(dir, "testpkg")
	err = os.Mkdir(pkgDir, 0755)
	require.NoError(t, err)

	var filenames []string
	for filename, src := range files {
		// Add package declaration if missing
		if !bytes.HasPrefix(bytes.TrimSpace([]byte(src)), []byte("package")) {
			src = "package testpkg\n\n" + src
		}

		// Write the source file into the package directory
		filePath := filepath.Join(pkgDir, filename)
		err = os.WriteFile(filePath, []byte(src), 0644)
		require.NoError(t, err)
		filenames = append(filenames, filename)
	}

	m, err := gocode.NewModule(dir)
	require.NoError(t, err)

	pkg, err := m.ReadPackage("testpkg", filenames)
	require.NoError(t, err)
	return pkg
}

var dedent = gocodetesting.Dedent

func TestNewGoGraphTestIdentifiers(t *testing.T) {
	files := map[string]string{
		"main.go": dedent(`
			package testpkg
			type A struct {}
			func F() {}
		`),
		"main_test.go": dedent(`
			package testpkg
			import "testing"
			type B struct {}
			func TestG(t *testing.T) {}
		`),
	}

	pkg := newTestPackageWithFiles(t, files)
	g, err := NewGoGraph(pkg)
	require.NoError(t, err)
	require.NotNil(t, g)

	actualTestIdentifiers := make([]string, 0, len(g.testIdentifiers))
	for ident := range g.testIdentifiers {
		actualTestIdentifiers = append(actualTestIdentifiers, ident)
	}
	sort.Strings(actualTestIdentifiers)

	expectedTestIdentifiers := []string{"B", "TestG"}
	sort.Strings(expectedTestIdentifiers)

	assert.Equal(t, expectedTestIdentifiers, actualTestIdentifiers)

	// Also good to check that regular identifiers are not in the test set.
	_, okA := g.testIdentifiers["A"]
	assert.False(t, okA)
	_, okF := g.testIdentifiers["F"]
	assert.False(t, okF)
}

func TestAllIdentifiers(t *testing.T) {
	src := dedent(`
		package testpkg

		type Foo struct {
			bar Bar
		}

		type Bar struct{}

		var globalVar Foo
		const myConst = 42

		func standaloneFunc() {}

		func (f *Foo) PtrMethod() {
			standaloneFunc()
		}

		func (b Bar) ValueMethod() {}

		// Anonymous variable
		var _ = Foo{}

		func init() {}
	`)

	pkg := newTestPackage(t, src)
	g, err := NewGoGraph(pkg)
	require.NoError(t, err)
	require.NotNil(t, g)

	identifiers := g.AllIdentifiers()

	// Sort for consistent comparison
	sort.Strings(identifiers)

	// Check that we have all the expected named identifiers
	expectedNamed := []string{
		"*Foo.PtrMethod",
		"Bar",
		"Bar.ValueMethod",
		"Foo",
		"globalVar",
		"myConst",
		"standaloneFunc",
		"_:test.go:21:5",
		"init:test.go:23:6",
	}

	assert.ElementsMatch(t, expectedNamed, identifiers, "Named identifiers should match expected")
	assert.Equal(t, 9, len(identifiers), "Should have 9 total identifiers (7 named + 1 anonymous + 1 init)")
}

func TestAllIdentifiersInlineType(t *testing.T) {
	src := dedent(`
		package testpkg

		func Test_something(t *testing.T) {
			t.Parallel()

			p := ptr(2)

			type args struct {
				first  interface{}
				second interface{}
			}
			tests := []struct {
				name string
				args args
				same BoolAssertionFunc
				ok   BoolAssertionFunc
			}{
				{
					name: "1 != 2",
					args: args{first: 1, second: 2},
					same: False,
					ok:   False,
				},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					same, ok := samePointers(tt.args.first, tt.args.second)
					tt.same(t, same)
					tt.ok(t, ok)
				})
			}
		}
	`)

	pkg := newTestPackage(t, src)
	g, err := NewGoGraph(pkg)
	require.NoError(t, err)
	require.NotNil(t, g)

	identifiers := g.AllIdentifiers()

	sort.Strings(identifiers)

	assert.ElementsMatch(t, []string{"Test_something"}, identifiers, "Named identifiers should match expected")
}

func TestAnonymousIdentifiers(t *testing.T) {
	// NOTE: The language fully supports this.
	// var _ SomeInterface = (*someStruct)(nil) // used in practice to statically assert interface compliance
	// func _() { ... } // used in practice in some schemes to statically assert generated code is up to date.

	src := dedent(`
		package testpkg

		var _ int
		const _ int
		type _ int
		func _() {}
	`)

	pkg := newTestPackage(t, src)
	g, err := NewGoGraph(pkg)
	require.NoError(t, err)
	require.NotNil(t, g)

	identifiers := g.AllIdentifiers()

	// Sort for consistent comparison
	sort.Strings(identifiers)

	// Check that we have all the expected named identifiers
	expectedNamed := []string{
		"_:test.go:3:5",
		"_:test.go:4:7",
		"_:test.go:5:6",
		"_:test.go:6:6",
	}

	assert.ElementsMatch(t, expectedNamed, identifiers)
	assert.Equal(t, 4, len(identifiers))
}

func TestWithoutTestIdentifiers(t *testing.T) {
	files := map[string]string{
		"app.go": dedent(`
			package main

			type ProdType struct{}
			type AnotherProdType struct{}

			func ProdFunc(p ProdType) {
				_ = AnotherProdType{}
			}

			func ProdUsesTestHelper() {
				_ = TestHelperType{}
			}
		`),
		"app_test.go": dedent(`
			package main

			import "testing"

			type TestHelperType struct{}

			func (p *ProdType) HelperMethodOnProdType() {}

			func TestMyCode(t *testing.T) {
				ProdFunc(ProdType{})
				var h TestHelperType
				_ = h
				var p ProdType
				p.HelperMethodOnProdType()
			}
		`),
	}

	pkg := newTestPackageWithFiles(t, files)
	g, err := NewGoGraph(pkg)
	require.NoError(t, err)

	// Assert all identifiers are present initially
	expectedAllOriginal := []string{
		"AnotherProdType",
		"ProdFunc",
		"ProdType",
		"ProdUsesTestHelper",
		"TestHelperType",
		"*ProdType.HelperMethodOnProdType",
		"TestMyCode",
	}
	assert.ElementsMatch(t, expectedAllOriginal, g.AllIdentifiers(), "original graph should have all identifiers")

	prodGraph := g.WithoutTestIdentifiers()

	// Assert test-related identifiers are removed from the prod graph
	expectedProdIdentifiers := []string{
		"AnotherProdType",
		"ProdFunc",
		"ProdType",
		"ProdUsesTestHelper",
	}
	assert.ElementsMatch(t, expectedProdIdentifiers, prodGraph.AllIdentifiers(), "prod graph should only have prod identifiers")

	// The new graph should not have any test identifiers in its own map
	assert.Empty(t, prodGraph.testIdentifiers)

	// Check the intraUses of the new graph
	expectedUses := map[string][]string{
		"ProdFunc": {"AnotherProdType", "ProdType"},
	}

	actualUses := make(map[string][]string)
	for def, uses := range prodGraph.intraUses {
		useList := []string{}
		for use := range uses {
			useList = append(useList, use)
		}
		sort.Strings(useList)
		actualUses[def] = useList
	}

	assert.Equal(t, expectedUses, actualUses)

	// For sanity checking, let's look at the original graph's test identifiers
	expectedTestIDs := []string{
		"TestHelperType",
		"TestMyCode",
		"*ProdType.HelperMethodOnProdType",
	}
	actualTestIDs := []string{}
	for id := range g.testIdentifiers {
		actualTestIDs = append(actualTestIDs, id)
	}
	assert.ElementsMatch(t, expectedTestIDs, actualTestIDs)
}

func TestWithoutIdentifiers(t *testing.T) {
	compareGraphs := func(t *testing.T, expected map[string][]string, actual map[string]map[string]struct{}) {
		t.Helper()
		convertedActual := make(map[string][]string)
		for k, v := range actual {
			s := make([]string, 0, len(v))
			for i := range v {
				s = append(s, i)
			}
			sort.Strings(s)
			convertedActual[k] = s
		}
		for _, s := range expected {
			sort.Strings(s)
		}
		assert.Equal(t, expected, convertedActual)
	}

	src := dedent(`
		package main
		type A struct{}
		type B struct{ a A }
		type C struct{ b B }
		var V C
		func F() {}
		func G() { F() }
	`)
	pkg := newTestPackage(t, src)
	g, err := NewGoGraph(pkg)
	require.NoError(t, err)

	// Original graph
	assert.ElementsMatch(t, []string{"A", "B", "C", "F", "G", "V"}, g.AllIdentifiers(), "original graph identifiers")
	expectedG := map[string][]string{
		"B": {"A"},
		"C": {"B"},
		"V": {"C"},
		"G": {"F"},
	}
	compareGraphs(t, expectedG, g.intraUses)

	// Test removing a single identifier that is a leaf dependency ("A")
	g2 := g.WithoutIdentifiers([]string{"A"})
	assert.ElementsMatch(t, []string{"B", "C", "F", "G", "V"}, g2.AllIdentifiers(), "g2 identifiers")
	expectedG2 := map[string][]string{
		"C": {"B"},
		"V": {"C"},
		"G": {"F"},
	}
	compareGraphs(t, expectedG2, g2.intraUses)

	// Test removing a middle identifier ("B")
	g3 := g.WithoutIdentifiers([]string{"B"})
	assert.ElementsMatch(t, []string{"A", "C", "F", "G", "V"}, g3.AllIdentifiers(), "g3 identifiers")
	expectedG3 := map[string][]string{
		"V": {"C"},
		"G": {"F"},
	}
	compareGraphs(t, expectedG3, g3.intraUses)
	assert.Empty(t, g3.testIdentifiers, "test identifiers should be empty for this test setup")

	// Test removing multiple identifiers
	g4 := g.WithoutIdentifiers([]string{"C", "F"})
	assert.ElementsMatch(t, []string{"A", "B", "G", "V"}, g4.AllIdentifiers(), "g4 identifiers")
	expectedG4 := map[string][]string{
		"B": {"A"},
	}
	compareGraphs(t, expectedG4, g4.intraUses)
	assert.Empty(t, g4.testIdentifiers, "test identifiers should be empty for this test setup")

	// Test removing an identifier that is part of a test file
	files := map[string]string{
		"a.go": dedent(`
			package main
			type A struct{}
			type B struct{}
		`),
		"a_test.go": dedent(`
			package main
			import "testing"
			type C struct{ a A }
			func TestSomething(t *testing.T) {
				var b B
				_ = b
			}
		`),
	}
	pkgWithTests := newTestPackageWithFiles(t, files)
	gWithTests, err := NewGoGraph(pkgWithTests)
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"A", "B", "C", "TestSomething"}, gWithTests.AllIdentifiers(), "gWithTests identifiers")
	require.Contains(t, gWithTests.testIdentifiers, "C")
	require.Contains(t, gWithTests.testIdentifiers, "TestSomething")

	// Remove a test identifier
	g5 := gWithTests.WithoutIdentifiers([]string{"C"})
	assert.ElementsMatch(t, []string{"A", "B", "TestSomething"}, g5.AllIdentifiers(), "g5 identifiers")
	expectedG5 := map[string][]string{
		"TestSomething": {"B"},
	}
	compareGraphs(t, expectedG5, g5.intraUses)
	assert.NotContains(t, g5.testIdentifiers, "C")
	assert.Contains(t, g5.testIdentifiers, "TestSomething")
}
