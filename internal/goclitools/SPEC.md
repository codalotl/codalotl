# goclitools

This package offers select functionality found in Go CLI tools (ex: `gofmt`, `gopls`, `goimports`) as conveniently callable functions. This package will just shell out to the tool.

## Functionality

### Tool discovery / startup validation

```go {api}
// ToolRequirement describes an external tool that should be available in PATH.
//
// InstallHint is optional; when provided, it should be a user-facing hint for how to install the tool (for example: "go install ...@latest").
type ToolRequirement struct {
	Name        string
	InstallHint string
}

// ToolStatus is the result of resolving a ToolRequirement via exec.LookPath.
//
// Path is empty when the tool could not be found.
type ToolStatus struct {
	Name        string
	Path        string
	InstallHint string
}

// CheckTools resolves each required tool using exec.LookPath and returns a status for each requirement in the same order. It never returns an error; callers can
// decide which missing tools are fatal.
func CheckTools(reqs []ToolRequirement) []ToolStatus

// DefaultRequiredTools returns the external tools expected by Codalotl's Go workflows.
func DefaultRequiredTools() []ToolRequirement
```

### gofmt

```go {api}
// Gofmt runs `gofmt` on filenameOrDir with -w -l. It returns true if anything was formatted.
func Gofmt(filenameOrDir string) (bool, error)
```

### Fixing Imports

```go {api}
// FixImports fixes imports for a single file or for all .go files in a directory (non-recursive), updating files in place. It returns true if any file was changed.
//
// It prefers goimports. When given a directory, goimports is run once on the directory's .go files. If goimports is unavailable, it falls back to gopls; since gopls
// does not accept a directory for import organization, each .go file is processed individually with `gopls imports -w`.
//
// An error is returned if no tools are available or if a tool returns an error.
func FixImports(filenameOrDir string) (bool, error)
```

### Renaming

```go {api}
// Rename renames the identifier at (line, column) in the given file to newName using gopls. It writes changes in-place. If gopls reports shadowing or other semantic
// issues, an error is returned.
func Rename(filePath string, line, column int, newName string) error
```

### References

```go {api}
// Ref is a parsed representation of something like this:
//
//	/abs/path/to/file.go:247:37-43
//
// ColumnStart and ColumnEnd are 1-based byte offsets in the line, and ColumnEnd is the first byte past the reference.
type Ref struct {
	AbsPath     string
	Line        int
	ColumnStart int
	ColumnEnd   int
}

// References calls `gopls references` and returns references to the identifier at line and column (1-based). Column is measured in utf-8 bytes (not unicode runes).
func References(filePath string, line, column int) ([]Ref, error)
```
