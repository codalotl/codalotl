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

## Data-Driven Agent/Tool Construction

YAML files can construct agents and tools, which can be added to the registry. Top-level keys: `agents` and `tools`, both arrays.

Agents:
- An agent object has 4 required fields: `name`, `prompts`, `tools`, and `mode`.
- `name` is the agent name.
- `prompts` is an array (each concatenated once resolved). Each element:
    - An object with one of three fields set (`name`, `file`, or `text`).
    - `name`: refers to an existing prompt. Built-in options are `base` and `package-base`, referring to the `generic` agent's prompt, and the `package_mode_no_context` agent's prompt, respectively.
    - `file`: refers to a textual file (usually a `.md` file) relative to the YAML file, which is read.
    - `text`: just use this text directly.
- `tools` is an array of strings. Each element can refer to an existing tool in the registry (ex: `ls`), or a new tool defined by the YAML file itself. Exactly one "virtual" tool: the `edit_files` tool refers to the `toolset_edit_files` tools.
- `mode` is one of `generic` or `package`.
- `include_package_mode_context` of true includes package env and `initialcontext.Create` (optional; only value if `mode` is `package`).

Tools:
- A tool must have `name`, `description`, `parameters`, and then one of {`command`, `subagent`}.
- `name` is the tool name.
- `description` is the tool description (this is sent to the LLM as the tool description).
- `parameters` is an object, which has fields that map to parameters. Each parameter must have `type` (ex: `string`), `description` (sent to LLM), and `required` (true or false). This maps to the construction of an `llmstream.ToolInfo`.
- `command` is used to map the tool to the execution of a shell command. Subfields:
    - `cmd`: the actual command to run (not including args).
    - `args`: array of strings. Each string can use Go templating.
    - `cwd`: optional. Default: the sandbox dir of callers. Can use Go templating.
- `subagent` is used to run a named agent.
    - `name`: name of the agent to use (either from this YAML file, previously added YAML files, or the base pre-installed `## Agents` above).
    - `package`: optional. If present, indicates we're using package mode. The only value supported is the name of a parameter, which is interpreted as the package to jail to.
    - `message`: Message to send. Uses Go templating.
    - NOTE: A package-mode agent must be supplied a package, and a non-package-mode agent must not be supplied a package.
- Fields compatible with Go templating are supplied:
    - parameters (e.g., a param named `path` is accessed as `{{ .path }}`).
    - Calling context:
        - `sandbox_dir`: the current sandbox dir
        - `package_dir`: the current package dir (relative to sandbox)
        - NOTE: we can add more things here as needed.

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

// AddYMLToRegistry adds agents and tools to reg based on the YAML file indicated by path. If an error occurs, reg will not be mutated.
//
// An error is returned for typical issues reading the YAML file, but also:
//   - If an agent/tool's name overwrites an existing agent/tool name.
func AddYMLToRegistry(reg *agentregistry.Registry, path string) error
```
