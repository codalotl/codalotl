# `codalotl_cli`

`codalotl_cli` runs selected `codalotl` CLI commands without using a raw shell.

It is meant for agent-safe product workflows such as documentation, SPEC status, and CAS maintenance, where the agent needs the same behavior a user would get from the CLI but only for a whitelisted command set.

## Inputs

- `subcommand`: required string; command path after `codalotl`.
- `argv`: required array of strings, or null. Null behaves like an empty array.

For example:

```json
{
  "subcommand": "docs add",
  "argv": ["--public-only", "internal/cli"]
}
```

## Output

The tool returns a JSON result with:

- `success`: whether the command exit code is 0.
- `command`: the full command vector, starting with `codalotl`.
- `exit_code`: process-style exit code.
- `stdout`: captured standard output.
- `stderr`: captured standard error.

Non-zero command exits are ordinary command results rather than tool infrastructure failures.

Errors include malformed tool parameters, command-tree construction failures, and rejected commands outside the whitelist.

## Behavior

- The agent supplies a `subcommand` string and an `argv` array.
- `subcommand` is the command path after `codalotl`, such as `docs add` or `cas ls-packages`.
- `argv` contains flags and positional arguments for that subcommand.
- Argument boundaries are preserved. The tool does not shell-parse one combined command string.
- The tool runs an in-process Codalotl command tree rather than execing a `codalotl` binary.
- The supplied command tree is the whitelist. Commands outside that tree are rejected as CLI usage errors.
- The whitelisted product command set includes:
    - `codalotl docs add`
    - `codalotl docs fix`
    - `codalotl docs status`
    - `codalotl spec status`
    - `codalotl cas ls-packages`
    - `codalotl cas recertify`
- `subcommand: "help"` and `subcommand: "--help"` print a catalog of whitelisted leaf commands.
- Passing `--help` in `argv` prints detailed help for the selected command.
- Help output presents commands as `codalotl ...`.
- Command stdout and stderr are captured separately.
- Command stdout is also streamed as visible tool output while the command runs when the agent runtime supports display-only tool output.
- Command stderr is captured for the agent result; it is not streamed as visible output.

## Presentation

Example display while running:

```text
• Running codalotl docs add --public-only internal/cli
```

Example display after completion:

```text
• Ran codalotl docs add --public-only internal/cli
```

The summary shows the `codalotl` command assembled from `subcommand` and `argv`. Arguments are shell-quoted when needed for readable presentation.

The presenter does not duplicate full captured stdout or stderr in the completion body. Visible stdout streaming, when available, owns user-facing command output while the command runs.

## Permissions

`codalotl_cli` does not expose arbitrary shell access. It can only invoke commands in its whitelisted Codalotl command tree.

The effects of a command follow the underlying command's product behavior. For example, documentation commands may edit Go files, status commands may inspect repository state, and CAS commands may read or write CAS files according to the CAS feature rules.
