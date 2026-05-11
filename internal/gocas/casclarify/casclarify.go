// Package casclarify stores clarify_public_api answers in Go CAS metadata.
package casclarify

import (
	"github.com/codalotl/codalotl/internal/gocas"
	"github.com/codalotl/codalotl/internal/gocode"
)

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

// Append stores entry alongside any existing entries for pkg.
func Append(db *gocas.DB, pkg *gocode.Package, entry Entry) error {
	_, metadata, err := Retrieve(db, pkg)
	if err != nil {
		return err
	}

	metadata.Entries = append(metadata.Entries, entry)
	return db.StoreOnPackage(pkg, Namespace, metadata)
}

// Retrieve loads clarify metadata for pkg.
//
// found reports whether a record existed.
func Retrieve(db *gocas.DB, pkg *gocode.Package) (found bool, metadata Metadata, err error) {
	found, _, err = db.RetrieveOnPackage(pkg, Namespace, &metadata)
	if err != nil {
		return false, Metadata{}, err
	}

	return found, metadata, nil
}
