package casconformance

import (
	"github.com/codalotl/codalotl/internal/codeunit"
	"github.com/codalotl/codalotl/internal/gocas"
	"github.com/codalotl/codalotl/internal/gocode"
)

// Namespace stores results produced by spec conformance checks.
const Namespace gocas.Namespace = "specconforms-1"

// Metadata is the stored JSON payload.
type Metadata struct {
	Conforms bool `json:"conforms"`
}

func defaultCodeUnit(pkg *gocode.Package) (*codeunit.CodeUnit, error) {
	return codeunit.DefaultGoCodeUnit(pkg.AbsolutePath())
}

// Store stores spec conformance metadata for pkg.
func Store(db *gocas.DB, pkg *gocode.Package, conforms bool) error {
	unit, err := defaultCodeUnit(pkg)
	if err != nil {
		return err
	}
	return db.StoreOnCodeUnit(unit, Namespace, Metadata{Conforms: conforms})
}

// Delete removes spec conformance metadata for pkg.
//
// Deleting a missing record is a no-op.
func Delete(db *gocas.DB, pkg *gocode.Package) error {
	unit, err := defaultCodeUnit(pkg)
	if err != nil {
		return err
	}
	return db.DeleteOnCodeUnit(unit, Namespace)
}

// Retrieve loads spec conformance metadata for pkg.
//
// found reports whether a record existed.
func Retrieve(db *gocas.DB, pkg *gocode.Package) (found bool, conforms bool, err error) {
	unit, err := defaultCodeUnit(pkg)
	if err != nil {
		return false, false, err
	}

	var md Metadata
	found, _, err = db.RetrieveOnCodeUnit(unit, Namespace, &md)
	if err != nil || !found {
		return found, false, err
	}
	return true, md.Conforms, nil
}
