# gocas

This package stores metadata about Go packages and code units using content-addressable storage. It wraps `internal/q/cas` to add Go-specific and application-specific niceties.

## Dependencies

- `internal/q/cas` - underlying content-addressable storage system.

## AdditionalInfo

- `StoreOnPackage` and `StoreOnCodeUnit` capture `cas.AdditionalInfo` in a best-effort way by shelling out to `git`.
- If `git` isn't found, there's no `git` repo, there's no current branch (or similar), no error is returned. Those fields are left as zero values on `cas.AdditionalInfo`.
    - These store methods should not fail just because the user doesn't use git or their git state is unusual.

## DB Root Selection

Go-aware CAS callers use this package to select filesystem CAS roots:

- `CODALOTL_CAS_DB`, if set, resolved to an absolute path.
- Otherwise nearest ancestor containing `.git`: `<git-root>/.codalotl/cas`.

Write authorization and directory creation are caller responsibilities.

## Public API

```go
// EnvCASDB is the environment variable that overrides the default CAS root.
const EnvCASDB = "CODALOTL_CAS_DB"

// Namespace is a logical partition + version for values stored in content-addressable storage (CAS).
//
// Namespace must be filesystem-safe (no path separators), because it is used as a directory name under the CAS root.
//
// Treat a Namespace like a schema/version string:
//   - Keep it stable for a given JSON shape + meaning.
//   - Bump it when the stored JSON schema or semantics change, to avoid decoding old data into a new type.
type Namespace string

// DB stores and retrieves Go-package-adjacent and code-unit-adjacent metadata in content-addressable storage (CAS).
//
// Keys are derived from either package files (see StoreOnPackage) or code-unit files (see StoreOnCodeUnit), plus a Namespace. Values are stored as JSON.
//
// DB wraps cas.DB to add:
//   - keying based on gocode.Package files or codeunit.CodeUnit files (file-content hashing)
//   - best-effort git metadata capture (returned as cas.AdditionalInfo)
//
// Most callers should use the methods on *DB, rather than calling methods on the embedded cas.DB directly.
type DB struct {
	// BaseDir is the project root used when hashing package and code-unit file paths.
	//
	// BaseDir must be absolute. All hashed package and code-unit file paths must be within BaseDir.
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

// RootDirForBaseDir returns the absolute CAS root for baseDir.
func RootDirForBaseDir(baseDir string) (string, error)

// NewDBForBaseDir returns a Go-aware CAS database for baseDir.
//
// BaseDir and the underlying CAS root are absolute.
func NewDBForBaseDir(baseDir string) (*DB, error)

// StoreOnCodeUnit stores jsonable for (unit, namespace).
//
// Storage key is content-addressed from the included files in unit and their file contents, plus namespace. Paths are interpreted relative to BaseDir.
//
// Key derivation ignores duplicate absolute paths and directories, requires all remaining files to be within BaseDir, and sorts files by their BaseDir-relative
// paths before hashing.
//
// If any included file cannot be read, StoreOnCodeUnit returns an error.
//
// jsonable must be encodable by encoding/json (and is stored as JSON bytes).
//
// StoreOnCodeUnit does not return the derived hash or filesystem path of the stored record. Use RetrieveOnCodeUnit to confirm a value can be loaded later.
//
// StoreOnCodeUnit returns an error only for "real" failures (I/O, JSON encoding, CAS write failures, etc). Lack of git information is not an error.
func (db *DB) StoreOnCodeUnit(unit *codeunit.CodeUnit, namespace Namespace, jsonable any) error

// RetrieveOnCodeUnit loads the stored value for (unit, namespace) into target.
//
// ok reports whether a value existed. When ok is false, target is left unchanged.
//
// additionalInfo is returned from the underlying CAS layer and may include best-effort git metadata captured at store time. Most callers should treat AdditionalInfo
// as optional; see cas.AdditionalInfo field docs for details.
//
// RetrieveOnCodeUnit returns an error only for "real" failures (I/O, JSON decode, CAS read failures, etc).
func (db *DB) RetrieveOnCodeUnit(unit *codeunit.CodeUnit, namespace Namespace, target any) (ok bool, additionalInfo cas.AdditionalInfo, err error)

// DeleteOnCodeUnit removes the stored value for (unit, namespace).
//
// Deleting a missing value is a no-op and returns nil.
//
// DeleteOnCodeUnit returns an error only for "real" failures (I/O, CAS delete failures, etc).
func (db *DB) DeleteOnCodeUnit(unit *codeunit.CodeUnit, namespace Namespace) error

// StoreOnPackage stores jsonable for (pkg, namespace).
//
// Storage key is content-addressed from the Go source files in pkg (including pkg.TestPackage, if present) and their file contents (paths are interpreted relative
// to BaseDir), plus namespace.
//
// If a package-local SPEC.md exists in the package directory, it is also included in the storage key.
//
// If any package file cannot be read, StoreOnPackage returns an error.
//
// jsonable must be encodable by encoding/json (and is stored as JSON bytes).
//
// StoreOnPackage returns an error only for "real" failures (I/O, JSON encoding, CAS write failures, etc). Lack of git information is not an error.
func (db *DB) StoreOnPackage(pkg *gocode.Package, namespace Namespace, jsonable any) error

// RetrieveOnPackage loads the stored value for (pkg, namespace) into target.
//
// ok reports whether a value existed. When ok is false, target is left unchanged.
//
// additionalInfo is returned from the underlying CAS layer and may include best-effort git metadata captured at store time. Most callers should treat AdditionalInfo
// as optional; see cas.AdditionalInfo field docs for details.
//
// RetrieveOnPackage returns an error only for "real" failures (I/O, JSON decode, CAS read failures, etc).
func (db *DB) RetrieveOnPackage(pkg *gocode.Package, namespace Namespace, target any) (ok bool, additionalInfo cas.AdditionalInfo, err error)
```
