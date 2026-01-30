package gocode

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUnattachedComments_SimpleAndNextLink(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gocode-unattached-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// File layout:
	// 1) An unattached header comment
	// 2) A const with its own comment (attached)
	// 3) A blank-line-separated unattached comment
	// 4) A type with its own comment (attached) following the unattached
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
	assert.NoError(t, os.WriteFile(filepath.Join(tempDir, fileName), []byte(src), 0644))

	module := &Module{
		Name:         "testmodule",
		AbsolutePath: tempDir,
		Packages:     make(map[string]*Package),
	}

	pkg, err := NewPackage("", tempDir, []string{fileName}, module)
	assert.NoError(t, err)

	// Expect two unattached comments: header and middle
	// Their Next should be the next declaration snippet in file order
	if assert.GreaterOrEqual(t, len(pkg.UnattachedComments), 2) {
		// Identify which is header vs middle by textual content
		var header, middle *UnattachedComment
		for _, u := range pkg.UnattachedComments {
			assert.False(t, u.AbovePackage)
			if u.Comment == "// unattached header\n" {
				header = u
			}
			if u.Comment == "// unattached middle\n" {
				middle = u
			}
		}

		if assert.NotNil(t, header, "header unattached comment not found") {
			// Next should be the const snippet (ConstX)
			if assert.NotNil(t, header.Next, "header.Next should not be nil") {
				ids := header.Next.IDs()
				assert.Contains(t, ids, "ConstX")
			}
		}

		if assert.NotNil(t, middle, "middle unattached comment not found") {
			// Next should be the type snippet (TypeY)
			if assert.NotNil(t, middle.Next, "middle.Next should not be nil") {
				ids := middle.Next.IDs()
				assert.Contains(t, ids, "TypeY")
			}
		}
	}
}

func TestUnattachedComments_NotInsideDecls(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gocode-unattached-test2")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

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

	fileName := "b.go"
	assert.NoError(t, os.WriteFile(filepath.Join(tempDir, fileName), []byte(src), 0644))

	module := &Module{
		Name:         "testmodule",
		AbsolutePath: tempDir,
		Packages:     make(map[string]*Package),
	}

	pkg, err := NewPackage("", tempDir, []string{fileName}, module)
	assert.NoError(t, err)

	// All comments are attached to declarations; expect zero unattached
	assert.Len(t, pkg.UnattachedComments, 0)
}

func TestUnattachedComments_AbovePackage_NoPackageDoc(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gocode-unattached-abovepkg-nodoc")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	src := dedent(`
        // unattached top

        package foo

        const K = 1
    `)

	fileName := "p1.go"
	assert.NoError(t, os.WriteFile(filepath.Join(tempDir, fileName), []byte(src), 0644))

	module := &Module{Name: "testmodule", AbsolutePath: tempDir, Packages: make(map[string]*Package)}
	pkg, err := NewPackage("", tempDir, []string{fileName}, module)
	assert.NoError(t, err)

	// Find the unattached top comment
	var ua *UnattachedComment
	for _, u := range pkg.UnattachedComments {
		if u.Comment == "// unattached top\n" && u.FileName == fileName {
			ua = u
			break
		}
	}
	if assert.NotNil(t, ua) {
		// Next should be first declaration (Const K)
		if assert.NotNil(t, ua.Next) {
			ids := ua.Next.IDs()
			assert.Contains(t, ids, "K")
		}
		assert.True(t, ua.AbovePackage)
	}
}

func TestUnattachedComments_AbovePackage_WithPackageDoc(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gocode-unattached-abovepkg-doc")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	src := dedent(`
        // unattached top

        // Package foo docs
        package foo

        type T struct{}
    `)

	fileName := "p2.go"
	assert.NoError(t, os.WriteFile(filepath.Join(tempDir, fileName), []byte(src), 0644))

	module := &Module{Name: "testmodule", AbsolutePath: tempDir, Packages: make(map[string]*Package)}
	pkg, err := NewPackage("", tempDir, []string{fileName}, module)
	assert.NoError(t, err)

	// Expect an unattached top comment whose Next is the package doc snippet
	var ua *UnattachedComment
	for _, u := range pkg.UnattachedComments {
		if u.Comment == "// unattached top\n" && u.FileName == fileName {
			ua = u
			break
		}
	}
	if assert.NotNil(t, ua) {
		if assert.NotNil(t, ua.Next) {
			// Next should be the PackageDocSnippet for this file
			if pds, ok := ua.Next.(*PackageDocSnippet); assert.True(t, ok) {
				assert.Equal(t, fileName, pds.FileName)
				assert.Equal(t, PackageIdentifierPerFile(fileName), pds.Identifier)
			}
		}
		assert.True(t, ua.AbovePackage)
	}
}

func TestUnattachedComments_Trailing_NoNext(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gocode-unattached-trailing")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	src := dedent(`
        package foo

        var A = 1

        // trailing unattached
    `)

	fileName := "p3.go"
	assert.NoError(t, os.WriteFile(filepath.Join(tempDir, fileName), []byte(src), 0644))

	module := &Module{Name: "testmodule", AbsolutePath: tempDir, Packages: make(map[string]*Package)}
	pkg, err := NewPackage("", tempDir, []string{fileName}, module)
	assert.NoError(t, err)

	var tail *UnattachedComment
	for _, u := range pkg.UnattachedComments {
		if u.Comment == "// trailing unattached\n" && u.FileName == fileName {
			tail = u
			break
		}
	}
	if assert.NotNil(t, tail) {
		assert.Nil(t, tail.Next)
	}
}
