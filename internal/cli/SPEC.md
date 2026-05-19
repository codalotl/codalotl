# cli

The `cli` package represents the codalotl CLI. It should be used by a very thin main package - the meat is here.

We assume the app is named `codalotl`.

We use the `internal/q/cli` CLI framework to implement it.

## Startup and Environment Validation

When codalotl starts, we load and validate configuration and required tools, except for commands that explicitly opt out (for example `version`, `pr new`, and `-h`).
- If there's an error parsing the config file, or a config option is invalid, an error message is displayed and codalotl exits.
- If there is no LLM configured (no LLM provider keys, including in ENV), an error message is displayed and codalotl exits.
	- Note: a key must exist for **usable** models. The `llmmodel` package supports more providers than the CLI config schema currently exposes.
- Required tools are checked:
	- `go`
	- `gopls`
	- `goimports`
	- `gofmt`
	- `git`
- If these tools are missing, an error message is displayed and codalotl exits. Installation instructions are given for tools installable with `go install`.

## Commands

Notes:
- Any argument <path/to/pkg> follows `product-spec/features/cli.md` single-package semantics.
	- Import paths take precedence.
	- Explicit relative dirs (`.`, `..`, `./foo`, `../foo`) and absolute dirs are direct filesystem paths.
	- Bare/slashed CWD-relative dirs that do not start with `.` are fallback package dirs if they do not resolve as import paths.
	- It may NOT use `...` package patterns unless the command explicitly accepts a package pattern.
- The root command does not accept a package/path argument. The only exception is `codalotl .`, which is treated as an alias for launching the TUI (for muscle memory with tools like `code .`).
- Command definitions provide q/cli help metadata: short/long descriptions, usage, positional args, and useful examples.
- `codalotl --help` is root-oriented. Tool-facing command catalogs may request q/cli leaf-command help from the same command tree.
- Tool-facing commands write user-visible stdout through `qcli.Context.Out`.

## Agent Session Wiring

- CLI startup installs agent tools such as `codalotl_cli` and `refactor` into `internal/agentbuilder`.
- `codalotl_cli` exposes these commands:
	- `codalotl docs add`
	- `codalotl docs fix`

### codalotl -h, codalotl --help

Prints standard usage.

### codalotl and codalotl .

The naked `codalotl` launches the TUI (`codalotl .` is an alias, supported so that muscle memory from things like `code .` work; any other path-like argument is an error).

If config sets `autoyes: true`, the TUI launches with auto-approved permission checks.

If the TUI (`internal/tui`) requests that a newly selected model be persisted (via `tui.Config.PersistModelID`), the CLI writes the model to `preferredmodel` in a JSON config file:
- If some config file explicitly set `preferredmodel` during load, update that same file.
- Otherwise, update the highest-precedence config file that contributed any values.
- If no config files contributed values, write to the global config at `~/.codalotl/config.json` (expanded cross-OS).

### codalotl exec [--package <path/to/pkg>] [--yes] [--no-color] [--json] [--model <id>] [--slash-command <cmd>] [<prompt> ...]

Runs the noninteractive agent (`internal/noninteractive`).

Notes:
- `<prompt>` is the end-user message. It is required unless `--slash-command` starts a session that can run without an initial message.
- `--package` enters package mode for the run.
- `--yes` auto-approves permission checks for the run.
- `--no-color` disables ANSI formatting.
- `--json` switches to newline-delimited JSON output.
- `--model` overrides the configured preferred model for the run.
- `--slash-command` applies a TUI-style slash command at session start before any `<prompt>` is sent.
	- Supported values:
		- `orchestrate`
		- `/orchestrate`
	- These start a fresh generic-mode orchestrator session around the built-in orchestrator agent, matching the TUI's `/orchestrate` behavior.
	- The slash-command name is user-facing; internal agent identifiers are not.
	- If `<prompt>` is also provided, it is sent as the initial user message in that orchestrator session.

### codalotl iterate [--prompt-file <path>] [--orchestrate] [--max-steps <n>] [--max-minutes <n>] [--decision-prompt <text>] [--continue-mode <mode>] [--yes] [--no-color] [--json] [--model <id>] [--slash-command <cmd>] [<prompt> ...]

Runs repeated noninteractive agent steps until iteration policy says stop.

Notes:
- Prompt source:
	- Use `<prompt> ...` when provided.
	- Or load the initial prompt from `--prompt-file`.
	- Exactly one prompt source is used, unless `--orchestrate` or `--slash-command` starts a session that can run without an initial message.
- `--orchestrate` is a convenience mode for the built-in orchestrator flow.
	- It behaves like starting an orchestrator session, matching `exec --slash-command=orchestrate`.
	- It may be used with or without an explicit prompt.
- `--max-steps` stops before starting a new prompt step once the limit is reached.
- `--max-minutes` stops before starting a new prompt step once elapsed time reaches the limit.
- `--decision-prompt` customizes the decision message used when the agent did not emit an explicit continue/stop token.
	- Default is a built-in prompt that asks for `STOP_ITERATION` vs `CONTINUE_ITERATION`.
	- `--decision-prompt=""` disables the extra decision step.
- `--continue-mode` controls whether the next prompt step uses a fresh session, a resumed session, or auto selection. Allowed values:
	- `fresh`
	- `resume`
	- `auto` (default)
- Accepts the same relevant execution flags as `exec` for model selection, formatting, JSON output, auto-approval, and slash-command setup, except it does not support `--package`.
- Prints iteration lifecycle metadata before and after each prompt step.
	- Human-readable mode prints concise status lines.
	- JSON mode emits newline-delimited iteration events in addition to the underlying noninteractive stream.
- If the current step does not finish successfully, iterate may retry according to iteration policy.
- If iteration stops because retries are exhausted, the command exits non-zero.
- Ctrl-C exits the iterate command rather than starting another iteration.

### codalotl version

Prints the codalotl version status, and the version itself, to stdout. The version must be by itself on the last line. If the latest version cannot be obtained in a timely fashion (250ms timeout), only the current version is displayed.

Example output:
```
The current version (1.2.3) is up to date.

1.2.3
```

Or:
```
An update is available: 1.2.4 (current 1.2.3)
Run go install github.com/codalotl/codalotl@latest

1.2.3
```

Or:
```
1.2.3
```

### codalotl config

Prints the codalotl configuration to stdout. Details:
- Prints the `Config` struct, but with some modifications (see below).
- Any present provider key is redacted. Uses reflection so any new provider added to the struct is automatically redacted.
- If a provider key is "", prints the corresponding value from ENV (see `llmmodel.ProviderKeyEnvVars`). Again, uses reflection.
- Below the printed `Config` struct, prints:
	- Which file(s) actually store the config. If multiple do (`cascade` merges config data) they are all listed.
	- The effective model (useful when no model is explicitely configured).
	- List of provider ENV keys to set.
	- Instructions on where the config file can be stored.

Example Output:
```
Current Configuration:
{
  "providerkeys": {
    "openai": "sk-p..._LQA",
    "anthropic": ""
  },
  "autoyes": true,
  "reflowwidth": 160,
  "theme": "",
  "preferredprovider": "",
  "preferredmodel": ""
}

Current Config Location(s): /home/someuser/.codalotl/config.json

Effective Model: gpt-5.5-high

To set LLM provider API keys, set one of these ENV variables:
- OPENAI_API_KEY
- ANTHROPIC_API_KEY

Global configuration can be stored in /home/someuser/.codalotl/config.json
Project-specific configuration can be stored in .codalotl/config.json
```

### codalotl pr new <feature-name> [--no-git]

Creates an orchestrator PR file and, unless `--no-git` is set, prepares a local git branch:
- Does not require LLM configuration or startup tool validation.
- Feature name is required, filesystem-safe, and used as the PR filename suffix.
- PR file path: `.prs/YYYY-MM-DD_<unix-seconds>_<feature-name>.md`
- Never overwrite an existing PR file.
- PR file starts with:

```markdown
# PR

## User Summary (do not modify)


```

Git behavior:
- Require git repo, clean workspace, and current branch `main` or `master`.
- Ensure current branch is up to date with its upstream.
- Create branch named `$CODALOTL_USER_INITIALS/<feature-name>` when initials are set, else `<feature-name>`.
- Add and commit PR file.
- If `origin` exists, push branch with upstream tracking.

If `--no-git` is set:
- Do not require git state.
- Only create the PR file.

### codalotl context public <path/to/pkg>

Prints out the public API of the package (see the `internal/gocodecontext` package).

### codalotl context initial <path/to/pkg>

Prints out the initial context to LLMs for the package (see the `internal/initialcontext` package).

### codalotl context packages [--search <go_regexp>] [--deps]

Prints a list of packages available in the module containing the current working directory, as LLM-ready context (see `internal/gocodecontext.PackageList`).

Notes:
- `--search` filters packages by interpreting `<go_regexp>` as a Go regexp.
- If `--deps` is set, packages from direct (non-`// indirect`) module dependencies are also included.
- The output format is intentionally opaque and may change; callers should treat it as text intended to be copied into an LLM prompt rather than parsed.

### codalotl docs reflow [--width <reflowwidth>] [--check] <path> ...

Reflows the specified path(s) using `updatedocs.ReflowDocumentationPaths`. Reflow width is pulled from config.
If `--width` is provided, it overrides the configured `reflowwidth` for that invocation only.
If `--check` is provided, reflow is run as a dry-run: no files are modified on disk, but the output still lists which files would change.

Output:
- Prints the list of modified `.go` files (one per line) to stdout, similar to `gofmt -l`. The paths are module-relative when available.

### codalotl docs add [--public-only] [--important] [--include-test] <path/to/pkg>

Adds missing package documentation comments using `docubot.AddDocs`.

Notes:
- `<path/to/pkg>` follows the usual single-package argument semantics described above.
- `--public-only` only documents exported identifiers.
- `--important` documents exported identifiers and important identifiers.
- `--public-only` and `--important` are mutually exclusive.
- `--include-test` includes test files, including black-box `_test` packages.
- Uses the effective model and configured `reflowwidth`.
- Detailed help covers options, `<path/to/pkg>`, and common examples.

Output:
- Prints a concise summary of the applied documentation changes.

### codalotl docs fix [--identifiers <comma-list>] <path/to/pkg>

Fixes materially false existing documentation comments using `docubot.FindAndFixDocErrors`.

Notes:
- `<path/to/pkg>` follows usual single-package argument semantics.
- Scans non-test, test, and black-box `_test` package docs.
- `--identifiers` limits checks to a comma-separated allowlist.
- Missing docs and non-material wording issues are ignored.
- Successful runs write `docs-fix-1` package CAS records keyed against fixed contents; identifier-limited records are not whole-package records.

Output:
- Prints concise fix summary without internal CAS metadata.

### codalotl spec diff <path/to/pkg_or_SPEC.md>

Prints a human/LLM-friendly diff between the public API declared in `SPEC.md` and the public API implemented in the corresponding `.go` files, using `internal/specmd`.

Argument semantics:
- `<path/to/pkg_or_SPEC.md>` may be:
	- A package directory (relative or absolute; optional trailing `/`), in which case the `SPEC.md` at `<dir>/SPEC.md` is used.
	- A `SPEC.md` file path (relative or absolute), in which case that file is used directly.
- Import-path-style package arguments (as accepted by other `<path/to/pkg>` args) are allowed; they are resolved to a package directory first, and then `SPEC.md` is loaded from that package directory.

Output:
- If differences are found, they are printed to stdout via `specmd.FormatDiffs`.
- If no differences are found, the command prints nothing and exits successfully.

### codalotl spec ls-mismatch <pkg/pattern>

Accepts a Go-style package pattern (including `./...`). Prints one line per package (prints the package, ex: `./path/to/pkg`) where `codalotl spec diff` produces a diff. If there's no SPEC.md with mismatched packages, there's no output. If `codalotl spec diff` would produce an error (but no diff), no line is output.

This may only produce a line for a dir if the dir has both a SPEC.md and a valid Go package.

### codalotl spec status

This prints out per-package SPEC.md status across modules discovered from the nearest git repo root via `gocode.DiscoverModules`.

Per line:
- package, repo-relative when repo-scoped (ex: `./path/to/pkg`)
- has SPEC.md file (ex: `true`)
- matching `Public API` - i.e., `codalotl spec diff` produces NO output (ex: `false`)
- impl conforms to spec as per cas system, via `casconformance.Retrieve` (ex: `true`)

Sort: 1. has_spec (true first) 2. api_match (true first) 3. conforms (true first) 4. package (a->z)

### codalotl cas get <namespace> <path/to/pkg>

Uses `internal/gocas` to get the stored value (and associated metadata) for (package, registered namespace), for the current package contents.

- `<namespace>` is the non-versioned namespace name.
- Prints entire record (including additional information) if found.
- Otherwise prints nothing and exits 1.

### codalotl cas ls-namespaces

Lists registered CAS namespaces and active versions, sorted by namespace name.

Output: one line per namespace, format `<namespace> <version>`; hash mode omitted.

### codalotl cas ls-summary <namespace> [--csv]

Displays a per-package CAS summary for a registered namespace across modules discovered from the nearest git repo root via `gocode.DiscoverModules`.

Columns:
- Package
- CAS: `yes` or `no`, based on current package content.
- Prev CAS: `yes`, `no`, or `-` when current CAS exists.
- Age: `-` or compact age of the relevant CAS entry.
- Churn %: `-` or approximate line churn relative to the previous CAS-covered state.

Pretty output is terminal-oriented. `--csv` emits CSV.

### codalotl cas ls-stale <namespace> [--stale-after-days=30] [--min-churn-percent=20]

Lists packages under nearest git repo, printed repo-relative (`./path/to/pkg`), whose current contents lack a CAS record for registered namespace, one per line.

Module discovery uses `gocode.DiscoverModules` from the nearest git repo root.

Defaults: 30 days, 20% churn.

Packages with no prior CAS record are always listed.

Filters are ORed:
- `--stale-after-days=N`: prior CAS record is at least N days old.
- `--min-churn-percent=N`: churn from prior CAS record is at least N%.

### codalotl cas prune [--days=N]

Deletes prune-eligible CAS records across modules discovered from nearest git repo.

Notes:
- Default `--days` is 30.
- Removes prior namespace versions and superseded package records older than `--days`.
- Preserves active current-content records.
- Output reports delete counts.

### codalotl cas recertify <path/to/pkg> --namespaces="<namespace1>[,<namespace2>,...]"

Copies recently invalidated CAS records forward to current package contents.

Notes:
- `<path/to/pkg>` follows usual single-package semantics.
- `--namespaces` is required: comma-separated registered namespace names.
- Existing current CAS records are left unchanged.
- Prior CAS records are never deleted or mutated.
- Output reports per-namespace no-op/recertified status and warnings.

## Configuration

This package is responsible for loading a configuation file and passing various configuration to other packages. The configuration is loaded with `internal/q/cascade`. The configuration is loaded and validated for all commands, except those that obviously don't need it, like `version` and `-h`. An invalid configuration prints out a helpful error message and returns with an non-zero exit code.

Commands that create local project scaffolding, such as `pr new`, can skip configuration loading.

The config file is loaded with this preference:
- `.codalotl/config.json` (starting from the working directory, recursively checking the parent, until some reasonable stop condition).
- `~/.codalotl/config.json` or `%LOCALAPPDATA%\.codalotl\config.json`.

Config:

```go {api}
// Config is codalotl's configuration loaded from a cascade of sources.
//
// NOTE: internal/q/cascade matches keys to struct field names case-insensitively; it does not use json tags. The json tags are for `codalotl config` output and
// for compatibility with typical config.json naming.
type Config struct {
	ProviderKeys          ProviderKeys       `json:"providerkeys"`
	CustomModels          []CustomModel      `json:"custommodels,omitempty"`
	ReflowWidth           int                `json:"reflowwidth"` // Max width when reflowing documentation. Defaults to 120.
	ReflowWidthProvidence cascade.Providence `json:"-"`

	// Lints configures the lint pipeline used by `codalotl context initial`. See internal/lints/SPEC.md for full details.
	Lints lints.Lints `json:"lints,omitempty"`

	DisableTelemetry      bool   `json:"disabletelemetry,omitempty"`
	DisableCrashReporting bool   `json:"disablecrashreporting,omitempty"`
	Theme                 string `json:"theme"` // Theme selects the TUI color palette. Allowed values: "", "dark", "light".

	// Optional. If set, use this provider if possible (lower precedence than PreferredModel, though). Allowed values are llmmodel's AllProviderIDs().
	PreferredProvider string `json:"preferredprovider"`

	// Optional. If set, use this model specifically. Allowed values are llmmodel's AvailableModelIDs().
	PreferredModel string `json:"preferredmodel"`

	// PreferredModelProvidence indicates which source set PreferredModel, when any source actually did. This is used to decide which config file should be updated if
	// the TUI asks to persist a newly selected model.
	PreferredModelProvidence cascade.Providence `json:"-"`
}

// ProviderKeys is kept separate so tests can easily validate its zero value.
type ProviderKeys struct {
	OpenAI    string `json:"openai"`
	Anthropic string `json:"anthropic"`
	Gemini    string `json:"gemini"`
}

type CustomModel struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Model    string `json:"model"`

	APIKeyEnv       string `json:"apikeyenv"`
	APIEndpointEnv  string `json:"apiendpointenv"`
	APIEndpointURL  string `json:"apiendpointurl"`
	ReasoningEffort string `json:"reasoningeffort"`
	ServiceTier     string `json:"servicetier"`
}
```

Notes:
- If a provider's key is configured via the configuration file, call `llmmodel.ConfigureProviderKey` to use it.
- Custom models are listed, they may be referred to by ID with `PreferredModel` (also, see `llmmodel.AddCustomModel`).
- Theme is passed to the TUI as its palette selection. If unset, the TUI uses its default/auto palette behavior.

## Metrics/Crash Reporting and Version Notices

We use `internal/q/remotemonitor` to report anonymous usage metrics, errors, and crashes to a server for analysis and diagnostics. This is also used to inform the user of new versions. Only pseudo-anonymous metrics are collected; never code, prompts, or user data. These can be opted out of.
- Opt-out is controlled by `DisableTelemetry` and `DisableCrashReporting` above (crash reporting is only panics).
- Version check opt-out is not needed, since no data is sent.
- The server that receives data is `https://codalotl.ai`.
	- The endpoints are `/v1/reports/events`, `/v1/reports/errors`, and `/v1/reports/panics`.
- The version check URL is `https://codalotl.github.io/codalotl/latest_version.json`.
- Any CLI command that doesn't load config (ex: `-h`, `codalotl version`) does NOT send events/errors/panics, because we don't know if telemetry is disabled.

If the version is out of date:
- When running certain CLI commands (non-TUI), the **first** output is:
	- `An update is available: %s (current %s)\nRun go install github.com/codalotl/codalotl@latest\n\n`
	- Commands where this is displayed: `codalotl config`.
- `codalotl version` also displays this, but also indicates if the version IS up to date. See that section for more details.

### Events

- There is one event fired per CLI invocation (including the TUI invocation, but excluding non-config-loaded commands). Ex: `codalotl` (starting TUI) fires an event; `codalotl context initial path/to/pkg` fires an event; `codalotl version` does NOT fire an event.
- The event name is lowercase and underscored. Ex: `start_tui`; `context_initial`.
- Events are reported asynchronously with stable props included.

### Errors

- If `Run` returns an error with exit code 1, the error is reported.
- Metadata will include `event` mapping to the same string as the event in `### Events`, or some reasonable fallback if that doesn't apply.

### Crashes

- Most CLI commands are run in a wrapped `WithPanicReporting`. Panics are reported.

### TUI

The TUI is its own beast with multiple goroutines and its own UI. Therefore, a `*remotemonitor.Monitor` is supplied to the TUI, so that it can display version upgrade notices and monitor its goroutines.

That being said, still treat the invocation of the TUI as any other command. Its invocation should be wrapped with panic reporting; this package DOES fire a `start_tui` event; if the invoked TUI returns an error, this package will report it.

## Version

Version is the version. Can either be set in source or via build tooling. It is not in the `Public API` section so we can bump version without triggering differences with the SPEC.

```go
// Version is the codalotl version. It is a var (not a const) so build tooling can override it.
var Version = "0.1.0"
```

## Public API

```go
// In/Out/Err override standard I/O. If nil, defaults are used. Overriding is useful for testing.
//
// Note that if Stdout/Stderr are overridden, we will pass them to other package's functions if they accept them. However, not all will; some packages will probably
// print to Stdout.
type RunOptions struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// Run runs the CLI with args (typically you'd use os.Args).
//
// It returns a recommended exit code (0, 1, or 2) and an error, if any:
//   - 0 -> err == nil
//   - 1 -> err != nil, but the structure of args is sound (flags are correct, etc).
//   - 2 -> err != nil, args parse error or misuse of flags, etc.
//
// Note that in cases of errors, Run has already displayed an error message to opts.Err || Stderr. Callers may use os.Exit with the exit code.
func Run(args []string, opts *RunOptions) (int, error)
```
