// Package cas provides a filesystem-backed, content-addressed metadata cache.
//
// It is designed for storing small JSON metadata records keyed by a content-derived hash. Callers compute a hash (from bytes, or from a set of files), then store
// and later retrieve metadata under (namespace, hash). If the underlying content changes, the hash changes and the cached record is naturally missed.
//
// Namespaces separate different kinds/versions of metadata (for example, "securityreview-1.0" or "docaudit-1.2") and should be filesystem-safe.
//
// Storage is rooted at DB.AbsRoot and uses a sharded directory structure:
//
//	<AbsRoot>/<namespace>/<hash[0:2]>/<hash[2:]>
//
// Records are written as JSON and are intended to be Git-friendly: merge conflicts occur only when different metadata is written for the same (namespace, hash).
package cas
