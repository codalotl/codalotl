// Package gorenamer applies identifier renames to Go packages.
//
// It operates on gocode.Package values and reports processed renames as successful or failed. The returned error is reserved for fatal failures that stop processing,
// so later requested renames may be absent from both result slices.
package gorenamer
