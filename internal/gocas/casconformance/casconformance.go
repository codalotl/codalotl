package casconformance

import (
	"github.com/codalotl/codalotl/internal/gocas"
	"github.com/codalotl/codalotl/internal/gocode"
)

// Namespace stores results produced by spec conformance checks.
const Namespace gocas.Namespace = "specconforms-1"

// Metadata is the stored JSON payload.
type Metadata struct {
	Conforms bool `json:"conforms"`
}

// Store stores spec conformance metadata for pkg.
func Store(db *gocas.DB, pkg *gocode.Package, conforms bool) error {
	return db.StoreOnPackage(pkg, Namespace, Metadata{Conforms: conforms})
}

// Retrieve loads spec conformance metadata for pkg.
//
// found reports whether a record existed.
func Retrieve(db *gocas.DB, pkg *gocode.Package) (found bool, conforms bool, err error) {
	var md Metadata
	found, _, err = db.RetrieveOnPackage(pkg, Namespace, &md)
	if err != nil || !found {
		return found, false, err
	}
	return true, md.Conforms, nil
}
