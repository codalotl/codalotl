# packagemode

The packagemode package is a SubAgent that runs any package-jailed agent with any package tool set.

Based on the configured tools, it can be used to create agents with various capabilities:
- `toolsets.LimitedPackageAgentTools`: can be used for `update_usage` - making any sort of small, mechanical change to a package, without recursively spawning `update_usage` or other SubAgents.
- `toolsets.PackageAgentTools`: spawn a full package mode subagent.

## Tools

The toolset will be injected. It is expected to be `toolsets.PackageAgentTools`.

## Public API

```go {api}
// Run runs an agent with the given instructions and tools on a specific package.
//
// It returns the agent's last message. An error is returned for invalid inputs, failure to communicate with the LLM, etc. If the LLM can't make the updates as per
// instructions, it may say so in its answer, which doesn't produce an error.
//
//   - authorizer is a code unit authorizer.
//   - goPkgAbsDir is the absolute path to a package.
//   - toolset are the tools available for use (injected to cut dependencies).
//   - instructions must contain enough information for an LLM to update the package (it won't have the context of the calling agent).
//   - lintSteps controls lint checks in initial context collection and lint-aware tools.
//
// Example instructions: "Update the package add a IsDefault field to the Configuration struct."
func Run(ctx context.Context, agentCreator agent.AgentCreator, authorizer authdomain.Authorizer, goPkgAbsDir string, toolset toolsetinterface.Toolset, instructions string, lintSteps []lints.Step, promptKind prompt.GoPackageModePromptKind) (string, error)
```
