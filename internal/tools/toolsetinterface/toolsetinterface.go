// toolsetinferface is designed to cut dependencies:
//   - a tool (ex: update_usage) in tools/pkgtools wants to create a subagent in subagents/update_usage.
//   - the subagent wants access to package tools (ex: get_public_api) in tools/pkgtools.
//   - Thus, a cycle.
//   - Often, I cut dependencies by duplicating interface types. But these aren't interfaces. It turns out that func types below cannot
//     be interchanged with separate types of the exact same shape, unlike interfaces.
//   - Therefore, we have this little package toolsetinterface with these shared types. (Yes, I could have made these interfaces instead)
package toolsetinterface

import (
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
)

// Toolset is a function that returns tools.
//
// sandboxDir is an absolute path that represents the "jail" that the agent runs in. However, it's `authorizer` that actually
// **implements** the jail. The purpose of accepting sandboxDir here is so that relative paths received by the LLM can be made absolute.
type Toolset func(sandboxDir string, authorizer authdomain.Authorizer) ([]llmstream.Tool, error)

// PackageToolset is a function that returns tools for that operate on a package located at goPkgAbsDir.
//
// Note that the package-jail authorizer prevents the agent from directly accessing files outside the package.
// Tools that need broader sandbox access should derive it from authorizer.WithoutCodeUnit().
//
// sandboxDir is simply the absolute path that relative paths received by the LLM are relative to. It is NOT the package jail dir.
type PackageToolset func(sandboxDir string, authorizer authdomain.Authorizer, goPkgAbsDir string) ([]llmstream.Tool, error)
