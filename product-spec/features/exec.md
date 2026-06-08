# exec

`codalotl exec` runs Codalotl's non-interactive agent once from the CLI, without launching the TUI.

It is for headless and scriptable use: quick repository questions, one-shot code changes, CI-like workflows, and terminal sessions where the user wants streamed agent output but no interactive chat UI.

## CLI

### codalotl exec [--package <path/to/pkg>] [--yes] [--no-color] [--json] [--model <id>] [--slash-command <cmd>] [<prompt> ...]

Runs one noninteractive agent turn.

Examples:

```bash
codalotl exec "Summarize this repository"
codalotl exec -p ./internal/cli "fix the failing test"
codalotl exec --json --model gpt-5.5-high "explain recent changes"
codalotl exec --yes --slash-command="/orchestrate" "implement the PR file"
```

`<prompt> ...` is joined into the end-user message sent to the agent. It is required unless `--slash-command` starts a session that can run without an initial prompt.

Flags:
- `-p, --package <path/to/pkg>`: run in package mode for one Go package. Package path follows `features/cli.md` package argument semantics and must resolve inside the sandbox dir.
- `-y, --yes`: auto-approve permission checks for this run.
- `--no-color`: disable ANSI formatting in human-readable output.
- `--json`: output newline-delimited JSON instead of human-readable terminal output.
- `--model <id>`: override configured preferred model for this run.
- `--slash-command <cmd>`: apply a TUI-style slash command at session start.

Supported slash commands (leading `/` is optional):
- `/orchestrate`

`--slash-command=orchestrate` starts the built-in PR orchestrator flow, matching TUI `/orchestrate`. It may run with or without an explicit prompt. Package mode does not apply to orchestrator startup.

## Permission Checks

`exec` never pauses for interactive permission input. Permission checks are decided automatically:
- `--yes` approves them for that run.
- `autoyes: true` in config approves them by default.
- Otherwise, permission checks are denied.

This makes `exec` usable in scripts and pipes without hidden prompts.

## Output

Human-readable mode streams agent text, tool progress, warnings, retries, and final status to stdout. It is meant for terminal users, not stable machine parsing.

On a successful run, `exec` prints a final completion line with token usage, like:

```text
Agent finished the turn. Tokens: input=10042 cached_input=32000 output=1043 total=43085
```

`--no-color` keeps this output plain.

### JSON

`--json` emits newline-delimited JSON: one event object per line, no surrounding array.

JSON mode is a stable structured stream for tools. It includes events like:
- `start`
- `user_message`
- `assistant_text`
- `assistant_reasoning`
- `tool_call`
- `tool_complete`
- `tool_output`
- `permission`
- `warning`
- `retry`
- `error`
- `canceled`
- `done`

`start` includes the effective cwd, package path if any, and model ID. Terminal event is `done`, `error`, or `canceled`.

## Exit Status

`exec` exits zero when the noninteractive run completes successfully.

It exits non-zero for usage errors, startup validation errors, cancellation, or unhandled agent failures such as persistent LLM/provider errors.

Many ordinary agent-observed failures, like a shell command exiting non-zero or a missing file read, are reported to the agent and do not by themselves make `codalotl exec` fail.
