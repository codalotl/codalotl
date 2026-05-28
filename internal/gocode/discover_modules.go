package gocode

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"
)

// DiscoverModules returns Go modules discovered from root.
//
// File roots are normalized to their parent directory. Results are sorted by absolute module path.
//
// If a Go workspace applies to root, DiscoverModules returns explicitly listed workspace modules. Otherwise it recursively finds go.mod files below root, skipping
// vendor, testdata, dot-prefixed, and underscore-prefixed directories during descent. Root itself is considered before exclusions.
func DiscoverModules(root string) ([]*Module, error) {
	rootDir, err := normalizeDiscoveryRoot(root)
	if err != nil {
		return nil, err
	}

	if workFile, ok, err := findApplicableGoWork(rootDir); err != nil {
		return nil, err
	} else if ok {
		return discoverWorkspaceModules(workFile)
	}

	return discoverModulesRecursive(rootDir)
}

// normalizeDiscoveryRoot returns the clean absolute directory where module discovery starts.
//
// An empty root is treated as ".". If root names a file, its parent directory is returned.
func normalizeDiscoveryRoot(root string) (string, error) {
	if root == "" {
		root = "."
	}

	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("absolute root: %w", err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("stat root: %w", err)
	}
	if !info.IsDir() {
		abs = filepath.Dir(abs)
	}

	return filepath.Clean(abs), nil
}

// findApplicableGoWork returns the go.work file that applies to rootDir, if any.
//
// GOWORK=off disables workspace discovery. An explicit GOWORK value other than "auto" is used directly after validation. Otherwise, rootDir and its parents are
// searched for go.work.
func findApplicableGoWork(rootDir string) (string, bool, error) {
	gowork := os.Getenv("GOWORK")
	if gowork == "off" {
		return "", false, nil
	}
	if gowork != "" && gowork != "auto" {
		abs, err := filepath.Abs(gowork)
		if err != nil {
			return "", false, fmt.Errorf("absolute GOWORK: %w", err)
		}
		if _, err := os.Stat(abs); err != nil {
			return "", false, fmt.Errorf("stat GOWORK: %w", err)
		}
		return filepath.Clean(abs), true, nil
	}

	for dir := rootDir; ; dir = filepath.Dir(dir) {
		workFile := filepath.Join(dir, "go.work")
		if _, err := os.Stat(workFile); err == nil {
			return workFile, true, nil
		} else if err != nil && !os.IsNotExist(err) {
			return "", false, fmt.Errorf("stat go.work: %w", err)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false, nil
		}
	}
}

// discoverWorkspaceModules returns the modules explicitly listed by a go.work file.
//
// Relative use paths are resolved from the directory containing workFile. The returned modules are de-duplicated and sorted by absolute path.
func discoverWorkspaceModules(workFile string) ([]*Module, error) {
	data, err := os.ReadFile(workFile)
	if err != nil {
		return nil, fmt.Errorf("read go.work: %w", err)
	}

	parsed, err := modfile.ParseWork(workFile, data, nil)
	if err != nil {
		return nil, fmt.Errorf("parse go.work: %w", err)
	}

	workDir := filepath.Dir(workFile)
	moduleDirs := make([]string, 0, len(parsed.Use))
	for _, use := range parsed.Use {
		if use == nil {
			continue
		}
		moduleDir := filepath.FromSlash(use.Path)
		if !filepath.IsAbs(moduleDir) {
			moduleDir = filepath.Join(workDir, moduleDir)
		}
		moduleDirs = append(moduleDirs, moduleDir)
	}

	return modulesFromDirs(moduleDirs)
}

// discoverModulesRecursive finds Go modules under rootDir by recursively searching for go.mod files.
//
// The root directory is considered even if its name would otherwise be skipped. Descending into vendor, testdata, dot-prefixed, and underscore-prefixed directories
// is skipped. Returned modules are sorted by absolute path.
func discoverModulesRecursive(rootDir string) ([]*Module, error) {
	var moduleDirs []string
	err := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() {
			return nil
		}

		if path != rootDir && shouldSkipModuleDiscoveryDir(d.Name()) {
			return filepath.SkipDir
		}

		if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
			moduleDirs = append(moduleDirs, path)
		} else if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("stat go.mod: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk modules: %w", err)
	}

	return modulesFromDirs(moduleDirs)
}

func shouldSkipModuleDiscoveryDir(name string) bool {
	return name == "vendor" ||
		name == "testdata" ||
		strings.HasPrefix(name, ".") ||
		strings.HasPrefix(name, "_")
}

// modulesFromDirs loads modules rooted at moduleDirs and returns them sorted by absolute path.
//
// Each directory is cleaned, made absolute, and de-duplicated before loading. A directory must contain the module's go.mod directly; resolving to an enclosing module
// is reported as an error.
func modulesFromDirs(moduleDirs []string) ([]*Module, error) {
	seen := make(map[string]struct{}, len(moduleDirs))
	modules := make([]*Module, 0, len(moduleDirs))
	for _, moduleDir := range moduleDirs {
		abs, err := filepath.Abs(moduleDir)
		if err != nil {
			return nil, fmt.Errorf("absolute module dir: %w", err)
		}
		abs = filepath.Clean(abs)
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}

		mod, err := NewModule(abs)
		if err != nil {
			return nil, fmt.Errorf("load module %s: %w", abs, err)
		}
		if mod.AbsolutePath != abs {
			return nil, fmt.Errorf("load module %s: go.mod not found in directory", abs)
		}
		modules = append(modules, mod)
	}

	sort.Slice(modules, func(i, j int) bool {
		return modules[i].AbsolutePath < modules[j].AbsolutePath
	})
	return modules, nil
}
