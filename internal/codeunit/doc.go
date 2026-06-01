// Package codeunit defines filesystem-backed code units: named sets of files rooted at a base directory.
//
// A code unit answers whether a file or directory belongs to the unit and can list the files it contains. It is useful for limiting tools, such as LLM agents, to
// a coherent part of a repository.
//
// For Go code, a package is often a good unit, but some workspaces are better modeled as a package subtree, such as a main package with supporting internal packages.
// DefaultGoCodeUnit provides shared default rules for common subtree-oriented Go package work.
package codeunit
