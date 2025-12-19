# updateusage

The updateusage package is a SubAgent that is intended to update the usage of some other upstream package's API, potentially because that package's API has changed in some way. In general, it is capable of making any sort of small, mechanical change to a package, that shouldn't require propagating changes to other packages. Other uses that I can imagine:
- Apply some coding standard to a package (ex: don't use filepath.Clean; follow these comment conventions).
- Use a particular helper package that is available in our repo (ex: use testify; use the fastjson package instead of json).

Long-term, this package should be usable in all languages. But today, it is only usable in Go.

## Tools

The toolset will be injected. It is expected to be `toolsets.LimitedPackageAgentTools` (i.e., most core tools but limited package tools). Notably, `update_usage` is missing.

## Prompt and Context

- Prompt: the generic prompt our agents use (codeai/prompt package).
- Initial context: `codeai/initialcontext.Create`.

## Public API

```go {api exact_docs}
// UpdateUsage updates a package according to the given instructions. It returns the agent's last message. An error is returned for invalid inputs, failure to communicate with the LLM, etc.
// If the LLM can't find the make the updates as per instructions, it may say so in its answer, which doesn't produce an error.
//   - sandboxAbsDir is used for tool construction and relative path resolution, not as a confinement mechanism.
//   - authorizer is optional. If present, it confines the SubAgent in some way (usually to a sandbox dir of some kind).
//   - goPkgAbsDir is the absolute path to a package.
//   - toolset toolsetinterface.Toolset are the tools available for use. Injected to cut dependencies. Should be ls/read_file.
//   - instructions must contain enough information for an LLM to update the package (it won't have the context of the calling agent). The instructions should often have **selection** instructions:
//     update this package IF it uses Xyz function.
//
// Example instructions: "Update the package to use testify."
func UpdateUsage(ctx context.Context, agentCreator agent.AgentCreator, sandboxAbsDir string, authorizer authdomain.Authorizer, goPkgAbsDir string, toolset toolsetinterface.PackageToolset, instructions string) (string, error)
```
