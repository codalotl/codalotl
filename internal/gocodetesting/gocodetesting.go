package gocodetesting

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/gocode"

	"github.com/stretchr/testify/assert"
)

// Dedent removes the common leading indentation from each non-blank line in s. Spaces and tabs both count as indentation; the smallest indent among non-blank lines
// is removed from all non-blank lines. Blank-only lines do not affect the indent; interior blank lines are preserved, and leading/trailing blank lines are trimmed.
// The result has no trailing spaces or tabs and always ends with a single '\n'. Dedent is useful for inline multi-line test fixtures that are indented along with
// surrounding code.
func Dedent(s string) string {
	s = strings.Trim(s, "\n") // drop leading/trailing blank lines
	lines := strings.Split(s, "\n")

	min := -1 // smallest indent seen so far
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" { // If the line is only whitespace, consider it fully blank
			lines[i] = ""
			continue
		}
		indent := len(line) - len(trimmed)
		if min == -1 || indent < min {
			min = indent
		}
	}

	if min > 0 { // nothing to do if min == 0 or no non‑blank lines
		for i, line := range lines {
			if len(line) >= min {
				lines[i] = line[min:]
			}
		}
	}
	return strings.TrimRight(strings.Join(lines, "\n"), " \t\n") + "\n"
}

// WithMultiCode creates a temporary Go module with path "mymodule" containing a single package "mypkg" (import path "mymodule/mypkg"), writes the provided files
// into that package (map keys are filenames), prepends "package mypkg" when missing, loads the package via gocode, and calls f with the loaded package. The temporary
// files are removed on return. If setup or loading fails, the test is failed and f is not called.
func WithMultiCode(t *testing.T, fileToCode map[string]string, f func(*gocode.Package)) {
	// Create a temporary directory
	tmpDir, err := os.MkdirTemp("", "gocodetesting-")
	assert.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create a package directory
	pkgDir := filepath.Join(tmpDir, "mypkg")
	err = os.Mkdir(pkgDir, 0755)
	assert.NoError(t, err)

	// Create a go.mod file with a Go version that supports generics
	err = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module mymodule\n\ngo 1.18\n"), 0644)
	assert.NoError(t, err)

	// Write the code to a file
	for fileName, fileContents := range fileToCode {
		codeBytes := []byte(fileContents)

		// automatically insert a `package mypkg` if there's no package keyword:
		if !bytes.Contains(codeBytes, []byte("\npackage ")) && !bytes.HasPrefix(codeBytes, []byte("package ")) {
			codeBytes = []byte("package mypkg\n\n" + fileContents)
		}

		err = os.WriteFile(filepath.Join(pkgDir, fileName), codeBytes, 0644)
		if !assert.NoError(t, err) {
			return
		}
	}

	// Create the module
	module, err := gocode.NewModule(tmpDir)
	assert.NoError(t, err)

	// Read and parse the package
	pkg, err := module.LoadPackageByRelativeDir("mypkg")
	if assert.NoError(t, err) {
		// Call the callback with the parsed package
		f(pkg)
	}
}

// WithCode creates a temporary Go module containing one package with a single source file, loads that package, and invokes f with the parsed package. The file is
// written as "mypkg/code.go" with the provided contents; if no package clause is present, "package mypkg" is added. The module path is "mymodule" and the package
// import path is "mymodule/mypkg". The environment targets Go 1.18 so generics are available.
//
// All temporary files are removed on return. If creating or loading the package fails, t is failed and f is not called. Use WithMultiCode for multi-file fixtures
// and AddPackage to add additional packages to a module created by WithMultiCode.
func WithCode(t *testing.T, code string, f func(*gocode.Package)) {
	WithMultiCode(t, map[string]string{"code.go": code}, f)
}

// AddPackage adds a new package to mod, which must have been created with WithMultiCode. It creates the directory for newPkgPath under mod.AbsolutePath, writes
// the provided files, and auto-inserts a package clause for any file that lacks one. The package name is derived from the last element of newPkgPath (ex: "other"
// for "foo/bar/other"), falling back to "main" if a name cannot be derived.
//
// newPkgPath is a module-relative path that becomes the import-path suffix of the new package. fileToCode maps filenames to their contents; files are created or
// overwritten as needed.
//
// AddPackage does not load the new package into gocode; load it separately via the Module's loading APIs if required.
//
// On error creating directories or writing files, AddPackage reports the failure on t and returns a non-nil error.
func AddPackage(t *testing.T, mod *gocode.Module, newPkgPath string, fileToCode map[string]string) error {
	// Determine the absolute directory for the new package and create it (including parents).
	pkgDir := filepath.Join(mod.AbsolutePath, newPkgPath)
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		assert.NoError(t, err)
		return err
	}

	// The package name is the last element in the path (e.g. "other" for "foo/bar/other").
	pkgName := filepath.Base(newPkgPath)
	if pkgName == "" || pkgName == "." || pkgName == string(filepath.Separator) {
		pkgName = "main" // sensible fallback; shouldn't normally happen in tests
	}

	// Write each file to the new package directory.
	for fileName, contents := range fileToCode {
		codeBytes := []byte(contents)

		// Auto-insert a package declaration if we don't see one.
		if !bytes.Contains(codeBytes, []byte("\npackage ")) && !bytes.HasPrefix(codeBytes, []byte("package ")) {
			codeBytes = []byte("package " + pkgName + "\n\n" + contents)
		}

		filePath := filepath.Join(pkgDir, fileName)
		if err := os.WriteFile(filePath, codeBytes, 0o644); err != nil {
			assert.NoError(t, err)
			return err
		}
	}

	return nil
}
