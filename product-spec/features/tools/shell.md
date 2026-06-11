# `shell`

`shell` runs a command-line program and returns its combined stdout and stderr.

## Inputs

- `command`: required array of strings; the command argv, like `["go", "test", "./..."]`.
- `timeout_ms`: optional timeout in milliseconds.
- `cwd`: optional working directory, absolute or sandbox-relative. Empty means the sandbox dir.
- `max_output_bytes`: optional byte limit for combined stdout and stderr.
- `request_permission`: optional boolean; asks the user for approval when the command needs explicit authorization.

## Output

The tool returns a structured command result with command details, process state, timeout status, duration, and output.

Output is combined stdout and stderr, subject to the configured byte limit. When output is truncated, the result keeps visible head and tail context around an elision marker.

Errors include invalid parameters, denied permissions, invalid working directories, process start failures, non-zero command exits, and timeouts.

Example output:

```text
{
  "content": "V",
  "success": true
}
Command: git status --short
Process State: exit status 0
Timeout: false
Duration: 5.706194ms
Output:
 M internal/example/example.go
?? internal/example/example_test.go
```

## Behavior

- The agent supplies a command as an argv array, preserving argument boundaries without shell-parsing a single command string.
- The command must include at least the program name.
- The tool runs the command in the sandbox dir by default.
- Relative working directories are resolved from the sandbox dir.
- If the agent does not supply a timeout, the command uses a default timeout of about 120 seconds.
- Caller-supplied output limits are clamped to static bounds.
- A command that exits non-zero or times out is still an ordinary agent-observed command outcome.

## Presentation

Example display while running:

```text
• Running go test ./...
```

Example display after completion:

```text
• Ran go test ./...
  └ ok   github.com/codalotl/codalotl/internal/agent
    ok   github.com/codalotl/codalotl/internal/tools/coretools
```

The summary uses the command argv, not cwd, timeout, or permission metadata.

Completed output is shown as a short body. If the command output is longer than the presentation limit, the body shows the first output lines and an omitted-line count, like:

```text
• Ran go test ./...
  └ ok   github.com/codalotl/codalotl/internal/agent
    ok   github.com/codalotl/codalotl/internal/tools/coretools
    ... +8 lines
```

The full agent-facing result may contain more command output than the human-facing presentation.

## Permissions

Shell commands are authorized before they run.

In the normal sandbox policy, the working directory must be inside the sandbox root. Safe commands may run automatically, blocked commands are denied, and dangerous or inscrutable commands may require user approval.

Package mode does not expose this generic raw shell tool. Package-mode command execution should use Go-aware tools or skill-backed command execution when those tools are available.
