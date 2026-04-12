# agentbuilder

`agentbuilder` registers our default agents/tools into `agentregistry`, keeping `agentregistry` deps low. It also exposes a way to create new agents and tools via YAML files.

## Documentation

`doc.go` should contain godoc documentation that lets consumers of this package know:
- which agents are available
- structure of a YAML file.

## Agents

- generic: toolset_core
- package_mode_no_context: toolset_package
    - No built-in context, even with context builders. Callers must supply it (reason: to support TUI's eager initialcontext generation).
- package_mode_default_context: toolset_package
    - The same as package_mode_no_context, except:
    - Uses `InitialTurnsBuilder` to add env + package initial context.
    - When the target dir is an existing package-mode directory but does not yet load as a Go package, startup still succeeds with fallback package-path context.
- limited_package_mode: toolset_limited_package
    - Uses the "Package Mode Update Usage" prompt kind
    - Uses `InitialTurnsBuilder` to add env + package initial context.
    - When the target dir is an existing package-mode directory but does not yet load as a Go package, startup still succeeds with fallback package-path context.
- clarify_public_api: toolset_simple_read_only
    - Prompt specialized for clarification requests.
    - Uses `InitialTurnsBuilder` to add sandbox/env + initial context from request path + identifier.

## Data-Driven Agent/Tool Construction

YAML files can construct agents and tools, which can be added to the registry. All agents above (except clarify_public_api) must be implementable with YAML files.

Top-level keys: `agents` and `tools`, both arrays.

Agents:
- An agent object has 4 required fields: `name`, `prompts`, `tools`, and `mode`.
- `name` is the agent name.
- `prompts` is an array; resolved elements are concatenated in order. Each element:
    - An object with one of three fields set (`name`, `file`, or `text`).
    - `name`: refers to an existing prompt. Built-in options are `base`, `package-base`, and `limited-package-base`, referring to the agent prompts from `generic`, `package_mode_no_context`, and `limited_package_mode`, respectively.
    - `file`: refers to a textual file (usually a `.md` file) relative to the YAML file, which is read.
    - `text`: just use this text directly.
- `tools` is an array of strings. Each element can refer to an existing tool in the registry (ex: `ls`), or a new tool defined by the YAML file itself. Exactly one virtual tool exists: `edit_files`, which refers to the `toolset_edit_files` tools.
- `mode` is one of `generic` or `package`.
- `include_package_mode_context` set to true includes package env and package initial context (optional; only valid if `mode` is `package`).
    - If the selected dir already exists but does not yet load as a Go package, include fallback package-path context instead of failing startup.
- `skills` is an optional boolean (default true) that adds skill support to the prompt (passing appropriate shell tool). Requires the tool `shell` or `skill_shell` to be present.
- `agentsmd` is an optional boolean (default true) that adds AGENTS.md initial-turn context when present.
    - Generic-mode agents read AGENTS.md from sandbox context.
    - Package-mode agents read AGENTS.md from target package context.
    - When combined with `include_package_mode_context`, AGENTS.md text precedes generated package context.

Tools:
- A tool must have `name`, `description`, `parameters`, `presenter`, and then one of {`command`, `subagent`}.
- `name` is the tool name.
- `description` is the tool description (this is sent to the LLM as the tool description).
- `parameters` is an object, which has fields that map to parameters. Each parameter must have `type` (ex: `string`), `description` (sent to LLM), and `required` (true or false). This maps to the construction of an `llmstream.ToolInfo`.
- `presenter` is an object which configures an `llmstream.Presenter` (used to format the tool call/response in the TUI). See `### Presenters` below.
- `command` is used to map the tool to the execution of a shell command. Subfields:
    - `cmd`: the actual command to run (not including args).
    - `args`: array of strings. Each string can use Go templating.
    - `cwd`: optional. Default: the sandbox dir of callers. Can use Go templating.
- `subagent` is used to run a named agent.
    - `name`: name of the agent to use (either from this YAML file, previously added YAML files, or the base pre-installed `## Agents` above).
    - `package`: optional. If present, indicates we're using package mode. The only value supported is the name of a parameter, whose value is interpreted as the package to jail to (relative path to sandbox or Go import path).
    - `message`: shorthand for sending one templated user message.
    - `messages`: optional array of user messages. Exactly one of `message` or `messages`.
        - Each message item sets exactly one of `name`, `file`, `text`, or `command`.
        - `name`, `file`, and `text` resolve like agent prompt blocks, then are rendered with Go templating.
        - `command` runs a templated command and uses its textual output as the message body.
    - `result_format`: optional. Supported values:
        - `text` (default): return final assistant text as-is.
        - `json`: parse final assistant text as JSON and return normalized JSON; invalid JSON is a tool error.
    - `package_restrictions`: optional; only relevant for package-based subagents. Subfields (all optional; all except `require_package_mode` only apply if already in package mode):
        - `disallow_self`: disallow the same package as is currently running.
        - `relation`: optional relationship between the current package and the target package. Supported values:
            - `direct_import_of_caller`: the target package must be directly imported by the current package.
            - `direct_importer_of_caller`: the target package must directly import the current package.
        - `allow_outside_sandbox`: allows calling the tool on packages outside of the sandbox (e.g., deps from go.mod or the stdlib). Default false.
        - `require_package_mode`: require the caller be in package mode. Default false.
    - NOTE: A package-mode agent must be supplied a package, and a non-package-mode agent must not be supplied a package.
- The following fields are available to Go templating:
    - parameters (e.g., a param named `path` is accessed as `{{ .path }}`).
    - Calling context:
        - `sandbox_dir`: the current sandbox dir
        - `package_dir`: the current package dir (relative to sandbox)
        - NOTE: we can add more things here as needed.

### Presenters

Currently, only "presets" are supported (in the future, we can add a more generalized solution if we want to). A preset has a name, and then each preset defines its own "arguments" that apply to it. For instance, if you use `name: subagent_q_and_a`, you must supply a `call_action` field indicating the action verb (among other arguments).

#### Preset: subagent_q_and_a

This preset is used for Q and A calls, where a subagent is invoked with a "question" and returns an "answer". It might be displayed in the TUI as:

```
• Implementing in path/to/pkg
  └ Add a feature that...
    Also don't forget to...
  • (... various subagent events ...)
• Implemented in path/to/pkg
  └ I finished adding the..
    I did not forget to...
```

Example YAML config:

```yaml
presenter:
  preset:
    name: subagent_q_and_a
    call_action: Implementing
    result_action: Implemented
    summary_items:
      - text: in
      - param: path
    call_body: instructions
    result_body: result.last_message
```

Notes:
- Behavior is always `CompletionBehaviorAppend`.
- The summary line is always joined with spaces.
- `call_action` and `result_action` are required. The verb used in the summary line (`RoleAction`).
- `summary_items` is an optional array of objects. Added after the verb.
    - Each object either has a `text` or `param` key.
    - text's value is used verbatim. Gets `RoleAccent`. 
    - param's value must match a parameter. Gets `RoleNormal`.
- `call_body` is required. Value must either be a named parameter, or `-` for no body.
- `result_body` is required. Value must be one of `result` (which displays `ToolResult.Result`), or `-` for no body.

## Toolsets

Toolsets are just a device used in this SPEC.md to factor the file (and may be used in non-exported code), not intended to be a public part of the API.

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
// BuildRegistry builds the registry.
func BuildRegistry() (*agentregistry.Registry, error)

// AddYAMLToRegistry adds agents and tools to reg based on the YAML file at path. If an error occurs, reg will not be mutated.
//
// Errors are returned for typical issues reading the YAML file, and also:
//   - If an agent/tool's name overwrites an existing agent/tool name.
func AddYAMLToRegistry(reg *agentregistry.Registry, path string) error
```
