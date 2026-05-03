// Package agentbuilder registers built-in agents and tools into agentregistry and can extend a registry from YAML files.
//
// BuildRegistry installs these built-in agents:
//   - generic: generic agent with read/write/planning tools plus optional externally supplied tools such as `codalotl_cli`.
//   - package_mode_no_context: package-mode agent with the full package toolset, but no precomputed package context.
//   - package_mode_default_context: package-mode agent with the full package toolset plus env and initial package context.
//   - limited_package_mode: package-mode agent with a narrower toolset for targeted package work.
//   - clarify_public_api: read-only agent that explains public API docs for one identifier.
//   - improve_public_api_docs: package-mode agent that may improve public API docs after a clarification answer.
//   - pr-review: generic review agent that emits structured JSON review findings.
//   - pr-orchestrator: generic agent that reviews a branch, reviews SPEC.md edits, and delegates package implementation work.
//
// OverrideTool installs a process-wide named tool builder that future BuildRegistry calls register before YAML agents are loaded.
//
// AddYAMLToRegistry loads YAML with top-level `agents` and `tools` arrays.
//
// Each `agents` entry includes:
//   - `name`: agent name.
//   - `prompts`: ordered prompt blocks; each item sets exactly one of `name`, `file`, or `text`.
//   - `tools`: ordered tool names. The virtual tool `edit_files` expands to the provider-specific edit tools. Allowlisted external tools such as `codalotl_cli` may
//     appear without a registered builder; missing allowlisted tools are omitted while other missing tools are errors.
//   - `mode`: `generic` or `package`.
//   - optional `include_package_mode_context`: only for package-mode agents; adds env plus initial package context.
//   - optional `skills`: defaults to true; requires `shell` or `skill_shell`.
//   - optional `agentsmd`: defaults to true; adds AGENTS.md initial-turn context from the sandbox or target package.
//
// Each `tools` entry includes:
//   - `name`, `description`, and `parameters`.
//   - optional `presenter`, which currently supports preset-based semantic formatting for tool call/result display.
//   - exactly one of `command` or `subagent`.
//   - `command` defines `cmd`, optional templated `args`, and optional templated `cwd`.
//   - `subagent` defines target agent `name`, either templated `message` or `subagent.messages`, optional `package`, optional `result_format`, and optional `package_restrictions`.
//   - `subagent.messages` is an ordered list of user messages. Each item uses exactly one of `name`, `file`, `text`, or `command`.
//   - `name`, `file`, and `text` message blocks are resolved to text first, then rendered with the same template data as command tools.
//   - `command` message blocks run a templated command and use its textual output as the message body.
//   - `result_format: json` parses the final assistant text as JSON and returns normalized JSON; `text` is the default.
//   - the preset `subagent_q_and_a` renders Q-and-A style subagent tools using `call_action`, `result_action`, optional `summary_items`, `call_body`, and `result_body`.
//   - the preset `review` renders the built-in review tool with fixed `base`-driven summaries and concise finding titles.
//
// Templated YAML fields can reference tool parameters plus calling-context values such as `sandbox_dir` and `package_dir`.
package agentbuilder
