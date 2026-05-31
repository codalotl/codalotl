// Package initialcontext builds an initial context bundle for an LLM working on a Go package.
//
// The bundle includes package metadata, a directory listing, package-level source summaries, sufficiently small test summaries, packages that import the target
// package, and current diagnostics, test, and lint status. Call Create to generate the bundle. When checks are skipped, diagnostics, tests, lints, and used-by lookup
// are not run, and the corresponding sections report that status.
package initialcontext
