# casconformance
Thin wrapper around `internal/gocas` for storing spec-conformance results for a `*gocode.Package`.

Conformance records for a package are keyed by the default Go code unit rooted at that package dir, not just the package's Go source files.

## Public API

```go
// Namespace stores results produced by spec conformance checks.
const Namespace gocas.Namespace = "specconforms-1"

// Metadata is the stored JSON payload.
type Metadata struct {
	Conforms bool `json:"conforms"`
}

// Store stores spec conformance metadata for pkg.
func Store(db *gocas.DB, pkg *gocode.Package, conforms bool) error

// Delete removes spec conformance metadata for pkg.
//
// Deleting a missing record is a no-op.
func Delete(db *gocas.DB, pkg *gocode.Package) error

// Retrieve loads spec conformance metadata for pkg.
//
// found reports whether a record existed.
func Retrieve(db *gocas.DB, pkg *gocode.Package) (found bool, conforms bool, err error)
```
