# agentbuilder

`agentbuilder` registers our agents into `agentregistry`, allowing `agentregistry` to keep low deps.

## Agents

- generic: core_agent toolset
- package_mode: package_agent toolset
- (other agents: will build later)

## Toolsets

Toolsets are just a device used in this SPEC.md file to factor this file (and may be used in non-exported code), not intended to be a public part of the API.

- edit_files:
    - when the model provider is openai: {`apply_patch`}
    - otherwise: {`write`, `edit`, `delete`}
- simple_read_only:
    - `ls`, `read_file`
- core_agent:
    - `read_file`, `ls`
    - the edit_files set
    - `shell`, `update_plan`
- package_agent:
    - `read_file`, `ls`
    - the edit_files set
    - `skill_shell`, `update_plan`
    - `diagnostics`, `fix_lints`, `run_tests`, `run_project_tests`
    - `module_info`, `get_public_api`, `clarify_public_api`, `get_usage`, `update_usage`, `change_api`
- limited_package_agent:
    - `read_file`, `ls`
    - the edit_files set
    - `skill_shell`
    - `diagnostics`, `fix_lints`, `run_tests`
    - `get_public_api`, `clarify_public_api`

## Public API

```go

const (
    AgentGeneric string = "generic"
    AgentPackageMode string = "package_mode"
)

// BuildRegistry builds the registry.
func BuildRegistry() (*agentregistry.Registry, error)
```
