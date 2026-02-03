package initialcontext

import (
	"fmt"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/gocode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreate_CodeAITools(t *testing.T) {
	// NOTE: normally I'd have a "Reflexive" test here that tests this package. But in this case, since the context runs the tests
	// for the package, we'd create an infinite loop. So, we pick another package.

	mod, err := gocode.NewModule(gocode.MustCwd())
	require.NoError(t, err)

	pkg, err := mod.LoadPackageByRelativeDir("internal/tools/coretools")
	require.NoError(t, err)

	got, err := Create(mod.AbsolutePath, pkg, false)
	require.NoError(t, err)

	// fmt.Println(got)

	assert.Contains(t, got, "<current-package>")
	assert.Contains(t, got, fmt.Sprintf("Package import path: %q", pkg.ImportPath))
	assert.Contains(t, got, "<pkg-map type=\"non-tests\">")
	assert.Contains(t, got, "// apply_patch.go:")
	assert.Contains(t, got, "<pkg-map type=\"tests\">")
	assert.Contains(t, got, "elided;")
	assert.Contains(t, got, "<used-by>")
	assert.Contains(t, got, "<diagnostics-status")
	assert.Contains(t, got, "<lint-status")
	assert.Contains(t, got, "<test-status")
	assert.NotContains(t, got, "tests not run; infinite recursion detected in initialcontext")
	assert.Contains(t, got, fmt.Sprintf("relative to the sandbox dir (%s)", mod.AbsolutePath))
}

func TestCreate_SkipAllChecks(t *testing.T) {
	mod, err := gocode.NewModule(gocode.MustCwd())
	require.NoError(t, err)

	pkg, err := mod.LoadPackageByRelativeDir("internal/tools/coretools")
	require.NoError(t, err)

	got, err := Create(mod.AbsolutePath, pkg, true)
	require.NoError(t, err)

	assert.Contains(t, got, "<diagnostics-status")
	assert.Contains(t, got, "diagnostics not run; deliberately skipped")
	assert.Contains(t, got, "<test-status")
	assert.Contains(t, got, "tests not run; deliberately skipped")
	assert.Contains(t, got, "<lint-status")
	assert.Contains(t, got, "lints not run; deliberately skipped")
}

func TestCreate_SkipTestsInRecursion(t *testing.T) {
	mod, err := gocode.NewModule(gocode.MustCwd())
	require.NoError(t, err)

	pkg, err := mod.LoadPackageByRelativeDir("internal/initialcontext")
	require.NoError(t, err)

	got, err := Create(mod.AbsolutePath, pkg, false)
	require.NoError(t, err)

	assert.Contains(t, got, "$ go test ./internal/initialcontext")
	assert.Contains(t, got, "tests not run; infinite recursion detected in initialcontext")
}

func TestLimitTestPkgMap_WithinLimit(t *testing.T) {
	sections := []string{`// foo_test.go:
package foo
import "testing"
func TestFoo(t *testing.T)`}

	got := limitTestPkgMap(sections, 25)
	assert.Equal(t, sections[0], got)
}

func TestLimitTestPkgMap_Elided(t *testing.T) {
	var decls []string
	for i := 0; i < 30; i++ {
		decls = append(decls, fmt.Sprintf("func TestThing%d(t *testing.T)", i))
	}

	section := "// foo_test.go:\npackage foo\n" + strings.Join(decls, "\n")
	got := limitTestPkgMap([]string{section}, 25)

	want := fmt.Sprintf("elided; limited to %d lines for tests", 25)
	assert.Equal(t, want, got)
}
