# casconformance

## Public API

```go

const (
	// Namespace stores results produced by spec conformance checks.
	Namespace gocas.Namespace = "specconforms-1"
)

type Metadata struct {
    Conforms bool `json:"conforms"`
}


func Store(db *gocas.DB, pkg *gocode.Package, conforms bool) error

func Retrieve(db *gocas.DB, pkg *gocode.Package) (found bool, conforms bool, err error)

```
