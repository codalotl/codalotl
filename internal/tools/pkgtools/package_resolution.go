package pkgtools

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	ModuleAbsDir  string
	PackageAbsDir string
	PackageRelDir string
	ImportPath    string
}

func (r resolvedPackageRef) isWithinSandbox(sandboxAbsDir string) bool {
	return isWithinDir(sandboxAbsDir, r.PackageAbsDir)
}

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
		m, p, r, ip, err := mod.ResolvePackageByImport(raw)
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
	stdRootAbsDir, stdRelDir := stdlibRootAndRel(packageAbsDir)
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

func stdlibRootAndRel(packageAbsDir string) (rootAbsDir string, relDir string) {
	if packageAbsDir == "" {
		return "", "."
	}
	goroot := runtime.GOROOT()
	if goroot != "" {
		gorootSrc := filepath.Join(goroot, "src")
		if isWithinDir(gorootSrc, packageAbsDir) {
			rel, err := filepath.Rel(gorootSrc, packageAbsDir)
			if err == nil {
				rel = filepath.ToSlash(rel)
				if rel == "" {
					return gorootSrc, "."
				}
				return gorootSrc, rel
			}
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
