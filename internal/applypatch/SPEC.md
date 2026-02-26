# applypatch

The applypatch package implements the OpenAI-style `apply_patch` patch application (patches start with `*** Begin Patch`).

This package also implements traditional string replacement patching, with fuzzy matching.

## Public API

The documentation for `ApplyPatch` is long and lives in the source code:

```go
func ApplyPatch(cwdAbsPath string, patch string) ([]FileChange, error)
```

```go
// Replace replaces findText with replacementText in absPath (which must be an absolute path). It edits the file in-place. If edits are made,
// the new file's contents are returned. If replaceAll is true, multiple replacements are made.
//
// A variety of heuristics are used to match findText. Replace does not merely do strict string replacement.
//
// An error is returned if:
//   - invalid inputs (ex: not absolute path; empty findText)
//   - file I/O errors and other Go-ism errors
//   - If replaceAll is true, findText must be unique (at whatever heuristic level we're using). IsInvalidPatch(err) will return true.
//   - if we couldn't find findText. IsInvalidPatch(err) will return true.
func Replace(absPath string, findText string, replacementText string, replaceAll bool) (string, error)
```