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
	name         string              // Name is the configured display name; an empty value is reported as "code unit".
	baseDir      string              // BaseDir is the cleaned absolute root directory for the code unit.
	includedDirs map[string]struct{} // IncludedDirs contains the cleaned absolute directories whose direct non-directory files are included.

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

// goModuleRelativePackagePath returns absBaseDir's slash-separated package path relative to the nearest containing Go module.
//
// The boolean result is false when no containing go.mod file can be found or the module-relative path cannot be determined.
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

// BaseDir returns the cleaned absolute root directory of the code unit.
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

// includeSubtreeUnlessContainsWithFilter recursively includes descendant directories under the base directory unless shouldSkipDir rejects the directory or the
// directory contains a non-directory file matching one of globPattern.
//
// Directories that are rejected or matched are not included and are not traversed.
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

// pruneStructuralDirsWithFilter removes included directories that only connect the base directory to other on-disk structure.
//
// The base directory is always kept. A non-base directory is kept if it has a non-directory file, has a kept child, or has no entries other than directories rejected
// by shouldSkipDir.
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

// normalizeExistingDir resolves dirPath to a cleaned absolute directory path.
//
// Relative paths are resolved against BaseDir. The method returns an error if dirPath is empty, cannot be inspected, or does not name a directory.
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

// includeExistingDir adds dirAbs to the included directory set and optionally includes its entire subtree.
//
// The parent of dirAbs must already be included unless dirAbs is the base directory. If shouldSkipDir rejects dirAbs, the method does nothing; when includeSubtree
// is true, rejected child directories are not walked.
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

// ensureParentIncluded verifies that dir is the base directory or that its parent directory is already included.
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

// includeReachableDirsNamedWithFilter includes every reachable directory with the given base name and its subtree.
//
// Search starts at the base directory and descends through directories that are already included or newly included by this method. Directories rejected by shouldSkipDir
// are ignored.
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

// skipDefaultGoCodeUnitDir reports whether dir should be omitted by the default Go code unit rules.
//
// The base directory is never skipped; other directories are skipped when their base name starts with ".".
func (c *CodeUnit) skipDefaultGoCodeUnitDir(dir string) bool {
	if dir == c.baseDir {
		return false
	}
	base := filepath.Base(dir)
	return len(base) > 0 && base[0] == '.'
}

// dirContainsPattern reports whether dir contains a non-directory entry whose name matches any glob pattern.
//
// Patterns use filepath.Match syntax and are matched against entry base names. The method returns false with no error when patterns is empty.
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

// dirHasNonDirFiles reports whether dir contains at least one non-directory entry.
//
// Read errors are reported as false.
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

// dirHasNoNonSkippedEntries reports whether dir has no entries except child directories rejected by shouldSkipDir.
//
// If shouldSkipDir is nil, the method reports whether dir is empty. Read errors are reported as false.
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
