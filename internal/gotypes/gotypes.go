package gotypes

import (
	"github.com/codalotl/codalotl/internal/gocode"
	"fmt"
	"go/types"
	"os"
	"strings"

	"golang.org/x/tools/go/packages"
)

// TypeInfo holds type-checker facts for a single package as produced by go/types. It is typically constructed by LoadTypeInfoInto
// and should be treated as a read-only snapshot of the package's type information.
type TypeInfo struct {
	// Info contains the go/types facts (Uses, Defs, Types, Selections, etc.) for the loaded package, keyed by AST nodes from
	// the package's current FileSet and AST.
	Info types.Info
}

// TODO: deal wtih test package

// LoadTypeInfoInto uses x/toolsgo/packages to load type information for pkg. ALERT: it replaces pkg's files' AST and FileSet information
// with a new version created from packages.Load, because Info's maps' keys are pointers from AST nodes.
func LoadTypeInfoInto(pkg *gocode.Package, includeTests bool) (*TypeInfo, error) {
	if pkg.IsTestPackage() && !includeTests {
		return nil, fmt.Errorf("pkg is a _test package, but includeTests is false")
	}

	cfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedCompiledGoFiles |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedForTest |
			packages.NeedSyntax,
		Dir: pkg.Module.AbsolutePath,
		Env: append(os.Environ(), "GO111MODULE=on"),
	}
	// Include test files (white-box) when present so type info covers _test.go files.
	cfg.Tests = true

	// If pkg is an external test package (package name ends with _test), load tests for
	// the base import path so that go/packages produces the test variants, and then
	// select the external-test package from the results.
	pattern := pkg.ImportPath
	if pkg.IsTestPackage() {
		pattern = strings.TrimSuffix(pkg.ImportPath, "_test")
	}

	pkgs, err := packages.Load(cfg, pattern)
	if err != nil {
		return nil, err
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no packages found for %q", pattern)
	}
	if n := packages.PrintErrors(pkgs); n > 0 {
		return nil, fmt.Errorf("failed to load package %q: %d error(s)", pattern, n)
	}

	loaded := pkgs[0]
	if pkg.IsTestPackage() {
		for _, cand := range pkgs {
			if cand.ForTest != "" && strings.HasSuffix(cand.Name, "_test") {
				loaded = cand
				break
			}
		}
	} else if !includeTests {
		for _, cand := range pkgs {
			if cand.ForTest == "" {
				loaded = cand
				break
			}
		}
	} else {
		for _, cand := range pkgs {
			if cand.ForTest != "" && !strings.HasSuffix(cand.Name, "_test") {
				loaded = cand
				break
			}
		}
	}

	// Ensure loaded has all our files:
	loadedGoFilesMap := make(map[string]bool)
	for _, f := range loaded.GoFiles {
		loadedGoFilesMap[f] = true
	}
	for _, f := range pkg.Files {
		if f.IsTest && !includeTests {
			continue
		}
		if !loadedGoFilesMap[f.AbsolutePath] {
			return nil, fmt.Errorf("load type info: loaded package is missing file %q (loaded.GoFiles: %s)", f.FileName, strings.Join(loaded.GoFiles, ", "))
		}

	}

	ti := loaded.TypesInfo

	// Replace pkg.Files' AST and FileSet with the ones produced by go/packages so that
	// the nodes referenced by types.Info.{Uses,Defs,Selections} match the nodes present
	// in pkg.Files. We match by absolute path to avoid relying on basenames.
	// Note: This only updates the primary package (not external test variants).
	absToFile := make(map[string]*gocode.File, len(pkg.Files))
	for _, f := range pkg.Files {
		absToFile[f.AbsolutePath] = f
	}

	for i, absPath := range loaded.CompiledGoFiles {
		if i < len(loaded.Syntax) {
			if f := absToFile[absPath]; f != nil {
				f.AST = loaded.Syntax[i]
				f.FileSet = loaded.Fset
			}
		}
	}

	return &TypeInfo{Info: *ti}, nil
}
