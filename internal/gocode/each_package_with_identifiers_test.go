package gocode

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEachPackageWithIdentifiers_DefaultIdentifiersAndOrder(t *testing.T) {
	// Create a temporary directory and files
	tempDir, err := os.MkdirTemp("", "eachpkg-ids-default")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// a.go: main package, non-test declarations
	aGo := dedent(`
		package foo

		type TypeA struct{}

		const ConstA = 1

		func FuncA() {}
	`)
	// b_test.go: white-box tests in main package
	bTestGo := dedent(`
		package foo

		func helper() {}

		func TestSample(t *testing.T) {}
	`)
	// blackbox_test.go: black-box test package
	bbTestGo := dedent(`
		package foo_test

		func BbHelper() {}

		func TestBlackbox(t *testing.T) {}
	`)

	// Write files
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "a.go"), []byte(aGo), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "b_test.go"), []byte(bTestGo), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "blackbox_test.go"), []byte(bbTestGo), 0644))

	// Module and package
	module := &Module{Name: "testmodule", AbsolutePath: tempDir, Packages: make(map[string]*Package)}
	pkg, err := NewPackage("", tempDir, []string{"a.go", "b_test.go", "blackbox_test.go"}, module)
	require.NoError(t, err)
	require.NotNil(t, pkg)
	require.NotNil(t, pkg.TestPackage)

	// Capture callbacks
	type call struct {
		pkg   *Package
		ids   []string
		onlyT bool
	}
	var calls []call

	cb := func(p *Package, ids []string, onlyTests bool) error {
		idsCopy := append([]string(nil), ids...)
		calls = append(calls, call{pkg: p, ids: idsCopy, onlyT: onlyTests})
		return nil
	}

	// Invoke with empty identifiers and default options (exclude testing funcs)
	options := FilterIdentifiersOptions{}
	err = EachPackageWithIdentifiers(pkg, nil, options, options, cb)
	require.NoError(t, err)

	// Expect 3 calls in order: primary(non-tests), primary(tests), test package
	if assert.Len(t, calls, 3) {
		// 1) primary, non-tests
		assert.Same(t, pkg, calls[0].pkg)
		assert.False(t, calls[0].onlyT)
		assert.Subset(t, calls[0].ids, []string{"TypeA", "ConstA", "FuncA"})

		// 2) primary, tests (helper only; TestSample excluded by default)
		assert.Same(t, pkg, calls[1].pkg)
		assert.True(t, calls[1].onlyT)
		assert.ElementsMatch(t, []string{"helper"}, calls[1].ids)

		// 3) black-box test package
		assert.Same(t, pkg.TestPackage, calls[2].pkg)
		assert.True(t, calls[2].onlyT)
		assert.ElementsMatch(t, []string{"BbHelper"}, calls[2].ids)
	}
}

func TestEachPackageWithIdentifiers_WithProvidedIdentifiers(t *testing.T) {
	// Setup temp module and package
	tempDir, err := os.MkdirTemp("", "eachpkg-ids-provided")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	aGo := dedent(`
		package foo

		type TypeA struct{}
		func FuncA() {}
	`)
	bTestGo := dedent(`
		package foo
		func helper() {}
	`)
	bbTestGo := dedent(`
		package foo_test
		func BbHelper() {}
	`)

	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "a.go"), []byte(aGo), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "b_test.go"), []byte(bTestGo), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "blackbox_test.go"), []byte(bbTestGo), 0644))

	module := &Module{Name: "testmodule", AbsolutePath: tempDir, Packages: make(map[string]*Package)}
	pkg, err := NewPackage("", tempDir, []string{"a.go", "b_test.go", "blackbox_test.go"}, module)
	require.NoError(t, err)
	require.NotNil(t, pkg)
	require.NotNil(t, pkg.TestPackage)

	var calls []struct {
		p   *Package
		ids []string
		ot  bool
	}
	cb := func(p *Package, ids []string, onlyTests bool) error {
		idsCopy := append([]string(nil), ids...)
		calls = append(calls, struct {
			p   *Package
			ids []string
			ot  bool
		}{p, idsCopy, onlyTests})
		return nil
	}

	// Provide a mix of ids across main package, its tests, the black-box test package, and a bogus id
	input := []string{"FuncA", "helper", "BbHelper", "NonExistent"}
	err = EachPackageWithIdentifiers(pkg, input, FilterIdentifiersOptions{}, FilterIdentifiersOptionsAll, cb)
	require.NoError(t, err)

	if assert.Len(t, calls, 3) {
		assert.Same(t, pkg, calls[0].p)
		assert.False(t, calls[0].ot)
		assert.ElementsMatch(t, []string{"FuncA"}, calls[0].ids)

		assert.Same(t, pkg, calls[1].p)
		assert.True(t, calls[1].ot)
		assert.ElementsMatch(t, []string{"helper"}, calls[1].ids)

		assert.Same(t, pkg.TestPackage, calls[2].p)
		assert.True(t, calls[2].ot)
		assert.ElementsMatch(t, []string{"BbHelper"}, calls[2].ids)
	}
}

func TestEachPackageWithIdentifiers_CallbackErrorStopsIteration(t *testing.T) {
	// Setup minimal temp module and package
	tempDir, err := os.MkdirTemp("", "eachpkg-ids-error")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	src := dedent(`
		package foo
		func FuncA() {}
	`)
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "a.go"), []byte(src), 0644))
	module := &Module{Name: "testmodule", AbsolutePath: tempDir, Packages: make(map[string]*Package)}
	pkg, err := NewPackage("", tempDir, []string{"a.go"}, module)
	require.NoError(t, err)

	count := 0
	cb := func(p *Package, ids []string, onlyTests bool) error {
		count++
		return assert.AnError
	}

	err = EachPackageWithIdentifiers(pkg, nil, FilterIdentifiersOptions{}, FilterIdentifiersOptions{}, cb)
	assert.Error(t, err)
	assert.Equal(t, 1, count, "callback should be invoked once and then stop on error")
}
