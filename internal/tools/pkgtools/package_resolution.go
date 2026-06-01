package pkgtools

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/codalotl/codalotl/internal/gocode"
)

// resolvedPackageRef is the canonical result of resolving a tool "package path" parameter.
//
// Tools should accept only:
//   - a package directory relative to the sandbox root (slash or OS-separated)
//   - a Go import path
//
// Tools must NOT accept absolute paths or file paths.
type resolvedPackageRef struct {
	ModuleAbsDir  string // ModuleAbsDir is the absolute module root containing the package, when one is available.
	PackageAbsDir string // PackageAbsDir is the absolute path to the resolved package directory.
	PackageRelDir string // PackageRelDir is the package directory relative to the resolved root, using slash separators.
	ImportPath    string // ImportPath is the fully qualified Go import path for the package.
}

// isWithinSandbox reports whether r's package directory is sandboxAbsDir or a descendant of it. It returns false when either path is empty or the paths cannot be
// related.
func (r resolvedPackageRef) isWithinSandbox(sandboxAbsDir string) bool {
	return isWithinDir(sandboxAbsDir, r.PackageAbsDir)
}

// resolveToolPackageRef resolves input as a tool package path from mod's build context. It accepts a sandbox-relative package directory or Go import path, rejects
// absolute paths and paths that escape with "..", and returns a validated package reference.
func resolveToolPackageRef(mod *gocode.Module, input string) (resolvedPackageRef, error) {
	if mod == nil {
		return resolvedPackageRef{}, errors.New("module required")
	}

	raw := strings.TrimSpace(input)
	if raw == "" {
		return resolvedPackageRef{}, errors.New("path is required")
	}
	if strings.Contains(raw, `\`) {
		return resolvedPackageRef{}, fmt.Errorf("path %q is invalid (backslashes are not allowed); use a sandbox-relative package directory or a Go import path", raw)
	}
	if filepath.IsAbs(raw) {
		return resolvedPackageRef{}, fmt.Errorf("path %q is invalid (absolute paths are not allowed); use a sandbox-relative package directory or a Go import path", raw)
	}

	// If caller supplied a directory, normalize it to a clean slash-separated path.
	// Keep the raw string for import path resolution.
	relDirCandidate := filepath.ToSlash(filepath.Clean(raw))
	if relDirCandidate == "." {
		relDirCandidate = "."
	} else if relDirCandidate == ".." || strings.HasPrefix(relDirCandidate, "../") {
		return resolvedPackageRef{}, fmt.Errorf("path %q is invalid; it must be relative to the sandbox root (cannot escape via ..)", raw)
	}

	// Heuristic preference:
	// - likely import paths (example.com/..., module-name/...): resolve as import first
	// - otherwise (internal/...): resolve as sandbox-relative dir first
	preferImport := strings.HasPrefix(raw, mod.Name+"/") || raw == mod.Name || hasDotInFirstPathSegment(raw)
	if strings.HasPrefix(raw, "./") || raw == "." || strings.HasPrefix(raw, "../") {
		preferImport = false
	}

	tryImport := func() (resolvedPackageRef, error) {
		importCandidate := raw
		if raw == mod.Name || strings.HasPrefix(raw, mod.Name+"/") {
			fqImportPath, _, err := resolveImportPath(mod.Name, raw)
			if err == nil {
				importCandidate = fqImportPath
			}
		}

		m, p, r, ip, err := mod.ResolvePackageByImport(importCandidate)
		if err != nil {
			return resolvedPackageRef{}, err
		}
		return resolvedPackageRef{ModuleAbsDir: m, PackageAbsDir: p, PackageRelDir: r, ImportPath: ip}, nil
	}
	tryRelDir := func() (resolvedPackageRef, error) {
		m, p, r, ip, err := mod.ResolvePackageByRelativeDir(relDirCandidate)
		if err != nil {
			return resolvedPackageRef{}, err
		}
		return resolvedPackageRef{ModuleAbsDir: m, PackageAbsDir: p, PackageRelDir: r, ImportPath: ip}, nil
	}

	var res resolvedPackageRef
	var err error

	if preferImport {
		res, err = tryImport()
		if err == nil {
			return validateResolvedPackageRef(res)
		}
		if !errors.Is(err, gocode.ErrResolveNotFound) {
			return resolvedPackageRef{}, err
		}

		res, err = tryRelDir()
		if err == nil {
			return validateResolvedPackageRef(res)
		}
		if errors.Is(err, gocode.ErrResolveNotFound) {
			return resolvedPackageRef{}, fmt.Errorf("package %q could not be resolved from this module's build context", raw)
		}
		return resolvedPackageRef{}, err
	}

	res, err = tryRelDir()
	if err == nil {
		return validateResolvedPackageRef(res)
	}
	if !errors.Is(err, gocode.ErrResolveNotFound) {
		return resolvedPackageRef{}, err
	}

	res, err = tryImport()
	if err == nil {
		return validateResolvedPackageRef(res)
	}
	if errors.Is(err, gocode.ErrResolveNotFound) {
		return resolvedPackageRef{}, fmt.Errorf("package %q could not be resolved from this module's build context", raw)
	}
	return resolvedPackageRef{}, err
}

func validateResolvedPackageRef(res resolvedPackageRef) (resolvedPackageRef, error) {
	if res.PackageAbsDir == "" {
		return resolvedPackageRef{}, errors.New("package directory not resolved")
	}
	info, err := os.Stat(res.PackageAbsDir)
	if err != nil {
		return resolvedPackageRef{}, err
	}
	if !info.IsDir() {
		return resolvedPackageRef{}, fmt.Errorf("resolved path %q is not a directory", res.PackageAbsDir)
	}
	return res, nil
}

func validateResolvedPackageRefInSandbox(sandboxAbsDir string, input string, res resolvedPackageRef) error {
	if sandboxAbsDir == "" {
		return errors.New("sandbox directory required")
	}
	if !res.isWithinSandbox(sandboxAbsDir) {
		return fmt.Errorf("path %q resolves to %q at %q, which is outside the sandbox", input, res.ImportPath, res.PackageAbsDir)
	}
	return nil
}

func validateResolvedPackageRefInModule(moduleAbsDir string, input string, res resolvedPackageRef) error {
	if moduleAbsDir == "" {
		return errors.New("module directory required")
	}
	if res.ModuleAbsDir != moduleAbsDir {
		return fmt.Errorf("path %q resolves to %q, which is not within the current module", input, res.ImportPath)
	}
	return nil
}

func resolvePackagePath(mod *gocode.Module, input string) (moduleAbsDir string, packageAbsDir string, packageRelDir string, fqImportPath string, fnErr error) {
	res, err := resolveToolPackageRef(mod, input)
	if err != nil {
		return "", "", "", "", err
	}
	return res.ModuleAbsDir, res.PackageAbsDir, res.PackageRelDir, res.ImportPath, nil
}

// loadPackageForResolved loads a package from an already resolved package location and sets its import path to fqImportPath. It supports packages in baseMod, packages
// in other modules, and standard library packages without a module root.
func loadPackageForResolved(baseMod *gocode.Module, moduleAbsDir string, packageAbsDir string, packageRelDir string, fqImportPath string) (*gocode.Package, error) {
	if baseMod == nil {
		return nil, errors.New("module required")
	}
	if packageAbsDir == "" {
		return nil, errors.New("package directory not resolved")
	}

	relDir, err := resolvedRelDir(moduleAbsDir, packageAbsDir, packageRelDir)
	if err != nil {
		return nil, err
	}

	// If we're still inside the sandbox module, use the already-loaded module cache.
	if moduleAbsDir == baseMod.AbsolutePath {
		pkg, err := baseMod.LoadPackageByRelativeDir(relDir)
		if err != nil {
			return nil, err
		}
		pkg.ImportPath = fqImportPath
		return pkg, nil
	}

	// For dependencies (or nested modules), load via a Module rooted at the resolved module dir.
	if moduleAbsDir != "" {
		depMod, err := gocode.NewModule(moduleAbsDir)
		if err != nil {
			return nil, err
		}
		pkg, err := depMod.LoadPackageByRelativeDir(relDir)
		if err != nil {
			return nil, err
		}
		pkg.ImportPath = fqImportPath
		return pkg, nil
	}

	// Standard library packages are not within a module. We still load them for docs.
	stdRootAbsDir, stdRelDir := stdlibRootAndRel(packageAbsDir, fqImportPath)
	stdMod := &gocode.Module{
		Name:         "",
		AbsolutePath: stdRootAbsDir,
		Packages:     map[string]*gocode.Package{},
	}
	pkg, err := stdMod.ReadPackage(stdRelDir, nil)
	if err != nil {
		return nil, err
	}
	pkg.ImportPath = fqImportPath
	return pkg, nil
}

// resolvedRelDir returns the slash-separated directory used to load a resolved package. It prefers packageRelDir when supplied, returns "." for packages without
// a module root, and otherwise computes the path from moduleAbsDir to packageAbsDir.
func resolvedRelDir(moduleAbsDir string, packageAbsDir string, packageRelDir string) (string, error) {
	if packageRelDir != "" {
		if packageRelDir == "." {
			return ".", nil
		}
		return packageRelDir, nil
	}
	if moduleAbsDir == "" {
		return ".", nil
	}
	rel, err := filepath.Rel(moduleAbsDir, packageAbsDir)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	if rel == "" {
		return ".", nil
	}
	return rel, nil
}

func stdlibRootAndRel(packageAbsDir string, importPath string) (rootAbsDir string, relDir string) {
	if packageAbsDir == "" {
		return "", "."
	}

	relDir = strings.TrimPrefix(filepath.ToSlash(importPath), "/")
	if relDir != "" && relDir != "." {
		rootAbsDir = packageAbsDir
		for range strings.Split(relDir, "/") {
			rootAbsDir = filepath.Dir(rootAbsDir)
		}
		if filepath.Clean(filepath.Join(rootAbsDir, filepath.FromSlash(relDir))) == filepath.Clean(packageAbsDir) {
			return rootAbsDir, relDir
		}
	}

	// Fallback: treat the package dir as the root. This yields correct docs even if we can't derive GOROOT.
	return packageAbsDir, "."
}

func hasDotInFirstPathSegment(p string) bool {
	seg, _, _ := strings.Cut(p, "/")
	return strings.Contains(seg, ".")
}

func isWithinDir(parentAbsDir string, childAbsPath string) bool {
	if parentAbsDir == "" || childAbsPath == "" {
		return false
	}
	rel, err := filepath.Rel(parentAbsDir, childAbsPath)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	return rel == "." || (!strings.HasPrefix(rel, "../") && rel != "..")
}
