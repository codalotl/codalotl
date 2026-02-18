# toolsets

The toolsets package offers bundles of tool configurations that agents/subagents can use.

## Public API

```go
// Options configures the toolset returned by the functions in this package.
//
// This is an alias to toolsetinterface.Options so that:
//   - external callers can depend on toolsets.Options, and
//   - toolsets.* functions can be passed directly as toolsetinterface.Toolset values.
type Options = toolsetinterface.Options

// CoreAgentTools offers tools similar to a Codex-style agent: read_file, ls, apply_patch, shell, and update_plan.
//
// sandboxDir is an absolute path that represents the "jail" that the agent runs in. However, it's authorizer that actually implements the jail. The purpose of accepting
// sandboxDir here is so that relative paths received by the LLM can be made absolute.
func CoreAgentTools(opts Options) ([]llmstream.Tool, error)

// PackageAgentTools offers tools that jail an agent to one code unit (in Go, typically a package), located at goPkgAbsDir:
//   - core tools: read_file, ls, apply_patch, skill_shell, update_plan
//   - extended tools: diagnostics (i.e., typecheck errors/build errors), fix_lints, run_tests, run_project_tests
//   - package tools: module_info, get_public_api, clarify_public_api, get_usage, update_usage, change_api
//
// Note that this set of tools requires a package-jail authorizer that prevents the agent from directly accessing files outside the package. Tools that need broader
// sandbox access derive it via authorizer.WithoutCodeUnit().
//
// sandboxDir is simply the absolute path that relative paths received by the LLM are relative to. It is NOT the package jail dir.
func PackageAgentTools(opts Options) ([]llmstream.Tool, error)

// SimpleReadOnlyTools offers ls and read_file. It can excel at a small research task (ex: clarifying documentation inside a package).
//
// sandboxDir is an absolute path that represents the "jail" that the agent runs in. However, it's authorizer that actually implements the jail. The purpose of accepting
// sandboxDir here is so that relative paths received by the LLM can be made absolute.
func SimpleReadOnlyTools(opts Options) ([]llmstream.Tool, error)

// LimitedPackageAgentTools offers more limited tools than PackageAgentTools that jail an agent to one code unit (in Go, typically a package), located at goPkgAbsDir:
//   - core tools: read_file, ls, apply_patch, skill_shell (not included: update_plan)
//   - extended tools: diagnostics (i.e., typecheck errors/build errors), fix_lints, run_tests (not included: run_project_tests)
//   - package tools: get_public_api, clarify_public_api (not included: get_usage, update_usage)
//
// These tools cannot spawn write-mode subagents that escape the original goPkgAbsDir (but they can spawn subagents with read access - e.g., clarify_public_api).
// They are intended to be used for subagents running update_usage. In other words, they target small, simple, mechanical code changes on a single package.
//
// See PackageAgentTools for other param descriptions.
func LimitedPackageAgentTools(opts Options) ([]llmstream.Tool, error)
```
