# gocas

This package stores metadata about Go packages using content addressible storage. It wraps `q/cas` to add Go-specific, and application-specific, niceities.

## Dependencies

- `internal/q/cas` - underlying content addressible storage system.

## Design Notes

- We can make more methods like StoreOnPackage, StoreOnSnippet, etc, that all take differently typed first args.
- Currently putting consts of Namespace in this package, but it may be better to move those to other packages.

## AdditionalInfo

- StoreOn* automatically constructs AdditionalInfo in a best-effort way by shelling out to `git`.
- If `git` isn't found or there's no `git` repo, no current branch (or similar), no error is returned - those fields will just be zero value on AdditionalInfo.
    - In other words, StoreOn shouldn't fail because the user doesn't use git or their git state is weird.
- Otherwise, an error is returned to the user. TODO: what specifically kind of errors are these? Are there actually any?

## Public API

```go

type Namespace string

const (
    NamespaceSpecConforms Namespace = "specconforms-1"
)


type DB struct {
    // BaseDir is the root of a project. File paths from, for instance, unit.IncludedFiles(), are hashed relative to thish base dir.
    BaseDir string
    cas.DB
}

// Store stores jsonable hashed off of unit.IncludedFiles() (relative to BaseDir).
//
// This typically stores on an entire directory of Go and non-Go files, potentially including nested data directories.
func (db *DB) StoreOnCodeUnit(unit *codeunit.CodeUnit, namespace Namespace, jsonable any) error

func (db *DB) Retrieve(unit *codeunit.CodeUnit, namespace Namespace, target any) (bool, cas.AdditionalInfo, error)

```