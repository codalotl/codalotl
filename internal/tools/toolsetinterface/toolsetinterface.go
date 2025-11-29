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
	"github.com/codalotl/codalotl/internal/tools/auth"
)

// Toolset is a function that returns tools.
//
// sandboxDir is an absolute path that represents the "jail" that the agent runs in. However, it's `authorizer` that actually
// **implements** the jail. The purpose of accepting sandboxDir here is so that relative paths received by the LLM can be made absolute.
type Toolset func(sandboxDir string, authorizer auth.Authorizer) ([]llmstream.Tool, error)

// PackageToolset is a function that returns tools for that operate on a package located at goPkgAbsDir.
//
// Note that this set of tools requires two authorizers:
//   - authorizer is the package-jail authorizer that prevents the agent from directly accessing files outside the package.
//   - sandboxAuthorizer is the sandboxDir jail. This comes into play when for tools designed to operate outside the package. Notably, `clarify_public_api`, `update_usage`, etc.
//
// sandboxDir is simply the absolute path that relative paths received by the LLM are relative to. It is NOT the package jail dir.
type PackageToolset func(sandboxDir string, authorizer auth.Authorizer, sandboxAuthorizer auth.Authorizer, goPkgAbsDir string) ([]llmstream.Tool, error)
