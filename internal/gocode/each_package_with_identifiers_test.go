package gocode

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type eachPackageCall struct {
	pkg       *Package
	ids       []string
	onlyTests bool
}

type eachPackageCallRecorder struct {
	calls []eachPackageCall
}

func (r *eachPackageCallRecorder) callback(pkg *Package, ids []string, onlyTests bool) error {
	r.calls = append(r.calls, eachPackageCall{
		pkg:       pkg,
		ids:       append([]string(nil), ids...),
		onlyTests: onlyTests,
	})
	return nil
}

func TestEachPackageWithIdentifiers_DefaultIdentifiersAndOrder(t *testing.T) {
	pkg, _ := newTestPackageFromFiles(t, map[string]string{
		"a.go": dedent(`
			package foo

			type TypeA struct{}

			const ConstA = 1

			func FuncA() {}
		`),
		"b_test.go": dedent(`
			package foo

			func helper() {}

			func TestSample(t *testing.T) {}
		`),
		"blackbox_test.go": dedent(`
			package foo_test

			func BbHelper() {}

			func TestBlackbox(t *testing.T) {}
		`),
	})
	require.NotNil(t, pkg.TestPackage)

	options := FilterIdentifiersOptions{}
	recorder := &eachPackageCallRecorder{}
	err := EachPackageWithIdentifiers(pkg, nil, options, options, recorder.callback)
	require.NoError(t, err)
	calls := recorder.calls

	// Expect 3 calls in order: primary(non-tests), primary(tests), test package
	if assert.Len(t, calls, 3) {
		// 1) primary, non-tests
		assert.Same(t, pkg, calls[0].pkg)
		assert.False(t, calls[0].onlyTests)
		assert.Subset(t, calls[0].ids, []string{"TypeA", "ConstA", "FuncA"})

		// 2) primary, tests (helper only; TestSample excluded by default)
		assert.Same(t, pkg, calls[1].pkg)
		assert.True(t, calls[1].onlyTests)
		assert.ElementsMatch(t, []string{"helper"}, calls[1].ids)

		// 3) black-box test package
		assert.Same(t, pkg.TestPackage, calls[2].pkg)
		assert.True(t, calls[2].onlyTests)
		assert.ElementsMatch(t, []string{"BbHelper"}, calls[2].ids)
	}
}

func TestEachPackageWithIdentifiers_WithProvidedIdentifiers(t *testing.T) {
	pkg, _ := newTestPackageFromFiles(t, map[string]string{
		"a.go": dedent(`
			package foo

			type TypeA struct{}
			func FuncA() {}
		`),
		"b_test.go": dedent(`
			package foo
			func helper() {}
		`),
		"blackbox_test.go": dedent(`
			package foo_test
			func BbHelper() {}
		`),
	})
	require.NotNil(t, pkg.TestPackage)

	recorder := &eachPackageCallRecorder{}
	input := []string{"FuncA", "helper", "BbHelper", "NonExistent"}
	err := EachPackageWithIdentifiers(pkg, input, FilterIdentifiersOptions{}, FilterIdentifiersOptionsAll, recorder.callback)
	require.NoError(t, err)
	calls := recorder.calls

	if assert.Len(t, calls, 3) {
		assert.Same(t, pkg, calls[0].pkg)
		assert.False(t, calls[0].onlyTests)
		assert.ElementsMatch(t, []string{"FuncA"}, calls[0].ids)

		assert.Same(t, pkg, calls[1].pkg)
		assert.True(t, calls[1].onlyTests)
		assert.ElementsMatch(t, []string{"helper"}, calls[1].ids)

		assert.Same(t, pkg.TestPackage, calls[2].pkg)
		assert.True(t, calls[2].onlyTests)
		assert.ElementsMatch(t, []string{"BbHelper"}, calls[2].ids)
	}
}

func TestEachPackageWithIdentifiers_CallbackErrorStopsIteration(t *testing.T) {
	pkg, _ := newTestPackageFromFiles(t, map[string]string{
		"a.go": dedent(`
			package foo
			func FuncA() {}
		`),
	})

	count := 0
	cb := func(p *Package, ids []string, onlyTests bool) error {
		count++
		return assert.AnError
	}

	err := EachPackageWithIdentifiers(pkg, nil, FilterIdentifiersOptions{}, FilterIdentifiersOptions{}, cb)
	assert.Error(t, err)
	assert.Equal(t, 1, count)
}
