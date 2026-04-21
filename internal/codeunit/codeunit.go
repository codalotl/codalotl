package codeunit

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// CodeUnit represents a set of files rooted at a base directory.
//   - If a dir is "included", all non-dir files in the dir are definitionally included.
//   - With the exception of the base dir, a dir can only be included if it's reachable from another included dir.
type CodeUnit struct {
	name         string
	baseDir      string
	includedDirs map[string]struct{}

	// We intentionally avoid caching per-file membership so massive codebases stay light,
	// freshly created files are immediately visible, and because this exists for an LLM loop, which is dominiated
	// by the LLM thinking.
}

// NewCodeUnit creates a new code unit named `name` (ex: "package codeunit") that includes absBaseDir and all direct files (but not directories) in it. It is non-recursive.
// absBaseDir must be absolute.
func NewCodeUnit(name string, absBaseDir string) (*CodeUnit, error) {
	if !filepath.IsAbs(absBaseDir) {
		return nil, fmt.Errorf("base directory must be absolute: %s", absBaseDir)
	}

	cleanBase := filepath.Clean(absBaseDir)
	info, err := os.Stat(cleanBase)
	if err != nil {
		return nil, fmt.Errorf("stat base directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("base path is not a directory: %s", cleanBase)
	}

	c := &CodeUnit{
		name:         name,
		baseDir:      cleanBase,
		includedDirs: make(map[string]struct{}),
	}

	c.includedDirs[cleanBase] = struct{}{}
	return c, nil
}

// DefaultGoCodeUnit builds the shared default code unit for subtree-oriented Go package work rooted at absBaseDir. It includes absBaseDir and direct files in it,
// recursively includes descendant dirs unless that dir contains `*.go`, includes reachable `testdata` dirs, prunes structural dirs, and excludes descendant dirs
// whose basename starts with `.`.
func DefaultGoCodeUnit(absBaseDir string) (*CodeUnit, error) {
	unit, err := NewCodeUnit(defaultGoCodeUnitName(absBaseDir), absBaseDir)
	if err != nil {
		return nil, err
	}

	if err := unit.includeSubtreeUnlessContainsWithFilter(unit.skipDefaultGoCodeUnitDir, "*.go"); err != nil {
		return nil, err
	}
	if err := unit.includeReachableDirsNamedWithFilter("testdata", unit.skipDefaultGoCodeUnitDir); err != nil {
		return nil, err
	}
	unit.pruneStructuralDirsWithFilter(unit.skipDefaultGoCodeUnitDir)
	return unit, nil
}

func defaultGoCodeUnitName(absBaseDir string) string {
	cleanBase := filepath.Clean(absBaseDir)
	if relPkgPath, ok := goModuleRelativePackagePath(cleanBase); ok {
		return "package " + relPkgPath
	}
	return "package " + filepath.Base(cleanBase)
}

func goModuleRelativePackagePath(absBaseDir string) (string, bool) {
	searchDir := filepath.Clean(absBaseDir)

	for {
		goModPath := filepath.Join(searchDir, "go.mod")
		info, err := os.Stat(goModPath)
		if err == nil && !info.IsDir() {
			relPath, err := filepath.Rel(searchDir, absBaseDir)
			if err != nil {
				return "", false
			}
			return filepath.ToSlash(relPath), true
		}
		if err != nil && !os.IsNotExist(err) {
			return "", false
		}

		parent := filepath.Dir(searchDir)
		if parent == searchDir {
			return "", false
		}
		searchDir = parent
	}
}

// Name returns the configured name, or "code unit" if "" was configured.
func (c *CodeUnit) Name() string {
	if c.name == "" {
		return "code unit"
	}
	return c.name
}

func (c *CodeUnit) BaseDir() string {
	return c.baseDir
}

// Includes returns true if the code unit includes path. path can be relative to baseDir or absolute. path can be a directory or a file. A non-existent path returns
// true iff its filepath.Dir is already in the file set.
func (c *CodeUnit) Includes(path string) bool {
	if path == "" {
		return false
	}

	absPath := path
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(c.baseDir, path)
	}
	absPath = filepath.Clean(absPath)

	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			parent := filepath.Dir(absPath)
			_, ok := c.includedDirs[parent]
			return ok
		}
		return false
	}

	if info.IsDir() {
		_, ok := c.includedDirs[absPath]
		return ok
	}

	parent := filepath.Dir(absPath)
	_, ok := c.includedDirs[parent]
	return ok
}

// IncludedFiles returns the absolute paths of all files and dirs in code unit, sorted lexicographically.
func (c *CodeUnit) IncludedFiles() []string {
	dirs := make([]string, 0, len(c.includedDirs))
	for dir := range c.includedDirs {
		dirs = append(dirs, dir)
	}
	slices.Sort(dirs)

	results := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		results = append(results, dir)

		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			results = append(results, filepath.Join(dir, entry.Name()))
		}
	}
	slices.Sort(results)
	return results
}

// IncludeEntireSubtree includes the entire subtree rooted in BaseDir().
func (c *CodeUnit) IncludeEntireSubtree() {
	_ = c.includeExistingDir(c.baseDir, true, nil)
}

// IncludeDir includes dirPath (and all files in it) in the code unit. dirPath must be a dir (either relative or absolute), and its parent must already be in the
// code unit. If includeSubtree is true, all directories in dirPath are recursively included.
func (c *CodeUnit) IncludeDir(dirPath string, includeSubtree bool) error {
	dirAbs, err := c.normalizeExistingDir(dirPath)
	if err != nil {
		return err
	}

	return c.includeExistingDir(dirAbs, includeSubtree, nil)
}

// IncludeSubtreeUnlessContains recursively includes all dirs in BaseDir() unless the directory contains files matched by any glob pattern. For example, in Go, we
// could do IncludeSubtreeUnlessContains("*.go") which will not include nested packages, but will include supporting data directories.
func (c *CodeUnit) IncludeSubtreeUnlessContains(globPattern ...string) error {
	return c.includeSubtreeUnlessContainsWithFilter(nil, globPattern...)
}

func (c *CodeUnit) includeSubtreeUnlessContainsWithFilter(shouldSkipDir func(string) bool, globPattern ...string) error {
	queue := []string{c.baseDir}

	for len(queue) > 0 {
		dir := queue[0]
		queue = queue[1:]

		entries, err := os.ReadDir(dir)
		if err != nil {
			return fmt.Errorf("read dir %s: %w", dir, err)
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			child := filepath.Join(dir, entry.Name())
			if shouldSkipDir != nil && shouldSkipDir(child) {
				continue
			}
			shouldSkip, err := c.dirContainsPattern(child, globPattern)
			if err != nil {
				return err
			}
			if shouldSkip {
				continue
			}
			if err := c.includeExistingDir(child, false, shouldSkipDir); err != nil {
				return err
			}
			queue = append(queue, child)
		}
	}

	return nil
}

// PruneEmptyDirs iteratively removes all leaf dirs that have no files (except for the base dir), until there is nothing left to prune.
func (c *CodeUnit) PruneEmptyDirs() {
	for {
		childCount := make(map[string]int)
		for dir := range c.includedDirs {
			if dir == c.baseDir {
				continue
			}
			parent := filepath.Dir(dir)
			childCount[parent]++
		}

		removed := false
		for dir := range c.includedDirs {
			if dir == c.baseDir {
				continue
			}
			if childCount[dir] > 0 {
				continue
			}
			if c.dirHasNonDirFiles(dir) {
				continue
			}
			delete(c.includedDirs, dir)
			removed = true
		}

		if !removed {
			break
		}
	}
}

// PruneStructuralDirs removes included dirs that exist only to reach other on-disk structure. A dir is kept if it has included files, has a kept descendant, or
// is an actually empty leaf dir on disk.
func (c *CodeUnit) PruneStructuralDirs() {
	c.pruneStructuralDirsWithFilter(nil)
}

func (c *CodeUnit) pruneStructuralDirsWithFilter(shouldSkipDir func(string) bool) {
	dirs := make([]string, 0, len(c.includedDirs))
	for dir := range c.includedDirs {
		dirs = append(dirs, dir)
	}
	slices.SortFunc(dirs, func(a, b string) int {
		aDepth := strings.Count(a, string(os.PathSeparator))
		bDepth := strings.Count(b, string(os.PathSeparator))
		switch {
		case aDepth > bDepth:
			return -1
		case aDepth < bDepth:
			return 1
		default:
			return 0
		}
	})

	keptChildren := make(map[string]int)
	keptDirs := make(map[string]struct{}, len(c.includedDirs))
	keptDirs[c.baseDir] = struct{}{}

	for _, dir := range dirs {
		if dir == c.baseDir {
			continue
		}

		if c.dirHasNonDirFiles(dir) || c.dirHasNoNonSkippedEntries(dir, shouldSkipDir) || keptChildren[dir] > 0 {
			keptDirs[dir] = struct{}{}
			keptChildren[filepath.Dir(dir)]++
		}
	}

	c.includedDirs = keptDirs
}

func (c *CodeUnit) normalizeExistingDir(dirPath string) (string, error) {
	if dirPath == "" {
		return "", errors.New("directory path is empty")
	}

	var abs string
	if filepath.IsAbs(dirPath) {
		abs = filepath.Clean(dirPath)
	} else {
		abs = filepath.Join(c.baseDir, dirPath)
		abs = filepath.Clean(abs)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("stat dir %s: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path is not a directory: %s", abs)
	}

	return abs, nil
}

func (c *CodeUnit) includeExistingDir(dirAbs string, includeSubtree bool, shouldSkipDir func(string) bool) error {
	if shouldSkipDir != nil && shouldSkipDir(dirAbs) {
		return nil
	}
	if err := c.ensureParentIncluded(dirAbs); err != nil {
		return err
	}
	if !includeSubtree {
		c.includedDirs[dirAbs] = struct{}{}
		return nil
	}

	return filepath.WalkDir(dirAbs, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() {
			return nil
		}
		if shouldSkipDir != nil && shouldSkipDir(path) {
			return fs.SkipDir
		}
		if err := c.ensureParentIncluded(path); err != nil {
			return err
		}
		c.includedDirs[path] = struct{}{}
		return nil
	})
}

func (c *CodeUnit) ensureParentIncluded(dir string) error {
	if dir == c.baseDir {
		return nil
	}
	parent := filepath.Dir(dir)
	if parent == dir {
		return fmt.Errorf("cannot determine parent for %s", dir)
	}
	if _, ok := c.includedDirs[parent]; !ok {
		return fmt.Errorf("parent directory %s is not included", parent)
	}
	return nil
}

func (c *CodeUnit) includeReachableDirsNamedWithFilter(name string, shouldSkipDir func(string) bool) error {
	queue := []string{c.baseDir}
	queued := map[string]struct{}{
		c.baseDir: {},
	}

	for len(queue) > 0 {
		dir := queue[0]
		queue = queue[1:]

		entries, err := os.ReadDir(dir)
		if err != nil {
			return fmt.Errorf("read dir %s: %w", dir, err)
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			child := filepath.Join(dir, entry.Name())
			if shouldSkipDir != nil && shouldSkipDir(child) {
				continue
			}

			if entry.Name() == name {
				if err := c.includeExistingDir(child, true, shouldSkipDir); err != nil {
					return err
				}
				if _, seen := queued[child]; !seen {
					queued[child] = struct{}{}
					queue = append(queue, child)
				}
				continue
			}

			if _, ok := c.includedDirs[child]; ok {
				if _, seen := queued[child]; !seen {
					queued[child] = struct{}{}
					queue = append(queue, child)
				}
				continue
			}
		}
	}

	return nil
}

func (c *CodeUnit) skipDefaultGoCodeUnitDir(dir string) bool {
	if dir == c.baseDir {
		return false
	}
	base := filepath.Base(dir)
	return len(base) > 0 && base[0] == '.'
}

func (c *CodeUnit) dirContainsPattern(dir string, patterns []string) (bool, error) {
	if len(patterns) == 0 {
		return false, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("read dir %s: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		for _, pattern := range patterns {
			matched, matchErr := filepath.Match(pattern, name)
			if matchErr != nil {
				return false, fmt.Errorf("invalid glob pattern %s: %w", pattern, matchErr)
			}
			if matched {
				return true, nil
			}
		}
	}
	return false, nil
}

func (c *CodeUnit) dirHasNonDirFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		return true
	}
	return false
}

func (c *CodeUnit) dirHasNoNonSkippedEntries(dir string, shouldSkipDir func(string) bool) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		child := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			if shouldSkipDir != nil && shouldSkipDir(child) {
				continue
			}
			return false
		}
		return false
	}
	return true
}
