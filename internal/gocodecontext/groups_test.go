package gocodecontext

import (
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

var Dedent = gocodetesting.Dedent

func TestGroups(t *testing.T) {
	tests := []struct {
		name       string
		code       string
		options    *GroupOptions // optional. If nil, use GroupOptions{IncludePackageDocs: true, IncludeExternalDeps: true}. NOTE: IncludePackageDocs: true MUST be there or tests will fail (this can be changed, of course)
		wantGroups [][]string    // any order, the test driver will handle

		// For deps, choose *any* identifier in a group for both key and value (no need for all IDs to be present)

		wantDirectDeps   map[string]string
		wantUsedByDeps   map[string]string
		wantExternalDeps map[string]string // key is any ident for a group; value is "some/import/path.myIdentifier"

		wantIsDocumented map[string]bool // map from *any* identifier in a group to its documentation status
	}{
		{
			name: "Simple const block",
			code: Dedent(`
				// These consts...
				const (
					A = 1
					B = 2
				)
			`),
			wantGroups: [][]string{
				{"A", "B"},
			},
			wantIsDocumented: map[string]bool{"A": false},
		},
		{
			name: "Simple const block, ConsiderConstBlocksDocumenting, not documented",
			code: Dedent(`
				const (
					A = 1
					B = 2 // B
				)
			`),
			options: &GroupOptions{ConsiderConstBlocksDocumenting: true, IncludePackageDocs: true},
			wantGroups: [][]string{
				{"A", "B"},
			},
			wantIsDocumented: map[string]bool{"A": false},
		},
		{
			name: "Simple const block, ConsiderConstBlocksDocumenting, documented",
			code: Dedent(`
				// These consts...
				const (
					A = 1
					B = 2 // B
				)
			`),
			options: &GroupOptions{ConsiderConstBlocksDocumenting: true, IncludePackageDocs: true},
			wantGroups: [][]string{
				{"A", "B"},
			},
			wantIsDocumented: map[string]bool{"A": true},
		},
		{
			name: "Function with dependency",
			code: Dedent(`
				// B is a function.
				func B() {}
				func A() { B() }
			`),
			wantGroups: [][]string{
				{"A"},
				{"B"},
			},
			wantDirectDeps:   map[string]string{"A": "B"},
			wantUsedByDeps:   map[string]string{"B": "A"},
			wantIsDocumented: map[string]bool{"A": false, "B": true},
		},
		{
			name: "Type with method",
			code: Dedent(`
				type T struct{}
				// M is a method.
				func (t T) M() {}
			`),
			wantGroups: [][]string{
				{"T"},
				{"T.M"},
			},
			wantDirectDeps:   map[string]string{"T.M": "T"},
			wantUsedByDeps:   map[string]string{"T": "T.M"},
			wantIsDocumented: map[string]bool{"T": false, "T.M": true},
		},
		{
			name: "Cyclic dependency",
			code: Dedent(`
				func A() { B() }
				func B() { A() }
			`),
			wantGroups: [][]string{
				{"A", "B"},
			},
			wantIsDocumented: map[string]bool{"A": false},
		},
		{
			name: "Value and type dependency",
			code: Dedent(`
				type MyInt int
				const C MyInt = 0
			`),
			wantGroups: [][]string{
				{"MyInt"},
				{"C"},
			},
			wantDirectDeps:   map[string]string{"C": "MyInt"},
			wantUsedByDeps:   map[string]string{"MyInt": "C"},
			wantIsDocumented: map[string]bool{"MyInt": false, "C": false},
		},
		{
			name: "Var block with SCC overlap",
			code: Dedent(`
				var (
					a = b
					b = a
					c = 0
				)
			`),
			wantGroups: [][]string{
				{"a", "b", "c"}, // all three from the block must stay together
			},
			wantDirectDeps:   map[string]string{},
			wantUsedByDeps:   map[string]string{}, // not important for this bug
			wantIsDocumented: map[string]bool{"a": false},
		},
		{
			name: "Var block with mixed SCC and non-SCC members",
			code: Dedent(`
				var (
					a = b
					b = a
				)
				var (
					c = a
					d = 1
				)
			`),
			wantGroups: [][]string{
				{"a", "b"},
				{"c", "d"},
			},
			wantDirectDeps: map[string]string{"c": "a"},
		},
		{
			name: "structs",
			code: Dedent(`
				// A
				type A struct {
					F1 int
					F2 int // F2
				}
				// B
				type B struct {
					A // A
					F3 // F3
				}
			`),
			wantGroups:       [][]string{{"A"}, {"B"}},
			wantDirectDeps:   map[string]string{"B": "A"},
			wantUsedByDeps:   map[string]string{"A": "B"},
			wantIsDocumented: map[string]bool{"A": false, "B": true},
		},
		{
			name: "external groups - basic dep",
			code: Dedent(`
				import "mymodule/other"

				type T struct{}
				func (t *T) foo() {
					other.Foo()
				}
			`),
			wantGroups:       [][]string{{"*T.foo"}, {"T"}},
			wantDirectDeps:   map[string]string{"*T.foo": "T"},
			wantExternalDeps: map[string]string{"*T.foo": "mymodule/other.Foo"},
			wantIsDocumented: map[string]bool{"*T.foo": false},
		},
		// NOTE: until we get gograph to typecheck multiple packages, we're not going to know about the *Other.FooBar dep.
		{
			name: "external groups - multiple deps",
			code: Dedent(`
				import "mymodule/other"

				type T struct{ other.Other }
				func (t *T) foo() {
					other.Foo()
				}
				func bar() {
					var t *other.Other
					t.FooBar()
				}
			`),
			wantGroups:       [][]string{{"*T.foo"}, {"T"}, {"bar"}},
			wantDirectDeps:   map[string]string{"*T.foo": "T"},
			wantExternalDeps: map[string]string{"*T.foo": "mymodule/other.Foo", "bar": "mymodule/other.Other"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gocodetesting.WithCode(t, tt.code, func(pkg *gocode.Package) {
				otherFile := Dedent(`
					package otherpkg

					// Other is documented
					type Other struct {}

					// Foo is documented
					func Foo() {}

					func Bar() {
						// undocumented
					}

					// FooBar documented
					func (o *Other) FooBar() {}
				`)
				err := gocodetesting.AddPackage(t, pkg.Module, "other", map[string]string{"other.go": otherFile})
				assert.NoError(t, err)

				options := &GroupOptions{IncludePackageDocs: true, IncludeExternalDeps: true}
				if tt.options != nil {
					options = tt.options
				}

				groups, err := Groups(pkg.Module, pkg, *options)
				assert.NoError(t, err)

				// The user's change introduces a "package" identifier. We need
				// to adjust the expectations for each test case. The package
				// is in its own group and is not documented in these tests.
				tt.wantGroups = append(tt.wantGroups, []string{"package"})
				if tt.wantIsDocumented == nil {
					tt.wantIsDocumented = make(map[string]bool)
				}
				tt.wantIsDocumented["package"] = false

				// For easier comparison, create a map from id to group
				idToGroup := make(map[string]*IdentifierGroup)
				for _, g := range groups {
					for _, id := range g.IDs {
						idToGroup[id] = g
					}
				}

				// Check groups
				var actualGroups [][]string
				for _, g := range groups {
					sort.Strings(g.IDs)
					actualGroups = append(actualGroups, g.IDs)
				}
				// Sort outer slice for consistent comparison
				sort.Slice(actualGroups, func(i, j int) bool {
					return strings.Join(actualGroups[i], ",") < strings.Join(actualGroups[j], ",")
				})
				sort.Slice(tt.wantGroups, func(i, j int) bool {
					sort.Strings(tt.wantGroups[i])
					sort.Strings(tt.wantGroups[j])
					return strings.Join(tt.wantGroups[i], ",") < strings.Join(tt.wantGroups[j], ",")
				})
				assert.Equal(t, tt.wantGroups, actualGroups, "groups don't match")

				// Check direct dependencies
				for from, to := range tt.wantDirectDeps {
					fromGroup := idToGroup[from]
					toGroup := idToGroup[to]
					if assert.NotNil(t, fromGroup) && assert.NotNil(t, toGroup) {
						found := false
						for _, dep := range fromGroup.DirectDeps {
							if dep == toGroup {
								found = true
								break
							}
						}
						assert.True(t, found, "direct dep from %s to %s not found", from, to)
					}
				}

				// Check used-by dependencies
				for from, to := range tt.wantUsedByDeps {
					fromGroup := idToGroup[from]
					toGroup := idToGroup[to]
					if assert.NotNil(t, fromGroup) && assert.NotNil(t, toGroup) {
						found := false
						for _, user := range fromGroup.UsedByDeps {
							if user == toGroup {
								found = true
								break
							}
						}
						assert.True(t, found, "used-by dep from %s to %s not found", from, to)
					}
				}

				// Check external dependencies
				for from, to := range tt.wantExternalDeps {
					fromGroup := idToGroup[from]
					if assert.NotNil(t, fromGroup) {
						found := false
						for _, dep := range fromGroup.DirectDeps {
							if dep.IsExternal {
								// fmt.Println(dep.externalImportPath, dep.ids)
								// Properties that currently exist of all external groups:
								assert.True(t, dep.IsDocumented)
								assert.EqualValues(t, 0, len(dep.DirectDeps))
								assert.EqualValues(t, 0, len(dep.UsedByDeps))
								assert.EqualValues(t, 1, len(dep.IDs))
								assert.True(t, dep.SnippetTokens > 0)
								assert.EqualValues(t, 0, dep.BodyTokens)

								depStr := dep.ExternalImportPath + "." + strings.Join(dep.IDs, ",")
								if to == depStr {
									found = true
									break
								}
							}
						}
						assert.True(t, found, "external dep from %s to %s not found", from, to)
					}
				}

				// Check documentation status
				for id, wantDoc := range tt.wantIsDocumented {
					group := idToGroup[id]
					if assert.NotNil(t, group, "group for id %s not found", id) {
						assert.Equal(t, wantDoc, group.IsDocumented, "documentation status for group with id %s doesn't match", id)
					}
				}
			})
		})
	}
}

func TestGroupsForIdentifiers(t *testing.T) {
	g1 := &IdentifierGroup{IDs: []string{"a", "b"}}
	g2 := &IdentifierGroup{IDs: []string{"c"}}
	g3 := &IdentifierGroup{IDs: []string{"d", "e"}}

	groups := []*IdentifierGroup{g1, g2, g3}

	tests := []struct {
		name     string
		ids      []string
		expected []*IdentifierGroup
	}{
		{
			name:     "matches multiple groups",
			ids:      []string{"b", "e"},
			expected: []*IdentifierGroup{g1, g3},
		},
		{
			name:     "duplicate ids still return unique groups",
			ids:      []string{"a", "a"},
			expected: []*IdentifierGroup{g1},
		},
		{
			name:     "multiple ids within a group only return the group once",
			ids:      []string{"a", "b"},
			expected: []*IdentifierGroup{g1},
		},
		{
			name:     "no matches returns nil",
			ids:      []string{"z"},
			expected: nil,
		},
		{
			name:     "empty ids returns nil",
			ids:      nil,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterGroupsForIdentifiers(groups, tt.ids)
			assert.Equal(t, tt.expected, got, "unexpected groups for ids %v", tt.ids)
		})
	}
}
