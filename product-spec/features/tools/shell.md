# `shell`

`shell` lets a generic agent run a command-line program and inspect its combined stdout and stderr.

## Availability

- Available in generic agents.
- Not available as the generic raw shell tool in package-mode agents.
- Not available to read-only helper agents unless their toolset explicitly includes shell access.

## Behavior

- The agent supplies a command as an argv array, preserving argument boundaries without shell-parsing a single command string.
- The command must include at least the program name.
- The tool runs the command in the sandbox dir by default.
- The agent can supply a working directory, absolute or sandbox-relative. Relative working directories are resolved from the sandbox dir.
- The agent can supply a timeout. If omitted, the command uses a default timeout of about 120 seconds.
- The tool captures combined stdout and stderr.
- The returned output is byte-limited. By default, the limit is 40,000 bytes; caller-supplied limits are clamped to static bounds.
- When output is truncated, the tool preserves head and tail context around a visible elision marker, without splitting UTF-8.
- A command that exits non-zero or times out is reported to the agent as a tool-level failure result, but it is still an ordinary agent-observed command outcome.

## Inputs

- `command`: required array of strings; the command argv, like `["go", "test", "./..."]`.
- `timeout_ms`: optional timeout in milliseconds.
- `cwd`: optional working directory, absolute or sandbox-relative. Empty means the sandbox dir.
- `max_output_bytes`: optional byte limit for combined stdout and stderr.
- `request_permission`: optional boolean; asks the user for approval when the command needs explicit authorization.

## Output

The tool returns a structured result indicating whether the command exited successfully and a text content block with command details.

The content includes the command, process state, whether the command timed out, duration, and output. Output is combined stdout and stderr, subject to the configured byte limit.

Errors include invalid parameters, denied permissions, invalid working directories, process start failures, non-zero command exits, and timeouts.

## Presentation

Human-facing output presents the command as a replace-style tool presentation.

While running:

```text
• Running go test ./...
```

On completion:

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

The full agent-facing result may contain more command output than the human-facing presentation, up to the tool's byte limit.

## Permissions

Shell commands are authorized before they run.

In the normal sandbox policy, the working directory must be inside the sandbox root. Safe commands may run automatically, blocked commands are denied, and dangerous or inscrutable commands may require user approval.

`request_permission` lets the agent ask for explicit approval when it knows a command is dangerous, sensitive, or materially operates outside the ordinary automatic authorization boundary.

Package mode does not expose this generic raw shell tool. Package-mode command execution should use Go-aware tools or skill-backed command execution when those tools are available.
