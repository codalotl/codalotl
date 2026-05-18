package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/codalotl/codalotl/internal/gocode"
	qcli "github.com/codalotl/codalotl/internal/q/cli"
)

func loadPackageArg(arg string) (*gocode.Package, *gocode.Module, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return nil, nil, qcli.UsageError{Message: "missing <path/to/pkg>"}
	}
	if strings.Contains(arg, "...") {
		return nil, nil, qcli.UsageError{Message: `package patterns ("...") are not supported; provide a single package directory`}
	}

	if isExplicitFilesystemPath(arg) {
		return loadPackageDirArg(arg)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, fmt.Errorf("get working directory: %w", err)
	}

	var importErr error
	if mod, err := gocode.NewModule(cwd); err == nil {
		pkg, pkgMod, err := loadPackageImportArg(mod, arg)
		if err == nil {
			return pkg, pkgMod, nil
		}
		importErr = err
	} else {
		importErr = err
	}

	if absDir, ok, err := resolveExistingDir(arg); err != nil {
		return nil, nil, err
	} else if ok && isPackageImportNotFound(importErr) {
		return loadPackageDir(absDir)
	}

	if importErr != nil && !isPackageImportNotFound(importErr) {
		return nil, nil, importErr
	}
	return nil, nil, qcli.UsageError{Message: fmt.Sprintf("package %q was not found as an import path or directory", arg)}
}

func resolveExistingDir(pathArg string) (absDir string, ok bool, _ error) {
	pathArg = strings.TrimSpace(pathArg)
	if pathArg == "" {
		return "", false, nil
	}

	// filepath.Clean handles optional trailing separators.
	clean := filepath.Clean(pathArg)
	cwd, err := os.Getwd()
	if err != nil {
		return "", false, fmt.Errorf("get working directory: %w", err)
	}

	if !filepath.IsAbs(clean) {
		clean = filepath.Join(cwd, clean)
	}

	info, err := os.Stat(clean)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	if !info.IsDir() {
		return "", false, nil
	}
	return clean, true, nil
}

func isExplicitFilesystemPath(arg string) bool {
	arg = strings.TrimSpace(arg)
	if arg == "." || arg == ".." || filepath.IsAbs(arg) {
		return true
	}
	return strings.HasPrefix(arg, "./") ||
		strings.HasPrefix(arg, "../") ||
		strings.HasPrefix(arg, `.\`) ||
		strings.HasPrefix(arg, `..\`)
}

func loadPackageDirArg(pathArg string) (*gocode.Package, *gocode.Module, error) {
	absDir, ok, err := resolveExistingDir(pathArg)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, qcli.UsageError{Message: fmt.Sprintf("package directory %q was not found", pathArg)}
	}
	return loadPackageDir(absDir)
}

func loadPackageDir(absDir string) (*gocode.Package, *gocode.Module, error) {
	mod, err := gocode.NewModule(absDir)
	if err != nil {
		return readPackageWithoutModule(absDir, filepath.Base(absDir))
	}

	rel, err := filepath.Rel(mod.AbsolutePath, absDir)
	if err != nil {
		return nil, nil, err
	}
	rel = filepath.Clean(rel)
	if rel == "." {
		rel = ""
	}
	pkg, err := mod.LoadPackageByRelativeDir(rel)
	if err != nil {
		return nil, nil, err
	}
	return pkg, mod, nil
}

func loadPackageImportArg(resolver *gocode.Module, arg string) (*gocode.Package, *gocode.Module, error) {
	importPath := strings.TrimRight(strings.TrimSpace(arg), "/")
	if importPath == "" {
		return nil, nil, gocode.ErrResolveNotFound
	}
	moduleAbsDir, packageAbsDir, packageRelDir, fqImportPath, err := resolver.ResolvePackageByImport(importPath)
	if err != nil {
		return nil, nil, err
	}
	if packageRelDir == "" {
		packageRelDir = "."
	}
	if moduleAbsDir == "" {
		return readPackageWithoutModule(packageAbsDir, fqImportPath)
	}

	mod, err := gocode.NewModule(moduleAbsDir)
	if err != nil {
		return nil, nil, err
	}
	pkg, err := mod.LoadPackageByRelativeDir(packageRelDir)
	if err != nil {
		return nil, nil, err
	}
	if fqImportPath != "" {
		pkg.ImportPath = fqImportPath
	}
	return pkg, mod, nil
}

func readPackageWithoutModule(packageAbsDir string, importPath string) (*gocode.Package, *gocode.Module, error) {
	if strings.TrimSpace(importPath) == "" {
		importPath = filepath.Base(packageAbsDir)
	}
	mod := &gocode.Module{
		Name:         "",
		AbsolutePath: packageAbsDir,
		Packages:     map[string]*gocode.Package{},
	}
	pkg, err := mod.ReadPackage(".", nil)
	if err != nil {
		return nil, nil, err
	}
	pkg.ImportPath = importPath
	return pkg, mod, nil
}

func isPackageImportNotFound(err error) bool {
	if err == nil ||
		errors.Is(err, gocode.ErrResolveNotFound) ||
		errors.Is(err, gocode.ErrImportNotInModule) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, " is not in std ") ||
		strings.Contains(msg, " is not in std (") ||
		strings.Contains(msg, "cannot find module providing package") ||
		strings.Contains(msg, "no required module provides package")
}

func resolvePackagePathInsideCWD(arg string) (string, error) {
	pkg, _, err := loadPackageArg(arg)
	if err != nil {
		return "", err
	}
	pkgPath, err := filepath.Abs(pkg.AbsolutePath())
	if err != nil {
		return "", err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(cwd, pkgPath)
	if err != nil {
		return "", err
	}
	if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)) {
		return pkgPath, nil
	}
	return "", qcli.UsageError{Message: fmt.Sprintf("package %q resolves outside the current working directory", arg)}
}
