package gocode

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPackageWithSingleFile(t *testing.T, fileName, src string) *Package {
	t.Helper()
	pkg, _ := newTestPackageFromFiles(t, map[string]string{fileName: src})
	return pkg
}

func requireUnattachedComment(t *testing.T, pkg *Package, fileName, comment string) *UnattachedComment {
	t.Helper()
	for _, u := range pkg.UnattachedComments {
		if u.FileName == fileName && u.Comment == comment {
			return u
		}
	}
	require.Failf(t, "unattached comment not found", "%s in %s", comment, fileName)
	return nil
}

func TestUnattachedComments_SimpleAndNextLink(t *testing.T) {
	src := dedent(`
        package foo

        // unattached header

        // ConstX is documented
        const ConstX = 1

        // unattached middle

        // TypeY is documented
        type TypeY struct{ A int }
    `)

	fileName := "a.go"
	pkg := newPackageWithSingleFile(t, fileName, src)

	for _, u := range pkg.UnattachedComments {
		assert.False(t, u.AbovePackage)
	}

	header := requireUnattachedComment(t, pkg, fileName, "// unattached header\n")
	require.NotNil(t, header.Next)
	assert.Contains(t, header.Next.IDs(), "ConstX")

	middle := requireUnattachedComment(t, pkg, fileName, "// unattached middle\n")
	require.NotNil(t, middle.Next)
	assert.Contains(t, middle.Next.IDs(), "TypeY")
}

func TestUnattachedComments_NotInsideDecls(t *testing.T) {
	// Comments that are part of decls (block doc/spec comments and field comments) must not be counted.
	src := dedent(`
        package foo

        // Block doc for consts
        const (
			// Not unattached

            // spec doc
            A = 1 // eol comment
        )

        // Block doc for types
        type (
			// Not unattached
            
			// spec doc
            T struct {
				// Not unattached

                // field doc
                X int // field eol

				// Not unattached
            }
			// Not unattached
        )
    `)

	pkg := newPackageWithSingleFile(t, "b.go", src)
	assert.Len(t, pkg.UnattachedComments, 0)
}

func TestUnattachedComments_AbovePackage_NoPackageDoc(t *testing.T) {
	src := dedent(`
        // unattached top

        package foo

        const K = 1
    `)

	fileName := "p1.go"
	pkg := newPackageWithSingleFile(t, fileName, src)

	ua := requireUnattachedComment(t, pkg, fileName, "// unattached top\n")
	require.NotNil(t, ua.Next)
	assert.Contains(t, ua.Next.IDs(), "K")
	assert.True(t, ua.AbovePackage)
}

func TestUnattachedComments_AbovePackage_WithPackageDoc(t *testing.T) {
	src := dedent(`
        // unattached top

        // Package foo docs
        package foo

        type T struct{}
    `)

	fileName := "p2.go"
	pkg := newPackageWithSingleFile(t, fileName, src)

	ua := requireUnattachedComment(t, pkg, fileName, "// unattached top\n")
	require.NotNil(t, ua.Next)
	pds, ok := ua.Next.(*PackageDocSnippet)
	require.True(t, ok)
	assert.Equal(t, fileName, pds.FileName)
	assert.Equal(t, PackageIdentifierPerFile(fileName), pds.Identifier)
	assert.True(t, ua.AbovePackage)
}

func TestUnattachedComments_Trailing_NoNext(t *testing.T) {
	src := dedent(`
        package foo

        var A = 1

        // trailing unattached
    `)

	fileName := "p3.go"
	pkg := newPackageWithSingleFile(t, fileName, src)

	tail := requireUnattachedComment(t, pkg, fileName, "// trailing unattached\n")
	assert.Nil(t, tail.Next)
}
