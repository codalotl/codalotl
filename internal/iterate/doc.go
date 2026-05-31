// Package iterate runs prompt-driven workflows step by step until iteration policy selects a stop condition.
//
// It coordinates stateful Runner implementations while owning retry handling, stop limits, continuation decision parsing, decision prompting, and fresh versus resumed
// session selection. The package does not print output or depend on a specific execution backend.
package iterate
