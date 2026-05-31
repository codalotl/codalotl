// Package gorenamer applies identifier renames to Go packages.
//
// It operates on gocode.Package values and reports each requested rename as either successful or failed, reserving the returned error for fatal failures that prevent
// processing.
package gorenamer
