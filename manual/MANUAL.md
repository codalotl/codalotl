# Codalotl

Codalotl is a Go-focused coding agent with two interfaces:
- `codalotl` launches an interactive terminal UI (TUI).
- `codalotl exec` runs a single noninteractive agent turn from the command line.

It is optimized for Go package workflows: package-scoped context, package-aware tools, and automatic post-edit checks in package mode.

## Getting Started

Choose your install path:
1. Install or upgrade directly with Go (recommended):

```bash
go install github.com/codalotl/codalotl@latest
```

2. Verify the install:

```bash
codalotl version
```

3. Configure a provider key. The primary built-in provider key is OpenAI:

```bash
export OPENAI_API_KEY="sk-..."
```

4. Start the TUI:

```bash
codalotl
```

5. Enter package mode for Go-specific behavior:

```text
/package .
```

Startup validation (for most commands) requires these tools on `PATH`:
- `go`
- `gopls`
- `goimports`
- `gofmt`
- `git`

Notes:
- `codalotl -h` and `codalotl version` skip config/tool validation.
- `codalotl .` is a valid alias for starting the TUI.

## Specific Go Support

Codalotl is not just a generic shell-wrapping agent. In package mode, it changes how the agent works:

- Package-scoped context: It starts with an automatic context bundle for one package (files, signatures, importers, diagnostics, tests, lints).
- Package-scoped tools: It can inspect and modify your package directly, then use dedicated tools for upstream/downstream package work.
- API-aware cross-package workflows:
  - Upstream read: `get_public_api`, `clarify_public_api`
  - Downstream impact: `get_usage`, `update_usage`
  - Upstream changes: `change_api`
- Automatic post-patch checks in package mode: after `apply_patch`, it runs diagnostics and lint-fix steps.
- Go lint pipeline integration: defaults and custom lint steps are applied consistently in context generation and patch workflows.

The result is a tighter loop for Go package maintenance and API evolution across packages.

## Package Mode

Package mode is Go-specific session behavior focused on one package path.

Enter/exit:
- Enter: `/package path/to/pkg`
- Exit: `/package` or `/generic`

What changes in package mode:
- The session is rebuilt with package-mode prompt/tooling.
- Direct read/write access is restricted to a package-scoped code unit.
- The UI starts background collection of initial package context.

Code unit boundary behavior:
- Includes the selected package directory.
- Recursively includes subdirectories only when they do not contain `*.go` files.
- Includes `testdata` directories directly under included directories, even if `testdata` contains `*.go` fixtures.
- Nested Go packages are excluded from direct read/write.

Cross-package operations are done with package tools rather than direct file reads:
- Read external package API/docs: `get_public_api`
- Ask clarification about API/docs: `clarify_public_api`
- Find downstream usage: `get_usage`
- Update downstream packages: `update_usage`
- Change upstream package API/behavior: `change_api`

`@` mentions for extra context:
- If your prompt mentions `@some/path`, codalotl grants read access to those paths for `read_file`/`ls`, even outside the package code unit.

Path reminder:
- Tool file paths are relative to sandbox root (usually your working directory), not package-relative.

## TUI

The TUI is the interactive coding agent.

### Slash Commands

Commands:
- `/quit`, `/exit`, `/logout`: exit the TUI.
- `/new`: start a new session (keeps active package if already in package mode).
- `/skills`: list installed skills and any skill loading issues.
- `/models`: list current model and available callable models.
- `/model <id>`: switch model and start a new session.
- `/package <path>`: enter package mode.
- `/package`: leave package mode.
- `/generic`: leave package mode.

### Keyboard Input

Primary controls:
- `Enter`: send message.
- `Ctrl-J`: insert newline in input.
- `ESC`:
    - If input has text: clear input.
    - Else: stop running agent.
    - Also closes overlays/dialogs and exits history cycling states when active.
- `Ctrl-C`:
    - If agent is running: stop current run.
    - If idle: quit the TUI.
- `Up`/`Down`: cycle message history.
- `Page Up`/`Page Down`/`Home`/`End`/Mouse wheel: scroll message area.
- `Ctrl-O` or terminal double-click: toggle overlay mode.

### Details View

The info panel (right side, if width allows), shows:
- Session ID
- Model
- Current session usage (tokens, cost)
- Current package mode/path
- Version upgrade notice when available

### Overlay Mode

Enter/Exit Overlay Mode with `Ctrl-O` or by double-clicking the terminal area.

Overlay Mode reveals two buttons, appearing below certain messages/tool calls in the Messages Area:

1. `copy`: lets you copy message and text from the TUI. The current workaround for being unable to select text.
2. `details`: shows a dialog with raw tool input/output and raw context sent to the LLM.

## CLI

`codalotl` supports interactive (TUI) and noninteractive (CLI) workflows.

Argument semantics for `<path/to/pkg>` (where relevant):
- Accepts package directories by relative or absolute path (both `some/pkg` and Go-style `./some/pkg` work).
- `...` package patterns are not implemented.

### `codalotl`

Launches the interactive TUI.

```bash
codalotl
```

### `codalotl -h` / `codalotl --help`

Shows command usage.

```bash
codalotl --help
```

This path skips startup validation.

### `codalotl version`

Prints installed version, and may include update status if available quickly.

```bash
codalotl version
```

### `codalotl config`

Prints effective configuration and config sources. See Config File below.

```bash
codalotl config
```

### `codalotl exec`

Runs one noninteractive agent turn from CLI.

```bash
codalotl exec -p ./internal/cli "fix failing test"
```

Flags:
- `-p, --package <path>`: run in package mode rooted at this package path.
- `-y, --yes`: auto-approve permission checks.
- `--no-color`: disable ANSI formatting.
- `--model <id>`: override configured preferred model for this run.

### `codalotl context public <path/to/pkg>`

Print public API documentation context for a package.

```bash
codalotl context public ./internal/cli
```

### `codalotl context initial <path/to/pkg>`

Print initial package context bundle used for package-mode.

```bash
codalotl context initial ./internal/cli
```

### `codalotl context packages`

Print package listing for the current module.

```bash
codalotl context packages
```

Flags:
- `-s, --search <go_regexp>`: filter package list by Go regexp.
- `--deps`: include direct dependency packages from `go.mod` (`require` entries excluding `// indirect`).

### `codalotl docs reflow <path>...`

Reflow Go documentation comments in one or more paths.

```bash
codalotl docs reflow some/pkg
```

Flags:
- `-w, --width <int>`: override configured `reflowwidth` for this run.
- `--check`: dry-run (print files that would change; do not write).

Output style is similar to `gofmt -l`: one file per line if modified.

## Configuration

Configuration is loaded from JSON files plus environment.

### Config File

Codalotl will search for `config.json` in the following locations, merging config values:

1. Nearest project config from cwd upward: `.codalotl/config.json` (highest priority)
2. Global: `~/.codalotl/config.json` (lowest priority)

Schema:

```json
{
  "providerkeys": {
    "openai": "sk-..."
  },
  "reflowwidth": 120,
  "lints": {
    "mode": "extend",
    "disable": [],
    "steps": []
  },
  "disabletelemetry": false,
  "disablecrashreporting": false,
  "preferredprovider": "",
  "preferredmodel": ""
}
```

Key fields:
- `providerkeys.openai`: OpenAI API key (ENV is also supported and preferred).
- `reflowwidth`: default doc reflow width (default 120).
- `lints`: lint pipeline config (see Lints below).
- `preferredprovider`, `preferredmodel`: default model selection hints.
- `disabletelemetry`, `disablecrashreporting`: opt out of event/error and panic reporting.

To see your config, run `codalotl config`.

### Models

Currently, only OpenAI models are supported. More providers will be added over time.

#### Choosing a Model

To set your model via the TUI:
1. List available models in the TUI: `/models`
2. Switch models: `/model <id>`

To set your model via the config file:

```json
{
  "preferredmodel": "gpt-5.2-medium"
}
```

To set your model for an `exec` run:

```bash
codalotl exec --model gpt-5.2-medium "your prompt"
```

#### API Key Config

Set your API key, we recommend setting an ENV variable.

For exmaple, you may add the following to something like your `.bashrc`:

```bash
export OPENAI_API_KEY="sk-..."
```

Alternatively, you can set it in `.codalotl/config.json`:

```json
{
  "providerkeys": {
    "openai": "sk-..."
  }
}
```

### AGENTS.md

Codalotl reads `AGENTS.md` instructions and injects them into the agent context automatically. The LLM does NOT need to manually Read `AGENTS.md`.

In package mode, multiple `AGENTS.md` files may be added to context if multiple exist (looking from package dir upward to sandbox dir).

### Skills

Skills are local instruction bundles (`SKILL.md`) available to the agent, following the specification at [agentskills.io](https://agentskills.io).

Some built-in system skills are auto installed.

You can add your own skills by placing them in a skill search path (and restarting the TUI):
- `.codalotl/skills` in current directory and each parent directory (e.g., project-based skills).
- `~/.codalotl/skills`: skills the user wants for all projects.

Skills can be listed in the TUI with `/skills`.

You can invoke skills by explicitly mentioning with a `$` prefix in a message (ex: `use $skill-creator to make a new skill that...`). The LLM may automatically decide to use a skill based on the task it is trying to accomplish.

Package mode note:
- Even though package mode does not typically have access to a Shell tool, it can use shell commands IF the skill indicates that it should, or if there's scripts to run in the skill.

### Lints

Lint pipeline config controls which checks/fixes run, and in which situations they run. Each linting tool can be either be check-only, or check-and-fix.

Run situations:
- `initial`: when automatic initial package context is built (lints just check).
- `patch`: automatically after patches (lints auto-fix).
- `fix`: when the Fix Lints tool is specifically run (lints auto-fix).

Defaults and preconfigured IDs:
- Default active lint pipeline: `gofmt`.
- Preconfigured step IDs you can add by `id`: `reflow`, `staticcheck`, `golangci-lint`.

How to think about lint situations:
- Keep `initial` fast and low-noise. It feeds the agent's starting context, so slow lints reduce responsiveness. Noisy lints district the LLM.
- Keep high-false-positive lints out of automatic paths. Prefer dedicated lint actions (`fix`) instead of always-on runs.

Recommended baseline config:

```json
{
  "reflowwidth": 120,
  "lints": {
    "mode": "extend",
    "steps": [
      { "id": "reflow" },
      { "id": "staticcheck" }
    ]
  }
}
```

Custom lint step config:
- A step can define `id`, optional `situations`, and `check`/`fix` commands.
    - Check-only linters should define a `check` command. Check-and-fix linters should define commands for both `check` and `fix`.
- Command fields:
    - `command`: executable to run (for example `gofmt`, `staticcheck`, `golangci-lint`).
    - `args`: argument list passed to `command`; each entry supports templates.
    - `cwd`: working directory for the command.
    - `messageifnooutput` (optional string): status text shown when the command emits no output. Used to guide LLM.
    - `outcomefailifanyoutput` (optional boolean): treat any stdout/stderr output as a failing outcome.
- Template variables available in lint commands:
    - `{{ .path }}`: absolute path to the target package directory.
    - `{{ .moduleDir }}`: absolute path to the module root (directory containing `go.mod`).
    - `{{ .relativePackageDir }}`: package path relative to `{{ .moduleDir }}`.
    - `{{ .RootDir }}`: sandbox dir.
- If a step runs in `initial`, `check` is required; if a step runs in `patch` or `fix`, at least one of `check` or `fix` is required.

Example custom step:

```json
{
  "lints": {
    "mode": "extend",
    "steps": [
      {
        "id": "govulncheck",
        "situations": ["fix"],
        "check": {
          "command": "govulncheck",
          "args": ["./{{ .relativePackageDir }}"],
          "cwd": "{{ .moduleDir }}",
          "messageifnooutput": "no issues found"
        }
      }
    ]
  }
}
```

Extend vs replace:
- `mode: "extend"` starts from defaults and appends your `steps`.
- `mode: "replace"` ignores defaults and uses only your `steps`.
- `disable` removes resolved steps by `id` (unknown IDs are ignored).
- Use `mode: "replace"` with `steps: []` to disable all lints.

Per-step `situations` behavior:
- Omitted or `null`: run in all situations.
- `[]`: run in no situations.

If you enable `reflow` (it normalizes documentation width and formatting), a one-time repo-wide reflow is recommended (otherwise you'll see reflow-related diffs in later tasks/commits).

One-time reflow of a module:

```bash
go list -f '{{.Dir}}' ./... | sort -u | xargs -I{} codalotl docs reflow "{}"
```

## Safety & Security

Codalotl has policy-based safety controls, not OS-level sandboxing. Its designed to prevent you from easily shooting yourself in the foot, but doesn't prevent attackers from doing so. UX is prioritized over hard security. You can achieve security by running in a container/VM.

Authorization model:
- Reads/writes are allowed for the sandbox dir (where the TUI was launched).
- In package mode, direct reads/writes are limited to the package (and supporting files).
- Access outside the sandbox dir requires user permission.

Shell command policy:
- Commands are categorized as safe, blocked, dangerous, or inscrutable.
- Blocked commands are denied.
- Dangerous/inscrutable commands require approval (except in auto-approve mode).
- Safe commands may still require approval if explicitly requested.

Use `@` file/dir mentions to allow read access to files outside the sandbox or outside the current package.

Telemetry and reporting:
- Codalotl can report pseudonymous usage events, errors, and panic diagnostics.
- It does not collect prompts, responses, or source code.
- Disable via config:

```json
{
  "disabletelemetry": true,
  "disablecrashreporting": true
}
```

## Status & Limitations

### Supported Platforms

Current practical status:
- Actively exercised in Unix-like environments (macOS/Linux).
- Windows code paths exist across terminal/clipboard layers, but cross-platform behavior is less battle-tested than Linux/macOS workflows.

### Unsupported Features

Known unsupported or intentionally omitted areas:
- MCP server integration is not supported.
  - Use built-in tools, shell workflows, and skills instead.
- Session resumption/persistence is not currently implemented.
