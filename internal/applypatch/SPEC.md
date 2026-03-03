# applypatch

The applypatch package implements the OpenAI-style `apply_patch` patch application (patches start with `*** Begin Patch`).

This package also implements traditional string replacement patching, with fuzzy matching.

## Replace Heuristics

`Replace` should be permissive and deterministic.
- Use progressively fuzzier matching levels; stop at the first level with viable matches.
- Avoid "creative" matches; require a clearly best candidate for single-replace operations.
- Match levels:
1. Literal match - exact byte-for-byte substring.
2. Newline-normalized match - treat `\n` and `\r\n` as equivalent.
3. Unicode/invisible normalization - normalize smart punctuation and common invisible spacing chars.
4. Indentation-normalized block match - allow uniform indentation delta for multiline `findText`.
5. Horizontal-whitespace-relaxed match - treat runs of spaces/tabs more loosely within a line.
6. Small typo-tolerant near match - last resort, only with strong similarity and clear winner.
- `replaceAll=false`: ambiguous match set should return invalid-patch error.
- `replaceAll=true`: replace all non-overlapping matches found at the selected level.
- Preserve file style when writing (newline convention; final newline behavior unless replacement clearly changes it).

## Public API

The documentation for `ApplyPatch` is long and lives in the source code:

```go
func ApplyPatch(cwdAbsPath string, patch string) ([]FileChange, error)
```

```go
// Replace replaces findText with replacementText in absPath (which must be an absolute path). It edits the file in place. If edits are made, the new file's contents
// are returned. If replaceAll is true, multiple replacements are made.
//
// A variety of heuristics are used to match findText. Replace does not merely do strict string replacement.
//
// An error is returned if:
//   - invalid inputs (ex: path is not absolute; findText is empty)
//   - file I/O errors and other Go errors
//   - if replaceAll is false and there are ambiguous matches at the selected heuristic level. IsInvalidPatch(err) will return true.
//   - if findText could not be found. IsInvalidPatch(err) will return true.
func Replace(absPath string, findText string, replacementText string, replaceAll bool) (string, error)
```
