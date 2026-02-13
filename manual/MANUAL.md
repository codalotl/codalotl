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

The TUI has:
- Message area (conversation + tool activity)
- Text area (prompt input)
- Permission view (when approvals are needed)
- Right-side info panel (when terminal width permits)

The app starts in generic mode. Package mode is opt-in with `/package`.

### Slash Commands

Stable user commands:
- `/quit`, `/exit`, `/logout`: exit the TUI.
- `/new`: start a new session (keeps active package if already in package mode).
- `/skills`: list installed skills and any skill loading issues.
- `/model`: list current model and available callable models.
- `/models`: alias for `/model` listing behavior.
- `/model <id>`: switch model and start a new session.
- `/package <path>`: enter package mode.
- `/package`: leave package mode.
- `/generic`: leave package mode.

Model persistence:
- If model persistence is enabled by the CLI integration, `/model <id>` also writes `preferredmodel` in config.

### Permissions

When a tool call needs approval, a permission panel appears.

Controls:
- `Y`: allow
- `N`: deny
- `ESC`: deny and stop the current agent run

In noninteractive mode (`codalotl exec`), there is no prompt:
- `--yes` means auto-allow permission requests.
- Without `--yes`, permission requests are auto-denied.

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
- `Up`/`Down`: cycle message history (with edit-aware cycling behavior).
- `PageUp`/`PageDown`/`Home`/`End`: scroll message area.
- Mouse wheel: scroll message area.
- `Ctrl-O` or terminal double-click: toggle overlay mode.

### Details View

Codalotl provides two “detail” surfaces in the TUI:

1. Info panel (right side, if width allows), showing:
- Session ID
- Active model
- Context usage remaining
- Estimated cost
- Token totals (input/cached/output)
- Current package mode/path
- Version upgrade notice when available

2. Overlay details dialog (opened from `details` buttons in overlay mode):
- Tool call metadata (tool, call id, type, provider)
- Raw tool input and result payloads
- Context-gathering payload for package context status lines
- JSON pretty formatting where applicable
- Binary/oversize protections for very large payloads

Close the dialog with `ESC`.

### Overlay Mode

Overlay mode adds clickable UI actions over messages.

Enter/exit:
- `Ctrl-O`
- Double-click in terminal area

Overlay actions currently include:
- `copy` button per message
- `details` button for tool/context-status messages

#### Copying Text

Copy behavior is best-effort and intentionally practical:
- Copies rendered plain text of the selected message.
- Tries OS clipboard integration when available.
- Also sends OSC52 clipboard data for terminal clipboard support.
- Shows transient `copied!` feedback in the UI.

## CLI

`codalotl` supports interactive and noninteractive workflows.

Argument semantics for `<path/to/pkg>` (where relevant):
- Accepts package directories by relative or absolute path.
- Accepts import paths for packages in the current module context.
- Optional trailing `/` is accepted.
- `...` package patterns are rejected.

Exit codes:
- `0`: success
- `1`: command/runtime/startup validation error
- `2`: argument or flag usage error

### `codalotl`

Launches the interactive TUI in generic mode.

```bash
codalotl
```

### `codalotl .`

Alias for starting the TUI.

```bash
codalotl .
```

Any other path-like argument at root command level is a usage error.

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

Output behavior:
- Up-to-date notice + version, or
- Update notice + install hint + version, or
- Version only (if latest version lookup times out/unavailable)

This command skips startup validation and telemetry event reporting.

### `codalotl config`

Prints effective configuration and config sources.

```bash
codalotl config
```

Includes:
- Redacted provider keys
- Config file locations that contributed values
- Effective model
- Relevant provider env vars
- Standard config file locations

May prepend update notice when out-of-date.

### `codalotl exec`

Runs one noninteractive agent turn from CLI.

```bash
codalotl exec -p ./internal/cli "fix failing test"
```

Flags:
- `-p, --package <path>`: run in package mode rooted at this package path (must be within cwd).
- `-y, --yes`: auto-approve permission checks.
- `--no-color`: disable ANSI formatting.
- `--model <id>`: override configured preferred model for this run.

### `codalotl context`

Namespace command for LLM-oriented context subcommands:
- `context public`
- `context initial`
- `context packages`

### `codalotl context public <path/to/pkg>`

Print public API documentation context for a package.

```bash
codalotl context public ./internal/cli
```

### `codalotl context initial <path/to/pkg>`

Print initial package context bundle used for package-mode startup.

```bash
codalotl context initial ./internal/cli
```

### `codalotl context packages`

Print LLM-oriented package listing for the current module context.

```bash
codalotl context packages
```

Flags:
- `-s, --search <go_regexp>`: filter package list by Go regexp.
- `--deps`: include packages from direct dependencies (`require` entries excluding `// indirect`).

The textual output is intended for LLM context, not stable machine parsing.

### `codalotl docs`

Namespace command for documentation tools:
- `docs reflow`

### `codalotl docs reflow <path>...`

Reflow Go documentation comments in one or more paths.

```bash
codalotl docs reflow --check .
```

Flags:
- `-w, --width <int>`: override configured `reflowwidth` for this run.
- `--check`: dry-run (print files that would change; do not write).

Output style is similar to `gofmt -l`: one file per line.

## Configuration

Configuration is loaded from JSON files plus environment.

### Config File

Config search/precedence:
1. Global: `~/.codalotl/config.json`
2. Nearest project config from cwd upward: `.codalotl/config.json`

Project config has higher precedence than global config.

Base schema:

```json
{
  "providerkeys": {
    "openai": "sk-..."
  },
  "custommodels": [],
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
- `providerkeys.openai`: OpenAI API key override (env fallback is supported).
- `custommodels`: register additional model IDs, providers, endpoints, env key names, and overrides.
- `reflowwidth`: default doc reflow width (must be > 0; default 120).
- `lints`: lint pipeline config.
- `preferredprovider`, `preferredmodel`: default model selection hints.
- `disabletelemetry`, `disablecrashreporting`: opt out of event/error and panic reporting.

Startup validation:
- Invalid config fails command startup (except `-h` and `version`).

### Models

Model selection behavior:
- Default fallback model is `gpt-5.2`.
- `/model` and `/models` list only models with effective API keys.
- `codalotl exec --model <id>` overrides `preferredmodel` for that run.

Provider key resolution precedence (per model):
1. Per-model actual key override
2. Per-model env var override
3. In-memory provider key override (from config load)
4. Provider default env var (for OpenAI: `OPENAI_API_KEY`)

Preferred model persistence:
- In TUI, model switching can persist to config.
- It updates the config file that originally set `preferredmodel`, else highest-precedence contributing config, else global config.

### AGENTS.md

Codalotl reads AGENTS instructions and injects them into agent context.

Resolution behavior:
- Searches from cwd upward to sandbox root for `AGENTS.md`.
- Includes all non-empty AGENTS files found.
- Orders output from farthest to nearest, so nearest instructions appear later and take precedence.

Mode behavior:
- Generic mode: AGENTS context is added at session start.
- Package mode: AGENTS context is prepended to generated package initial context.

### Skills

Skills are local instruction bundles (`SKILL.md`) available to the agent.

Default behavior:
- Built-in system skills are auto-installed under `~/.codalotl/skills/.system`.
- `/skills` lists installed valid skills and shows load/validation issues.

Search paths:
- `.codalotl/skills` in current directory and each parent directory
- `~/.codalotl/skills`
- `~/.codalotl/skills/.system`

Package mode note:
- Package mode does not expose general `shell`; it exposes `skill_shell` and expects shell execution only when a skill requires it.

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
    - `attrs` (optional array of strings): key/value attributes attached to command output metadata. Used to guide LLM.
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
