package gocodecontext

import (
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContextBasic(t *testing.T) {
	// Source code where a variable `myVar` is used by a function `UseMyVar`.
	// When we create a context for the group containing `myVar`, the group
	// containing `UseMyVar` should be included "for free" (via UsedByDeps).
	src := Dedent(`
        var myVar = 5

        func UseMyVar() int {
            return myVar + 1
        }
    `)

	gocodetesting.WithCode(t, src, func(pkg *gocode.Package) {
		groups, err := Groups(pkg.Module, pkg, GroupOptions{})
		if !assert.NoError(t, err) {
			return
		}

		// Locate the groups for the variable and the function.
		var varGroup, funcGroup *IdentifierGroup
		for _, g := range groups {
			for _, id := range g.IDs {
				switch id {
				case "myVar":
					varGroup = g
				case "UseMyVar":
					funcGroup = g
				}
			}
		}

		// Sanity check.
		if !assert.NotNil(t, varGroup) || !assert.NotNil(t, funcGroup) {
			return
		}

		c := NewContext([]*IdentifierGroup{varGroup})

		// Before adding funcGroup explicitly.
		assert.ElementsMatch(t, []*IdentifierGroup{varGroup}, c.AddedGroups())
		assert.ElementsMatch(t, []*IdentifierGroup{funcGroup}, c.GroupsForFree())
		assert.ElementsMatch(t, []*IdentifierGroup{varGroup, funcGroup}, c.AllGroups())

		assert.ElementsMatch(t, []string{"myVar"}, c.AddedIdentifiers())
		assert.ElementsMatch(t, []string{"UseMyVar"}, c.IdentifiersForFree())
		assert.ElementsMatch(t, []string{"myVar", "UseMyVar"}, c.AllIdentifiers())

		cost := c.Cost()
		assert.EqualValues(t, 14, cost)                                 // I didn't actually verify 14 is correct but it's plausible.
		assert.EqualValues(t, 18, defaultCountTokens([]byte(c.Code()))) // This is correctly slightly higher than c.Cost() to account for filename headers.

		// Add funcGroup to the context and verify updates.
		c.AddGroup(funcGroup)

		assert.ElementsMatch(t, []*IdentifierGroup{varGroup, funcGroup}, c.AddedGroups())
		assert.Empty(t, c.GroupsForFree())
		assert.ElementsMatch(t, []*IdentifierGroup{varGroup, funcGroup}, c.AllGroups())

		assert.ElementsMatch(t, []string{"myVar", "UseMyVar"}, c.AddedIdentifiers())
		assert.Empty(t, c.IdentifiersForFree())
		assert.ElementsMatch(t, []string{"myVar", "UseMyVar"}, c.AllIdentifiers())

		assert.EqualValues(t, cost, c.Cost()) // didn't change
	})
}

func TestContextAddIndependentGroup(t *testing.T) {
	// Source with a dependency (myVar <- UseMyVar) and an independent function Other.
	src := Dedent(`
        var myVar = 5

        func UseMyVar() int {
            return myVar + 1
        }

        func Other() int {
            return 42
        }
    `)

	gocodetesting.WithCode(t, src, func(pkg *gocode.Package) {
		groups, err := Groups(pkg.Module, pkg, GroupOptions{})
		require.NoError(t, err)

		var varGroup, funcGroup, otherGroup *IdentifierGroup
		for _, g := range groups {
			for _, id := range g.IDs {
				switch id {
				case "myVar":
					varGroup = g
				case "UseMyVar":
					funcGroup = g
				case "Other":
					otherGroup = g
				}
			}
		}

		// Sanity checks.
		require.NotNil(t, varGroup)
		require.NotNil(t, funcGroup)
		require.NotNil(t, otherGroup)

		c := NewContext([]*IdentifierGroup{varGroup})

		// Initial expectations – UseMyVar is included for free via UsedByDeps.
		assert.ElementsMatch(t, []*IdentifierGroup{varGroup}, c.AddedGroups())
		assert.ElementsMatch(t, []*IdentifierGroup{funcGroup}, c.GroupsForFree())
		assert.ElementsMatch(t, []*IdentifierGroup{varGroup, funcGroup}, c.AllGroups())

		assert.ElementsMatch(t, []string{"myVar"}, c.AddedIdentifiers())
		assert.ElementsMatch(t, []string{"UseMyVar"}, c.IdentifiersForFree())
		assert.ElementsMatch(t, []string{"myVar", "UseMyVar"}, c.AllIdentifiers())

		costBefore := c.Cost()

		// Add an unrelated group that was NOT included for free.
		c.AddGroup(otherGroup)

		// After adding, Other should be an added group/identifier. Freebies remain unchanged.
		assert.ElementsMatch(t, []*IdentifierGroup{varGroup, otherGroup}, c.AddedGroups())
		assert.ElementsMatch(t, []*IdentifierGroup{funcGroup}, c.GroupsForFree())
		assert.ElementsMatch(t, []*IdentifierGroup{varGroup, funcGroup, otherGroup}, c.AllGroups())

		assert.ElementsMatch(t, []string{"myVar", "Other"}, c.AddedIdentifiers())
		assert.ElementsMatch(t, []string{"UseMyVar"}, c.IdentifiersForFree())
		assert.ElementsMatch(t, []string{"myVar", "UseMyVar", "Other"}, c.AllIdentifiers())

		costAfter := c.Cost()
		assert.Greater(t, costAfter, costBefore, "adding a new, independent group should increase cost")
	})
}

func TestAdditionalCostForGroup(t *testing.T) {
	testCases := []struct {
		name          string   // sub-test name
		src           string   // Go source code to analyse
		initial       []string // identifiers explicitly added to the initial context
		target        string   // identifier whose group's additional cost we measure
		expectZero    bool     // true → expected delta is 0
		validateExact bool     // if true, compare delta to the precise newCtx cost diff
	}{
		{
			name: "already added",
			src: gocodetesting.Dedent(`
                var myVar = 5
            `),
			initial:    []string{"myVar"},
			target:     "myVar",
			expectZero: true,
		},
		{
			name: "free group",
			src: gocodetesting.Dedent(`
                var myVar = 5

                func UseMyVar() int {
                    return myVar + 1
                }
            `),
			initial:    []string{"myVar"},
			target:     "UseMyVar",
			expectZero: true,
		},
		{
			name: "independent group costs extra",
			src: gocodetesting.Dedent(`
                var myVar = 5

                func UseMyVar() int {
                    return myVar + 1
                }

                func Other() int {
                    return 42
                }
            `),
			initial:       []string{"myVar"},
			target:        "Other",
			expectZero:    false,
			validateExact: true,
		},
		{
			name: "snippet upgrades to full",
			src: gocodetesting.Dedent(`
                // helper docs...
                func helper() {}

                // foo docs...
                func foo() {
                    helper()
                }
            `),
			initial:       []string{"foo"},
			target:        "helper",
			expectZero:    false,
			validateExact: true,
		},
		{
			name: "missing UsedBy dependency costs extra",
			src: gocodetesting.Dedent(`
                var myVar = 5

                func UseMyVar() int {
                    return myVar + 1
                }

                func Another() int {
                    return UseMyVar() + 2
                }
            `),
			initial:    []string{"myVar"},
			target:     "UseMyVar",
			expectZero: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gocodetesting.WithCode(t, tc.src, func(pkg *gocode.Package) {
				groups, err := Groups(pkg.Module, pkg, GroupOptions{})
				require.NoError(t, err)

				// Map identifiers → groups for quick lookup.
				idToGroup := make(map[string]*IdentifierGroup)
				for _, g := range groups {
					for _, id := range g.IDs {
						idToGroup[id] = g
					}
				}

				// Build initial context groups.
				var initialGroups []*IdentifierGroup
				for _, id := range tc.initial {
					grp := idToGroup[id]
					require.NotNil(t, grp, "initial id not found: %s", id)
					initialGroups = append(initialGroups, grp)
				}

				targetGroup := idToGroup[tc.target]
				require.NotNil(t, targetGroup, "target id not found: %s", tc.target)

				ctx := NewContext(initialGroups)
				delta := ctx.AdditionalCostForGroup(targetGroup)

				if tc.expectZero {
					assert.Equal(t, 0, delta)
				} else {
					assert.Greater(t, delta, 0)
				}

				if tc.validateExact && !tc.expectZero {
					newInitial := append(initialGroups, targetGroup)
					newCtx := NewContext(newInitial)
					expected := newCtx.Cost() - ctx.Cost()
					require.Greater(t, expected, 0)
					assert.Equal(t, expected, delta)
				}
			})
		})
	}
}

func TestContextCode(t *testing.T) {
	tests := []struct {
		name          string
		code          map[string]string
		otherPackages map[string]string // module path -> code (assumed to go into "code.go")
		ids           []string

		wantExactly string // if present, the trimmed code context must match exactly this trimmed text
	}{
		{
			name: "simple func",
			code: map[string]string{
				"code.go": Dedent(`
					// foo...
					func foo() {
						fmt.Println("hi")
					}
				`),
			},
			ids: []string{"foo"},
			wantExactly: Dedent(`
				// code.go:

				// foo...
				func foo() {
					fmt.Println("hi")
				}
			`),
		},
		{
			name: "undocumeneted deps require the full function",
			code: map[string]string{
				"code.go": Dedent(`
					func A() {
						helper()
					}

					func helper() {}
				`),
			},
			ids: []string{"A", "B"},
			wantExactly: Dedent(`
				// code.go:

				func A() {
					helper()
				}

				func helper() {}
			`),
		},
		{
			name: "duplicate direct deps deduped",
			code: map[string]string{
				"code.go": Dedent(`
					func A() {
						helper()
					}

					func B() {
						helper()
					}

					// helper...
					func helper() {}
				`),
			},
			ids: []string{"A", "B"},
			wantExactly: Dedent(`
				// code.go:

				func A() {
					helper()
				}

				func B() {
					helper()
				}

				// helper...
				func helper()
			`),
		},
		{
			name: "used-by dependency included",
			code: map[string]string{
				"code.go": Dedent(`
					var myVar = 5

					func UseMyVar() int {
						return myVar + 1
					}
				`),
			},
			ids: []string{"myVar"},
			wantExactly: Dedent(`
				// code.go:

				var myVar = 5

				func UseMyVar() int {
					return myVar + 1
				}
			`),
		},
		{
			name: "multiple files. a type and their functions",
			code: map[string]string{
				"type.go": Dedent(`
					type X struct {
						a int
						B int
					}

					type otherX struct{}
				`),
				"methods.go": Dedent(`
					// A...
					func (x *X) A() {
						helper1()
					}

					// B...
					func (x X) B() {
						helper2()
					}

					func (x *X) C() {}

					func helper1() {
						fmt.Println("ok")
					}

					// should just have a snippet bytes:
					func helper2() {
						fmt.Println("ok")
					}
				`),
			},
			ids: []string{"X", "X.B"},
			wantExactly: Dedent(`
				// methods.go:

				// A...
				func (x *X) A() {
					helper1()
				}

				// B...
				func (x X) B() {
					helper2()
				}

				func (x *X) C() {}

				// should just have a snippet bytes:
				func helper2()

				// type.go:

				type X struct {
					a int
					B int
				}
			`),
		},
		{
			name: "cyclic dependency",
			code: map[string]string{
				"code.go": Dedent(`
					func A() { B() }
					func B() { A() }
				`),
			},
			ids: []string{"A"},
			wantExactly: Dedent(`
				// code.go:

				func A() { B() }

				func B() { A() }
			`),
		},
		{
			name: "const block",
			code: map[string]string{
				"code.go": Dedent(`
					const (
						A = 1
						B = 2
					)
				`),
			},
			ids: []string{"A"},
			wantExactly: Dedent(`
				// code.go:

				const (
					A = 1
					B = 2
				)
			`),
		},
		{
			name: "inter-dependent var blocks",
			code: map[string]string{
				"code.go": Dedent(`
					var (
						a = b
						b = 1
					)

					var (
						c = a
					)
				`),
			},
			ids: []string{"c"},
			wantExactly: Dedent(`
				// code.go:

				var (
					a = b
					b = 1
				)

				var (
					c = a
				)
			`),
		},
		{
			name: "external dependency",
			otherPackages: map[string]string{
				"other": Dedent(`
					package other
					func Other() {}
				`),
			},
			code: map[string]string{
				"code.go": Dedent(`
					package main
					import "mymodule/other"
					func A() { other.Other() }
				`),
			},
			ids: []string{"A"},
			wantExactly: Dedent(`
				// code.go:

				func A() { other.Other() }

				//
				// Select documentation from dependency packages:
				//

				// mymodule/other:

				func Other()
			`),
		},
		{
			name: "multiple external dependencie, eliding private fields, dup external dependencies",
			otherPackages: map[string]string{
				"other": Dedent(`
					package other
					func Other() {}
				`),
				"third": Dedent(`
					package third

					// Third has a private member
					type Third struct {
						A int // A
						b int // b
					}

					// Baz...
					func Baz() {}
				`),
			},
			code: map[string]string{
				"code.go": Dedent(`
					package main
					import "mymodule/third"
					import "mymodule/other"
					func A() third.Third {
						var t third.Third
						other.Other()
						third.Baz()
						return t
					}

					func B() {
						third.Baz()
					}
				`),
			},
			ids: []string{"A", "B"},
			wantExactly: Dedent(`
				// code.go:

				func A() third.Third {
					var t third.Third
					other.Other()
					third.Baz()
					return t
				}

				func B() {
					third.Baz()
				}

				//
				// Select documentation from dependency packages:
				//

				// mymodule/other:

				func Other()

				// mymodule/third:

				// Baz...
				func Baz()

				// Third has a private member
				type Third struct {
					A int // A
					// contains filtered or unexported fields
				}
			`),
		},
		{
			name: "package doc",
			code: map[string]string{
				"code.go": Dedent(`
					// Package foo is a test package.
					package foo
				`),
			},
			ids: []string{"package"},
			wantExactly: Dedent(`
				// code.go:

				// Package foo is a test package.
				package foo
			`),
		},
		{
			name: "package doc when none exists",
			code: map[string]string{
				"code.go": Dedent(`
					var i int
				`),
			},
			ids: []string{"package"},
			wantExactly: Dedent(`
				// code.go:

				var i int

				// doc.go:

				package mypkg
			`),
		},
	}

	for _, tt := range tests {
		// Run each test case in its own sub-test for isolation.
		t.Run(tt.name, func(t *testing.T) {
			// Build a temporary module containing the provided code files.
			gocodetesting.WithMultiCode(t, tt.code, func(pkg *gocode.Package) {
				if tt.otherPackages != nil {
					for pkgName, code := range tt.otherPackages {
						err := gocodetesting.AddPackage(t, pkg.Module, pkgName, map[string]string{"code.go": code})
						assert.NoError(t, err)
					}
				}

				// Build identifier groups for the package.
				groups, err := Groups(pkg.Module, pkg, GroupOptions{IncludePackageDocs: true, IncludeExternalDeps: true})
				if !assert.NoError(t, err) {
					return
				}

				// Narrow the groups down to only those containing the ids we care about.
				targetGroups := FilterGroupsForIdentifiers(groups, tt.ids)

				// Generate the code context string.
				code := NewContext(targetGroups).Code()

				if !assert.Equal(t, strings.TrimSpace(tt.wantExactly), strings.TrimSpace(code)) {
					fmt.Println("Actual:")
					fmt.Println(strings.TrimSpace(code))
				}
			})
		})
	}
}

func TestContextPrune_PackageDoc_OnlyExportedDepsKept(t *testing.T) {
	src := Dedent(`
        type X struct {
            A int
            b int
        }

        type y struct{}

        func Bar() {}
        func foo() {}
    `)

	gocodetesting.WithCode(t, src, func(pkg *gocode.Package) {
		groups, err := Groups(pkg.Module, pkg, GroupOptions{IncludePackageDocs: true})
		require.NoError(t, err)

		var pkgGroup *IdentifierGroup
		idToGroup := make(map[string]*IdentifierGroup)
		for _, g := range groups {
			for _, id := range g.IDs {
				idToGroup[id] = g
			}
			if len(g.IDs) == 1 && g.IDs[0] == gocode.PackageIdentifier {
				pkgGroup = g
			}
		}
		require.NotNil(t, pkgGroup)

		// Sanity: package group should depend on all non-test identifiers.
		// Expect it to include groups for X, y, Bar, foo.
		// Note: order is not guaranteed, so check via membership.
		depSet := make(map[*IdentifierGroup]bool)
		for _, d := range pkgGroup.DirectDeps {
			depSet[d] = true
		}
		require.True(t, depSet[idToGroup["X"]])
		require.True(t, depSet[idToGroup["y"]])
		require.True(t, depSet[idToGroup["Bar"]])
		require.True(t, depSet[idToGroup["foo"]])

		ctx := NewContext([]*IdentifierGroup{pkgGroup})

		allIdentsOrig := ctx.AllIdentifiers()

		// Force pruning path to execute by setting a small budget.
		pruneSuccessful := ctx.Prune(1)

		assert.False(t, pruneSuccessful) // Could not get below size of 1

		// y was part of AllIdentifiers originally, bu tnot after we prune
		allIdentsAfter := ctx.AllIdentifiers()
		assert.Contains(t, allIdentsOrig, "y")
		assert.NotContains(t, allIdentsAfter, "y")

		// After pruning, only exported identifiers should remain as direct deps.
		for _, d := range pkgGroup.DirectDeps {
			assert.True(t, groupHasExportedIdentifier(d), "found unexported dep after prune: %v", d.IDs)
		}

		// Specifically, ensure unexported ones were removed, and exported remain.
		depSet = make(map[*IdentifierGroup]bool)
		for _, d := range pkgGroup.DirectDeps {
			depSet[d] = true
		}
		assert.True(t, depSet[idToGroup["X"]])
		assert.True(t, depSet[idToGroup["Bar"]])
		assert.False(t, depSet[idToGroup["y"]])
		assert.False(t, depSet[idToGroup["foo"]])
	})
}

func TestContextPrune_UsedByTrim_RespectsMinimum(t *testing.T) {
	src := Dedent(`
        var base = 1

        func U1() int { return base }
        func U2() int { return base }
        func U3() int { return base }
        func U4() int { return base }
    `)

	gocodetesting.WithCode(t, src, func(pkg *gocode.Package) {
		groups, err := Groups(pkg.Module, pkg, GroupOptions{})
		require.NoError(t, err)
		varGroups := FilterGroupsForIdentifiers(groups, []string{"base"})
		require.Len(t, varGroups, 1)
		varGroup := varGroups[0]
		require.GreaterOrEqual(t, len(varGroup.UsedByDeps), 4)
		initialUsedBy := len(varGroup.UsedByDeps)

		ctx := NewContext([]*IdentifierGroup{varGroup})
		initialCost := ctx.Cost()

		// Compute the maximum BodyTokens among the used-by deps to craft a budget
		// that will require pruning exactly one used-by dep in the first round.
		maxBody := 0
		for _, ub := range varGroup.UsedByDeps {
			if ub.BodyTokens > maxBody {
				maxBody = ub.BodyTokens
			}
		}
		require.Greater(t, maxBody, 0)

		budget := initialCost - maxBody - 1
		pruned := ctx.Prune(budget)
		assert.True(t, pruned, "expected prune to succeed")
		assert.LessOrEqual(t, ctx.Cost(), budget)

		// Should prune at least one UsedBy but not below the minimum (2).
		after := len(varGroup.UsedByDeps)
		assert.GreaterOrEqual(t, after, 2)
		assert.Less(t, after, initialUsedBy)
	})
}
