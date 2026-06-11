# `skill_shell`

`skill_shell` runs a shell command directed by an active skill or package workflow.

It is not a general-purpose shell exploration tool. Agents use it when a skill references a command, script, or workflow step that cannot be expressed through a more specific Codalotl tool.

## Inputs

- `command`: required argv-style command array; the first item is the executable.
- `skill`: required name of the skill that directed the command.
- `timeout_ms`: optional timeout in milliseconds; values less than or equal to zero use the default timeout.
- `cwd`: optional working directory, absolute or sandbox-relative; empty uses the sandbox dir.
- `max_output_bytes`: optional maximum bytes of combined stdout and stderr returned to the agent.
- `request_permission`: optional boolean; asks the user for approval when the command or material access is outside the current automatic authorization boundary.

## Output

On success, the tool returns a structured result indicating that the command completed successfully and includes command metadata, timeout status, duration, and combined output.

On failure, the tool returns enough information for the agent to understand whether the command failed, timed out, could not be started, exceeded authorization, or produced invalid parameters.

Output may be byte-limited. When output is limited, the result should make the elision visible rather than silently changing meaning.

Example output:

```text
{
  "content": "V",
  "success": true
}
Command: go tool cover -func=coverage.out
Process State: exit status 0
Timeout: false
Duration: 18.421ms
Output:
github.com/example/project/internal/foo/foo.go:12:	Parse		88.9%
total:						(statements)	83.2%
```

## Behavior

- The agent supplies an argv-style command and the name of the skill that directed the command.
- The command should come from a skill instruction, a script located in or referenced by a skill, or a package-mode workflow that explicitly allows skill-backed commands.
- Relative working directories are resolved from the sandbox dir.
- An empty working directory uses the sandbox dir.
- The command runs with a timeout. If the agent does not supply one, Codalotl uses a default timeout.
- The tool returns combined stdout and stderr together with command metadata.
- The tool limits very large output by preserving head and tail content around a visible elision marker.
- Purpose-built Go tools such as `run_tests`, `diagnostics`, and `fix_lints` should be used when they fit the task.

## Presentation

Example display while running:

```text
• Running go tool cover -func=coverage.out
```

Example display after completion:

```text
• Ran go tool cover -func=coverage.out
  └ total:                                      (statements)    83.2%
```

The summary uses the command argv, not the skill name, working directory, timeout, or permission metadata.

If command output has more than a few display lines, the presentation shows the first lines and an omitted-line count rather than dumping the full output into the progress stream.

## Permissions

Commands are authorized before execution.

In package mode, `skill_shell` preserves package-mode intent: the agent may run skill-referenced commands that support the selected package workflow, while ordinary code inspection, edits, tests, diagnostics, and cross-package work should continue to use package-aware tools when available.
