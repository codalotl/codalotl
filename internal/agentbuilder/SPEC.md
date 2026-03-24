# agentbuilder

`agentbuilder` registers our agents into `agentregistry`, allowing `agentregistry` to keep low deps.

## Agents

- generic: core_agent toolset
- package_mode: package_agent toolset
    - No built-in context, even with context builders. Callers must supply it (reason: to support TUI's eager initialcontext generation).
- (other agents: will build later)

## Toolsets

Toolsets are just a device used in this SPEC.md file to factor this file (and may be used in non-exported code), not intended to be a public part of the API.

- edit_files:
    - when the model provider is openai: {`apply_patch`}
    - otherwise: {`write`, `edit`, `delete`}
- simple_read_only:
    - `ls`, `read_file`
- core_agent:
    - `read_file`, `ls`, `shell`, `update_plan`
    - the edit_files set
- package_agent:
    - `read_file`, `ls`, `skill_shell`, `update_plan`
    - the edit_files set
    - `diagnostics`, `fix_lints`, `run_tests`, `run_project_tests`
    - `module_info`, `get_public_api`, `clarify_public_api`, `get_usage`, `update_usage`, `change_api`
- limited_package_agent:
    - `read_file`, `ls`, `skill_shell`
    - the edit_files set
    - `diagnostics`, `fix_lints`, `run_tests`
    - `get_public_api`, `clarify_public_api`
    - (NOTE: no way to spawn mutative subagents, like `update_usage` and `change_api`)

## Public API

```go
const (
	AgentGeneric     string = "generic"
	AgentPackageMode string = "package_mode"
)

// BuildRegistry builds the registry.
func BuildRegistry() (*agentregistry.Registry, error)
```
