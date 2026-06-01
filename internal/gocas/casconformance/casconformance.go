package casconformance

import (
	"github.com/codalotl/codalotl/internal/gocas"
	"github.com/codalotl/codalotl/internal/gocode"
)

// NamespaceSpec stores results produced by spec conformance checks.
var NamespaceSpec = gocas.NamespaceSpec{
	Name:     "specconforms",
	Version:  1,
	HashMode: gocas.HashModeCodeUnit,
}

// Metadata is the stored JSON payload.
type Metadata struct {
	Conforms bool `json:"conforms"` // Conforms reports whether the package conforms to the spec.
}

// Store stores spec conformance metadata for pkg.
func Store(db *gocas.DB, pkg *gocode.Package, conforms bool) error {
	return db.Store(pkg, NamespaceSpec, Metadata{Conforms: conforms})
}

// Delete removes spec conformance metadata for pkg.
//
// Deleting a missing record is a no-op.
func Delete(db *gocas.DB, pkg *gocode.Package) error {
	return db.Delete(pkg, NamespaceSpec)
}

// Retrieve loads spec conformance metadata for pkg.
//
// found reports whether a record existed.
func Retrieve(db *gocas.DB, pkg *gocode.Package) (found bool, conforms bool, err error) {
	var md Metadata
	found, _, err = db.Retrieve(pkg, NamespaceSpec, &md)
	if err != nil || !found {
		return found, false, err
	}
	return true, md.Conforms, nil
}
