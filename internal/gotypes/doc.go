// Package gotypes loads and exposes go/types facts for a single package.
//
// The package provides TypeInfo, a read-only snapshot of type-checker results (ex: Uses, Defs, Types, Selections) keyed
// by the package's current FileSet and AST, and LoadTypeInfoInto, which populates that snapshot by invoking golang.org/x/tools/go/packages.
//
// Loading replaces the target package's FileSet and AST with those created by packages.Load so that the go/types.Info maps
// are keyed by the corresponding nodes. Callers should treat the resulting TypeInfo as immutable. Set includeTests to true
// to load and type-check test files alongside the package.
package gotypes
