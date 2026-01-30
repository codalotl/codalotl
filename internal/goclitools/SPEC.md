# goclitools

This package offers select functionality found in Go CLI tools (ex: `gofmt`, `gopls`, `goimports`) as conveniently callable functions. This package will just shell out to the tool.

## Functionality

### Tool discovery / startup validation

```go {api}
// ToolRequirement describes an external tool expected in PATH.
// InstallHint is optional and should be a user-facing install command.
type ToolRequirement struct {
    Name string
    InstallHint string
}

// ToolStatus is the result of resolving a ToolRequirement with exec.LookPath.
// Path is empty when the tool is not found.
type ToolStatus struct {
    Name string
    Path string
    InstallHint string
}

// CheckTools resolves each tool name via exec.LookPath and returns a status for
// each requirement. It never returns an error.
func CheckTools(reqs []ToolRequirement) []ToolStatus

// DefaultRequiredTools returns Codalotl's default set of Go workflow tools:
// go, gopls, goimports, gofmt, git.
func DefaultRequiredTools() []ToolRequirement
```

### gofmt

```go {api}
// Gofmt runs `gofmt` on filenameOrDir with -w -l. It returns true if anything was formatted.
Gofmt(filenameOrDir string) (bool, error)
```

### Fixing Imports

```go {api}
// FixImports fixes imports for a single file or for all .go files in a directory
// (non-recursive), updating files in place. It returns true if any file was changed.
//
// It prefers goimports. When given a directory, goimports is run once on the
// directory's .go files. If goimports is unavailable, it falls back to gopls; since gopls
// does not accept a directory for import organization, each .go file is processed
// individually with `gopls imports -w`.
//
// An error is returned if no tools are available or if a tool returns an error.
FixImports(filenameOrDir string) (bool, error)
```

### Renaming

```go {api}
// Rename renames the identifier at (line, column) in the given file to newName using gopls.
// It writes changes in-place. If gopls reports shadowing or other semantic issues, an error is returned.
func Rename(filePath string, line, column int, newName string) error
```

### References

```go {api}
// Ref is a parsed representation of something like this: `/Users/david/code/myproj/clitool/main.go:247:37-43`.
// In the above example, the 6 bytes of the line would be line[36], line[37], line[38], line[39], line[40], line[41].
type Ref struct {
    AbsPath string // Must be absolute path
    Line int // 1-based line number
    ColumnStart int // 1 based column (measured in bytes).
    ColumnEnd int  // 1 based column (measured in bytes). This is the first byte **past** the reference.
}

// References calls `gopls references` and returns references to the identifier at line and column (1-based). Column is measured in utf-8 bytes (not unicode runes).
func References(filePath string, line, column int) ([]Ref, error)
```