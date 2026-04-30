# cli

`cli` provides the `codalotl_cli` tool: an agent-facing wrapper around selected codalotl CLI commands.

The tool runs an in-process, whitelisted `internal/q/cli` command tree. It does not exec a `codalotl` binary, so it works in tests, local `go run` workflows, and uninstalled development checkouts.

## Behavior

- Tool name: `codalotl_cli`.
- Params:
	- `subcommand string`: command path after `codalotl`, such as `context initial` or `docs add`.
	- `argv []string`: flags and args for the subcommand.
- Tool schema marks both params required. Null `argv` behaves like empty argv.
- `argv` preserves argument boundaries; the tool does not shell-parse an args string.
- `subcommand` names command-path tokens. Flags and positional args belong in `argv`.
- Empty `subcommand` is a usage error.
- `subcommand: "help"` and `subcommand: "--help"` render a leaf-command catalog for whitelisted commands.
- Per-command `--help` renders detailed q/cli help for that command.
- Help output always presents commands as `codalotl ...`, never as package-internal names.
- Built-in `-h`/`--help` is supported but not listed as an ordinary option.
- Only commands present in the supplied command tree are invokable.
- Command stdout and stderr are captured separately and returned to the LLM.
- Context cancellation is propagated to command handlers.

## Whitelisted Command Tree

Callers supply a command-tree factory. Each invocation uses a fresh command tree.

The supplied tree is the whitelist. The package does not know about `internal/cli` or decide which codalotl commands belong in the tool.

Command handlers intended for `codalotl_cli` write user-visible output through `qcli.Context.Out`.

## Result

Tool result is JSON:

```json
{
  "success": true,
  "command": ["codalotl", "context", "initial", "internal/cli"],
  "exit_code": 0,
  "stdout": "...",
  "stderr": ""
}
```

`success` is true only for exit code 0. Non-zero command exits are reported in `Result`, not as tool infrastructure errors.

## Presentation

Presentation:
- In progress: `Running codalotl docs add --public-only internal/cli`
- Complete: `Ran codalotl docs add --public-only internal/cli`

The presenter does not duplicate full command output. Output is for the LLM result; streaming display, when available, owns user-visible stdout.

## Public API

```go
const ToolNameCodalotlCLI = "codalotl_cli"
```

```go
// CommandTreeFunc returns a fresh whitelisted codalotl command tree.
type CommandTreeFunc func() *qcli.Command

// NewCodalotlCLITool creates the codalotl_cli tool.
func NewCodalotlCLITool(newCommandTree CommandTreeFunc) llmstream.Tool
```

```go
// Params are the codalotl_cli tool parameters.
type Params struct {
	Subcommand string   `json:"subcommand"`
	Argv       []string `json:"argv"`
}
```

```go
// Result is the machine-readable codalotl_cli tool result.
type Result struct {
	Success  bool     `json:"success"`
	Command  []string `json:"command"`
	ExitCode int      `json:"exit_code"`
	Stdout   string   `json:"stdout"`
	Stderr   string   `json:"stderr"`
}
```
