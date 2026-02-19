# cas

`cas` is a filesystem-backed, content-addressed metadata cache.

- Compute a content key ("blob") from bytes, or from a set of files.
- Store JSON metadata under `(namespace, blob)`.
- Retrieve metadata later by recomputing the blob. If content changes, the blob changes and the record is naturally missed.

This package stores metadata (not the original file contents). It is optimized for small-to-medium JSON blobs.

## Use cases

Originally designed to cache metadata derived from Go packages.

Example 1: an analyzer computes a security review for a package. Store the review output keyed by the package blob. Later, if the package's files are unchanged, retrieve the existing review. If the package changes, recompute.

Example 2: given a Go function, it's surrounding code, and its documentation (as a []byte blob), compute/store whether the documentation is correct. As code mutates, we can track whether we need to re-analyze whether the docs are up-to-date and accurate.

## Model

- A "record" is keyed by:
    - `namespace` (separates different kinds of metadata)
    - `hash` (content-derived key; usually from `*Blob`)
- `namespace` is an opaque identifier representing the category of the metadata result. Ex: "securityreview-1.0"; "docaudit-1.2". Recommended to be versioned.
- `Options` are optional provenance about how/when a record was computed (as well as other options for storing).
    - Intended for debugging, audits, and future behaviors.
    - Not required to store a record.
    - Enables non-breaking changes to `Store` API by adding fields.

## Storage

- Backed by the filesystem, rooted at `DB.AbsRoot`.
- Data in this should be designed to be checked into git repositories.
    - Avoid filesystem layouts that create merge conflicts. For instance: a single "index" file for a Go package would have merge conflicts if two engineers were both working on this package.

## Hashing

- Blob hashing should be deterministic across runs for the same inputs.
- For file-backed blobs, hashing should be order-independent with respect to `filePaths`.

## Public API

```go
// Blob is a deterministic content-derived key for bytes or a set of files.
type Blob struct{}

// Hash returns a stable, filesystem-safe identifier for this blob.
func (b *Blob) Hash() string

// Hasher identifies a CAS record by hash.
type Hasher interface {
	Hash() string
}

// NewBlob computes a blob for the provided files.
//
// Hash should change when any file's bytes change, or when the set of provided paths changes.
//
// filePaths order doesn't matter. Each element must be a path that can be read.
func NewBlob(filePaths []string) (*Blob, error)

// NewBlobFromBytes returns a blob for the provided bytes.
func NewBlobFromBytes(p []byte) *Blob

// DB is a filesystem-backed metadata store rooted at AbsRoot.
type DB struct {
	AbsRoot string
}

// Options are optional provenance about when/how a record was computed.
type Options struct {
	GitClean     bool   // True if computed with a clean git worktree (if known).
	GitCommit    string // Git commit the metadata was computed against (if known).
	GitMergeBase string // Merge-base for GitCommit (if relevant/known).
}

// Store serializes jsonable (as JSON, using json.Marshal) and stores it for (namespace, hasher.Hash()).
func (db *DB) Store(hasher Hasher, namespace string, jsonable any, opts *Options) error

// Retrieve loads metadata for (namespace, hasher.Hash()) into target. It returns whether metadata was found, and any error. The metadata not being found does not
// by itself constitute an error.
//
// target must be a pointer that is passed to json.Unmarshal.
func (db *DB) Retrieve(hasher Hasher, namespace string, target any) (bool, error)
```
