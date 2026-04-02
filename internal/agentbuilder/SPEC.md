# agentbuilder

`agentbuilder` registers our agents into `agentregistry`, allowing `agentregistry` to keep low deps.

## Agents

- generic: toolset_core
- package_mode_no_context: toolset_package
    - No built-in context, even with context builders. Callers must supply it (reason: to support TUI's eager initialcontext generation).
- package_mode_default_context: toolset_package
    - The same as package_mode_no_context, except:
    - Uses `InitialTurnsBuilder` to add with context (e.g., env and calls `initialcontext.Create`).
- limited_package_mode: toolset_limited_package
    - Uses the "Package Mode Update Usage" prompt kind
    - Uses `InitialTurnsBuilder` to add with context (e.g., env and calls `initialcontext.Create`).
- clarify_public_api: simple_read_only toolset
    - Prompt specialized for clarification requests.
    - Uses `InitialTurnsBuilder` to add sandbox/env + initial context from request path + identifier.

## Toolsets

Toolsets are just a device used in this SPEC.md file to factor this file (and may be used in non-exported code), not intended to be a public part of the API.

- toolset_edit_files:
    - when the model provider is openai: {`apply_patch`}
    - otherwise: {`write`, `edit`, `delete`}
- toolset_simple_read_only:
    - {`ls`, `read_file`}
- toolset_core:
    - {`read_file`, `ls`, `shell`, `update_plan`}
    - toolset_edit_files
- toolset_package:
    - {`read_file`, `ls`, `skill_shell`, `update_plan`}
    - toolset_edit_files
    - {`diagnostics`, `fix_lints`, `run_tests`, `run_project_tests`}
    - {`module_info`, `get_public_api`, `clarify_public_api`, `get_usage`, `update_usage`, `change_api`}
- toolset_limited_package:
    - {`read_file`, `ls`, `skill_shell`} - NOTE: no `update_plan`
    - toolset_edit_files
    - {`diagnostics`, `fix_lints`, `run_tests`} - NOTE: no `run_project_tests`
    - {`get_public_api`, `clarify_public_api`} - NOTE: no way to spawn mutative subagents, like `update_usage` and `change_api`

## Public API

```go
const (
	AgentGeneric              string = "generic"
	AgentPackageModeNoContext string = "package_mode_no_context"
)

// BuildRegistry builds the registry.
func BuildRegistry() (*agentregistry.Registry, error)
```
