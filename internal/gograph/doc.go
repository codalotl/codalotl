// Package gograph builds dependency graphs for Go package-level identifiers.
//
// A graph models dependencies between declarations in one package: an edge from A to B means identifier A references identifier B. The package also records references
// to identifiers in other packages and provides queries for direct dependencies, leaves, and strongly and weakly connected components.
package gograph
