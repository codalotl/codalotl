package gocode

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/packages"
)

// Module describes a Go module rooted at a directory containing a go.mod file. It records the module path, absolute root on disk, and a cache of packages loaded from that module. Create
// Modules with NewModule and load packages via the Load* and ReadPackage methods.
type Module struct {
	Name         string              // ex: "" or "github.com/foo/bar"
	AbsolutePath string              // ex: "/path/to/package"
	Packages     map[string]*Package // map of importPath to package; populated via LoadPackage/LoadAllPackages. TODO: make private
	cloned       bool                // true if this module was produced via CloneWithoutPackages
}

// NewModule returns a Module representing an existing Go module. It finds the nearest go.mod file starting from the path. The anyPath parameter can be any folder or filename in the
// Go module (ex: a Go file, the go.mod file itself, or a folder).
func NewModule(anyPath string) (*Module, error) {
	// Find the module root (directory containing go.mod)
	moduleRoot, err := findModuleRoot(anyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to find module root: %w", err)
	}

	// Extract the module name from go.mod
	moduleName, err := extractModuleName(moduleRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to extract module name: %w", err)
	}

	return &Module{
		Name:         moduleName,
		AbsolutePath: moduleRoot,
		Packages:     make(map[string]*Package),
	}, nil
}

var ErrResolveNotFound = errors.New("could not find package")

// ResolvePackageByRelativeDir resolves pkgRelativeDir relative to m.AbsolutePath.
// The result is equivalent to Go tooling (ex: `go list`) as if run from within m: the target must be importable from code in m (i.e., in m's module graph). This does not scan
// arbitrary packages on disk; directories in nested modules not in m's module graph are treated as not found.
//
// Argument pkgRelativeDir is a dir relative to m.AbsolutePath. Ex: "internal/foo"; "."; "" (same as "."); "./some/pkg/path". Any relative path that escapes m.AbsolutePath is rejected.
//
// It returns:
//   - moduleAbsDir: absolute dir of the package's containing module.
//   - packageAbsDir: absolute dir of the package.
//   - packageRelDir: relative dir of the package (relative to moduleAbsDir). Ex: "."; "some/pkg/path". "." is always returned if pkg dir is the same as the module dir. "" is returned if the package is not in a module.
//   - fqImportPath: fully qualified import path of the package (importable from code in m).
//   - fnErr: error, if any. ErrResolveNotFound if the package cannot be found, or some non-nil error if other errors are encountered.
func (m *Module) ResolvePackageByRelativeDir(pkgRelativeDir string) (moduleAbsDir string, packageAbsDir string, packageRelDir string, fqImportPath string, fnErr error) {
	rel := pkgRelativeDir
	for strings.HasPrefix(rel, "./") {
		rel = strings.TrimPrefix(rel, "./")
	}
	if rel == "" {
		rel = "."
	}
	if filepath.IsAbs(rel) {
		return "", "", "", "", fmt.Errorf("ResolvePackageByRelativeDir: pkgRelativeDir must be relative, got %q", pkgRelativeDir)
	}

	abs := filepath.Join(m.AbsolutePath, rel)
	back, err := filepath.Rel(m.AbsolutePath, abs)
	if err != nil {
		return "", "", "", "", fmt.Errorf("ResolvePackageByRelativeDir: validate relative dir: %w", err)
	}
	if back == ".." || strings.HasPrefix(back, ".."+string(filepath.Separator)) {
		return "", "", "", "", fmt.Errorf("ResolvePackageByRelativeDir: pkgRelativeDir escapes module root: %q", pkgRelativeDir)
	}

	// Fast fail for missing directories to avoid confusing go/packages errors.
	if info, err := os.Stat(abs); err != nil {
		if os.IsNotExist(err) {
			return "", "", "", "", fmt.Errorf("%w: directory does not exist: %s", ErrResolveNotFound, abs)
		}
		return "", "", "", "", fmt.Errorf("ResolvePackageByRelativeDir: stat: %w", err)
	} else if !info.IsDir() {
		return "", "", "", "", fmt.Errorf("%w: not a directory: %s", ErrResolveNotFound, abs)
	}

	pattern := "."
	if rel != "." {
		pattern = "./" + filepath.ToSlash(rel)
	}

	pkg, err := resolveOnePackage(m.AbsolutePath, pattern)
	if err != nil {
		return "", "", "", "", err
	}

	moduleAbsDir = ""
	if pkg.Module != nil {
		moduleAbsDir = pkg.Module.Dir
	}
	fqImportPath = pkg.PkgPath

	packageAbsDir = pkg.Dir
	if packageAbsDir == "" && len(pkg.GoFiles) > 0 {
		packageAbsDir = filepath.Dir(pkg.GoFiles[0])
	}
	if packageAbsDir == "" {
		return "", "", "", "", fmt.Errorf("ResolvePackageByRelativeDir: go/packages returned empty package dir for %q", pkgRelativeDir)
	}

	if moduleAbsDir != "" {
		r, err := filepath.Rel(moduleAbsDir, packageAbsDir)
		if err != nil {
			return "", "", "", "", fmt.Errorf("ResolvePackageByRelativeDir: compute packageRelDir: %w", err)
		}
		packageRelDir = filepath.ToSlash(r)
	} else {
		packageRelDir = ""
	}

	return moduleAbsDir, packageAbsDir, packageRelDir, fqImportPath, nil
}

// ResolvePackageByImport resolves importPath as a Go import as seen from code in m. The target must be importable from m (i.e., in m's module graph); otherwise ErrResolveNotFound.
// importPath must be a valid import path (as might be found in a source file's `import ( ... )` section), not a relative path like "./foo".
//
// The result is equivalent to Go tooling (ex: `go list`) as if run from within m. Actual implementation may vary, but callers can assume equivalent semantics.
//
// It returns:
//   - moduleAbsDir: absolute dir of the package's containing module (or "" if no module, as in stdlib packages, for instance).
//   - packageAbsDir: absolute dir of the package.
//   - packageRelDir: relative dir of the package (relative to moduleAbsDir). Ex: "."; "some/pkg/path". "." is always returned if pkg dir is the same as the module dir. "" is returned if the package is not in a module.
//   - fqImportPath: fully qualified import path of the package (importable from code in m).
//   - fnErr: error, if any. ErrResolveNotFound if the package cannot be found, or some non-nil error if other errors are encountered.
func (m *Module) ResolvePackageByImport(importPath string) (moduleAbsDir string, packageAbsDir string, packageRelDir string, fqImportPath string, fnErr error) {
	if importPath == "" {
		return "", "", "", "", fmt.Errorf("ResolvePackageByImport: importPath is empty")
	}
	if strings.Contains(importPath, "...") {
		return "", "", "", "", fmt.Errorf("ResolvePackageByImport: importPath must not be a pattern: %q", importPath)
	}
	if strings.HasPrefix(importPath, ".") || strings.HasPrefix(importPath, "/") {
		return "", "", "", "", fmt.Errorf("ResolvePackageByImport: importPath must not be relative or absolute: %q", importPath)
	}

	pkg, err := resolveOnePackage(m.AbsolutePath, importPath)
	if err != nil {
		return "", "", "", "", err
	}

	moduleAbsDir = ""
	if pkg.Module != nil {
		moduleAbsDir = pkg.Module.Dir
	}
	fqImportPath = pkg.PkgPath

	packageAbsDir = pkg.Dir
	if packageAbsDir == "" && len(pkg.GoFiles) > 0 {
		packageAbsDir = filepath.Dir(pkg.GoFiles[0])
	}
	if packageAbsDir == "" {
		return "", "", "", "", fmt.Errorf("ResolvePackageByImport: go/packages returned empty package dir for %q", importPath)
	}

	if moduleAbsDir != "" {
		r, err := filepath.Rel(moduleAbsDir, packageAbsDir)
		if err != nil {
			return "", "", "", "", fmt.Errorf("ResolvePackageByImport: compute packageRelDir: %w", err)
		}
		packageRelDir = filepath.ToSlash(r)
	} else {
		packageRelDir = ""
	}

	return moduleAbsDir, packageAbsDir, packageRelDir, fqImportPath, nil
}

func resolveOnePackage(fromDir string, pattern string) (*packages.Package, error) {
	cfg := &packages.Config{
		Mode: packages.NeedModule | packages.NeedFiles | packages.NeedName,
		Dir:  fromDir,
		Env:  os.Environ(),
	}

	pkgs, err := packages.Load(cfg, pattern)
	if err != nil {
		if resolveNotFound(err, pkgs) {
			return nil, fmt.Errorf("%w: %v", ErrResolveNotFound, err)
		}
		return nil, fmt.Errorf("go/packages load %q from %q: %w", pattern, fromDir, err)
	}

	if len(pkgs) == 0 {
		return nil, fmt.Errorf("%w: no packages matched %q", ErrResolveNotFound, pattern)
	}
	if len(pkgs) != 1 {
		return nil, fmt.Errorf("expected 1 package for %q, got %d", pattern, len(pkgs))
	}

	pkg := pkgs[0]
	if len(pkg.Errors) > 0 {
		if resolveNotFound(nil, pkgs) {
			return nil, fmt.Errorf("%w: %s", ErrResolveNotFound, pkg.Errors[0].Msg)
		}
		return nil, fmt.Errorf("go/packages load %q: %s", pattern, pkg.Errors[0].Msg)
	}

	// Some failures show up as empty PkgPath and/or Dir without a hard error.
	if pkg.PkgPath == "" || (pkg.Dir == "" && len(pkg.GoFiles) == 0) {
		return nil, fmt.Errorf("%w: could not resolve %q", ErrResolveNotFound, pattern)
	}

	return pkg, nil
}

func resolveNotFound(loadErr error, pkgs []*packages.Package) bool {
	var msgs []string
	if loadErr != nil {
		msgs = append(msgs, loadErr.Error())
	}
	for _, p := range pkgs {
		for _, e := range p.Errors {
			msgs = append(msgs, e.Msg)
		}
	}

	// These are go list error fragments that indicate "not found" rather than a hard internal error.
	needles := []string{
		"no required module provides package",
		"cannot find module providing package",
		"cannot find package",
		"main module does not contain package",
		"does not contain package",
		"build constraints exclude all Go files",
		"no Go files",
		"is in a module rooted at",
	}

	for _, msg := range msgs {
		for _, n := range needles {
			if strings.Contains(msg, n) {
				return true
			}
		}
	}
	return false
}

// LoadAllPackages recursively traverses the module root looking for Go packages. It calls ReadPackage for each package it finds. All loaded packages are stored in m.Packages.
func (m *Module) LoadAllPackages() error {
	return m.traverseDirectory(m.AbsolutePath, "")
}

// LoadPackageByRelativeDir loads a package from a directory relative to the module root. It returns a cached copy if available; otherwise, it reads from disk and caches the result.
func (m *Module) LoadPackageByRelativeDir(relativeDir string) (*Package, error) {

	importPath := importPathFromRelativeDir(m.Name, relativeDir)

	if pkg, ok := m.Packages[importPath]; ok {
		return pkg, nil
	}

	return m.ReadPackage(relativeDir, nil)
}

// ErrImportNotInModule is returned by LoadPackageByImportPath when the requested import path does not belong to the module.
var ErrImportNotInModule = errors.New("import path not module")

// LoadPackageByImportPath loads a package by import path. It returns any cached copy if present; otherwise, it loads from disk and caches it. Any import path not in the module returns
// the error ErrImportNotInModule.
func (m *Module) LoadPackageByImportPath(importPath string) (*Package, error) {
	if pkg, ok := m.Packages[importPath]; ok {
		return pkg, nil
	}

	if importPath == m.Name {
		return m.LoadPackageByRelativeDir("")
	} else if relativeDir, ok := strings.CutPrefix(importPath, m.Name+"/"); ok {
		return m.LoadPackageByRelativeDir(relativeDir)
	} else {
		return nil, ErrImportNotInModule
	}
}

// traverseDirectory recursively walks a directory, looking for Go packages and calling ReadPackage for each package it finds. Initially, relativeDir should be "" for the root module.
// It accumulates subdirectories as traversal proceeds (ex: "foo/bar").
func (m *Module) traverseDirectory(absDirPath string, relativeDir string) error {
	// Read the directory entries
	entries, err := os.ReadDir(absDirPath)
	if err != nil {
		return fmt.Errorf("failed to read directory %s: %w", absDirPath, err)
	}

	// Check if this directory contains Go files
	var goFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			goFiles = append(goFiles, entry.Name())
		}
	}

	// If this directory contains Go files, it's a package
	if len(goFiles) > 0 {
		// Read the package
		_, err := m.ReadPackage(relativeDir, goFiles)
		if err != nil {
			return fmt.Errorf("failed to read package %s: %w", relativeDir, err)
		}
	}

	// Recursively traverse subdirectories
	for _, entry := range entries {
		if entry.IsDir() {
			// Skip directories that start with a dot (hidden directories)
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}

			// Skip directories that are likely not Go packages
			if entry.Name() == "vendor" || entry.Name() == "testdata" {
				continue
			}

			// Construct the directory and import path for the subdirectory
			subRelativeDir := relativeDir
			if subRelativeDir != "" {
				subRelativeDir = filepath.Join(subRelativeDir, entry.Name())
			} else {
				subRelativeDir = entry.Name()
			}

			// Recursively traverse the subdirectory
			subAbsDirPath := filepath.Join(absDirPath, entry.Name())
			err := m.traverseDirectory(subAbsDirPath, subRelativeDir)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// ReadPackage reads the package at relativeDir and all Go files it contains. In addition to returning the package, it caches it in m. The goFileNames parameter is a list of .go files
// in the package (filenames only; no directory, even relative to the module). If goFileNames is nil, ReadPackage discovers the files. If any .go files are specified in goFileNames,
// they are assumed to be the complete list; this function does not verify correctness.
//
// TODO: We likely want to make this method private and have callers rely on LoadPackageByRelativeDir/LoadPackageByImportPath.
func (m *Module) ReadPackage(relativeDir string, goFileNames []string) (*Package, error) {

	if goFileNames != nil && len(goFileNames) == 0 {
		return nil, fmt.Errorf("goFileNames supplied, but empty")
	}

	// Determine the package directory path
	var absPkgDir string
	if relativeDir == "" {
		absPkgDir = m.AbsolutePath
	} else {
		absPkgDir = filepath.Join(m.AbsolutePath, relativeDir)
	}

	// Check if the directory exists
	if _, err := os.Stat(absPkgDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("package directory does not exist: %s", absPkgDir)
	}

	// If goFileNames is nil, find all .go files in the package directory
	if goFileNames == nil {
		buildGoFiles, err := goFilesInDirForConfig(absPkgDir, "", "", nil)
		if err != nil {
			return nil, fmt.Errorf("failed to list go files: %w", err)
		}
		goFileNames = buildGoFiles

		// If no Go files were found, return an error
		if len(goFileNames) == 0 {
			return nil, fmt.Errorf("no Go files found in package directory: %s", absPkgDir)
		}
	}

	pkg, err := NewPackage(relativeDir, absPkgDir, goFileNames, m)
	if err != nil {
		return nil, fmt.Errorf("could not ReadPackage: %w", err)
	}

	return pkg, nil
}

// findModuleRoot returns the root directory of the Go module (the one containing go.mod).
func findModuleRoot(path string) (string, error) {
	// If path is a directory, use it directly; otherwise get its parent directory
	dir := path
	if info, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("failed to stat path: %w", err)
	} else {
		if !info.IsDir() {
			dir = filepath.Dir(path)
		}
	}

	// Walk up the directory tree until we find a go.mod file
	for {
		// Check if go.mod exists in the current directory
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}

		// Move up to the parent directory
		parent := filepath.Dir(dir)
		if parent == dir {
			// We've reached the root of the filesystem without finding go.mod
			return "", fmt.Errorf("no go.mod file found in parent directories")
		}
		dir = parent
	}
}

// extractModuleName extracts the module name from the go.mod file.
func extractModuleName(moduleRoot string) (string, error) {
	data, err := os.ReadFile(filepath.Join(moduleRoot, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	if p := modfile.ModulePath(data); p != "" {
		return p, nil
	}
	return "", errors.New("go.mod has no module directive")
}

// CloneWithoutPackages creates a temporary clone of the module that contains only the go.mod file. The returned module has no packages and has its cloned flag set to true.
func (m *Module) CloneWithoutPackages() (*Module, error) {
	tmpDir, err := os.MkdirTemp("", "gomod-clone-")
	if err != nil {
		return nil, fmt.Errorf("make temp dir: %w", err)
	}

	srcModFile := filepath.Join(m.AbsolutePath, "go.mod")
	data, err := os.ReadFile(srcModFile)
	if err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("read go.mod: %w", err)
	}

	err = os.WriteFile(filepath.Join(tmpDir, "go.mod"), data, 0644)
	if err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("write go.mod: %w", err)
	}

	return &Module{
		Name:         m.Name,
		AbsolutePath: tmpDir,
		Packages:     make(map[string]*Package),
		cloned:       true,
	}, nil
}

// ClonePackage copies pkg into the cloned module m and returns the new package. The destination module m must be cloned.
func (m *Module) ClonePackage(pkg *Package) (*Package, error) {
	if !m.cloned {
		return nil, fmt.Errorf("ClonePackage only allowed on cloned modules")
	}
	if pkg == nil || pkg.Module == nil {
		return nil, fmt.Errorf("invalid package")
	}

	destDir := filepath.Join(m.AbsolutePath, pkg.RelativeDir)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}

	var fileNames []string
	for name, f := range pkg.Files {
		fileNames = append(fileNames, name)
		if err := os.WriteFile(filepath.Join(destDir, name), f.Contents, 0644); err != nil {
			return nil, fmt.Errorf("write file: %w", err)
		}
	}
	// Also include external test package files, if present, so clones fully represent the package.
	if pkg.TestPackage != nil {
		for name, f := range pkg.TestPackage.Files {
			fileNames = append(fileNames, name)
			if err := os.WriteFile(filepath.Join(destDir, name), f.Contents, 0644); err != nil {
				return nil, fmt.Errorf("write file: %w", err)
			}
		}
	}

	newPkg, err := NewPackage(pkg.RelativeDir, destDir, fileNames, m)
	if err != nil {
		return nil, err
	}

	return newPkg, nil
}

// DeleteClone removes the temporary directory created by CloneWithoutPackages. It does nothing for modules not produced by CloneWithoutPackages.
func (m *Module) DeleteClone() {
	if m.cloned {
		if strings.Contains(m.AbsolutePath, "gomod-clone-") { // Safeguard to avoid deleting actual code
			os.RemoveAll(m.AbsolutePath)
		}
	}
}
