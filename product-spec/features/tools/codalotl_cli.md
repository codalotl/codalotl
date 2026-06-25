# `codalotl_cli`

`codalotl_cli` runs selected `codalotl` CLI commands without using a raw shell. This lets us build CLI-first functionality, and then expose it directly to the agent.

This has a number of benefits:
- Reduces the number of miscellaneous tools we need to expose to the LLM.
- Lets us implement once as a CLI command, then re-use that functionality.
- Works even when `codalotl` is being run via `go run .`.

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

While the end-user-visible output may be sanitized in various ways, the stdout field that the LLM seems should be ~complete and unaltered.

Example output:

```json
{
  "command": [
    "codalotl",
    "docs",
    "add",
    "internal/gotypes"
  ],
  "exit_code": 0,
  "stderr": "",
  "stdout": "Everything is already documented\nApplied 0 documentation change(s).\n",
  "success": true
}
```

## Behavior

- The agent supplies a `subcommand` string and an `argv` array.
- `subcommand` is the command path after `codalotl`, such as `docs add` or `cas ls-packages`.
- `argv` contains flags and positional arguments for that subcommand.
- Argument boundaries are preserved. The tool does not shell-parse one combined command string.
- The tool runs an in-process Codalotl command tree rather than execing a `codalotl` binary.
- Only certain commands are exposed. The whitelisted product command set includes:
    - `codalotl docs add`
    - `codalotl docs fix`
    - `codalotl docs status`
    - `codalotl spec status`
    - `codalotl cas ls-packages`
    - `codalotl cas recertify`
- `subcommand: "help"` and `subcommand: "--help"` print a catalog of whitelisted leaf commands.
- Passing `--help` in `argv` prints detailed help for the selected command.

## Presentation

Example display:

```text
• Running codalotl docs add --public-only internal/cli
  • Need docs for 12 identifiers
  • > Requesting docs for 12 identifiers: Builder, Config, NewRunner ...
    Got 12 snippets. 12/12 successful.
• Ran codalotl docs add --public-only internal/cli
```

Streamed output appears beneath the running command. A streamed chunk is one nested tool-output message. The chunks can be based on newlines and time. For instance, if a CLI command outputs text every few seeonds, each will get its own `•`. But if a CLI command outputs several lines instantaneously, they'll be in the same `•`.
