# casclarify

Stores `clarify_public_api` answers for a `*gocode.Package` via `internal/gocas`.

Records are keyed like `gocas.StoreOnPackage`: target package Go files plus package-local `SPEC.md`.

In-play records are clarify CAS records from current git workstream: uncommitted/staged records whose hash matches current package, plus records added on current branch even after package hash drift. If git state cannot be read, no records are found.

## Public API

```go
// Namespace stores clarify_public_api answers.
const Namespace gocas.Namespace = "clarify-public-api-1"

// Entry is one clarification captured for a target package.
type Entry struct {
	OriginPackage string `json:"origin_package"`
	TargetPackage string `json:"target_package"`
	Identifier    string `json:"identifier"`
	Question      string `json:"question"`
	Answer        string `json:"answer"`
}

// Metadata is the stored JSON payload.
type Metadata struct {
	Entries []Entry `json:"entries"`
}

// InPlayRecord is a clarify CAS record selected for the current workstream.
type InPlayRecord struct {
	Path          string // Absolute path to the CAS record file.
	TargetPackage string // Target package import path, when known.
	Metadata      Metadata
}

// Append stores entry alongside any existing entries for pkg.
func Append(db *gocas.DB, pkg *gocode.Package, entry Entry) error

// Retrieve loads clarify metadata for pkg.
//
// found reports whether a record existed.
func Retrieve(db *gocas.DB, pkg *gocode.Package) (found bool, metadata Metadata, err error)

// FindInPlay finds clarify records relevant to the current git workstream.
func FindInPlay(db *gocas.DB, mod *gocode.Module) ([]InPlayRecord, error)

// Delete removes this clarify record from disk.
func (record InPlayRecord) Delete() error
```
