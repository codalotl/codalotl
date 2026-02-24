# casconformance
Thin wrapper around `internal/gocas` for storing spec-conformance results for a `*gocode.Package`.

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

// Retrieve loads spec conformance metadata for pkg.
//
// found reports whether a record existed.
func Retrieve(db *gocas.DB, pkg *gocode.Package) (found bool, conforms bool, err error)
```
