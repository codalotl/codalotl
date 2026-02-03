package initialcontext

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodecontext"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"github.com/codalotl/codalotl/internal/tools/exttools"
)

const maxTestPkgMapLines = 25
const recursionEnvVar = "CODEAI_INITIALCONTEXT_ACTIVE_TESTS"

// Create returns an initial bundle of information for an LLM starting to work on a single Go package:
//   - Package information (paths, import)
//   - All files/dirs in the package's directory.
//   - All package-level identifiers (ex: vars/consts/funcs/types), their signatures, and imports, but without comments.
//   - A list of all packages that import your package.
//   - Current state of build errors, tests, and lints.
//
// If skipAllChecks is true, this function does not run diagnostics, tests, lints, or used-by
// lookups. Instead, it emits the corresponding status blocks with a "not run" message.
func Create(pkg *gocode.Package, skipAllChecks bool) (string, error) {
	if pkg == nil {
		return "", fmt.Errorf("nil package")
	}
	if pkg.Module == nil {
		return "", fmt.Errorf("package %q has no module", pkg.ImportPath)
	}

	ctx := context.Background()
	absPkgPath := pkg.AbsolutePath()
	moduleAbsPath := pkg.Module.AbsolutePath

	sections := []string{
		currentPackageSection(pkg),
	}

	lsOutput, err := coretools.RunLs(ctx, absPkgPath)
	if err != nil {
		return "", fmt.Errorf("list package directory: %w", err)
	}
	sections = append(sections, lsOutput)

	nonTestSignatures, err := gocodecontext.InternalPackageSignatures(pkg, false, false)
	if err != nil {
		return "", fmt.Errorf("render package signatures: %w", err)
	}
	sections = append(sections, formatSection("pkg-map", `type="non-tests"`, strings.TrimSpace(nonTestSignatures)))

	testSections := make([]string, 0, 2)

	testSignatures, err := gocodecontext.InternalPackageSignatures(pkg, true, false)
	if err != nil {
		return "", fmt.Errorf("render test signatures: %w", err)
	}
	if trimmed := strings.TrimSpace(testSignatures); trimmed != "" {
		testSections = append(testSections, trimmed)
	}

	if pkg.TestPackage != nil {
		externalTests, err := gocodecontext.InternalPackageSignatures(pkg.TestPackage, true, false)
		if err != nil {
			return "", fmt.Errorf("render external test signatures: %w", err)
		}
		if trimmed := strings.TrimSpace(externalTests); trimmed != "" {
			testSections = append(testSections, trimmed)
		}
	}

	testsContent := limitTestPkgMap(testSections, maxTestPkgMapLines)
	sections = append(sections, formatSection("pkg-map", `type="tests"`, testsContent))

	if skipAllChecks {
		sections = append(sections,
			skippedDiagnosticsStatus(pkg),
			skippedTestStatus(pkg),
			skippedLintStatus(pkg),
		)
	} else {
		usedByPackages, err := usedBy(pkg)
		if err != nil {
			return "", fmt.Errorf("resolve used-by packages: %w", err)
		}
		sections = append(sections, formatSection("used-by", "", strings.Join(usedByPackages, "\n")))

		diagnosticsOutput, err := exttools.RunDiagnostics(ctx, moduleAbsPath, absPkgPath)
		if err != nil {
			return "", fmt.Errorf("collect diagnostics: %w", err)
		}
		sections = append(sections, diagnosticsOutput)

		testOutput, err := runTestsWithRecursionGuard(ctx, pkg, moduleAbsPath, absPkgPath)
		if err != nil {
			return "", fmt.Errorf("collect test status: %w", err)
		}
		sections = append(sections, testOutput)

		lintOutput, err := exttools.CheckLints(ctx, moduleAbsPath, absPkgPath)
		if err != nil {
			return "", fmt.Errorf("collect lint status: %w", err)
		}
		sections = append(sections, lintOutput)
	}

	var filtered []string
	for _, section := range sections {
		if section == "" {
			continue
		}
		filtered = append(filtered, section)
	}

	return strings.Join(filtered, "\n\n"), nil
}

func currentPackageSection(pkg *gocode.Package) string {
	var b strings.Builder
	b.WriteString("<current-package>\n")
	fmt.Fprintf(&b, "Module path: %q\n", pkg.Module.AbsolutePath)
	fmt.Fprintf(&b, "Package relative path: %q\n", pkg.RelativeDir)
	fmt.Fprintf(&b, "Absolute package path: %q\n", pkg.AbsolutePath())
	fmt.Fprintf(&b, "Package import path: %q\n", pkg.ImportPath)
	b.WriteString("</current-package>")
	return b.String()
}

// usedBy returns a slice of sorted packages (their import paths) that use this package within this module.
func usedBy(pkg *gocode.Package) ([]string, error) {
	// NOTE: we could implement this in terms of:
	// go list -f '{{range .Imports}}{{if eq . "'"axi/codeai/tools/coretools"'"}}{{$.ImportPath}}{{"\n"}}{{end}}{{end}}' ./...
	// However, the below implementation has slightly better observed performance. 68.728458ms vs 207.141583ms on this repo at the time of writing.
	// I have an intuition that the go list approach will be more robust in the long term, since it automatically handles all corner cases
	// of Go. But for now, we'll just go with this impl.
	if pkg == nil {
		return nil, fmt.Errorf("nil package")
	}
	if pkg.Module == nil {
		return nil, fmt.Errorf("package %q has no module", pkg.ImportPath)
	}

	if err := pkg.Module.LoadAllPackages(); err != nil {
		return nil, fmt.Errorf("load module packages: %w", err)
	}

	target := pkg.ImportPath
	seen := make(map[string]struct{})

	for _, candidate := range pkg.Module.Packages {
		if candidate == nil {
			continue
		}
		if candidate.ImportPath == target {
			continue
		}

		if _, ok := candidate.ImportPaths[target]; ok {
			seen[candidate.ImportPath] = struct{}{}
		}

		if candidate.TestPackage != nil {
			if _, ok := candidate.TestPackage.ImportPaths[target]; ok {
				seen[candidate.TestPackage.ImportPath] = struct{}{}
			}
		}
	}

	result := make([]string, 0, len(seen))
	for path := range seen {
		result = append(result, path)
	}
	sort.Strings(result)

	return result, nil
}

func formatSection(tagName, attrs, content string) string {
	var b strings.Builder
	b.WriteByte('<')
	b.WriteString(tagName)
	if attrs != "" {
		b.WriteByte(' ')
		b.WriteString(attrs)
	}
	b.WriteString(">\n")
	if content != "" {
		b.WriteString(content)
		if !strings.HasSuffix(content, "\n") {
			b.WriteByte('\n')
		}
	}
	b.WriteString("</")
	b.WriteString(tagName)
	b.WriteString(">")
	return b.String()
}

func limitTestPkgMap(sections []string, maxLines int) string {
	if len(sections) == 0 {
		return ""
	}

	var trimmed []string
	for _, section := range sections {
		section = strings.TrimSpace(section)
		if section == "" {
			continue
		}
		trimmed = append(trimmed, section)
	}

	if len(trimmed) == 0 {
		return ""
	}

	content := strings.Join(trimmed, "\n\n")
	if lineCount(content) <= maxLines {
		return content
	}

	return fmt.Sprintf("elided; limited to %d lines for tests", maxLines)
}

func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// runTestsWithRecursionGuard protects us from the situation where a package's tests invoke
// `initialcontext.Create`, which then tries to collect the package's own test output again.
// Example: the tests for `axi/codeai/cmd/codagent` call this helper on their package, and the
// helper in turn calls `go test ./codeai/cmd/codagent`. That second `go test` rebuilds and
// reruns the same test binary, which reaches back into `initialcontext.Create`, which launches
// another `go test`, and so on forever.
//
// The guard works in two layers:
//  1. We thread every package we are currently testing through `CODEAI_INITIALCONTEXT_ACTIVE_TESTS`.
//     Before launching `go test`, we append the current import path to the chain; if the path is
//     already present, we know a parent call is already exercising that package and we short-circuit
//     with a fake status.
//  2. Some recursion loops do not use the env var—for example, when `go test` directly executes the
//     binary for the package under test. In that case we detect the loop by recognizing that the
//     current process was booted by `go test` (testing flags are registered) and that our cwd matches
//     the package directory. If both are true, we are already running inside that package's own
//     `go test` process, so we skip invoking it again.
func runTestsWithRecursionGuard(ctx context.Context, pkg *gocode.Package, moduleAbsPath, pkgAbsPath string) (string, error) {
	if recursionDetected(pkg.ImportPath) || selfTestRecursionDetected(pkg) {
		return fakeTestStatus(pkg), nil
	}

	prevValue, hadPrev := os.LookupEnv(recursionEnvVar)
	nextValue := pkg.ImportPath
	if hadPrev && prevValue != "" {
		nextValue = prevValue + ":" + pkg.ImportPath
	}

	if err := os.Setenv(recursionEnvVar, nextValue); err != nil {
		return "", fmt.Errorf("set %s: %w", recursionEnvVar, err)
	}
	defer func() {
		if !hadPrev {
			_ = os.Unsetenv(recursionEnvVar)
			return
		}
		_ = os.Setenv(recursionEnvVar, prevValue)
	}()

	return exttools.RunTests(ctx, moduleAbsPath, pkgAbsPath, "", false)
}

func recursionDetected(pkgImportPath string) bool {
	env := os.Getenv(recursionEnvVar)
	if env == "" {
		return false
	}
	for _, entry := range strings.Split(env, ":") {
		if entry == pkgImportPath {
			return true
		}
	}
	return false
}

func selfTestRecursionDetected(pkg *gocode.Package) bool {
	if pkg == nil {
		return false
	}
	if !isGoTestProcess() {
		return false
	}
	cwd, err := os.Getwd()
	if err != nil {
		return false
	}
	absPkgPath := pkg.AbsolutePath()
	if absPkgPath == "" {
		return false
	}
	return sameDir(absPkgPath, cwd)
}

func isGoTestProcess() bool {
	if flag.Lookup("test.run") != nil {
		return true
	}
	if flag.Lookup("test.v") != nil {
		return true
	}
	if flag.Lookup("test.timeout") != nil {
		return true
	}
	return false
}

func sameDir(a, b string) bool {
	if a == "" || b == "" {
		return false
	}

	aResolved, err := filepath.EvalSymlinks(a)
	if err != nil {
		aResolved = filepath.Clean(a)
	}

	bResolved, err := filepath.EvalSymlinks(b)
	if err != nil {
		bResolved = filepath.Clean(b)
	}

	return aResolved == bResolved
}

func fakeTestStatus(pkg *gocode.Package) string {
	target := goTestTarget(pkg)
	return fmt.Sprintf(`<test-status ok="unknown">
$ go test %s
(tests not run; infinite recursion detected in initialcontext)
</test-status>`, target)
}

func skippedDiagnosticsStatus(pkg *gocode.Package) string {
	target := goTestTarget(pkg)
	return fmt.Sprintf(`<diagnostics-status ok="unknown">
$ go build -o /dev/null %s
(diagnostics not run; deliberately skipped)
</diagnostics-status>`, target)
}

func skippedTestStatus(pkg *gocode.Package) string {
	target := goTestTarget(pkg)
	return fmt.Sprintf(`<test-status ok="unknown">
$ go test %s
(tests not run; deliberately skipped)
</test-status>`, target)
}

func skippedLintStatus(pkg *gocode.Package) string {
	target := goTestTarget(pkg)
	return fmt.Sprintf(`<lint-status ok="unknown">
$ gofmt -l %s
(lints not run; deliberately skipped)
</lint-status>`, target)
}

func goTestTarget(pkg *gocode.Package) string {
	rel := strings.TrimPrefix(pkg.RelativeDir, "./")
	if rel == "" || rel == "." {
		return "./"
	}
	return "./" + rel
}
