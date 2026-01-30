package reorgbot

import (
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyReorg_PreserveUnattachedAbovePackage_NoPkgDoc(t *testing.T) {
	// File has an unattached comment above the package clause. After reorg, the
	// comment should be preserved and emitted before the first snippet that follows it.
	gocodetesting.WithMultiCode(t, map[string]string{
		"a.go": gocodetesting.Dedent(`
            // unattached top

            package mypkg

            // Foo is documented
            type Foo int
        `),
	}, func(pkg *gocode.Package) {
		// Build snippet map
		ids := pkg.Identifiers(true)
		_, idToSnippet := codeContextForPackage(pkg, ids, false)

		// Find Foo id
		var fooID string
		for id, sn := range idToSnippet {
			if ts, ok := sn.(*gocode.TypeSnippet); ok {
				if len(ts.Identifiers) == 1 && ts.Identifiers[0] == "Foo" {
					fooID = id
					break
				}
			}
		}
		require.NotEmpty(t, fooID)

		// Organize Foo into reorg.go
		org := map[string][]string{"reorg.go": {fooID}}
		require.NoError(t, applyReorganization(pkg, org, idToSnippet, false))

		// Reload and assert content
		pkg, _ = pkg.Module.ReadPackage(pkg.RelativeDir, nil)
		got := string(pkg.Files["reorg.go"].Contents)
		want := gocodetesting.Dedent(`
            package mypkg

            // unattached top

            // Foo is documented
            type Foo int
        `)
		assertSameSource(t, want, got)
	})
}

func TestApplyReorg_UnattachedAbovePackage_WithPackageDoc(t *testing.T) {
	// File has an unattached comment above a proper package doc. The unattached comment
	// should remain above the package doc snippet in the destination file.
	gocodetesting.WithMultiCode(t, map[string]string{
		"f.go": gocodetesting.Dedent(`
            // unattached top

            // Package mypkg docs
            package mypkg

            type Q int
        `),
	}, func(pkg *gocode.Package) {
		ids := pkg.Identifiers(true)
		_, idToSnippet := codeContextForPackage(pkg, ids, false)

		// Find the package doc snippet id for f.go and Q type id
		var pkgDocID string
		for id, sn := range idToSnippet {
			if pds, ok := sn.(*gocode.PackageDocSnippet); ok {
				if pds.FileName == "f.go" {
					pkgDocID = id
					break
				}
			}
		}
		require.NotEmpty(t, pkgDocID)

		org := map[string][]string{"reorg.go": {pkgDocID}}
		require.NoError(t, applyReorganization(pkg, org, idToSnippet, false))

		pkg, _ = pkg.Module.ReadPackage(pkg.RelativeDir, nil)
		got := string(pkg.Files["reorg.go"].Contents)
		// unattached should appear before the package doc comment
		posUA := strings.Index(got, "// unattached top")
		posPkgDoc := strings.Index(got, "// Package mypkg docs")
		require.NotEqual(t, -1, posUA)
		require.NotEqual(t, -1, posPkgDoc)
		assert.Less(t, posUA, posPkgDoc)
	})
}

func TestApplyReorg_CreateFileForOnlyUnattachedComments(t *testing.T) {
	// File contains only unattached comments (no decls). After reorg, the file should
	// be created with those comments.
	gocodetesting.WithMultiCode(t, map[string]string{
		"d.go": gocodetesting.Dedent(`
            // only unattached 1
            // only unattached 2

            package mypkg
        `),
		"e.go": gocodetesting.Dedent(`
            package mypkg

            // Baz is documented
            type Baz int
        `),
	}, func(pkg *gocode.Package) {
		ids := pkg.Identifiers(true)
		_, idToSnippet := codeContextForPackage(pkg, ids, false)

		// Organize only Baz
		var bazID string
		for id, sn := range idToSnippet {
			if ts, ok := sn.(*gocode.TypeSnippet); ok {
				if len(ts.Identifiers) == 1 && ts.Identifiers[0] == "Baz" {
					bazID = id
					break
				}
			}
		}
		require.NotEmpty(t, bazID)

		org := map[string][]string{"reorg.go": {bazID}}
		require.NoError(t, applyReorganization(pkg, org, idToSnippet, false))

		// Reload and assert d.go exists with its comments
		pkg, _ = pkg.Module.ReadPackage(pkg.RelativeDir, nil)
		_, ok := pkg.Files["d.go"]
		assert.True(t, ok, "d.go should exist after reorg")
		got := string(pkg.Files["d.go"].Contents)
		assert.Contains(t, got, "// only unattached 1")
		assert.Contains(t, got, "// only unattached 2")
	})
}

func TestApplyReorg_RelocateOrphanUnattached_ToOriginalFile(t *testing.T) {
	// File has a trailing unattached comment and no snippets included in newOrganization.
	// The unattached comment should be written back to the same filename.
	gocodetesting.WithMultiCode(t, map[string]string{
		"b.go": gocodetesting.Dedent(`
            package mypkg

            var A = 1

            // trailing unattached
        `),
		"c.go": gocodetesting.Dedent(`
            package mypkg

            // Bar is documented
            type Bar int
        `),
	}, func(pkg *gocode.Package) {
		ids := pkg.Identifiers(true)
		_, idToSnippet := codeContextForPackage(pkg, ids, false)

		// Pick Bar only to ensure b.go has no snippets assigned
		var barID string
		for id, sn := range idToSnippet {
			if ts, ok := sn.(*gocode.TypeSnippet); ok {
				if len(ts.Identifiers) == 1 && ts.Identifiers[0] == "Bar" {
					barID = id
					break
				}
			}
		}
		require.NotEmpty(t, barID)

		org := map[string][]string{"reorg.go": {barID}}
		require.NoError(t, applyReorganization(pkg, org, idToSnippet, false))

		// Reload and assert that b.go exists and contains only the package and the unattached comment
		pkg, _ = pkg.Module.ReadPackage(pkg.RelativeDir, nil)
		_, ok := pkg.Files["b.go"]
		assert.True(t, ok, "b.go should exist after reorg")
		got := string(pkg.Files["b.go"].Contents)
		// We expect package clause and the trailing unattached comment.
		assert.Contains(t, got, "package mypkg")
		assert.Contains(t, got, "// trailing unattached")
	})
}
