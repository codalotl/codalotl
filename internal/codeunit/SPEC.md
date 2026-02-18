# codeunit

codeunit is a package intended to serve as a cross-language abstraction that defines a grouping, or unit, of code. It can answer the question of which files are, and are not, in the unit.

Grouping a codebase into units is useful so that we can limit an LLM to work on a single unit at a time. An LLM limited to a small subsection can reason about, and understand, that subsection more effectively.

In Go, a package is a great default codeunit. But sometimes Go codebases are constructed such that one main package is supported by several small ones (possibly internal). A human engineer typically considers this whole tree as a single "unit". Therefore, we need the ability to model this.

## Usage

A typical usage pattern for a Go code unit might be:

```go
unit, err := NewCodeUnit("package " + pkgName, pkgDir)
if err != nil {
    return err
}
err = unit.IncludeSubtreeUnlessContains("*.go", "go.mod") // include all dirs but stop when we get to other Go files or a go.mod file.
// (handle err)
err = unit.IncludeDir("testdata", true) // testdata can include .go files; they're not a real package.
// (handle err -- testdata might not exist, which is okay)

// removes empty "hierarchy-based" dirs. ex: /path/to/mymod has a package; /path/to/mymod/providers/openai and /path/to/mymod/providers/anthropic exist. The providers dir
// is otherwise empty, and so will be pruned. It's not intended to be part of this unit.
unit.PruneEmptyDirs()

// ...

// Later, an agent can determine which files are in its set:
files := unit.IncludedFiles()

// Alternatively, if an agent that is jailed to a code unit tries to read or edit a file, we can see if that's allowed:
if !unit.Includes(fileToRead) {
    return errors.New("not allowed: file is not in code unit")
}
```

## Public Interface

```go {api}
// CodeUnit represents a set of files rooted at a base directory.
//   - If a dir is "included", all non-dir files in the dir are definitionally included.
//   - With the exception of the base dir, a dir can only be included if it's reachable from another included dir.
type CodeUnit struct {
	name    string
	baseDir string

	// can include other fields
}

// NewCodeUnit creates a new code unit named `name` (ex: "package codeunit") that includes absBaseDir and all direct files (but not directories) in it. It is non-recursive.
// absBaseDir must be absolute.
func NewCodeUnit(name string, absBaseDir string) (*CodeUnit, error)

// Name returns the configured name, or "code unit" if "" was configured.
func (c *CodeUnit) Name() string

func (c *CodeUnit) BaseDir() string

// Returns true if the code unit includes path. path can be relative to baseDir or absolute. path can be a directory or a file. A non-existent path return true iff
// its filepath.Dir is in the file set.
func (c *CodeUnit) Includes(path string) bool

// IncludedFiles returns the absolute paths of all files and dirs in code unit, sorted lexicographically.
func (c *CodeUnit) IncludedFiles() []string

// IncludeEntireSubtree includes the entire subtree rooted in BaseDir().
func (c *CodeUnit) IncludeEntireSubtree()

// IncludeDir includes dirPath (and all files in it) in the code unit. dirPath must be a dir (either relative or absolute), and its parent must already be in the
// code unit. If includeSubtree is true, all directories in dirPath are recursively included.
func (c *CodeUnit) IncludeDir(dirPath string, includeSubtree bool) error

// IncludeSubtreeUnlessContains recursively includes all dirs in BaseDir() unless the directory contains files matched by any glob pattern. For example, in Go, we
// could do IncludeSubtreeUnlessContains("*.go") which will not include nested packages, but will include supporting data directories.
func (c *CodeUnit) IncludeSubtreeUnlessContains(globPattern ...string) error

// PruneEmptyDirs iteratively removes all leaf dirs that have no files (except for the base dir), until there is nothing left to prune.
func (c *CodeUnit) PruneEmptyDirs()
```
