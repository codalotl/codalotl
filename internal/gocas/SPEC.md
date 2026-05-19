# gocas

This package stores metadata about Go packages and code units using content-addressable storage. It wraps `internal/q/cas` to add Go-specific and application-specific niceties.

## Dependencies

- `internal/q/cas` - underlying content-addressable storage system.

## AdditionalInfo

- `Store` captures `cas.AdditionalInfo` in a best-effort way by shelling out to `git`.
- `RecertifyPackage` captures current `cas.AdditionalInfo` and sets recertification provenance fields.
- If `git` isn't found, there's no `git` repo, there's no current branch (or similar), no error is returned. Those fields are left as zero values on `cas.AdditionalInfo`.
    - Record writes should not fail just because the user doesn't use git or their git state is unusual.

## DB Root Selection

Go-aware CAS callers use this package to select filesystem CAS roots:

- `CODALOTL_CAS_DB`, if set, resolved to an absolute path.
- Otherwise nearest ancestor containing `.git`: `<git-root>/.codalotl/cas`.

Write authorization and directory creation are caller responsibilities.

## Namespaces

Callers pass `NamespaceSpec`.

- `Name` is stable, non-versioned owner/user name.
- `Version` forms filesystem namespace `<Name>-<Version>`.
- `HashMode` selects package-file or default-code-unit hashing.
- Conversion to `internal/q/cas` namespace strings belongs here.

## Package Recertification

Package recertification asserts current package contents remain compliant with a recently invalidated CAS record.

- Uses same hash mode, file selection, and key derivation as `Store`, `Retrieve`, and `SummarizePackage`.
- Existing current CAS record: no-op, `current` status.
- No current record and no matching prior invalidated record: normal no-op, `no-prior` status.
- Matching prior invalidated record: copy most recent prior primary JSON payload to current hash, `recertified` status.
- New record `AdditionalInfo` reflects current paths/git metadata and sets `Recertified`, `RecertifiedFromHash`, and `RecertifiedFromRecord`.
- Source/prior records are never deleted or rewritten.
- Warnings highlight higher-risk recertifications: dirty current git worktree, large churn, old source record.

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

// HashMode selects which files participate in a Go CAS hash.
type HashMode string

const (
	HashModePackage  HashMode = "package"   // HashModePackage hashes package Go files and package-local SPEC.md.
	HashModeCodeUnit HashMode = "code-unit" // HashModeCodeUnit hashes the package's default Go code unit.
)

// NamespaceSpec describes a CAS namespace.
type NamespaceSpec struct {
	Name     string
	Version  int
	HashMode HashMode
}

// Namespace returns the versioned filesystem namespace, such as "specconforms-1".
func (spec NamespaceSpec) Namespace() Namespace

// DB stores and retrieves Go-package-adjacent and code-unit-adjacent metadata in content-addressable storage (CAS).
//
// Keys are derived from a gocode.Package and NamespaceSpec. Values are stored as JSON.
//
// DB wraps cas.DB to add:
//   - keying based on package or default-code-unit files (file-content hashing)
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

// PackageRecordSummary describes one CAS record relevant to a Go package.
type PackageRecordSummary struct {
	Hash           string             // Hash is the CAS hash used as the record key within the namespace.
	Time           time.Time          // Time is the best-effort record time. It prefers the CAS add commit time and falls back when unavailable.
	AdditionalInfo cas.AdditionalInfo // AdditionalInfo is the CAS metadata stored beside the primary JSON payload.
}

// PackageSummary describes current and prior CAS state for a package in one namespace.
type PackageSummary struct {
	// Current is non-nil when a CAS record exists for the package's current contents.
	Current *PackageRecordSummary

	// PriorInvalidated is the most relevant older matching record when Current is nil.
	PriorInvalidated *PackageRecordSummary

	// ChurnPercent is the best-effort changed-line percentage versus the newest prior record with a verified git baseline.
	ChurnPercent *float64
}

// PackageRecertificationStatus describes the outcome of package recertification.
type PackageRecertificationStatus string

const (
	PackageRecertificationStatusCurrent     PackageRecertificationStatus = "current"
	PackageRecertificationStatusRecertified PackageRecertificationStatus = "recertified"
	PackageRecertificationStatusNoPrior     PackageRecertificationStatus = "no-prior"
)

// PackageRecertificationResult describes a package recertification attempt.
type PackageRecertificationResult struct {
	Status       PackageRecertificationStatus
	CurrentHash  string
	SourceHash   string
	SourceRecord string
	Warnings     []string
}

// RootDirForBaseDir returns the absolute CAS root for baseDir.
func RootDirForBaseDir(baseDir string) (string, error)

// NewDBForBaseDir returns a Go-aware CAS database for baseDir.
//
// BaseDir and the underlying CAS root are absolute.
func NewDBForBaseDir(baseDir string) (*DB, error)

// Store stores jsonable for (pkg, spec).
//
// Storage is keyed by the pair (spec.Namespace(), content hash). The content hash is computed from files selected by spec.HashMode; spec.Namespace() is passed to
// the underlying CAS as the namespace and is not mixed into the hash bytes. Paths are interpreted relative to BaseDir.
//
// HashModePackage uses package Go source files, package test files, and package-local SPEC.md.
//
// HashModeCodeUnit uses the default Go code unit rooted at pkg.
//
// Key derivation ignores duplicate absolute paths and directories, requires all remaining files to be within BaseDir, and sorts files by their BaseDir-relative
// paths before hashing.
//
// The selected files are hashed with BaseDir-relative path identity, so both file contents and their BaseDir-relative paths participate in the content hash.
//
// If any included file cannot be read, Store returns an error.
//
// jsonable must be encodable by encoding/json (and is stored as JSON bytes).
//
// Store does not return the derived hash or filesystem path of the stored record. Use Retrieve to confirm a value can be loaded later.
//
// Store returns an error only for "real" failures (I/O, JSON encoding, CAS write failures, etc). Lack of git information is not an error.
func (db *DB) Store(pkg *gocode.Package, spec NamespaceSpec, jsonable any) error

// Retrieve loads the stored value for (pkg, spec) into target.
//
// ok reports whether a value existed. When ok is false, target is left unchanged.
//
// additionalInfo is returned from the underlying CAS layer and may include best-effort git metadata captured at store time. Most callers should treat AdditionalInfo
// as optional; see cas.AdditionalInfo field docs for details.
//
// Retrieve returns an error only for "real" failures (I/O, JSON decode, CAS read failures, etc).
func (db *DB) Retrieve(pkg *gocode.Package, spec NamespaceSpec, target any) (ok bool, additionalInfo cas.AdditionalInfo, err error)

// SummarizePackage returns current and prior CAS record state for (pkg, spec).
//
// It uses the same hash mode and file selection as Store and Retrieve. Missing CAS roots or namespaces are treated as empty stores. Corrupt or unrelated prior records
// are skipped, while errors looking up the current hash are returned.
func (db *DB) SummarizePackage(pkg *gocode.Package, spec NamespaceSpec) (PackageSummary, error)

// RecertifyPackage asserts that pkg's current contents remain compliant with a recently invalidated CAS record for spec.
//
// If current package contents already have a CAS record, RecertifyPackage is a no-op. If there is no matching prior invalidated record, it returns a no-prior result.
// Otherwise it copies the most recent matching prior record payload to the current content hash, updates AdditionalInfo for current package state, marks the new
// record as recertified, and leaves existing records unchanged.
func (db *DB) RecertifyPackage(pkg *gocode.Package, spec NamespaceSpec) (PackageRecertificationResult, error)

// Delete removes the stored value for (pkg, spec).
//
// Deleting a missing value is a no-op and returns nil.
//
// Delete returns an error only for "real" failures (I/O, CAS delete failures, etc).
func (db *DB) Delete(pkg *gocode.Package, spec NamespaceSpec) error
```
