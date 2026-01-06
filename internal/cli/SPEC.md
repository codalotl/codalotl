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

### codalotl -h, codalotl --help

Prints standard usage.

### codalotl

The naked `codalotl` launches the TUI (`internal/tui`).

If the TUI requests that a newly selected model be persisted (via `tui.Config.PersistModelID`), the CLI writes the model to `preferredmodel` in a JSON config file:
- If some config file explicitly set `preferredmodel` during load, update that same file.
- Otherwise, update the highest-precedence config file that contributed any values.
- If no config files contributed values, write to the global config at `~/.codalotl/config.json` (expanded cross-OS).

### codalotl version

Prints the codalotl version to stdout.

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
    "anthropic": "",
    "openai": "sk-p..._LQA",
    "xai": "",
    "gemini": ""
  },
  "maxwidth": 160,
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
	MaxWidth              int                `json:"maxwidth"` // Max width when reflowing documentation. Default to 120
	MaxWidthProvidence    cascade.Providence `json:"-"`
	DisableTelemetry      bool               `json:"disabletelemetry,omitempty"`
	DisableCrashReporting bool               `json:"disablecrashreporting,omitempty"`

	// Optional. If set, use this provider if possible (lower precedence than PreferredModel, though). Allowed values are llmmodel's AllProviderIDs().
	PreferredProvider string `json:"preferredprovider"`

	// Optional. If set, use this model specifically. Allowed values are llmmodel's AvailableModelIDs().
	PreferredModel string `json:"preferredmodel"`
	PreferredModelProvidence cascade.Providence `json:"-"`
}

// NOTE: separate struct so we can easily test zero value
type ProviderKeys struct {
	OpenAI      string `json:"openai"`

	// NOTE: in the future, we may add these:
	// Anthropic   string `json:"anthropic"`
	// XAI         string `json:"xai"`
	// Gemini      string `json:"gemini"`
}
```

Notes:
- If a provider's key is configured via the configuration file, call `llmmodel.ConfigureProviderKey` to use it.


## Public API

```go
// Version is the codalotl version. It is a var (not a const) so build tooling can override it.
var Version = "0.1.0"

// In/Out/Err override standard I/O. If nil, defaults are used. Overriding is useful for testing.
//
// Note that if Stdout/Stderr are overridden, we will pass them to other package's functions if they accept them. However, not all will; some packages will probably print to Stdout.
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
