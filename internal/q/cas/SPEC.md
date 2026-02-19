# cas

`cas` is a filesystem-backed, content-addressed metadata cache.

- Compute a content key (hash) from bytes, or from a set of files.
- Store JSON metadata under `(namespace, hash)`.
- Retrieve metadata later by recomputing the hash. If content changes, the hash changes and the record is naturally missed.

This package stores metadata. It is optimized for small (< 2kb) JSON metadata.

## Use cases

Originally designed to cache metadata derived from Go packages.

Example 1: an analyzer computes a security review for a package. Store the review output keyed by the package's files. Later, if the package's files are unchanged, retrieve the existing review. If the package changes, recompute.

Example 2: given a Go function, its surrounding code, and its documentation (as a []byte), compute/store whether the documentation is correct. As code mutates, we can track whether we need to re-analyze whether the docs are up-to-date and accurate.

## Model

- A "record" is keyed by:
    - `namespace` (separates different kinds of metadata)
    - `hash` (content-derived key)
- `namespace` is an identifier representing the category of the metadata result. Ex: "securityreview-1.0"; "docaudit-1.2". Recommended to be versioned.
- `AdditionalInfo` are optional provenance/info about how/when a record was computed. Stored in same record as primary metadata payload.

## Storage

- Backed by the filesystem, rooted at `DB.AbsRoot`.
- Data stored in this DB can be checked into git repositories with minimal merge conflicts.
    - Merge conflict only if metadata is computed against the exact same content.
    - But if two engineers are simultaneously coding against the same package, they typically shouldn't get merge conflicts on this cas system.
 - Records stored at `DB.AbsRoot/<namespace>/<hash[0:2]>/<hash[2:]>`

## Public API

```go
// Hasher identifies a CAS record by hash.
type Hasher interface {
	// Hash must be filesystem safe with no separators.
	Hash() string
}

// NewFileSetHasher returns a Hasher for the given paths. Path order does not matter. Paths should be relative paths, as they are used to compute the hash (and should
// ideally be stable across computers).
//
// Returns an error if, for instance, a path cannot be read.
func NewFileSetHasher(paths []string) (Hasher, error)

// NewDirRelativeFileSetHasher is like NewFileSetHasher, but the hash is based on the dir-relative portion of p in paths. Returns an error if any p in paths is not
// in dir.
//
// This allows the group of files to be moved as a unit without affecting their hash.
func NewDirRelativeFileSetHasher(dir string, paths []string) (Hasher, error)

// NewBytesHasher returns a Hasher for the bytes.
func NewBytesHasher(b []byte) Hasher

// DB is a filesystem-backed metadata store rooted at AbsRoot.
type DB struct {
	AbsRoot string
}

// AdditionalInfo is saved and retrieved besides the primary Store'd payload.
type AdditionalInfo struct {
	// Seconds since epoch.
	UnixTimestamp int

	// Caller-supplied opaque paths. Caller may often try to align these with paths passed to, eg, NewFileSetHasher, but this package does not verify.
	Paths []string

	GitClean     bool   // True if computed with a clean git worktree.
	GitCommit    string // Git commit the metadata was computed against.
	GitMergeBase string // Merge-base for GitCommit (if relevant).
}

// Options let callers supply AdditionalInfo fields if desired. Also exists for extensibility of Store in the future.
type Options struct {
	AdditionalInfo
}

// Store serializes jsonable (as JSON, using json.Marshal) and stores it for (namespace, hasher.Hash()). namespace must be filesystem safe with no separators. If
// opts is passed with non-zero AdditionalInfo, the additional info is stored (in the same record as jsonable).
func (db *DB) Store(hasher Hasher, namespace string, jsonable any, opts *Options) error

// Retrieve loads metadata for (namespace, hasher.Hash()) into target. It returns whether metadata was found, additional info, and any error. Metadata not being
// found is not, by itself, an error.
//   - target must be a pointer that is passed to json.Unmarshal.
//   - namespace must be filesystem safe with no separators.
func (db *DB) Retrieve(hasher Hasher, namespace string, target any) (bool, AdditionalInfo, error)
```
