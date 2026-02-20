# gocas

This package stores metadata about Go packages using content-addressable storage. It wraps `internal/q/cas` to add Go-specific and application-specific niceties.

## Dependencies

- `internal/q/cas` - underlying content-addressable storage system.

## Design Notes

- We can make more methods like StoreOnPackage, StoreOnSnippet, etc, that all take differently typed first args.
- Currently putting consts of Namespace in this package, but it may be better to move those to other packages.

## AdditionalInfo

- StoreOn* captures `cas.AdditionalInfo` in a best-effort way by shelling out to `git`.
- If `git` isn't found, there's no `git` repo, there's no current branch (or similar), no error is returned. Those fields are left as zero values on `cas.AdditionalInfo`.
    - StoreOn* should not fail just because the user doesn't use git or their git state is unusual.
- Other errors (I/O, permissions, etc) are returned.

## Public API

```go
// Namespace is a logical partition + version for values stored in content-addressable storage (CAS).
//
// Namespace must be filesystem-safe (no path separators), because it is used as a directory name under the CAS root.
//
// Treat a Namespace like a schema/version string:
//   - Keep it stable for a given JSON shape + meaning.
//   - Bump it when the stored JSON schema or semantics change, to avoid decoding old data into a new type.
type Namespace string

const (
	// NamespaceSpecConforms stores results produced by spec conformance checks.
	//
	// Version suffix is bumped when the stored JSON schema or semantics change.
	NamespaceSpecConforms Namespace = "specconforms-1"
)

// DB stores and retrieves Go-package-adjacent metadata in content-addressable storage (CAS).
//
// Keys are derived from the contents of a code unit (see StoreOnCodeUnit) plus a Namespace. Values are stored as JSON.
//
// DB wraps cas.DB to add:
//   - keying based on codeunit.CodeUnit (file-content hashing)
//   - best-effort git metadata capture (returned as cas.AdditionalInfo)
//
// Most callers should use the methods on *DB, rather than calling methods on the embedded cas.DB directly.
type DB struct {
	// BaseDir is the project root used when hashing paths from a code unit.
	//
	// BaseDir must be absolute. All paths returned by unit.IncludedFiles() must be within BaseDir.
	//
	// Hashing is based on the BaseDir-relative portion of each path, so hashing the same project from different working directories produces the same keys.
	BaseDir string

	// DB is the underlying filesystem-backed metadata store.
	//
	// AbsRoot must be an absolute path and is the root directory where records are stored:
	//
	//	<AbsRoot>/<namespace>/<hash[0:2]>/<hash[2:]>
	cas.DB
}

// StoreOnCodeUnit stores jsonable for (unit, namespace).
//
// Storage key is content-addressed from unit.IncludedFiles() and their file contents (paths are interpreted relative to BaseDir), plus namespace.
//
// If any included file cannot be read, StoreOnCodeUnit returns an error.
//
// jsonable must be encodable by encoding/json (and is stored as JSON bytes).
//
// StoreOnCodeUnit returns an error only for "real" failures (I/O, JSON encoding, CAS write failures, etc). Lack of git information is not an error.
func (db *DB) StoreOnCodeUnit(unit *codeunit.CodeUnit, namespace Namespace, jsonable any) error

// Retrieve loads the stored value for (unit, namespace) into target.
//
// ok reports whether a value existed. When ok is false, target is left unchanged.
//
// additionalInfo is returned from the underlying CAS layer and may include best-effort git metadata captured at store time. Most callers should treat AdditionalInfo
// as optional; see cas.AdditionalInfo field docs for details.
//
// Retrieve returns an error only for "real" failures (I/O, JSON decode, CAS read failures, etc).
func (db *DB) Retrieve(unit *codeunit.CodeUnit, namespace Namespace, target any) (ok bool, additionalInfo cas.AdditionalInfo, err error)
```
