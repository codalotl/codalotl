package gopackagediff

import (
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var dedent = gocodetesting.Dedent

func TestDiff(t *testing.T) {
	intPtr := func(i int) *int { return &i }
	boolPtr := func(b bool) *bool { return &b }

	type changeSpec struct {
		oldIDs          []string // Match a change whose OldIdentifiers exactly equal this set (order-insensitive). Leave empty to ignore.
		newIDs          []string // Match a change whose NewIdentifiers exactly equal this set (order-insensitive). Leave empty to ignore.
		kind            string   // Expected snippet kind: "func", "type", "value", or "package". Empty to ignore.
		idsChanged      *bool    // If set, assert c.IdentifiersChanged equals this value.
		codeChanged     *bool    // If set, assert (c.OldCode != c.NewCode) equals this value.
		oldCodeContains []string // Each string must be contained in c.OldCode. Ignored if empty.
		newCodeContains []string // Each string must be contained in c.NewCode. Ignored if empty.
	}

	type testCase struct {
		name            string            // Subtest name.
		origFiles       map[string]string // Original package files: fileName -> code.
		newFiles        map[string]string // New package files: fileName -> code.
		identifiers     []string          // If non-nil, limit Diff to these identifiers (union across pkgs).
		files           []string          // If non-nil, limit Diff to identifiers present in these files (by file name only).
		excludeFuncBody bool              // Pass-through to Diff: ignore function body changes when true.

		expectLen      *int         // If set, assert len(changes) equals this value.
		expectAdded    []string     // Identifiers expected to be additions (OldSnippet == nil). Compared against NewIdentifiers aggregated over added changes.
		expectDeleted  []string     // Identifiers expected to be deletions (NewSnippet == nil). Compared against OldIdentifiers aggregated over deleted changes.
		expectModified []string     // Identifiers expected to have code changes among matched pairs. Subset match against aggregated ids from modified changes.
		expectSpecs    []changeSpec // Order irrelevant

		assertFn func(t *testing.T, changes []*Change) // Optional custom assertions, run last.
	}

	cases := []testCase{
		{
			name: "basic documentation change",
			origFiles: map[string]string{
				"a.go": dedent(`
					// Foo...
					func Foo() {}

					// Bar...
					func Bar() {}
				`),
			},
			newFiles: map[string]string{
				"a.go": dedent(`
					// Foo does...
					func Foo() {}

					// Bar...
					func Bar() {}
				`),
			},
			expectLen: intPtr(1),
			expectSpecs: []changeSpec{{
				newIDs:          []string{"Foo"},
				oldCodeContains: []string{"// Foo..."},
				newCodeContains: []string{"// Foo does..."},
			}},
		},
		{
			name: "no changes yields no diffs",
			origFiles: map[string]string{
				"a.go": "package mypkg\n\n// Foo returns 1.\nfunc Foo() int { return 1 }\n",
			},
			newFiles: map[string]string{
				"a.go": "package mypkg\n\n// Foo returns 1.\nfunc Foo() int { return 1 }\n",
			},
			expectLen: intPtr(0),
		},
		{
			name: "no changes yields no diffs even if file changes",
			origFiles: map[string]string{
				"a.go": "package mypkg\n\n// Foo returns 1.\nfunc Foo() int { return 1 }\n",
			},
			newFiles: map[string]string{
				"b.go": "package mypkg\n\n// Foo returns 1.\nfunc Foo() int { return 1 }\n",
			},
			expectLen: intPtr(0),
		},
		{
			name: "no changes even if reorder",
			origFiles: map[string]string{
				"a.go": "package mypkg\n\nfunc Foo() int { return 1 }\nfunc Bar() int { return 1 }\n",
			},
			newFiles: map[string]string{
				"a.go": "package mypkg\n\nfunc Bar() int { return 1 }\nfunc Foo() int { return 1 }\n",
			},
			expectLen: intPtr(0),
		},
		{
			name: "added function produces addition change",
			origFiles: map[string]string{
				"a.go": "",
			},
			newFiles: map[string]string{
				"a.go": "// Foo returns 1.\nfunc Foo() int { return 1 }\n",
			},
			expectLen:   intPtr(1),
			expectAdded: []string{"Foo"},
		},
		{
			name: "deleted var produces deletion change",
			origFiles: map[string]string{
				"a.go": "var X = 1\n",
			},
			newFiles: map[string]string{
				"a.go": "",
			},
			expectLen:     intPtr(1),
			expectDeleted: []string{"X"},
		},
		{
			name:            "func body change ignored when excludeFuncBody=true",
			origFiles:       map[string]string{"a.go": "func Foo() int { return 1 }\n"},
			newFiles:        map[string]string{"a.go": "func Foo() int { return 2 }\n"},
			excludeFuncBody: true,
			expectLen:       intPtr(0),
		},
		{
			name:            "func body change detected when excludeFuncBody=false",
			origFiles:       map[string]string{"a.go": "func Foo() int { return 1 }\n"},
			newFiles:        map[string]string{"a.go": "func Foo() int { return 2 }\n"},
			excludeFuncBody: false,
			expectLen:       intPtr(1),
			expectModified:  []string{"Foo"},
			expectSpecs: []changeSpec{{
				oldIDs:      []string{"Foo"},
				newIDs:      []string{"Foo"},
				kind:        "func",
				idsChanged:  boolPtr(false),
				codeChanged: boolPtr(true),
			}},
		},
		{
			name:           "signature change detected regardless of excludeFuncBody",
			origFiles:      map[string]string{"a.go": "func Foo(a int) int { return a }\n"},
			newFiles:       map[string]string{"a.go": "func Foo(a string) int { return 0 }\n"},
			expectLen:      intPtr(1),
			expectModified: []string{"Foo"},
		},
		{
			name:      "file filter restricts diffs to listed files",
			origFiles: map[string]string{"a.go": "func Foo() {}\n", "b.go": "func Bar() {}\n"},
			newFiles:  map[string]string{"a.go": "func Foo() {}\n", "b.go": "// Bar changed\nfunc Bar() {}\n"},
			files:     []string{"a.go"},
			expectLen: intPtr(0),
		},
		{
			name:        "identifier filter restricts diffs to listed identifiers",
			origFiles:   map[string]string{"a.go": "func Foo() {}\nfunc Bar() {}\n"},
			newFiles:    map[string]string{"a.go": "func Foo() {}\n// Bar changed\nfunc Bar() {}\n"},
			identifiers: []string{"Foo"},
			expectLen:   intPtr(0),
		},
		{
			name: "splitting a const block",
			origFiles: map[string]string{
				"a.go": dedent(`
					const (
						A int = 1
						B int = 2
					)
				`),
			},
			newFiles: map[string]string{
				"a.go": dedent(`
					const A int = 1
					const B int = 2
				`),
			},
			expectLen: intPtr(2),
			expectSpecs: []changeSpec{{
				oldIDs:      []string{"A", "B"},
				kind:        "value",
				idsChanged:  boolPtr(true),
				codeChanged: boolPtr(true),
			}},
			assertFn: func(t *testing.T, changes []*Change) {
				// One matched modification (block -> single) and one pure addition.
				var addIds []string
				var modOldIDs [][]string
				var modNewIDs [][]string
				for _, c := range changes {
					if c.OldSnippet == nil && c.NewSnippet != nil {
						addIds = append(addIds, c.NewIdentifiers...)
					}
					if c.OldSnippet != nil && c.NewSnippet != nil {
						modOldIDs = append(modOldIDs, c.OldIdentifiers)
						modNewIDs = append(modNewIDs, c.NewIdentifiers)
					}
				}
				require.Len(t, addIds, 1)
				require.True(t, addIds[0] == "A" || addIds[0] == "B")
				require.Len(t, modOldIDs, 1)
				require.ElementsMatch(t, []string{"A", "B"}, modOldIDs[0])
				require.Len(t, modNewIDs, 1)
				require.Len(t, modNewIDs[0], 1)
				require.True(t, modNewIDs[0][0] == "A" || modNewIDs[0][0] == "B")
				require.NotEqual(t, addIds[0], modNewIDs[0][0])
			},
		},
		{
			name: "combining a const block. Also different file for no reason",
			origFiles: map[string]string{
				"a.go": dedent(`
					const A int = 1
					const B int = 2 
				`),
			},
			newFiles: map[string]string{
				"b.go": dedent(`
					const (
						A int = 1
						B int = 2
					)
				`),
			},
			expectLen: intPtr(2),
			expectSpecs: []changeSpec{{
				newIDs:      []string{"A", "B"},
				kind:        "value",
				idsChanged:  boolPtr(true),
				codeChanged: boolPtr(true),
			}},
			assertFn: func(t *testing.T, changes []*Change) {
				// Expect exactly one deletion (the single not chosen for the match) and one matched modification.
				var delIds []string
				var modOld []string
				for _, c := range changes {
					if c.NewSnippet == nil && c.OldSnippet != nil {
						delIds = append(delIds, c.OldIdentifiers...)
					}
					if c.NewSnippet != nil && c.OldSnippet != nil && snippetKind(c.NewSnippet) == "value" && len(c.NewIdentifiers) == 2 {
						modOld = append(modOld, c.OldIdentifiers...)
					}
				}
				require.Len(t, delIds, 1)
				require.Len(t, modOld, 1)
				// delIds[0] must be A or B, and must not equal the one that was matched.
				require.True(t, delIds[0] == "A" || delIds[0] == "B")
				require.True(t, modOld[0] == "A" || modOld[0] == "B")
				require.NotEqual(t, delIds[0], modOld[0])
			},
		},
		{
			name: "merge two consts into big block",
			origFiles: map[string]string{
				"a.go": dedent(`
					const A int = 1
					const B int = 2 
					const (
						C int = 3
						D int = 4
					)
				`),
			},
			newFiles: map[string]string{
				"a.go": dedent(`
					const (
						A int = 1
						B int = 2
						C int = 3
						D int = 4
					)
				`),
			},
			expectLen:     intPtr(3),
			expectDeleted: []string{"A", "B"},
			expectSpecs: []changeSpec{{
				oldIDs:      []string{"C", "D"},
				newIDs:      []string{"A", "B", "C", "D"},
				kind:        "value",
				idsChanged:  boolPtr(true),
				codeChanged: boolPtr(true),
			}},
		},
		{
			name: "change func to var",
			origFiles: map[string]string{
				"a.go": dedent(`
					func f() {}
				`),
			},
			newFiles: map[string]string{
				"b.go": dedent(`
					var f = func() {}
				`),
			},
			expectLen:     intPtr(2),
			expectAdded:   []string{"f"},
			expectDeleted: []string{"f"},
			expectSpecs: []changeSpec{
				{oldIDs: []string{"f"}, kind: "func", idsChanged: boolPtr(true), codeChanged: boolPtr(true)},
				{newIDs: []string{"f"}, kind: "value", idsChanged: boolPtr(true), codeChanged: boolPtr(true)},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var oldPkg, newPkg *gocode.Package

			gocodetesting.WithMultiCode(t, tc.origFiles, func(p *gocode.Package) {
				oldPkg = p
			})
			require.NotNil(t, oldPkg)

			gocodetesting.WithMultiCode(t, tc.newFiles, func(p *gocode.Package) {
				newPkg = p
			})
			require.NotNil(t, newPkg)

			changes, err := Diff(oldPkg, newPkg, tc.identifiers, tc.files, tc.excludeFuncBody)
			require.NoError(t, err)

			for _, c := range changes {
				assert.NotEqual(t, c.OldCode, c.NewCode)
			}

			// High-leverage assertions:
			if tc.expectLen != nil {
				require.Len(t, changes, *tc.expectLen)
			}

			if len(tc.expectAdded) > 0 || len(tc.expectDeleted) > 0 || len(tc.expectModified) > 0 {
				var added, deleted, modified []string
				for _, c := range changes {
					if c.OldSnippet == nil {
						added = append(added, c.NewIdentifiers...)
						continue
					}
					if c.NewSnippet == nil {
						deleted = append(deleted, c.OldIdentifiers...)
						continue
					}
					if c.OldCode != c.NewCode {
						ids := c.NewIdentifiers
						if len(ids) == 0 {
							ids = c.OldIdentifiers
						}
						modified = append(modified, ids...)
					}
				}
				if len(tc.expectAdded) > 0 {
					require.ElementsMatch(t, tc.expectAdded, added)
				}
				if len(tc.expectDeleted) > 0 {
					require.ElementsMatch(t, tc.expectDeleted, deleted)
				}
				if len(tc.expectModified) > 0 {
					// Check that expected modified are included (there may be more depending on grouping).
					for _, id := range tc.expectModified {
						require.Contains(t, modified, id)
					}
				}
			}

			if len(tc.expectSpecs) > 0 {
				equalSets := func(a, b []string) bool {
					if len(a) != len(b) {
						return false
					}
					m := make(map[string]int)
					for _, s := range a {
						m[s]++
					}
					for _, s := range b {
						if m[s] == 0 {
							return false
						}
						m[s]--
					}
					for _, v := range m {
						if v != 0 {
							return false
						}
					}
					return true
				}
				containsAll := func(haystack string, needles []string) bool {
					for _, n := range needles {
						if n == "" {
							continue
						}
						if !strings.Contains(haystack, n) {
							return false
						}
					}
					return true
				}

				for _, spec := range tc.expectSpecs {
					found := false
					for _, c := range changes {
						if len(spec.oldIDs) > 0 && !equalSets(spec.oldIDs, c.OldIdentifiers) {
							continue
						}
						if len(spec.newIDs) > 0 && !equalSets(spec.newIDs, c.NewIdentifiers) {
							continue
						}
						// Candidate match found; assert remaining properties.
						if spec.kind != "" {
							if c.OldSnippet != nil {
								require.Equal(t, spec.kind, snippetKind(c.OldSnippet))
							} else if c.NewSnippet != nil {
								require.Equal(t, spec.kind, snippetKind(c.NewSnippet))
							}
						}
						if spec.idsChanged != nil {
							require.Equal(t, *spec.idsChanged, c.IdentifiersChanged)
						}
						if spec.codeChanged != nil {
							require.Equal(t, *spec.codeChanged, c.OldCode != c.NewCode)
						}
						require.True(t, containsAll(c.OldCode, spec.oldCodeContains))
						require.True(t, containsAll(c.NewCode, spec.newCodeContains))
						found = true
						break
					}
					require.True(t, found, "expected change not found: %+v", spec)
				}
			}

			// Custom assertion hook (optional)
			if tc.assertFn != nil {
				tc.assertFn(t, changes)
			}
		})
	}
}
