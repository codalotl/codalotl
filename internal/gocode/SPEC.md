# gocode

`gocode` models Go modules and packages for Codalotl features.

## Module Discovery

Module discovery follows Go project layout conventions.

- If root is a file, discovery starts from its parent directory.
- Returned modules are sorted by absolute module path.
- If a Go workspace applies to root, explicitly listed workspace modules are returned.
- Otherwise, discovery recursively searches below root for `go.mod` files, skipping broad-recursive exclusions: `vendor`, `testdata`, dot-prefixed dirs, underscore-prefixed dirs.
- Root directory itself is considered before recursive exclusions are applied.

## Public API

```go {api}
// DiscoverModules returns Go modules discovered from root.
//
// File roots are normalized to their parent directory. Results are sorted by
// absolute module path.
//
// If a Go workspace applies to root, DiscoverModules returns explicitly listed
// workspace modules. Otherwise it recursively finds go.mod files below root,
// skipping vendor, testdata, dot-prefixed, and underscore-prefixed directories
// during descent. Root itself is considered before exclusions.
func DiscoverModules(root string) ([]*Module, error)
```
