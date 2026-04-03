// Package agentbuilder registers built-in agents and tools into agentregistry and can extend a registry from YAML files.
//
// BuildRegistry installs these built-in agents:
//   - generic: generic agent with read/write/planning tools.
//   - package_mode_no_context: package-mode agent with the full package toolset, but no precomputed package context.
//   - package_mode_default_context: package-mode agent with the full package toolset plus env and initial package context.
//   - limited_package_mode: package-mode agent with a narrower toolset for targeted package work.
//   - clarify_public_api: read-only agent that explains public API docs for one identifier.
//   - pr-orchestrator: generic agent that reviews a branch and delegates package implementation work.
//
// AddYAMLToRegistry loads YAML with top-level `agents` and `tools` arrays.
//
// Each `agents` entry includes:
//   - `name`: agent name.
//   - `prompts`: ordered prompt blocks; each item sets exactly one of `name`, `file`, or `text`.
//   - `tools`: ordered tool names. The virtual tool `edit_files` expands to the provider-specific edit tools.
//   - `mode`: `generic` or `package`.
//   - optional `include_package_mode_context`: only for package-mode agents; adds env plus initial package context.
//   - optional `skills`: defaults to true; requires `shell` or `skill_shell`.
//
// Each `tools` entry includes:
//   - `name`, `description`, and `parameters`.
//   - exactly one of `command` or `subagent`.
//   - `command` defines `cmd`, optional templated `args`, and optional templated `cwd`.
//   - `subagent` defines target agent `name`, templated `message`, optional `package`, and optional `package_restrictions`.
//
// Templated YAML fields can reference tool parameters plus calling-context values such as `sandbox_dir` and `package_dir`.
package agentbuilder
