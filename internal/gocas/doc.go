// Package gocas stores Go package and code-unit metadata in content-addressable storage.
//
// It wraps the generic CAS database with Go-specific key derivation, namespace versioning, root selection, JSON value storage, and best-effort provenance metadata.
// Hashes can be based on package files or on a package's default code unit.
//
// The package selects CAS roots from CODALOTL_CAS_DB or from .codalotl/cas under the nearest git root. It also provides package recertification and pruning helpers
// for maintaining Go-aware CAS records.
package gocas
