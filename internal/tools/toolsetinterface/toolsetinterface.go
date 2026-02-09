// Package toolsetinterface defines shared types used to wire up toolsets without introducing import cycles.
//
// The motivating cycle is roughly:
//   - a tool (ex: update_usage) in tools/pkgtools wants to create a subagent in subagents/update_usage.
//   - the subagent wants access to package tools (ex: get_public_api) that also live in tools/pkgtools.
//
// Often this is solved by duplicating *interfaces* across packages, but these are function types. In Go, distinct named
// function types are not interchangeable even when they have the same signature. So this tiny package holds the shared types.
package toolsetinterface

import (
	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
)

// Options configures a Toolset.
type Options struct {
	// SandboxDir is the sandbox root used to resolve relative paths provided by the LLM into absolute paths. The authorizer
	// implements the actual access constraints ("jail").
	SandboxDir string

	Authorizer authdomain.Authorizer

	// GoPkgAbsDir is the absolute path of the Go package directory that package-scoped toolsets operate on. It is required only
	// for package-scoped toolsets.
	GoPkgAbsDir string

	// LintSteps are the linting steps that can be used by tools that need to check/fix formatting or other repo conventions.
	LintSteps []lints.Step
}

// Toolset returns tools configured by opts.
type Toolset func(opts Options) ([]llmstream.Tool, error)
