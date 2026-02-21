# cli

The `cli` package represents the codalotl CLI. It should be used by a very thin main package - the meat is here.

We assume the app is named `codalotl`.

We use the `internal/q/cli` CLI framework to implement it.

## Startup and Environment Validation

When codalotl starts, we load and validate configuration and required tools (exception: `version` and `-h` do not load/validate and always succeed).
- If there's an error parsing the config file, or a config option is invalid, an error message is displayed and codalotl exits.
- If there is no LLM configured (no LLM provider keys, including in ENV), an error message is displayed and codalotl exits.
	- Note: a key must exist for **usable** models. The `llmmodel` package has more providers than we actually support right now.
- Required tools are checked:
	- `go`
	- `gopls`
	- `goimports`
	- `gofmt`
	- `git`
- If these tools are missing, an error message is displayed and codalotl exits. Installation instructions are given for tools installable with `go install`.

## Commands

Notes:
- Any argument <path/to/pkg> can either use a Go-style package path (ex: `.`; `..`; `./internal/cli`) to a single package OR a relative/absolute dir (ex: `internal/cli`; `/home/proj/codalotl/internal/cli`), with optional trailing `/`.
    - It may NOT use `...` package patterns (if we need this, we'll invent a new identifier for it, for instance: <package_pattern>).
- The root command does not accept a package/path argument. The only exception is `codalotl .`, which is treated as an alias for launching the TUI (for muscle memory with tools like `code .`).

### codalotl -h, codalotl --help

Prints standard usage.

### codalotl and codalotl .

The naked `codalotl` launches the TUI (`codalotl .` is an alias, supported so that muscle memory from things like `code .` work; any other path-like argument is an error).

If the TUI (`internal/tui`) requests that a newly selected model be persisted (via `tui.Config.PersistModelID`), the CLI writes the model to `preferredmodel` in a JSON config file:
- If some config file explicitly set `preferredmodel` during load, update that same file.
- Otherwise, update the highest-precedence config file that contributed any values.
- If no config files contributed values, write to the global config at `~/.codalotl/config.json` (expanded cross-OS).

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
    "openai": "sk-p..._LQA"
  },
  "reflowwidth": 160,
  "preferredprovider": "",
  "preferredmodel": ""
}

Current Config Location(s): /home/someuser/.codalotl/config.json

Effective Model: gpt-5.2

To set LLM provider API keys, set one of these ENV variables:
- OPENAI_API_KEY

Global configuration can be stored in /home/someuser/.codalotl/config.json
Project-specific configuration can be stored in .codalotl/config.json
```

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

### codalotl cas set <namespace> <path/to/pkg> <value>

Uses `internal/gocas` to set `<value>` for (package, namespace).

Notes:
- `<namespace>` is a schema/version string and must be filesystem-safe (no path separators), since it is used as a directory name under the CAS root.
- Storage key is content-addressed from the Go package's source file paths + contents (plus namespace). Changing package contents changes the key.
- `<value>` must be JSON-encodable (ex: `"OK"`, or `'{"result": "ok"}'`).

The BaseDir is the package's module dir. The database dir is, by priority:
- CODALOTL_CAS_DB, if set
- Let `$NEAREST_GIT_DIR` = nearest dir containing a `.git` entry (dir or file); `$NEAREST_GIT_DIR/.codalotl/cas` (if `$NEAREST_GIT_DIR` is not blank)
- BaseDir

### codalotl cas get <namespace> <path/to/pkg>

Uses `internal/gocas` to get the stored value (and associated metadata) for (package, namespace), for the current package contents. Prints entire record (including additional information) if found. Otherwise prints nothing and exits 1.

## Configuration

This package is responsible for loading a configuation file and passing various configuration to other packages. The configuration is loaded with `internal/q/cascade`. The configuration is loaded and validated for all commands, except those that obviously don't need it, like `version` and `-h`. An invalid configuration prints out a helpful error message and returns with an non-zero exit code.

The config file is loaded with this preference:
- `.codalotl/config.json` (starting from the working directory, recursively checking the parent, until some reasonable stop condition).
- `~/.codalotl/config.json` or `%LOCALAPPDATA%\.codalotl\config.json`.

Config:

```go
// Note to self about how cascade currently maps to fields: it does NOT use json. It's just fieldname lowercase.
type Config struct {
	ProviderKeys          ProviderKeys       `json:"providerkeys"`
	CustomModels          []CustomModel      `json:"custommodels,omitempty"`
	ReflowWidth           int                `json:"reflowwidth"` // Max width when reflowing documentation. Default to 120
	ReflowWidthProvidence cascade.Providence `json:"-"`

	// Lints configures the lint pipeline used by `codalotl context initial`. See internal/lints/SPEC.md for full details.
	//
	// NOTE: for now, this is only used by the `context initial` command.
	Lints lints.Lints `json:"lints,omitempty"`

	DisableTelemetry      bool `json:"disabletelemetry,omitempty"`
	DisableCrashReporting bool `json:"disablecrashreporting,omitempty"`

	// Optional. If set, use this provider if possible (lower precedence than PreferredModel, though). Allowed values are llmmodel's AllProviderIDs().
	PreferredProvider string `json:"preferredprovider"`

	// Optional. If set, use this model specifically. Allowed values are llmmodel's AvailableModelIDs().
	PreferredModel string `json:"preferredmodel"`

	PreferredModelProvidence cascade.Providence `json:"-"`
}

// NOTE: separate struct so we can easily test zero value
type ProviderKeys struct {
	OpenAI string `json:"openai"`

	// NOTE: in the future, we may add these:
	// Anthropic   string `json:"anthropic"`
	// XAI         string `json:"xai"`
	// Gemini      string `json:"gemini"`
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
