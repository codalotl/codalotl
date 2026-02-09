# initialcontext

This package creates an initial bundle of information for an LLM starting to work on a single Go package:
- Package information (paths, import)
- All files/dirs in the package's directory.
- All package-level identifiers (ex: vars/consts/funcs/types), their signatures, and imports, but without comments (for non-tests).
- If the tests are "sufficiently small", the same is provided for test files. Otherwise, a note is inserted into the returned context indicating tests were elided.
- A list of all packages that import your package.
- Current state of build errors, tests, and lints.

Optionally, the caller can disable all checks (diagnostics/tests/lints). In that mode, this package does not run any of those
commands (or the used-by lookup); it emits the corresponding status blocks with a "not run" message.

## Dependencies and Correctness Delegation

This package uses:
- `internal/tools/coretools` for `ls`
- `internal/tools/exttools` for `diagnostics-status` and `test-status` (ex: it calls `exttools.RunDiagnostics`)
- `internal/lints` for `lint-status` (it calls `lints.Run` in `check` mode)

The exact formatting of `<diagnostics-status>` / `<test-status>` / `<lint-status>` is governed by those helper packages' intended
behavior, even if it differs slightly from this spec.

The `pkg-map` section is mostly built by the `internal/gocodecontext`'s `InternalPackageSignatures` func (the only comments are file markers).

Commands are typically run with `q/cmdrunner`:
- `ok="true"` generally means successful execution, 0 exit code, no issues found.
- `ok="false"` generally means any of: bad execution, a non-zero exit code, or issues/lints were found.

## Example Context

```txt
<current-package>
Module path: "/abs/path/to/somemodule"
Package relative path: "some/pkg"
Absolute package path: "/abs/path/to/somemodule/some/pkg"
Package import path: "somemodule/some/pkg"
</current-package>

<ls ok="true">
$ ls -1p
SPEC.md
bar/
bar.go
foo/
foo.go
foo_test.go
</ls>

<pkg-map type="non-tests">
// foo.go:
package mypkg
func foo(i int) string

// bar.go:
package mypkg
import "fmt"
var helper = foo(1)
type Widget struct {
    Name string
}
func bar()
</pkg-map>

<pkg-map type="tests">
// foo_test.go:
package mypkg
import "testing"
func TestFoo(t *testing.T)
</pkg-map>

<used-by>
somemodule/another/pkgc
somemodule/other/pkga
somemodule/other/pkgb
</used-by>

<diagnostics-status ok="true">
$ go build -o /dev/null ./some/pkg
</diagnostics-status>

<test-status ok="true">
$ go test ./some/pkg
ok      somemodule/some/pkg        (cached)
</test-status>

<lint-status ok="false">
<command ok="true" mode="check" message="no issues found">
$ gofmt -l ./some/pkg
</command>
<command ok="false" mode="check">
$ codalotl docs reflow --check --width=120 ./some/pkg
some/pkg/foo.go
</command>
</lint-status>
```

## Notable Private Interface Funcs

```go {api exact_docs}
// usedBy returns a slice of sorted packages (their import paths) that use this package within this module.
func usedBy(pkg *gocode.Package) ([]string, error)
```

## Public Interface

```go {api exact_docs}
// Create returns an initial bundle of information for an LLM starting to work on a single Go package:
//   - Package information (paths, import)
//   - All files/dirs in the package's directory.
//   - All package-level identifiers (ex: vars/consts/funcs/types), their signatures, and imports, but without comments.
//   - A list of all packages that import your package.
//   - Current state of build errors, tests, and lints.
//
// If skipAllChecks is true, this function does not run diagnostics, tests, lints, or used-by sections. Instead, it
// emits the corresponding status blocks with a "not run" message.
//
// lintSteps controls which lints are run. If lintSteps is nil, lints.DefaultSteps() is used.
func Create(pkg *gocode.Package, lintSteps []lints.Step, skipAllChecks bool) (string, error)
```
